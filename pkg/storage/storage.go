// Package storage stores uploaded media (dish photos, logos, avatars,
// vehicle photos) in S3-compatible object storage (MinIO in dev). Objects
// are public-read; documents persist the public URL.
package storage

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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
)

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
	"receipt":  {proofTypes, MaxProofBytes, "jpeg, png, webp, pdf"},
	"document": {proofTypes, MaxProofBytes, "jpeg, png, webp, pdf"},
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
}

type Store struct {
	client *minio.Client
	bucket string
	public string
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
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, cfg.Bucket)
	if err := client.SetBucketPolicy(ctx, cfg.Bucket, policy); err != nil {
		return nil, fmt.Errorf("storage: bucket policy: %w", err)
	}

	public := strings.TrimSuffix(cfg.PublicBaseURL, "/")
	if public == "" {
		scheme := "http"
		if cfg.UseSSL {
			scheme = "https"
		}
		public = scheme + "://" + cfg.Endpoint
	}
	return &Store{client: client, bucket: cfg.Bucket, public: public}, nil
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
	prefix := s.public + "/" + s.bucket + "/"
	if !strings.HasPrefix(publicURL, prefix) {
		return "", false
	}
	key := strings.TrimPrefix(publicURL, prefix)
	if key == "" || strings.Contains(key, "..") {
		return "", false
	}
	return key, true
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
		return nil, fmt.Errorf("storage: %q is not an object of bucket %s", publicURL, s.bucket)
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
