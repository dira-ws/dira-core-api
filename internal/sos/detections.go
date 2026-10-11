package sos

// MESURER LES FAUSSES ALERTES — `POST /sos/detections`.
//
// ⚠️⚠️ CE QU'ON NE VOYAIT PAS, ET POURQUOI C'EST GRAVE.
//
// Quand une détection propose l'alerte et que la personne annule, RIEN NE
// PART. Le serveur ne voit que les comptes à rebours confirmés ou expirés. Il
// était donc impossible de savoir si un seuil déclenche trop — et, bien pire,
// impossible de savoir s'il ne déclenche JAMAIS.
//
// Les deux erreurs ne se ressemblent pas. Un seuil trop BAS remplit la file de
// l'exploitation de faux, les opérateurs apprennent à les ignorer, et ça finit
// par se voir. Un seuil trop HAUT ne laisse AUCUNE TRACE : pas d'alerte, pas de
// faux, pas de plainte — juste une détection éteinte que tout le monde croit
// allumée. C'est pour ce deuxième cas que cette route existe.
//
// ⚠️ ET ELLE NE PASSE JAMAIS DEVANT UNE ALERTE. Les applications l'appellent
// sans insister ; un échec n'est pas réessayé, il attend le prochain envoi. Le
// plafond par compte rend `429` et ne bloque JAMAIS `POST /sos` : un compte
// bavard en comptes rendus doit rester capable d'appeler au secours.

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// DetectionCollection : les comptes rendus de détection.
const DetectionCollection = "sos_detections"

// Les dénouements d'une proposition.
const (
	// OutcomeCancelled : la personne a annulé. ⚠️ C'EST LE SEUL QUE LE SERVEUR
	// NE VOYAIT PAS, et c'est tout l'objet de cette route.
	OutcomeCancelled = "cancelled"
	OutcomeConfirmed = "confirmed"
	OutcomeExpired   = "expired"
)

var detectionOutcomes = map[string]bool{
	OutcomeCancelled: true, OutcomeConfirmed: true, OutcomeExpired: true,
}

// DetectionBatchMax : 50 comptes rendus par envoi.
//
// ⚠️ UN LOT PLUS GRAND NE SERAIT PAS « PLUS EFFICACE » : il serait plus long à
// écrire, donc plus long à refuser quand le plafond est atteint, sur une route
// que les applications appellent en arrière-plan depuis un réseau de bord de
// route.
const DetectionBatchMax = 50

// DetectionDailyCap : le plafond par compte et par jour.
//
// ⚠️ PAR COMPTE, ET NON PAR APPAREIL comme le proposait le contrat — parce que
// c'est ce que nous savons identifier. Le compte rendu porte le MODÈLE de
// l'appareil (« SM-X200 »), pas son identifiant, et c'est délibéré : il sert à
// comprendre pourquoi un seuil de bruit se comporte autrement sur un téléphone
// bas de gamme, pas à suivre quelqu'un. Un compte est borné à trois appareils
// par la plateforme : le plafond revient au même, sans rien pister de plus.
const DetectionDailyCap = 200

// DetectionRetention : 90 jours, puis seulement des agrégats.
//
// ⚠️ UN INDEX TTL, et pas un balayage nocturne : une purge qu'il faut penser à
// lancer est une purge qui ne tourne pas. Mongo le fait tout seul.
const DetectionRetention = 90 * 24 * time.Hour

// Detection est UNE proposition de détection, telle qu'on la mesure.
//
// ⚠️⚠️ NI SON, NI POSITION, NI IDENTIFIANT DE COURSE. Cette route mesure des
// SEUILS, pas des gens. Tout ce qui ne sert pas à régler un nombre n'entre
// pas : un champ en trop se remplirait, puis se garderait, puis servirait à
// autre chose.
type Detection struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	UserID primitive.ObjectID `bson:"user_id"`
	// Country et App : les deux axes du réglage.
	Country string `bson:"country,omitempty"`
	App     string `bson:"app,omitempty"`
	Source  string `bson:"source"`
	Outcome string `bson:"outcome"`
	// Peak : le plus fort mesuré — en g (secousse, choc) ou en dBFS (bruit).
	Peak *float64 `bson:"peak,omitempty"`
	// LoudMS : la durée forte qui a déclenché. Bruit seulement.
	LoudMS int `bson:"loud_ms,omitempty"`
	// Revision : LE RÉGLAGE EN VIGUEUR au moment de la mesure.
	//
	// ⚠️ SANS ELLE, ON LIRAIT « 12 % DE CES PROPOSITIONS SONT ANNULÉES » SANS
	// SAVOIR SOUS QUEL SEUIL — c'est-à-dire qu'on ne pourrait rien conclure
	// après avoir réglé quoi que ce soit, et que la mesure ne servirait à rien.
	Revision int    `bson:"revision"`
	Vertical string `bson:"vertical,omitempty"`
	// DeviceModel : le MODÈLE, jamais l'identifiant. Un même cri ne donne pas
	// le même dBFS d'un téléphone à l'autre : c'est la seule raison pour
	// laquelle ce champ existe.
	DeviceModel string `bson:"device_model,omitempty"`
	// AlertID : rempli quand l'alerte est bel et bien partie.
	AlertID *primitive.ObjectID `bson:"alert_id,omitempty"`
	At      time.Time           `bson:"at"`
	// ExpiresAt : la date à laquelle Mongo efface la ligne.
	ExpiresAt time.Time `bson:"expires_at"`
}

// DetectionItem est un compte rendu tel qu'une application l'envoie.
//
// ⚠️ AUCUN `validate:` SUR LES VALEURS, ET C'EST VOULU. Un tag ferait lever
// `validation_failed`, que `httpx.Message` traduit PAR LE CODE : la réponse
// sortirait en « Requête invalide » sur une route qu'une application appelle en
// arrière-plan et dont personne ne lit le refus. Ce qui est bancal est
// NORMALISÉ (voir `normaliseDetection`), ce qui est hors sujet est IGNORÉ.
type DetectionItem struct {
	Source      string   `json:"source"`
	Outcome     string   `json:"outcome"`
	Peak        *float64 `json:"peak"`
	LoudMS      int      `json:"loud_ms"`
	Revision    int      `json:"revision"`
	Vertical    string   `json:"vertical"`
	DeviceModel string   `json:"device_model"`
	AlertID     string   `json:"alert_id"`
	At          string   `json:"at"`
}

// DetectionBatch est un envoi.
type DetectionBatch struct {
	Items []DetectionItem `json:"items"`
}

var (
	// ⚠️ LE SEUL REFUS DE FORME DE CETTE ROUTE, et il porte sa phrase : un lot
	// trop grand est une erreur de câblage côté application, pas un accident de
	// réseau, et celui qui la lit est un développeur.
	errBatchTooLarge = apperr.New("detections_batch_too_large",
		"send at most 50 detection reports per call",
		http.StatusUnprocessableEntity)
	// ⚠️ `429`, ET IL NE BLOQUE RIEN D'AUTRE. Les comptes rendus sont une
	// mesure : les perdre coûte une statistique. Les laisser épuiser quoi que
	// ce soit d'autre coûterait une alerte.
	errDetectionCap = apperr.New("detections_quota_reached",
		"enough detection reports for today: the rest is dropped, and nothing else is affected",
		http.StatusTooManyRequests)
)

// DetectionRepository écrit et agrège les comptes rendus.
type DetectionRepository struct{ col *mongo.Collection }

func NewDetectionRepository(m *db.Mongo) *DetectionRepository {
	return &DetectionRepository{col: m.Collection(DetectionCollection)}
}

// EnsureIndexes : la purge, le plafond, et la vue de la console.
func (r *DetectionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// ⚠️ LA PURGE EST UN INDEX TTL : une purge qu'il faut penser à lancer
		// est une purge qui ne tourne pas.
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("sos_detections_ttl"),
		},
		// Le plafond du jour : combien ce compte en a déjà envoyé.
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "at", Value: -1}}},
		// La vue de la console : pays × application × source × révision.
		{Keys: bson.D{
			{Key: "country", Value: 1}, {Key: "app", Value: 1},
			{Key: "source", Value: 1}, {Key: "revision", Value: -1},
		}},
	})
	return err
}

func (r *DetectionRepository) countToday(ctx context.Context, user primitive.ObjectID, now time.Time) (int, error) {
	since := now.Add(-24 * time.Hour)
	n, err := r.col.CountDocuments(ctx, bson.M{"user_id": user, "at": bson.M{"$gte": since}})
	return int(n), err
}

func (r *DetectionRepository) insert(ctx context.Context, rows []Detection) error {
	if len(rows) == 0 {
		return nil
	}
	docs := make([]any, 0, len(rows))
	for _, d := range rows {
		docs = append(docs, d)
	}
	// ⚠️ NON ORDONNÉ : une ligne qui passe mal ne doit pas emporter les
	// quarante-neuf autres. Ce sont des mesures, pas une transaction.
	_, err := r.col.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	return err
}

// DetectionStat est une ligne de la vue console.
type DetectionStat struct {
	Country  string `json:"country,omitempty"`
	App      string `json:"app,omitempty"`
	Source   string `json:"source"`
	Revision int    `json:"revision"`
	// Proposed : combien de fois la détection a proposé l'alerte.
	Proposed int `json:"proposed"`
	// Cancelled : combien de fois la personne a annulé — le chiffre que le
	// serveur ne voyait pas.
	Cancelled int `json:"cancelled"`
	Confirmed int `json:"confirmed"`
	Expired   int `json:"expired"`
	// CancelRate : le taux d'annulation, en pour cent.
	//
	// ⚠️ UN POINTEUR, ABSENT QUAND RIEN N'A ÉTÉ PROPOSÉ — ce n'est pas « 0 % ».
	// « 0 % d'annulation » se lit « la détection est parfaitement réglée » :
	// l'inverse exact de « elle n'a jamais déclenché », qui est la panne
	// silencieuse qu'on cherche.
	CancelRate *int `json:"cancel_rate,omitempty"`
	// PeakMin, PeakMax, PeakAvg : la répartition du plus fort mesuré.
	PeakMin *float64 `json:"peak_min,omitempty"`
	PeakMax *float64 `json:"peak_max,omitempty"`
	PeakAvg *float64 `json:"peak_avg,omitempty"`
	// Devices : les modèles qui déclenchent le plus, les cinq premiers.
	Devices []DeviceCount `json:"devices,omitempty"`
}

// DeviceCount : un modèle d'appareil et son nombre de propositions.
type DeviceCount struct {
	Model string `json:"model"`
	N     int    `json:"n"`
}

func (r *DetectionRepository) stats(ctx context.Context, f DetectionFilter) ([]DetectionStat, error) {
	// ⚠️ BORNÉE AU PAYS DE LA REQUÊTE (`country.Restrict`), comme toute lecture
	// de cette plateforme : la console de Dakar n'a pas à régler les seuils de
	// Lomé sur des mesures faites à Lomé. Le filtre était posé à la main, et un
	// test de frontière l'a refusé — à juste titre : une restriction écrite à
	// la main est une restriction qu'on oublie la fois suivante.
	match := country.Restrict(ctx, bson.M{})
	if f.App != "" {
		match["app"] = f.App
	}
	if f.Source != "" {
		match["source"] = f.Source
	}
	if !f.Since.IsZero() {
		match["at"] = bson.M{"$gte": f.Since}
	}
	cur, err := r.col.Aggregate(ctx, []bson.M{
		{"$match": match},
		{"$group": bson.M{
			"_id": bson.M{
				"country": "$country", "app": "$app",
				"source": "$source", "revision": "$revision",
			},
			"proposed": bson.M{"$sum": 1},
			"cancelled": bson.M{"$sum": bson.M{
				"$cond": []any{bson.M{"$eq": []any{"$outcome", OutcomeCancelled}}, 1, 0}}},
			"confirmed": bson.M{"$sum": bson.M{
				"$cond": []any{bson.M{"$eq": []any{"$outcome", OutcomeConfirmed}}, 1, 0}}},
			"expired": bson.M{"$sum": bson.M{
				"$cond": []any{bson.M{"$eq": []any{"$outcome", OutcomeExpired}}, 1, 0}}},
			"peak_min": bson.M{"$min": "$peak"},
			"peak_max": bson.M{"$max": "$peak"},
			"peak_avg": bson.M{"$avg": "$peak"},
			"devices":  bson.M{"$push": "$device_model"},
		}},
		{"$sort": bson.M{"proposed": -1}},
		{"$limit": 200},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var out []DetectionStat
	for cur.Next(ctx) {
		var row struct {
			ID struct {
				Country  string `bson:"country"`
				App      string `bson:"app"`
				Source   string `bson:"source"`
				Revision int    `bson:"revision"`
			} `bson:"_id"`
			Proposed  int      `bson:"proposed"`
			Cancelled int      `bson:"cancelled"`
			Confirmed int      `bson:"confirmed"`
			Expired   int      `bson:"expired"`
			PeakMin   *float64 `bson:"peak_min"`
			PeakMax   *float64 `bson:"peak_max"`
			PeakAvg   *float64 `bson:"peak_avg"`
			Devices   []string `bson:"devices"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		s := DetectionStat{
			Country: row.ID.Country, App: row.ID.App,
			Source: row.ID.Source, Revision: row.ID.Revision,
			Proposed: row.Proposed, Cancelled: row.Cancelled,
			Confirmed: row.Confirmed, Expired: row.Expired,
			PeakMin: row.PeakMin, PeakMax: row.PeakMax, PeakAvg: row.PeakAvg,
			Devices: topModels(row.Devices),
		}
		// ⚠️ PAS DE TAUX SANS PROPOSITION. Voir `CancelRate`.
		if row.Proposed > 0 {
			rate := row.Cancelled * 100 / row.Proposed
			s.CancelRate = &rate
		}
		out = append(out, s)
	}
	return out, cur.Err()
}

func topModels(all []string) []DeviceCount {
	n := map[string]int{}
	for _, m := range all {
		if m != "" {
			n[m]++
		}
	}
	out := make([]DeviceCount, 0, len(n))
	for m, c := range n {
		out = append(out, DeviceCount{Model: m, N: c})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].N > out[i].N || (out[j].N == out[i].N && out[j].Model < out[i].Model) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// DetectionFilter borne la vue de la console. ⚠️ Sans pays : il vient du
// CONTEXTE, par `country.Restrict`.
type DetectionFilter struct {
	App    string
	Source string
	Since  time.Time
}

/* ------------------------------------------------------------- le service -- */

// RecordDetections enregistre un envoi de comptes rendus.
//
// ⚠️ ELLE NE REFUSE QUE DEUX CHOSES : un lot trop grand, et le plafond du jour.
// Tout le reste est normalisé ou ignoré. Une application qui envoie un champ
// qu'on ne connaît pas, un `outcome` inventé ou une date illisible doit voir sa
// mesure prise — ou perdue en silence —, jamais un refus qu'elle devra traiter.
func (s *Service) RecordDetections(ctx context.Context, userID string, batch DetectionBatch) error {
	if s.detections == nil {
		// Sans dépôt branché, on ne mesure pas, et ce n'est pas une panne :
		// l'application n'a rien à faire de cette réponse.
		return nil
	}
	if len(batch.Items) > DetectionBatchMax {
		return errBatchTooLarge
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	used, err := s.detections.countToday(ctx, uid, now)
	if err != nil {
		return apperr.Internal(err)
	}
	if used >= DetectionDailyCap {
		return errDetectionCap
	}
	room := DetectionDailyCap - used

	code := country.FromContext(ctx)
	app := ""
	if s.accounts != nil {
		app = s.accounts.AppOf(ctx, userID)
	}

	rows := make([]Detection, 0, len(batch.Items))
	for _, it := range batch.Items {
		if len(rows) == room {
			// ⚠️ ON ÉCRIT CE QUI TIENT ET ON JETTE LE RESTE, sans refuser
			// l'envoi : la personne n'y peut rien, et un `429` sur un lot à
			// moitié écrit ferait réessayer un lot déjà compté.
			break
		}
		d, ok := normaliseDetection(it, uid, code, app, now)
		if ok {
			rows = append(rows, d)
		}
	}
	if err := s.detections.insert(ctx, rows); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// normaliseDetection met un compte rendu en forme. `false` = inutilisable.
//
// ⚠️ UNE LIGNE INUTILISABLE EST JETÉE, PAS REFUSÉE. C'est une mesure : la
// perdre coûte une statistique, refuser l'envoi coûterait les quarante-neuf
// autres.
func normaliseDetection(it DetectionItem, uid primitive.ObjectID, code, app string, now time.Time) (Detection, bool) {
	src := SanitiseSource(it.Source)
	// ⚠️ LE BOUTON N'EST PAS UNE DÉTECTION : il n'a rien proposé, il a été
	// pressé. Compter ses « annulations » fausserait tous les taux.
	if src == SourceButton {
		return Detection{}, false
	}
	outcome := strings.ToLower(strings.TrimSpace(it.Outcome))
	if !detectionOutcomes[outcome] {
		return Detection{}, false
	}
	at := now
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(it.At)); err == nil {
		// ⚠️ UNE DATE DANS L'AVENIR EST RAMENÉE À MAINTENANT. Un téléphone mal
		// réglé ne doit pas se ranger en tête d'un classement par date, ni
		// survivre à la purge pour toujours.
		if t.After(now) {
			t = now
		}
		at = t.UTC()
	}
	d := Detection{
		UserID: uid, Country: code, App: app,
		Source: src, Outcome: outcome,
		Revision: it.Revision, Peak: it.Peak,
		At: at, ExpiresAt: at.Add(DetectionRetention),
	}
	if it.LoudMS > 0 && it.LoudMS < 600000 {
		d.LoudMS = it.LoudMS
	}
	v := strings.ToLower(strings.TrimSpace(it.Vertical))
	if v == "vtc" || v == "food" {
		d.Vertical = v
	}
	// ⚠️ LE MODÈLE EST BORNÉ ET MIS EN FORME : ce qui vient d'un réseau n'est
	// jamais écrit tel quel.
	if m := truncate(strings.TrimSpace(it.DeviceModel), 40); m != "" {
		d.DeviceModel = m
	}
	if id, err := primitive.ObjectIDFromHex(strings.TrimSpace(it.AlertID)); err == nil {
		d.AlertID = &id
	}
	return d, true
}

// DetectionStats rend la vue de la console.
func (s *Service) DetectionStats(ctx context.Context, f DetectionFilter) ([]DetectionStat, error) {
	if s.detections == nil {
		return []DetectionStat{}, nil
	}
	// ⚠️ LE PAYS N'EST PAS POSÉ ICI : `stats` le prend du CONTEXTE, par
	// `country.Restrict`. Le poser à deux endroits aurait fait deux règles.
	out, err := s.detections.stats(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if out == nil {
		// ⚠️ UNE TRANCHE VIDE, JAMAIS `nil` : `items: null` fait planter une
		// console qui boucle dessus sans vérifier.
		out = []DetectionStat{}
	}
	return out, nil
}
