package country

// CE QUE LE PAYS DÉCIDE DU BOUTON D'ALERTE.
//
// Le bouton SOS lui-même n'est pas réglable : il existe dans toutes les
// applications, partout, et c'est la seule chose de ce fichier qui ne se
// discute pas. Ce qui se règle, c'est ce qui l'entoure — les DÉTECTIONS qui le
// proposent sans qu'on le touche, le temps laissé pour annuler, et le numéro de
// secours du pays.
//
// ⚠️ POURQUOI PAR PAYS. Les numéros d'urgence ne sont pas les mêmes d'un pays à
// l'autre, et c'est la raison qui suffirait à elle seule. Mais les détections
// aussi : là où les routes défoncées font claquer un téléphone dix fois par
// trajet, la détection de choc remplit la file de faux et l'exploitation cesse
// de la regarder. C'est une décision d'exploitation, pays par pays, pas une
// constante de code.
//
// ⚠️⚠️ LES APPLICATIONS NE REÇOIVENT AUCUN NUMÉRO À COMPOSER, ET C'EST LA
// DÉCISION CENTRALE DE CE FICHIER.
//
// Ce n'est pas une précaution de données, c'est le PROTOCOLE : l'alerte part au
// SERVICE CLIENT, un opérateur appelle d'abord la personne, et c'est LUI qui
// appelle les secours s'il le faut. Le téléphone de quelqu'un en danger ne
// compose rien.
//
// Pourquoi c'est mieux, et pas seulement différent :
//   - QUELQU'UN RÉPOND TOUJOURS. Les secours d'un pays peuvent sonner dans le
//     vide — un relevé mené en Guinée en 2024 a trouvé plusieurs numéros
//     officiels hors service. Un opérateur qui tombe sur un numéro mort
//     l'entend, raccroche et prend le suivant ; une personne en panique, non.
//   - L'OPÉRATEUR SAIT CE QU'IL DIT. « Un chauffeur à tel carrefour, voiture
//     grise, immatriculée, course en cours » se transmet ; un passager terrorisé
//     ne décrit pas sa position.
//   - ET LE PREMIER APPEL EST SOUVENT LE BON : la moitié des cas se règlent en
//     joignant la personne — un téléphone tombé, un dos-d'âne, une dispute qui
//     s'est calmée. Appeler la police pour ça la ferait cesser de nous écouter.
//
// Les numéros, eux, viennent du CATALOGUE (`pkg/country.Emergency`), au même
// titre que la monnaie : ce sont des faits du pays. L'exploitation les CONFIRME
// ou les corrige ici, pays par pays, et c'est son réglage qui fait foi — parce
// que les sources publiques se contredisent et qu'un numéro change sans
// prévenir. Ils ne sortent que vers la CONSOLE.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/sosdetect"
)

// Défauts des détections, quand le pays n'a rien réglé.
const (
	// defaultSOSShake : la SECOUSSE est allumée d'office.
	//
	// Elle ne coûte rien (l'accéléromètre tourne déjà), elle ne demande aucune
	// permission, et c'est le geste le plus utile du lot : on secoue un
	// téléphone qu'on ne peut pas regarder.
	defaultSOSShake = true
	// defaultSOSCrash : la DÉTECTION DE CHOC est allumée d'office.
	//
	// Un accident est le cas où personne n'appuiera sur rien. Le compte à
	// rebours est là pour les faux positifs.
	defaultSOSCrash = true
	// defaultSOSVoice : le MOT-CLÉ VOCAL est ÉTEINT d'office.
	//
	// ⚠️ ET C'EST LE SEUL DES TROIS QUI LE SOIT. Il demande d'écouter le micro
	// en permanence : une permission que l'utilisateur doit comprendre, une
	// batterie qui descend, et une surveillance qu'on n'allume pas à l'insu de
	// quelqu'un. Un pays qui le veut l'allume — explicitement, et en l'ayant
	// expliqué dans son application.
	defaultSOSVoice = false
	// defaultSOSNoise : le GRAND BRUIT est ÉTEINT d'office, comme le mot-clé.
	//
	// ⚠️ CE N'EST PAS LE MOT-CLÉ, ET LES CONFONDRE SERAIT UNE ERREUR DE
	// CONSENTEMENT. `voice` reconnaît des MOTS — il faut comprendre ce qu'on
	// allume. `noise` ne mesure qu'un NIVEAU SONORE : rien n'est reconnu, rien
	// n'est transcrit, rien ne sort du téléphone. C'est beaucoup moins
	// intrusif, et pourtant il reste éteint d'office pour la même raison que
	// l'autre : il écoute le micro, et on n'allume pas un micro à l'insu de
	// quelqu'un. Un pays qui le veut l'allume, explicitement.
	defaultSOSNoise = false
	// defaultSOSCountdown : dix secondes pour annuler.
	//
	// ⚠️ LE RÉGLAGE LE PLUS DÉLICAT DU FICHIER, parce qu'il se trompe dans les
	// DEUX sens. Trop court (3 s) : chaque dos-d'âne part en alerte, l'opérateur
	// apprend à les ignorer, et la vraie se noie. Trop long (60 s) : quelqu'un
	// à qui on arrache son téléphone a tout le temps de voir l'alerte annulée
	// par son agresseur. Dix secondes laissent le temps de lire, de comprendre
	// et d'appuyer — et pas celui de fouiller une poche.
	defaultSOSCountdown = 10
)

// Genres de numéros de secours.
const (
	EmergencyPolice    = "police"
	EmergencyAmbulance = "ambulance"
	EmergencyFire      = "fire"
	// EmergencyPlatform : le numéro de l'EXPLOITATION elle-même.
	//
	// ⚠️ IL COMPTE AUTANT QUE LES AUTRES. Quelqu'un dont la course tourne mal
	// n'a pas toujours affaire à la police : un passager agressif, une dispute
	// sur un prix, une route bloquée. Appeler Dira est souvent le bon geste, et
	// sans ce numéro l'application n'a que la police à proposer — ce qui fait
	// soit un appel de trop, soit aucun appel.
	EmergencyPlatform = "platform"
)

// EmergencyKinds est la liste admise.
var EmergencyKinds = []string{EmergencyPolice, EmergencyAmbulance, EmergencyFire, EmergencyPlatform}

func validEmergencyKind(k string) bool {
	for _, v := range EmergencyKinds {
		if v == k {
			return true
		}
	}
	return false
}

// EmergencyNumber est UN numéro à composer, tel que l'application le montre.
type EmergencyNumber struct {
	Kind string `bson:"kind" json:"kind"`
	// Label : ce qui s'écrit sur le bouton, dans la langue de l'exploitation —
	// « Police secours », « SAMU », « Astreinte Dira ». Vide, l'application
	// nomme le genre elle-même.
	Label string `bson:"label,omitempty" json:"label,omitempty"`
	// Number : tel qu'on le COMPOSE dans ce pays. Les numéros courts (17, 118)
	// ne s'écrivent pas en E.164 et ne doivent pas être « corrigés » en +228 17.
	Number string `bson:"number" json:"number"`
}

// SOS est la politique d'alerte d'un pays.
type SOS struct {
	// Shake, Crash, Voice : des POINTEURS, parce que « non réglé » et « éteint »
	// ne sont pas la même chose. Un `bool` nu aurait éteint la secousse dans
	// tout pays enregistré avant ce réglage — c'est-à-dire tous.
	Shake *bool `bson:"shake,omitempty"`
	Crash *bool `bson:"crash,omitempty"`
	Voice *bool `bson:"voice,omitempty"`
	Noise *bool `bson:"noise,omitempty"`
	// CountdownSeconds : le délai d'annulation d'une détection. Zéro = le défaut.
	CountdownSeconds int               `bson:"countdown_seconds,omitempty"`
	Numbers          []EmergencyNumber `bson:"numbers,omitempty"`
	// Thresholds : À PARTIR DE QUAND une détection se déclenche, pour tout le
	// pays. AppThresholds raffine par application.
	//
	// ⚠️⚠️ DEUX ÉTAGES, ET LE SECOND EXISTE POUR UNE RAISON MESURABLE : une
	// moto dans le vent, une voiture avec la radio et un passager à pied
	// n'entendent pas le même bruit et ne subissent pas les mêmes secousses.
	// Le pays règle ce qui tient à la ROUTE (elle secoue tout le monde pareil),
	// l'application ce qui tient au VÉHICULE.
	Thresholds    sosdetect.Patch            `bson:"thresholds,omitempty"`
	AppThresholds map[string]sosdetect.Patch `bson:"app_thresholds,omitempty"`
	// ThresholdsRevision augmente à chaque enregistrement QUI CHANGE une
	// valeur.
	//
	// ⚠️ ELLE VOYAGE JUSQUE DANS LA MESURE DES FAUSSES ALERTES. Sans elle, on
	// lirait « 12 % de ces propositions sont annulées » sans savoir sous quel
	// seuil — c'est-à-dire qu'on ne pourrait rien conclure après avoir réglé
	// quoi que ce soit. ⚠️ Et elle n'augmente QUE si une valeur change :
	// réenregistrer deux fois le même écran ne doit pas couper une série de
	// mesures en deux.
	ThresholdsRevision int `bson:"thresholds_revision,omitempty"`
	// VerifiedAt, VerifiedBy : QUELQU'UN A COMPOSÉ CES NUMÉROS, et ça a répondu.
	//
	// ⚠️ C'EST UN FAIT DISTINCT DE « DES NUMÉROS SONT ENREGISTRÉS », et les
	// confondre était un défaut de la première version : `confirmed` valait
	// « la liste n'est pas vide », si bien qu'un simple enregistrement — ou un
	// jeu de données rejoué — se présentait à l'opérateur comme une
	// vérification humaine. Or ce drapeau ne sert qu'à une chose : lui dire
	// s'il est le PREMIER à essayer. Mentir dessus le rendait inutile.
	//
	// ⚠️ ON NE PEUT PAS LE DÉDUIRE, ni le programmer. Vérifier un numéro de
	// secours, c'est le COMPOSER et entendre quelqu'un répondre. Aucun code ne
	// fait ça : la console a donc un geste explicite, et c'est une personne qui
	// l'engage — avec son nom, parce qu'on vient le lui demander le jour où le
	// numéro ne répond plus.
	VerifiedAt *time.Time          `bson:"numbers_verified_at,omitempty"`
	VerifiedBy *primitive.ObjectID `bson:"numbers_verified_by,omitempty"`
}

// SOSResponse est la politique telle que les APPLICATIONS la reçoivent — tous
// les champs posés, aucun à deviner.
//
// ⚠️ ELLE NE PORTE AUCUN NUMÉRO, et c'est structurel plutôt que documentaire.
// Écrire « n'affichez pas de bouton d'appel » dans une spec, en servant le
// champ quand même, aurait fini par un bouton : un champ qui existe se câble.
// Les numéros vivent dans `SOSAdminResponse`, que seule la console lit.
type SOSResponse struct {
	// Button est TOUJOURS `true`. Il est rendu quand même, et c'est délibéré :
	// une application qui lit ce bloc doit pouvoir écrire son écran sans cas
	// particulier, et le jour où quelqu'un demanderait à couper le bouton dans
	// un pays, le champ existe pour qu'on puisse répondre non par écrit.
	Button bool `json:"button"`
	Shake  bool `json:"shake"`
	Crash  bool `json:"crash"`
	Voice  bool `json:"voice"`
	// Noise : le GRAND BRUIT — un niveau sonore, sans reconnaissance de mots.
	// ⚠️ DISTINCT DE `voice`, qui reconnaît des MOTS. Les fondre aurait fait
	// allumer une transcription à qui ne voulait qu'un sonomètre.
	Noise bool `json:"noise"`
	// CountdownSeconds : le délai AVANT envoi d'une détection. ⚠️ Il ne
	// s'applique PAS au bouton : appuyer, c'est avoir déjà décidé.
	CountdownSeconds int `json:"countdown_seconds"`
	// CallsBack dit à l'application CE QU'ELLE DOIT PROMETTRE : « le service
	// client a été prévenu et va vous appeler ».
	//
	// ⚠️ SERVI PLUTÔT QUE SUPPOSÉ, parce que c'est la seule chose que la
	// personne cherche à savoir après avoir appuyé, et parce qu'une application
	// qui écrirait « appelez la police » à la place enverrait quelqu'un composer
	// un numéro qu'on ne lui a pas donné. Vaut toujours `true` aujourd'hui ; le
	// champ existe pour que l'écran n'ait rien à deviner, et pour qu'on puisse
	// répondre par écrit le jour où on demanderait l'inverse.
	CallsBack bool `json:"calls_back"`
}

// SOSAdminResponse est la politique telle que LA CONSOLE la lit — avec les
// numéros, parce que c'est elle qui appelle.
type SOSAdminResponse struct {
	SOSResponse
	// Numbers : ce que l'opérateur compose. Le catalogue du pays, remplacé par
	// le réglage de l'exploitation quand il y en a un.
	Numbers []EmergencyNumber `json:"numbers"`
	// VerifiedAt : quand quelqu'un les a composés. ⚠️ SERVI avec `Confirmed`,
	// parce que « vérifié il y a trois ans » et « vérifié hier » ne valent pas
	// la même chose — un numéro d'urgence change sans prévenir, et une
	// confirmation vieillit.
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	// Confirmed dit si quelqu'un a COMPOSÉ ces numéros et entendu une réponse.
	//
	// ⚠️ SERVI, ET LA CONSOLE DOIT LE MONTRER. Les sources publiques se
	// contredisent, et un numéro officiel peut être hors service : un opérateur
	// qui compose doit savoir si quelqu'un chez nous a déjà vérifié, ou s'il
	// est le premier à essayer. « Non confirmé » n'empêche pas d'appeler — ça
	// dit de vérifier qu'on est bien tombé au bon endroit.
	//
	// ⚠️ ET IL NE SE DÉDUIT PAS DE LA PRÉSENCE DE NUMÉROS. Enregistrer une
	// liste n'est pas l'avoir appelée ; un jeu de données rejoué non plus.
	Confirmed bool `json:"confirmed"`

	// LES SEUILS, tels que la console les ÉDITE et les MONTRE.
	//
	// ⚠️⚠️ TROIS CHAMPS ET NON UN, parce que l'écran a trois questions à
	// répondre d'un coup : « qu'est-ce que l'application reçoit ? »
	// (`effective`), « qu'est-ce qui a été surchargé ici ? » (`thresholds` et
	// `app_thresholds`, vides quand rien ne l'est), et « qu'ai-je le droit de
	// taper ? » (`bounds`). Servir seulement les valeurs effectives aurait
	// rendu impossible de distinguer « réglé à 2,7 » d'« hérité à 2,7 » — donc
	// impossible de proposer « revenir au défaut », et impossible de savoir si
	// quelqu'un a décidé ce nombre ou s'il traîne depuis l'origine.
	Thresholds    sosdetect.Patch            `json:"thresholds"`
	AppThresholds map[string]sosdetect.Patch `json:"app_thresholds"`
	// Effective : ce que CHAQUE application reçoit, cascade faite et bornes
	// appliquées. Les trois colonnes de l'écran.
	Effective map[string]sosdetect.Thresholds `json:"effective"`
	// Bounds : les bornes des champs de saisie. ⚠️ SERVIES PLUTÔT QUE
	// RECOPIÉES DANS LA CONSOLE : la table vit dans `pkg/sosdetect`, et une
	// console qui porterait la sienne refuserait un jour ce que le serveur
	// accepte, ou l'inverse.
	Bounds map[string]sosdetect.Bound `json:"bounds"`
	// Fixed nomme les seuils que le serveur a dû REMPLACER parce qu'ils
	// étaient hors bornes en base. ⚠️ Vide presque toujours — et quand il ne
	// l'est pas, la console doit le dire : c'est une valeur que quelqu'un croit
	// avoir réglée et qui ne s'applique pas.
	Fixed              []string `json:"fixed,omitempty"`
	ThresholdsRevision int      `json:"thresholds_revision"`
}

// SOSUpdateRequest règle la politique depuis la console. Tout est facultatif.
type SOSUpdateRequest struct {
	Shake *bool `json:"shake"`
	Crash *bool `json:"crash"`
	Voice *bool `json:"voice"`
	Noise *bool `json:"noise"`
	// Thresholds, AppThresholds : les seuils, par pays et par application.
	//
	// ⚠️⚠️ PRÉSENT = REMPLACE TOUT CE NIVEAU, exactement comme `Numbers` juste
	// en dessous, et pour la même raison : en JSON, « champ absent » et
	// « champ à null » ne se distinguent pas une fois décodés dans un
	// pointeur. Avec une fusion champ par champ, « revenir au défaut pour
	// `crash.min_g` » aurait été INEXPRIMABLE — on aurait pu tout régler, et
	// jamais rien dérégler.
	//
	// La console envoie donc l'écran entier : ce qu'elle omet redevient le
	// défaut, un objet vide remet toute la colonne par défaut. « Revenir aux
	// valeurs par défaut », par champ ou par colonne, est le MÊME geste.
	Thresholds    *sosdetect.Patch           `json:"thresholds"`
	AppThresholds map[string]sosdetect.Patch `json:"app_thresholds"`
	// CountdownSeconds : de 3 à 60 secondes.
	//
	// ⚠️ LES DEUX BORNES SONT DES GARDE-FOUS, pas des préférences. En dessous de
	// 3 s, personne n'a le temps de lire l'écran : l'application enverrait des
	// alertes que son propriétaire n'a pas voulues. Au-delà de 60 s, un
	// agresseur a le temps de trouver le téléphone et d'annuler — le compte à
	// rebours devient le délai qui protège l'agresseur.
	CountdownSeconds *int `json:"countdown_seconds" validate:"omitempty,min=3,max=60"`
	// Numbers remplace la liste ENTIÈRE quand il est présent. Une liste vide
	// explicite efface les numéros — ce qui cache les boutons d'appel.
	Numbers *[]EmergencyNumber `json:"numbers" validate:"omitempty,max=6,dive"`
}

var (
	errNothingToVerify = apperr.Validation(
		"nothing to verify: this country has no emergency number at all").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
	errNoSOSUpdate = apperr.Validation(
		"nothing to update: send shake, crash, voice, countdown_seconds or numbers")
	errBadEmergencyKind = apperr.Validation(
		"unknown emergency number kind: use police, ambulance, fire or platform").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
	// ⚠️ UN NUMÉRO VIDE EST REFUSÉ, alors que tout le reste de ce module est
	// tolérant. Un bouton d'appel sans numéro est le seul cas vraiment
	// indéfendable : il a l'air de marcher, on appuie, et rien ne se passe.
	// Mieux vaut l'absence de bouton, et c'est ce que donne une liste vide.
	errEmptyEmergencyNumber = apperr.Validation(
		"an emergency number cannot be blank: remove the entry instead").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
)

// sosResponse complète les trous avec les défauts.
func sosResponse(s SOS) SOSResponse {
	out := SOSResponse{
		Button:           true,
		Shake:            defaultSOSShake,
		Crash:            defaultSOSCrash,
		Voice:            defaultSOSVoice,
		Noise:            defaultSOSNoise,
		CountdownSeconds: s.CountdownSeconds,
		CallsBack:        true,
	}
	if s.Shake != nil {
		out.Shake = *s.Shake
	}
	if s.Crash != nil {
		out.Crash = *s.Crash
	}
	if s.Voice != nil {
		out.Voice = *s.Voice
	}
	if s.Noise != nil {
		out.Noise = *s.Noise
	}
	if out.CountdownSeconds == 0 {
		out.CountdownSeconds = defaultSOSCountdown
	}
	return out
}

// sosAdminResponse ajoute les numéros, pour la console.
//
// ⚠️ LE CATALOGUE D'ABORD, LE RÉGLAGE ENSUITE — exactement comme la monnaie
// d'un pays. Le catalogue donne le fait connu, l'exploitation le corrige quand
// elle a vérifié, et c'est son réglage qui gagne.
//
// ⚠️ UN GENRE RÉGLÉ REMPLACE CELUI DU CATALOGUE, IL NE S'AJOUTE PAS. Deux
// « police » sur l'écran d'un opérateur, c'est une hésitation d'une seconde au
// moment où il n'en a pas.
func sosAdminResponse(code string, s SOS) SOSAdminResponse {
	out := SOSAdminResponse{
		SOSResponse: sosResponse(s),
		// ⚠️ UNE TRANCHE VIDE, JAMAIS `nil` : `numbers: null` en JSON fait
		// planter une console qui boucle dessus sans vérifier.
		Numbers: []EmergencyNumber{},
		// ⚠️ « CONFIRMÉ » VEUT DIRE « QUELQU'UN LES A COMPOSÉS », et rien
		// d'autre. Il valait « la liste n'est pas vide » dans la première
		// version : un enregistrement suffisait à faire croire à une
		// vérification humaine, et l'opérateur perdait l'avertissement qui
		// l'aurait fait vérifier qu'il tombe au bon endroit.
		Confirmed:  s.VerifiedAt != nil,
		VerifiedAt: s.VerifiedAt,
	}
	seen := make(map[string]bool, len(s.Numbers))
	for _, n := range s.Numbers {
		seen[n.Kind] = true
		out.Numbers = append(out.Numbers, n)
	}
	if info, ok := country.Lookup(code); ok {
		for _, n := range catalogueNumbers(info.Emergency) {
			if !seen[n.Kind] {
				out.Numbers = append(out.Numbers, n)
			}
		}
	}
	// LES SEUILS — les surcharges telles quelles, et ce que chaque application
	// reçoit vraiment.
	out.Thresholds = s.Thresholds
	// ⚠️ UNE CARTE VIDE, JAMAIS `nil` : `app_thresholds: null` en JSON fait
	// planter une console qui boucle dessus sans vérifier — le même piège que
	// `numbers: null`, déjà payé une fois dans ce fichier.
	out.AppThresholds = map[string]sosdetect.Patch{}
	for app, p := range s.AppThresholds {
		out.AppThresholds[app] = p
	}
	out.Effective = map[string]sosdetect.Thresholds{}
	seenFixed := map[string]bool{}
	for _, app := range sosdetect.Apps {
		eff, fixed := thresholdsOf(s, app)
		out.Effective[app] = eff
		for _, k := range fixed {
			if !seenFixed[k] {
				seenFixed[k], out.Fixed = true, append(out.Fixed, k)
			}
		}
	}
	out.Bounds = sosdetect.Bounds
	out.ThresholdsRevision = s.ThresholdsRevision
	return out
}

// catalogueNumbers traduit les numéros du catalogue en lignes affichables.
//
// Les libellés sont écrits ici, dans la langue de l'exploitation : le catalogue
// porte des FAITS (un numéro), pas la façon de les présenter.
func catalogueNumbers(e country.Emergency) []EmergencyNumber {
	out := make([]EmergencyNumber, 0, 3)
	for _, c := range []struct {
		kind, label, number string
	}{
		{EmergencyPolice, "Police secours", e.Police},
		{EmergencyFire, "Sapeurs-pompiers", e.Fire},
		{EmergencyAmbulance, "Ambulance / SAMU", e.Ambulance},
	} {
		// ⚠️ UN NUMÉRO VIDE NE DONNE PAS DE LIGNE. Les sources ne concordent
		// pas pour l'ambulance de plusieurs pays ; une ligne « Ambulance : »
		// sans numéro serait un bouton qui ne mène à rien, ce qui est pire que
		// son absence.
		if c.number == "" {
			continue
		}
		out = append(out, EmergencyNumber{Kind: c.kind, Label: c.label, Number: c.number})
	}
	return out
}

// normaliseNumbers met la liste en forme et refuse ce qui ne se compose pas.
func normaliseNumbers(in []EmergencyNumber) ([]EmergencyNumber, error) {
	out := make([]EmergencyNumber, 0, len(in))
	seen := map[string]bool{}
	for _, n := range in {
		n.Kind = strings.ToLower(strings.TrimSpace(n.Kind))
		if !validEmergencyKind(n.Kind) {
			return nil, errBadEmergencyKind
		}
		// ⚠️ ON NE GARDE QUE CE QUI SE COMPOSE : chiffres, `+`, `*`, `#`. Les
		// espaces et tirets de présentation sont retirés — un composeur de
		// téléphone les accepte, mais pas tous, et « 17 » doit rester « 17 ».
		n.Number = keepDialable(n.Number)
		if n.Number == "" {
			return nil, errEmptyEmergencyNumber
		}
		n.Label = strings.TrimSpace(n.Label)
		if len(n.Label) > 60 {
			n.Label = n.Label[:60]
		}
		// ⚠️ UN SEUL NUMÉRO PAR GENRE. Deux « police » feraient deux boutons
		// identiques sur l'écran d'urgence, et personne ne saurait lequel
		// appuyer — au moment précis où il ne faut pas réfléchir.
		if seen[n.Kind] {
			continue
		}
		seen[n.Kind] = true
		out = append(out, n)
	}
	return out, nil
}

func keepDialable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '+' || r == '*' || r == '#' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SOSPolicy rend la politique d'un pays, telle que la console la lit — avec les
// numéros.
func (s *Service) SOSPolicy(ctx context.Context, code string) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, inst.Security.SOS)
	return &out, nil
}

// EmergencyOf rend les numéros EFFECTIFS d'un pays — l'adaptateur que le module
// d'alerte appelle pour les poser sur l'écran de l'opérateur.
//
// ⚠️ AU MIEUX : un pays inconnu ou une base muette rendent le catalogue, jamais
// une erreur. Faire échouer l'affichage d'une alerte SOS parce qu'un réglage est
// illisible serait exactement la mauvaise façon d'échouer.
func (s *Service) EmergencyOf(ctx context.Context, code string) ([]EmergencyNumber, bool) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, false
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		out := sosAdminResponse(info.Code, SOS{})
		return out.Numbers, false
	}
	out := sosAdminResponse(info.Code, inst.Security.SOS)
	return out.Numbers, out.Confirmed
}

// UpdateSOS règle la politique d'alerte d'un pays.
func (s *Service) UpdateSOS(ctx context.Context, code string, req SOSUpdateRequest) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Shake == nil && req.Crash == nil && req.Voice == nil && req.Noise == nil &&
		req.CountdownSeconds == nil && req.Numbers == nil &&
		req.Thresholds == nil && req.AppThresholds == nil {
		return nil, errNoSOSUpdate
	}
	// ⚠️ UNE APPLICATION INCONNUE EST REFUSÉE, alors que presque tout le reste
	// de ce module est tolérant. Un seuil rangé sous `"drivers"` au pluriel ne
	// s'appliquerait à personne, et l'écran afficherait quand même la valeur
	// saisie : on croirait avoir réglé le choc des chauffeurs pendant des mois.
	for app := range req.AppThresholds {
		if !sosdetect.ValidApp(app) {
			return nil, errSOSUnknownApp.WithMeta(map[string]any{
				"fields": []string{"app_thresholds"}, "reason": app,
			})
		}
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Security.SOS
	if req.Shake != nil {
		cur.Shake = req.Shake
	}
	if req.Crash != nil {
		cur.Crash = req.Crash
	}
	if req.Voice != nil {
		cur.Voice = req.Voice
	}
	if req.Noise != nil {
		cur.Noise = req.Noise
	}
	// LES SEUILS. ⚠️ PRÉSENT = REMPLACE CE NIVEAU : c'est ce qui rend « revenir
	// au défaut » exprimable (voir `SOSUpdateRequest`).
	before := thresholdSnapshot(cur)
	if req.Thresholds != nil {
		cur.Thresholds = *req.Thresholds
	}
	if req.AppThresholds != nil {
		cur.AppThresholds = map[string]sosdetect.Patch{}
		for app, p := range req.AppThresholds {
			// ⚠️ UNE SURCHARGE VIDE N'EST PAS STOCKÉE. La garder laisserait
			// dans la base des colonnes « réglées à rien », que la console
			// montrerait comme des surcharges — et on chercherait quelle
			// valeur elles portent.
			if !p.Empty() {
				cur.AppThresholds[app] = p
			}
		}
	}
	// ⚠️⚠️ ON VALIDE SUR LE RÉSULTAT, PAS SUR LA SAISIE, et pour CHAQUE
	// application. Une surcharge de pays parfaitement valide peut rendre
	// impossible la colonne d'une application précise : c'est la combinaison
	// qui éteint une détection, jamais un champ tout seul.
	for _, app := range sosdetect.Apps {
		if err := checkThresholds(app, cur.Thresholds, cur.AppThresholds[app]); err != nil {
			return nil, err
		}
	}
	// ⚠️ LA RÉVISION N'AUGMENTE QUE SI UNE VALEUR A CHANGÉ. Réenregistrer deux
	// fois le même écran couperait sinon la série de mesures en deux, et on ne
	// saurait plus comparer un taux d'annulation à celui d'avant le réglage.
	if thresholdSnapshot(cur) != before {
		cur.ThresholdsRevision++
	}
	if req.CountdownSeconds != nil {
		cur.CountdownSeconds = *req.CountdownSeconds
	}
	if req.Numbers != nil {
		nums, err := normaliseNumbers(*req.Numbers)
		if err != nil {
			return nil, err
		}
		// ⚠️ CHANGER UN NUMÉRO RETIRE LA CONFIRMATION. Elle portait sur la
		// liste qu'on avait appelée ; la garder après une modification
		// présenterait un numéro jamais composé comme vérifié — exactement le
		// mensonge que ce drapeau existe pour éviter.
		if !sameNumbers(cur.Numbers, nums) {
			cur.VerifiedAt, cur.VerifiedBy = nil, nil
		}
		cur.Numbers = nums
	}
	sec := inst.Security
	sec.SOS = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, cur)
	return &out, nil
}

// VerifyNumbers enregistre que quelqu'un a COMPOSÉ les numéros de ce pays.
//
// ⚠️ UN GESTE À PART DE L'ENREGISTREMENT, et c'est tout l'intérêt. Saisir une
// liste et l'avoir appelée sont deux choses : la première se fait depuis un
// bureau avec une page de résultats de recherche, la seconde demande un
// téléphone et trente secondes par numéro. Les fondre en un seul bouton aurait
// fait de « confirmé » un synonyme de « enregistré », c'est-à-dire rien.
//
// ⚠️ ET ELLE PORTE UN NOM. On vient le demander à quelqu'un le jour où un
// numéro ne répond plus — pas pour le blâmer, mais parce qu'il sait quand il a
// appelé et ce qu'il a entendu.
//
// ⚠️ ELLE EXIGE QU'IL Y AIT DES NUMÉROS À AVOIR APPELÉS : confirmer une liste
// vide n'est pas une vérification, c'est une case cochée.
func (s *Service) VerifyNumbers(ctx context.Context, code, actorID string) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Security.SOS
	// ⚠️ ON VÉRIFIE SUR LA LISTE EFFECTIVE — catalogue compris. L'exploitation
	// n'a rien à ressaisir pour confirmer : si le catalogue donne 17 et 18 et
	// qu'ils répondent, il n'y a rien à corriger, et exiger une saisie
	// identique aurait fait recopier des chiffres pour rien (donc en faire
	// une faute de frappe une fois sur dix).
	effective := sosAdminResponse(info.Code, cur)
	if len(effective.Numbers) == 0 {
		return nil, errNothingToVerify
	}
	// ⚠️ LA LISTE EST FIGÉE AU MOMENT DE LA CONFIRMATION. Sans cela, on
	// confirmerait le catalogue — qui peut changer au prochain déploiement —, et
	// la confirmation porterait sur des numéros que personne n'a appelés.
	cur.Numbers = effective.Numbers
	now := time.Now().UTC()
	cur.VerifiedAt = &now
	if aid, err := primitive.ObjectIDFromHex(actorID); err == nil {
		cur.VerifiedBy = &aid
	}
	sec := inst.Security
	sec.SOS = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, cur)
	return &out, nil
}

// SOSOf rend la politique pour les APPLICATIONS — l'adaptateur que le module
// d'alerte appelle.
//
// ⚠️ AU MIEUX, comme tout le reste de ce fichier : une base muette rend les
// DÉFAUTS. Faire échouer l'écran d'urgence parce qu'un réglage est illisible
// serait exactement la mauvaise façon d'échouer.
func (s *Service) SOSOf(ctx context.Context, code string) SOSResponse {
	info, ok := country.Lookup(code)
	if !ok {
		return sosResponse(SOS{})
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return sosResponse(SOS{})
	}
	return sosResponse(inst.Security.SOS)
}

// sameNumbers dit si deux listes portent les mêmes numéros.
//
// ⚠️ ELLE COMPARE LE GENRE ET LE NUMÉRO, PAS LE LIBELLÉ. Renommer un bouton
// « Police » en « Police secours » ne change pas ce qu'on compose : retirer la
// confirmation pour une correction d'orthographe aurait appris à l'exploitation
// que ce drapeau ne veut rien dire.
func sameNumbers(a, b []EmergencyNumber) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]string, len(a))
	for _, n := range a {
		seen[n.Kind] = n.Number
	}
	for _, n := range b {
		if seen[n.Kind] != n.Number {
			return false
		}
	}
	return true
}

// --- LES SEUILS : À PARTIR DE QUAND UNE DÉTECTION SE DÉCLENCHE --------------

var (
	// ⚠️ LE REFUS PORTE LE CHAMP ET SES BORNES. « Valeur invalide » renverrait
	// quelqu'un essayer au hasard sur un réglage qui décide si une alerte part.
	errSOSThresholdRange = apperr.New("sos_threshold_out_of_range",
		"this detection threshold is outside the range we accept",
		http.StatusUnprocessableEntity)
	// ⚠️⚠️ LA RÈGLE QUI ÉTEINT UNE DÉTECTION SANS LE DIRE : une fenêtre plus
	// courte que la durée forte qu'elle doit contenir ne peut JAMAIS se
	// remplir. Elle a son propre refus parce que sa cause n'est pas dans le
	// champ qu'on vient de taper — on règle la durée, et c'est la FENÊTRE qui
	// devient impossible.
	errSOSNoiseWindow = apperr.New("sos_noise_window_too_short",
		"the noise window cannot be shorter than the loud duration it must contain: widen the window, or shorten the duration",
		http.StatusUnprocessableEntity)
	errSOSUnknownApp = apperr.New("sos_unknown_app",
		"detection thresholds are set per app: use client, driver or courier",
		http.StatusUnprocessableEntity)
)

// ThresholdsOf rend les seuils EFFECTIFS d'un pays pour une application.
//
// ⚠️ AU MIEUX, comme tout ce fichier : un pays inconnu ou une base muette
// rendent les défauts. Faire échouer l'écran d'urgence parce qu'un réglage est
// illisible serait exactement la mauvaise façon d'échouer.
//
// ⚠️ ET LE SECOND RETOUR N'EST PAS DÉCORATIF : il nomme les seuils qu'il a
// fallu REMPLACER parce qu'ils étaient hors bornes en base. Un seuil
// silencieusement remplacé est ce qu'on cherche pendant trois jours le soir où
// une alerte n'est pas partie.
func (s *Service) ThresholdsOf(ctx context.Context, code, app string) (sosdetect.Thresholds, []string) {
	cur := SOS{}
	if info, ok := country.Lookup(code); ok {
		if inst, _, err := s.repo.One(ctx, info.Code); err == nil {
			cur = inst.Security.SOS
		}
	}
	return thresholdsOf(cur, app)
}

func thresholdsOf(cur SOS, app string) (sosdetect.Thresholds, []string) {
	out, fixed := sosdetect.Resolve(app, cur.Thresholds, cur.AppThresholds[app])
	out.Revision = cur.ThresholdsRevision
	return out, fixed
}

// checkThresholds refuse une saisie de console hors bornes.
//
// ⚠️ DEUX VÉRIFICATIONS, ET ELLES NE SE REMPLACENT PAS. La première regarde
// chaque champ SEUL ; la seconde regarde les valeurs EFFECTIVES, parce que la
// règle qui lie la fenêtre à la durée ne peut pas se voir sur une saisie
// isolée — régler la seule durée à 2 000 ms est irréprochable champ par champ,
// et éteint pourtant la détection.
func checkThresholds(app string, countryPatch, appPatch sosdetect.Patch) error {
	for _, p := range []sosdetect.Patch{countryPatch, appPatch} {
		if key, ok := sosdetect.Check(p); !ok {
			return errSOSThresholdRange.WithMeta(map[string]any{
				"fields": []string{key},
				"reason": sosdetect.Describe(key),
			})
		}
	}
	effective := sosdetect.Defaults(app).Apply(countryPatch).Apply(appPatch)
	if key, ok := sosdetect.CheckPair(effective); !ok {
		return errSOSNoiseWindow.WithMeta(map[string]any{"fields": []string{key}})
	}
	return nil
}

// thresholdSnapshot rend une empreinte comparable des seuils d'un pays.
//
// ⚠️ UNE CHAÎNE ET NON UN `reflect.DeepEqual` : la carte des surcharges
// d'application n'a pas d'ordre, et deux cartes identiques se comparent mal.
// L'encodage trie ses clés, donc deux réglages identiques donnent la même
// chaîne — ce qui est exactement ce que « rien n'a changé » veut dire.
func thresholdSnapshot(s SOS) string {
	keys := make([]string, 0, len(s.AppThresholds))
	for k := range s.AppThresholds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	enc := json.NewEncoder(&b)
	_ = enc.Encode(s.Thresholds)
	for _, k := range keys {
		b.WriteString(k)
		p := s.AppThresholds[k]
		_ = enc.Encode(p)
	}
	return b.String()
}
