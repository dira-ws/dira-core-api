// Package storage stores uploaded media (dish photos, logos, avatars,
// vehicle photos) in S3-compatible object storage (MinIO in dev). Objects
// are public-read; documents persist the public URL.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// CE QUE CHAQUE GENRE DE TÉLÉVERSEMENT ACCEPTE, ET JUSQU'À QUELLE TAILLE.
//
// ⚠️ UNE TABLE PAR GENRE, ET PLUS « LES IMAGES PARTOUT ». La règle d'avant
// était « toutes les images sur tous les points d'envoi, plus la vidéo sur le
// feed ». Elle paraissait prudente et laissait trois trous :
//
//   - le **SVG était accepté comme photo**. Un SVG n'est pas une image, c'est
//     un document qui peut porter du script : en accepter un comme avatar ou
//     comme reçu, c'est accepter du code fourni par un inconnu dans un
//     fichier qu'on resservira. Il n'a de sens que pour un PICTOGRAMME, où il
//     est précisément le bon format ;
//   - le **PDF n'était accepté nulle part**, alors qu'un justificatif de
//     dépense ou une attestation d'assurance est un PDF neuf fois sur dix.
//     On demandait d'en photographier l'écran ;
//   - **un seul plafond pour tout** : 5 Mio, trop peu pour un scan de carte
//     grise, beaucoup trop pour un pictogramme de 2 Ko.
//
// Chaque genre déclare donc ce qu'il prend. Ajouter un format à un genre est
// un geste qu'on fait exprès, pour ce genre-là.
type Kind struct {
	// Types : type de contenu accepté → extension du fichier déposé.
	Types map[string]string
	// Max : la taille maximale, en octets.
	Max int64
	// Label décrit les formats, pour la phrase de refus — celle que
	// l'utilisateur lit quand son fichier est rejeté.
	Label string
}

// Les formats, nommés une fois.
var (
	// photoTypes : de vraies photographies. ⚠️ SANS SVG.
	photoTypes = map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp",
	}
	// pictoTypes : un pictogramme, où le vectoriel est le bon format.
	pictoTypes = map[string]string{
		"image/png": ".png", "image/webp": ".webp", "image/svg+xml": ".svg",
	}
	// proofTypes : une PREUVE — facture, reçu, quittance, attestation,
	// casier. La photo d'un papier, ou le papier lui-même en PDF.
	proofTypes = map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp",
		"application/pdf": ".pdf",
	}
	// VideoTypes : réservées au feed. Une photo de plat ou un logo d'enseigne
	// n'a aucune raison d'être une vidéo, et l'y autoriser ouvrirait un dépôt
	// de gros fichiers sur tous les points d'entrée.
	VideoTypes = map[string]string{
		"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov",
	}
	// feedTypes : le feed prend les deux.
	feedTypes = merge(photoTypes, VideoTypes)
	// soundTypes : un SON DE FOND pour un carrousel du feed.
	//
	// ⚠️ TROIS FORMATS COMPRESSÉS, ET PAS DE WAV. Trente secondes de WAV
	// pèsent cinq mégaoctets ; en AAC, trois cents kilooctets. Sur les
	// réseaux d'Afrique de l'Ouest, le premier ne se télécharge pas pendant
	// qu'on fait défiler un feed — autant ne pas avoir de son.
	soundTypes = map[string]string{
		"audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/aac": ".aac",
		"audio/ogg": ".ogg",
	}
	// voiceTypes : LE MESSAGE VOCAL D'UNE PERSONNE, gardé pour qu'elle puisse
	// le réécouter et pour que le support l'entende.
	//
	// ⚠️⚠️ UN GENRE À LUI, ET SURTOUT PAS `sound` ÉLARGI. Ce sont deux choses
	// qui n'ont en commun que d'être du son : `sound` est une boucle de
	// musique PUBLIQUE, choisie par un marchand dans un catalogue, servie à
	// tout le feed ; un vocal est la VOIX de quelqu'un, privée, gardée parce
	// qu'une commande en dépend. Élargir `sound` aurait rendu un message vocal
	// sélectionnable comme musique de fond d'un carrousel — et aurait fait
	// passer la voix d'un client dans le feed public.
	//
	// ⚠️ PLUS DE FORMATS QUE `sound`, parce que ce n'est pas nous qui
	// encodons : c'est le téléphone. Android rend du 3GP ou du WebM/Opus,
	// iOS du M4A. Refuser ce qu'un téléphone produit, c'est refuser le vocal
	// à la moitié du marché.
	// speechTypes : ce qu'un moteur de SYNTHÈSE a le droit de produire.
	//
	// ⚠️⚠️ PAS DE WAV, ET J'AI ESSAYÉ DE L'Y METTRE. Le raisonnement était
	// « notre moteur en sort, et refuser obligerait à réencoder ce que nous
	// venons de produire » — et il oubliait QUI TÉLÉCHARGE. Ce son part vers un
	// téléphone sur un réseau d'ici : vingt secondes de WAV pèsent un
	// demi-mégaoctet, la même phrase en opus en pèse trente kilooctets. C'est
	// le moteur qui encode, parce que c'est le seul endroit où cela ne coûte
	// rien. Un garde de ce paquet l'a dit avant moi (`kinds_test.go`).
	speechTypes = map[string]string{
		"audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/m4a": ".m4a",
		"audio/aac": ".aac", "audio/ogg": ".ogg", "audio/opus": ".opus",
		"audio/webm": ".webm",
	}
	voiceTypes = map[string]string{
		"audio/mp4": ".m4a", "audio/m4a": ".m4a", "audio/x-m4a": ".m4a",
		"audio/aac": ".aac", "audio/mpeg": ".mp3", "audio/ogg": ".ogg",
		"audio/opus": ".opus", "audio/webm": ".webm", "audio/3gpp": ".3gp",
	}
)

// Les plafonds, nommés par ce qu'ils bornent.
const (
	// MaxUploadBytes : une photographie.
	MaxUploadBytes = 5 << 20 // 5 Mio
	// MaxPictoBytes : un pictogramme. ⚠️ Serré exprès : au-delà, ce n'est plus
	// un pictogramme, et un SVG d'un mégaoctet est presque toujours un
	// document déguisé.
	MaxPictoBytes = 512 << 10 // 512 Kio
	// MaxProofBytes : un scan de papier, souvent multipage. Plus large qu'une
	// photo, sans devenir un espace de stockage libre.
	MaxProofBytes = 15 << 20 // 15 Mio
	// MaxVideoBytes : une vidéo promo dure quelques secondes mais pèse des
	// dizaines de mégaoctets.
	MaxVideoBytes = 60 << 20 // 60 Mio
	// MaxVoiceBytes borne un message vocal.
	//
	// ⚠️ LE MÊME PLAFOND QUE LA TRANSCRIPTION (`llm.MaxAudioBytes`), et il le
	// faut : un vocal qu'on accepterait de garder mais que le transcripteur
	// refuse serait un fichier stocké dont personne ne saura jamais ce qu'il
	// dit. Deux mégaoctets, c'est une minute large en opus ou en AAC mono.
	MaxVoiceBytes = 2 << 20 // 2 Mio
	// MaxSpeechBytes borne une phrase DITE PAR LE SERVEUR.
	//
	// ⚠️ LE MÊME PLAFOND QU'UN VOCAL, et il est très large pour l'usage : une
	// phrase de confirmation compressée pèse quelques dizaines de kilooctets.
	// Deux mégaoctets ne sont donc pas une cible mais une BORNE — ce qui
	// empêche qu'un moteur mal réglé, ou un texte de dix mille mots, remplisse
	// le seau une phrase à la fois.
	MaxSpeechBytes = 2 << 20 // 2 Mio
	// MaxSoundBytes : trente secondes d'AAC pèsent trois cents kilooctets.
	//
	// ⚠️ SERRÉ EXPRÈS, à deux mégaoctets : c'est dix fois ce qu'il faut, et
	// c'est ce qui empêche qu'un album entier entre par cette porte. Un son de
	// fond de feed se télécharge pendant qu'on fait défiler.
	MaxSoundBytes = 2 << 20 // 2 Mio
)

// privateKinds : les genres que le dépôt NE SERT PAS en lecture publique.
//
// ⚠️⚠️ CE SEUL MOT DÉCIDE DE LA VIE PRIVÉE D'UNE PERSONNE. Le reste du
// contenu de ce dépôt est public par nature — une photo de plat, un
// pictogramme de classe, une bannière : ils sont faits pour être servis à tout
// le monde, par un navigateur, sans jeton. La VOIX de quelqu'un, non. Elle
// était pourtant servie exactement comme eux : URL non signée, politique de
// lecture publique sur tout le seau, clé de la forme
// `voice/<userId>/<uuid>.m4a` (analytics#33 §1).
//
// ⚠️ ET LE GENRE EST LE BON ENDROIT POUR CE CHOIX, parce que c'est lui qui
// préfixe la clé : « privé » devient une règle de chemin, que la politique du
// seau et le cycle de vie des objets savent tous deux appliquer. Le dire
// ailleurs — sur l'appelant, sur le module — aurait demandé que chacun y
// pense.
var privateKinds = map[string]bool{KindVoice: true, KindSpeech: true, KindDocument: true, KindReceipt: true}

// expiringKinds : les genres dont les OBJETS s'effacent d'eux-mêmes, au bout
// de `Config.PrivateRetention`.
//
// ⚠️⚠️ PRIVÉ N'EST PAS ÉPHÉMÈRE, et les deux tables sont séparées pour cela.
// Le cycle de vie du seau était posé sur TOUS les genres privés : rendre
// privées les pièces de conformité les aurait aussi fait EFFACER au bout de
// quatre-vingt-dix jours — un permis, un casier judiciaire, une carte grise
// disparus du seau pendant que leur ligne disait « validé ». Une voix est un
// confort qu'on purge ; une pièce est une PREUVE, dont la durée de garde est
// une décision d'exploitation (le temps d'une habilitation, d'une obligation
// comptable) et non une constante de ce paquet.
var expiringKinds = map[string]bool{KindVoice: true, KindSpeech: true}

// Les genres nommés. ⚠️ EN CONSTANTES PARCE QU'ILS VOYAGENT : un service qui
// dépose un son écrit `storage.KindSpeech`, et une faute de frappe devient
// alors une erreur de compilation plutôt qu'un objet rangé sous un préfixe que
// la politique du seau ne couvre pas — c'est-à-dire un objet PUBLIC.
const (
	// KindVoice : le message vocal d'une personne.
	KindVoice = "voice"
	// KindSpeech : une phrase DITE PAR LE SERVEUR — la réponse de l'assistant
	// lue à voix haute, un texte de l'interface.
	//
	// ⚠️⚠️ PRIVÉ COMME LA VOIX, et pour une raison qui se voit moins : ce que
	// le serveur dit porte ce que la personne a demandé. « De Moursal à
	// Chagoua, quatre mille cinq cent cinquante francs » est son adresse et le
	// prix de sa course, dans un fichier audio. Public, il serait lisible par
	// qui connaît l'URL.
	//
	// ⚠️ ET LA CLÉ PORTE LE COMPTE (`speech/<compte>/…`) : c'est ce qui permet
	// de l'effacer avec lui, comme sa voix.
	KindSpeech = "speech"
	// KindDocument : une pièce de conformité — identité, permis, assurance,
	// carte grise, contrôle technique, casier judiciaire.
	//
	// ⚠️⚠️ PRIVÉE, SERVIE SIGNÉE, ET JAMAIS ÉPHÉMÈRE : c'est une PREUVE, dont la
	// durée de garde est une décision d'exploitation (voir `expiringKinds`).
	KindDocument = "document"
	// KindReceipt : la pièce d'une déclaration d'argent — facture, reçu,
	// quittance. Privée et jamais éphémère, pour la même raison.
	KindReceipt = "receipt"
)

// Private dit si ce genre exige une URL signée.
func Private(kind string) bool { return privateKinds[kind] }

// Kinds : le genre préfixe la clé de l'objet, et dit ce qu'il accepte.
var Kinds = map[string]Kind{
	// Des photographies, servies dans les applications et les consoles.
	"dish":      {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"store":     {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"brand":     {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"vehicle":   {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"avatar":    {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"banner":    {photoTypes, MaxUploadBytes, "jpeg, png, webp"},
	"equipment": {photoTypes, MaxUploadBytes, "jpeg, png, webp"},

	// Des PICTOGRAMMES : les modes de véhicule et les marqueurs de carte. Le
	// vectoriel y est le bon format — une icône doit rester nette à toutes
	// les tailles.
	"class":  {pictoTypes, MaxPictoBytes, "png, webp, svg"},
	"marker": {pictoTypes, MaxPictoBytes, "png, webp, svg"},

	// Le FEED : photos et vidéos courtes.
	"feed": {feedTypes, MaxVideoBytes, "jpeg, png, webp — vidéos : mp4, webm, mov"},

	// LE SON DE FOND d'un carrousel du feed.
	//
	// ⚠️ UN GENRE À LUI, et non `feed` élargi : les plafonds n'ont rien à voir
	// (deux mégaoctets contre soixante), et surtout une photo de plat n'a
	// aucune raison d'être un fichier audio. Élargir `feed` aurait ouvert le
	// dépôt de sons sur tous les points d'entrée qui acceptent une image.
	"sound": {soundTypes, MaxSoundBytes, "mp3, m4a, aac, ogg"},

	// LE MESSAGE VOCAL d'un client — gardé pour être réécouté, et pour que le
	// support entende ce que l'IA a entendu.
	//
	// ⚠️ SÉPARÉ DE `sound` : voir `voiceTypes`. Une boucle de feed et la voix
	// d'une personne ne se rangent pas au même endroit, ne s'écoutent pas par
	// les mêmes gens, et ne se gardent pas aussi longtemps.
	"voice": {voiceTypes, MaxVoiceBytes, "m4a, aac, mp3, ogg, opus, webm, 3gp"},

	// LA VOIX DU SERVEUR : une phrase synthétisée, dite à quelqu'un.
	//
	// ⚠️ UN GENRE À LUI, et non `voice` élargi. Les deux sont privés et les
	// deux sont du son, mais ils ne se gardent pas pour les mêmes raisons : un
	// vocal est une PIÈCE — on le réécoute pour trancher un litige —, une
	// phrase dite est un CONFORT, qu'on peut toujours refaire. Mêlés, la purge
	// de l'un aurait emporté l'autre, et le genre ne dirait plus lequel on
	// regarde dans un seau.
	//
	// ⚠️ ET LE WAV N'Y EST PAS, bien que notre moteur en sorte : ce son part
	// vers un téléphone sur un réseau d'ici, où vingt secondes non compressées
	// pèsent un demi-mégaoctet contre trente kilooctets en opus. C'est le
	// moteur qui encode — le seul endroit où cela ne coûte rien.
	"speech": {speechTypes, MaxSpeechBytes, "mp3, m4a, aac, ogg, opus, webm"},

	// LES PREUVES. ⚠️ LES DEUX SEULS GENRES QUI PRENNENT LE PDF, et c'est
	// pour cela qu'ils sont séparés des photos :
	//
	//   - `receipt` : la pièce d'une déclaration d'entrée ou de sortie
	//     d'argent — facture, reçu, quittance ;
	//   - `document` : une pièce de conformité — identité, permis, assurance,
	//     carte grise, contrôle technique, casier judiciaire.
	//
	// ⚠️ `document` MANQUAIT. Les pièces de conformité n'avaient aucun genre à
	// elles : elles passaient par un genre prévu pour des photographies, donc
	// sans PDF possible, et rangées sous un préfixe qui ne les désignait pas.
	// Une attestation d'assurance est un PDF neuf fois sur dix.
	KindReceipt:  {proofTypes, MaxProofBytes, "jpeg, png, webp, pdf"},
	KindDocument: {proofTypes, MaxProofBytes, "jpeg, png, webp, pdf"},
}

// merge réunit deux tables de formats. ⚠️ Une fonction plutôt qu'une table
// recopiée : le feed accepte les photos ET les vidéos, et deux listes à tenir
// à jour divergent au premier format ajouté.
func merge(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// Known dit si ce genre existe.
func Known(kind string) bool { _, ok := Kinds[kind]; return ok }

// KindNames rend les genres connus, triés — pour la phrase de refus.
func KindNames() []string {
	out := make([]string, 0, len(Kinds))
	for k := range Kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Accepts tells whether a kind takes this content type, and with which
// extension. Point de décision UNIQUE : le handler HTTP et le stockage
// doivent appliquer la même règle, sinon l'un accepte ce que l'autre rejette.
func Accepts(kind, contentType string) (ext string, ok bool) {
	k, known := Kinds[kind]
	if !known {
		return "", false
	}
	ext, ok = k.Types[contentType]
	return ext, ok
}

// LabelFor décrit les formats d'un genre, pour la phrase de refus.
func LabelFor(kind string) string { return Kinds[kind].Label }

// MaxBytesFor returns the size ceiling of a kind.
//
// ⚠️ PAR GENRE, et non plus « 5 Mio sauf le feed ». Un plafond unique était
// trop court pour un scan de carte grise et trop large pour un pictogramme.
func MaxBytesFor(kind string) int64 {
	if k, ok := Kinds[kind]; ok && k.Max > 0 {
		return k.Max
	}
	return MaxUploadBytes
}

// MaxMiBFor rend le plafond d'un genre en mébioctets, prêt à entrer dans une
// phrase de refus : « 5 », « 15 », « 60 », « 0.5 ».
//
// ⚠️ UN NOMBRE, ET SANS SON UNITÉ : l'unité s'écrit dans la phrase TRADUITE
// (« Mio » en français, « MiB » en anglais), parce qu'une valeur servie avec
// son unité aurait imposé le mot français à la phrase anglaise — et c'est
// exactement le genre de détail qu'on ne voit qu'en relisant une capture
// d'écran d'un utilisateur.
func MaxMiBFor(kind string) string {
	mib := float64(MaxBytesFor(kind)) / float64(1<<20)
	if mib == math.Trunc(mib) {
		return strconv.FormatInt(int64(mib), 10)
	}
	return strconv.FormatFloat(mib, 'f', -1, 64)
}

type Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
	// PublicBaseURL is what browsers use to fetch objects
	// (e.g. http://localhost:9000). Defaults to the endpoint scheme+host.
	PublicBaseURL string
	// OwnsPolicy : CE service pose la politique de lecture du seau et son
	// cycle de vie. Un seul doit le faire — le socle.
	//
	// ⚠️⚠️ LE SEAU EST PARTAGÉ, ET LE DERNIER SERVICE DÉMARRÉ GAGNAIT. Chaque
	// service qui ouvrait le dépôt reposait SA politique, celle de la version
	// du socle qu'il embarquait : un service en retard d'une version défaisait
	// au redémarrage ce qu'un autre venait de poser. Une règle de vie privée ne
	// peut pas dépendre de l'ordre dans lequel des conteneurs redémarrent.
	OwnsPolicy bool
	// PrivateRetention : au bout de combien de temps les objets des genres
	// PRIVÉS s'effacent d'eux-mêmes. Zéro = aucun cycle de vie posé.
	//
	// ⚠️⚠️ C'EST LE DÉPÔT QUI PURGE, ET NON UNE TÂCHE DE FOND À NOUS. Le TTL
	// Mongo de 90 jours effaçait la LIGNE `ai_voice_notes`, jamais l'OBJET : la
	// voix restait dans le seau indéfiniment, et plus aucune ligne ne disait
	// qu'elle existait (analytics#33 §1). Une purge écrite par nous aurait eu
	// le même défaut qu'une sauvegarde qu'on ne restaure jamais — elle marche
	// jusqu'au jour où le processus ne tourne plus, et personne ne le voit.
	// Le cycle de vie du seau, lui, tourne sans nous.
	PrivateRetention time.Duration
}

type Store struct {
	client *minio.Client
	bucket string
	public string
	// signer signe les URL POUR L'HÔTE PUBLIC.
	//
	// ⚠️⚠️ UN SECOND CLIENT, ET C'EST UNE NÉCESSITÉ, PAS UN CONFORT. La
	// signature SigV4 couvre l'en-tête `Host` : une URL signée pour
	// `minio:9000` — le nom de service interne — et servie sur
	// `files.dira.llc` est REFUSÉE (`403 SignatureDoesNotMatch`). Mesuré en
	// préproduction : remplacer l'hôte après coup ne marche pas.
	//
	// ⚠️ ET CE CLIENT N'ÉMET AUCUNE REQUÊTE. Il ne sert qu'à CALCULER une
	// signature ; l'hôte public n'a donc pas à être joignable depuis le
	// service, ce qui est précisément le cas en production (le service parle
	// au dépôt par le réseau interne, le téléphone par l'adresse publique).
	signer *minio.Client
}

// New connects to the object store and ensures the bucket exists with a
// public-read policy (dev-friendly; production may front it with a CDN).
func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: make bucket: %w", err)
		}
	}
	if cfg.OwnsPolicy {
		if err := client.SetBucketPolicy(ctx, cfg.Bucket, bucketPolicy(cfg.Bucket)); err != nil {
			return nil, fmt.Errorf("storage: bucket policy: %w", err)
		}
	}

	// LE CYCLE DE VIE DES GENRES QUI EXPIRENT — au mieux : un dépôt qui ne
	// sait pas le poser (une version ancienne, une permission manquante) ne
	// doit pas empêcher le service de démarrer. On le dit, et la purge se fera
	// à la main. ⚠️ Par le PROPRIÉTAIRE seulement, comme la politique.
	if cfg.OwnsPolicy && cfg.PrivateRetention > 0 {
		if err := setPrivateLifecycle(ctx, client, cfg.Bucket, cfg.PrivateRetention); err != nil {
			slog.WarnContext(ctx, "storage: PRIVATE OBJECTS WILL NOT EXPIRE BY THEMSELVES",
				"bucket", cfg.Bucket, "retention", cfg.PrivateRetention, "error", err)
		}
	}

	public := strings.TrimSuffix(cfg.PublicBaseURL, "/")
	if public == "" {
		scheme := "http"
		if cfg.UseSSL {
			scheme = "https"
		}
		public = scheme + "://" + cfg.Endpoint
	}
	st := &Store{client: client, bucket: cfg.Bucket, public: public, signer: client}
	// LE SIGNEUR DE L'HÔTE PUBLIC — voir `Store.signer`.
	//
	// ⚠️ AU MIEUX : sans lui, on signe pour l'hôte interne, et une URL de genre
	// privé sera refusée par le dépôt. On le dit fort plutôt que de servir des
	// adresses mortes en silence.
	if host, secure, ok := hostOf(public); ok && host != cfg.Endpoint {
		pub, err := minio.New(host, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: secure,
		})
		if err != nil {
			slog.WarnContext(ctx, "storage: SIGNED URLS WILL BE REFUSED — cannot sign for the public host",
				"public_host", host, "error", err)
		} else {
			st.signer = pub
		}
	}
	return st, nil
}

// hostOf extrait l'hôte et le schéma d'une base publique.
//
// ⚠️ ELLE EXISTE PARCE QUE LA SIGNATURE COUVRE L'HÔTE. `PublicBaseURL` est une
// URL complète (`https://files.dira.llc`), le client MinIO veut un `host:port`
// et un drapeau TLS : les confondre donne une signature calculée pour le
// mauvais hôte, c'est-à-dire un `403` que rien n'explique.
func hostOf(base string) (host string, secure, ok bool) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", false, false
	}
	return u.Host, u.Scheme == "https", true
}

// Put streams an object and returns its public URL. The key is
// <kind>/<entity>/<uuid><ext>; entity may be empty.
func (s *Store) Put(ctx context.Context, kind, entity, contentType string, size int64, r io.Reader) (string, error) {
	ext, ok := Accepts(kind, contentType)
	if !ok {
		return "", fmt.Errorf("storage: unsupported content type %q for kind %q", contentType, kind)
	}
	key := kind + "/"
	if entity != "" {
		key += entity + "/"
	}
	key += uuid.NewString() + ext
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("storage: put: %w", err)
	}
	return s.public + "/" + s.bucket + "/" + key, nil
}

// Key extracts the object key from a public URL produced by Put. Returns false
// for a URL that does not belong to this bucket — un worker ne doit pas suivre
// une URL arbitraire fournie par un appelant.
func (s *Store) Key(publicURL string) (string, bool) {
	return keyOf(s.public, s.bucket, publicURL)
}

// keyOf : la clé d'un objet de CE seau, désigné par son adresse — nue ou
// SIGNÉE. PURE.
//
// ⚠️ UNE ADRESSE SIGNÉE EST UNE ADRESSE DE CE SEAU : la signature est une
// requête ajoutée (`?X-Amz-…`), qui ne fait pas partie de la clé. Sans cette
// lecture, une application qui renvoie l'adresse qu'on lui a servie — signée —
// se ferait refuser comme « étrangère », ou ferait ranger la signature dans la
// base, où elle expirerait.
func keyOf(public, bucket, rawURL string) (string, bool) {
	bare := Bare(rawURL)
	prefix := public + "/" + bucket + "/"
	if !strings.HasPrefix(bare, prefix) {
		return "", false
	}
	key := strings.TrimPrefix(bare, prefix)
	if key == "" || strings.Contains(key, "..") {
		return "", false
	}
	return key, true
}

// errOutside : l'adresse ne désigne pas un objet de ce dépôt.
//
// ⚠️ L'ERREUR NE PORTE PAS L'ADRESSE. Elle remonte jusqu'aux journaux, qui se
// gardent des mois et se lisent largement : l'adresse d'une pièce d'identité
// n'a rien à y faire, même refusée.
var errOutside = errors.New("storage: address outside this bucket")

// Bare rend une adresse SANS sa signature ni son fragment — celle qu'on range.
//
// ⚠️ ON RANGE L'ADRESSE NUE, JAMAIS LA SIGNÉE : une adresse signée expire, et
// une ligne relue trois jours plus tard porterait un lien mort que plus rien
// ne saurait refaire. On signe à la LECTURE.
func Bare(rawURL string) string {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// KindOf : le genre d'un objet de ce seau — le premier segment de sa clé.
func (s *Store) KindOf(rawURL string) string {
	key, ok := s.Key(rawURL)
	if !ok {
		return ""
	}
	kind, _, _ := strings.Cut(key, "/")
	return kind
}

// Adopt range un objet de ce seau SOUS UN GENRE, et rend sa nouvelle adresse
// NUE.
//
// ⚠️⚠️ ELLE EXISTE PARCE QUE LES APPLICATIONS NE DÉPOSENT PAS FORCÉMENT SOUS
// LE BON GENRE. Le genre décide de ce qu'un objet EST pour le dépôt — sa
// politique, sa durée de vie, son effacement — et le serveur est le seul à
// pouvoir corriger un dépôt mal rangé sans attendre une nouvelle version de
// chaque application.
//
// ⚠️ COPIER D'ABORD, RETIRER ENSUITE, et l'original n'est retiré que si la
// copie a réussi : l'inverse perdrait la pièce sur une coupure. Un original
// qui résiste au retrait est journalisé — la pièce est rangée, mais son ancien
// lien public vit encore, et il faut pouvoir le savoir.
//
// Un objet déjà sous ce genre est rendu tel quel (nu) : le geste se rejoue.
func (s *Store) Adopt(ctx context.Context, rawURL, kind, entity string) (string, error) {
	key, ok := s.Key(rawURL)
	if !ok {
		return "", errOutside
	}
	dst, moved := adoptKey(key, kind, entity)
	if !moved {
		return s.public + "/" + s.bucket + "/" + key, nil
	}
	if _, err := s.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: dst},
		minio.CopySrcOptions{Bucket: s.bucket, Object: key}); err != nil {
		return "", fmt.Errorf("storage: ranger %s sous %s: %w", key, kind, err)
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		slog.WarnContext(ctx, "storage: the original stays — its old public link still works",
			"key", key, "adopted_as", dst, "error", err)
	}
	return s.public + "/" + s.bucket + "/" + dst, nil
}

// adoptKey : la clé d'un objet rangé sous un genre — PURE. `moved` est faux
// quand l'objet y est déjà.
//
// ⚠️ L'EXTENSION SUIT L'OBJET (une photo reste `.jpg`, un PDF `.pdf`), et le
// nom est NEUF : la nouvelle adresse ne dit rien de l'ancienne.
func adoptKey(key, kind, entity string) (string, bool) {
	if strings.HasPrefix(key, kind+"/") {
		return key, false
	}
	ext := ""
	if i := strings.LastIndex(key, "."); i > strings.LastIndex(key, "/") {
		ext = key[i:]
	}
	dst := kind + "/"
	if entity = strings.Trim(entity, "/"); entity != "" && !strings.Contains(entity, "/") && !strings.Contains(entity, "..") {
		dst += entity + "/"
	}
	return dst + uuid.NewString() + ext, true
}

// Exists dit si l'URL désigne un objet DE CE BUCKET qui existe encore. Un
// provisionnement s'en sert pour reprendre les médias d'un jeu de données
// existant — une photo de plat, un avatar — sans jamais promettre une image
// qui ne serait pas là : une URL d'un autre environnement, ou d'un objet
// effacé, vaut « non ».
func (s *Store) Exists(ctx context.Context, publicURL string) bool {
	key, ok := s.Key(publicURL)
	if !ok {
		return false
	}
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	return err == nil
}

// Get opens an object previously stored by Put, addressed by its public URL.
func (s *Store) Get(ctx context.Context, publicURL string) (io.ReadCloser, error) {
	key, ok := s.Key(publicURL)
	if !ok {
		return nil, errOutside
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, nil
}

// Remove deletes an object addressed by its public URL. Une URL étrangère au
// bucket est ignorée sans erreur : l'appelant nettoie au mieux, il ne doit pas
// échouer pour autant.
func (s *Store) Remove(ctx context.Context, publicURL string) error {
	key, ok := s.Key(publicURL)
	if !ok {
		return nil
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: remove %s: %w", key, err)
	}
	return nil
}

// bucketPolicy rend la politique de lecture du seau.
//
// ⚠️⚠️ UN `Allow` LARGE ET UN `Deny` NOMMÉ, et l'ordre ne compte pas : en S3
// comme en MinIO, un `Deny` explicite l'emporte TOUJOURS sur un `Allow`, quel
// que soit l'ordre des clauses. C'est ce qui rend ce couple sûr — l'inverse
// (lister les préfixes publics) aurait rendu public tout genre ajouté ensuite,
// et c'est exactement le défaut qu'on corrige : la voix est devenue publique
// parce que la politique disait « tout ».
//
// ⚠️ ET LE `Deny` PORTE SUR `s3:*`, pas seulement sur la lecture : un seau dont
// les objets privés se LISTENT laisse deviner les clés des autres, et un
// `voice/<userId>/` listable dirait au passage combien de vocaux a envoyés un
// compte donné.
func bucketPolicy(bucket string) string {
	var deny []string
	for kind := range privateKinds {
		deny = append(deny, fmt.Sprintf(`"arn:aws:s3:::%s/%s/*"`, bucket, kind))
	}
	sort.Strings(deny)
	statements := fmt.Sprintf(
		`{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}`,
		bucket)
	if len(deny) > 0 {
		statements += fmt.Sprintf(
			`,{"Effect":"Deny","Principal":{"AWS":["*"]},"Action":["s3:*"],"Resource":[%s]}`,
			strings.Join(deny, ","))
	}
	return `{"Version":"2012-10-17","Statement":[` + statements + `]}`
}

// SignedURL rend une URL à DURÉE LIMITÉE pour un objet de ce dépôt.
//
// ⚠️⚠️ C'EST LA SEULE FAÇON DE SERVIR UN GENRE PRIVÉ, et la durée est courte
// exprès : une URL signée qui vivrait des jours serait une URL publique avec
// une date d'expiration, et elle se retrouverait dans un journal, une capture
// d'écran, un historique de navigateur. Les applications relisent le fil quand
// la leur expire — c'est ce que fait déjà `GET /analytics/ai/thread`.
//
// ⚠️ ELLE REFUSE UNE URL QUI N'EST PAS DE CE SEAU : `Key` le vérifie. Signer
// une adresse fournie par un appelant ferait de ce dépôt un signeur d'URL
// arbitraires.
func (s *Store) SignedURL(ctx context.Context, publicURL string, ttl time.Duration) (string, error) {
	key, ok := s.Key(publicURL)
	if !ok {
		return "", errOutside
	}
	// ⚠️⚠️ SIGNÉ PAR LE CLIENT DE L'HÔTE PUBLIC, et non par celui du réseau
	// interne. La signature SigV4 couvre `Host` : une URL signée pour
	// `minio:9000` puis servie sur `files.dira.llc` est refusée
	// (`403 SignatureDoesNotMatch`). Mesuré en préproduction — remplacer
	// l'hôte après la signature ne marche PAS.
	u, err := s.signer.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("storage: signer %s: %w", key, err)
	}
	return u.String(), nil
}

// RemovePrefix efface TOUS les objets d'un préfixe.
//
// ⚠️⚠️ ELLE EXISTE POUR L'EFFACEMENT D'UN COMPTE. L'effacement ne retirait que
// les objets ENCORE RÉFÉRENCÉS par une ligne de base : passé la rétention de la
// ligne, la voix restait dans le seau — après la suppression du compte
// (analytics#33 §1). Le préfixe, lui, est exact : la clé porte l'identifiant du
// compte.
//
// ⚠️⚠️ ELLE EXIGE UN GENRE **ET** UNE ENTITÉ, c'est-à-dire deux segments
// pleins. Ce n'est pas une précaution de style : un `userID` non renseigné
// produirait `voice/`, et `voice/` est la voix de TOUT LE MONDE. Une chaîne
// vide, elle, effacerait le seau entier — les photos de plats comprises.
func (s *Store) RemovePrefix(ctx context.Context, prefix string) error {
	prefix, ok := namedPrefix(prefix)
	if !ok {
		return fmt.Errorf("storage: préfixe trop large: %q", prefix)
	}
	objects := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	for err := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		if err.Err != nil {
			return fmt.Errorf("storage: effacer %s: %w", err.ObjectName, err.Err)
		}
	}
	return nil
}

// setPrivateLifecycle fait EXPIRER LES OBJETS des genres privés.
//
// ⚠️ UNE RÈGLE PAR GENRE, nommée par son préfixe : c'est le dépôt qui compte
// les jours, pas nous. Et une règle par genre plutôt qu'une règle globale,
// parce que les photos de plats n'ont aucune raison de disparaître.
func setPrivateLifecycle(ctx context.Context, client *minio.Client, bucket string, after time.Duration) error {
	days := int(after.Hours() / 24)
	if days < 1 {
		days = 1
	}
	return client.SetBucketLifecycle(ctx, bucket, expiryRules(days))
}

// expiryRules : les règles du cycle de vie — PURE, pour que l'épreuve voie
// exactement ce qui sera posé.
func expiryRules(days int) *lifecycle.Configuration {
	cfg := lifecycle.NewConfiguration()
	kinds := make([]string, 0, len(expiringKinds))
	for kind := range expiringKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		cfg.Rules = append(cfg.Rules, lifecycle.Rule{
			ID:         "dira-expire-" + kind,
			Status:     "Enabled",
			RuleFilter: lifecycle.Filter{Prefix: kind + "/"},
			Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(days)},
		})
	}
	return cfg
}

// namedPrefix normalise un préfixe et dit s'il nomme bien UN GENRE ET UNE
// ENTITÉ.
//
// ⚠️ PURE, ET C'EST POUR CELA QU'ELLE EXISTE : la borne se vérifie sans dépôt,
// donc l'épreuve n'a pas à faire semblant d'avoir un seau — et `RemovePrefix`
// refuse AVANT tout appel réseau, ce qui est la seule façon d'être sûr qu'un
// préfixe dangereux ne part jamais.
func namedPrefix(prefix string) (string, bool) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	parts := strings.Split(prefix, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return prefix, false
	}
	return prefix + "/", true
}
