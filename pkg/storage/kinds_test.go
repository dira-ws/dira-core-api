package storage

import "testing"

// CE QUE CHAQUE GENRE ACCEPTE — éprouvé genre par genre, parce que c'est
// exactement ce que l'ancienne règle « les images partout » ne disait pas.

// ⚠️ LE PDF N'EST ACCEPTÉ QUE SUR LES PREUVES. Nulle part ailleurs : un plat,
// un avatar, une bannière, un pictogramme sont des images, et un PDF accepté
// là serait un document resservi comme une illustration.
func TestOnlyProofKindsTakePDF(t *testing.T) {
	preuves := map[string]bool{"receipt": true, "document": true}
	for kind := range Kinds {
		_, ok := Accepts(kind, "application/pdf")
		if preuves[kind] && !ok {
			t.Errorf("%s doit accepter le PDF : un justificatif en est un neuf fois sur dix", kind)
		}
		if !preuves[kind] && ok {
			t.Errorf("%s ne doit PAS accepter le PDF", kind)
		}
	}
}

// ⚠️ LE SVG N'EST ACCEPTÉ QUE SUR UN PICTOGRAMME. Un SVG n'est pas une image :
// c'est un document qui peut porter du script. En accepter un comme avatar ou
// comme reçu, c'est accepter du code fourni par un inconnu dans un fichier
// qu'on resservira.
func TestOnlyPictogramKindsTakeSVG(t *testing.T) {
	pictos := map[string]bool{"class": true, "marker": true}
	for kind := range Kinds {
		_, ok := Accepts(kind, "image/svg+xml")
		if pictos[kind] && !ok {
			t.Errorf("%s doit accepter le SVG : une icône doit rester nette à toutes les tailles", kind)
		}
		if !pictos[kind] && ok {
			t.Errorf("%s ne doit PAS accepter le SVG — du script dans un fichier resservi", kind)
		}
	}
}

// ⚠️ LA VIDÉO NE PASSE QUE PAR LE FEED. Partout ailleurs, elle ouvrirait un
// dépôt de gros fichiers sur un point d'entrée prévu pour une vignette.
func TestOnlyTheFeedTakesVideo(t *testing.T) {
	for kind := range Kinds {
		_, ok := Accepts(kind, "video/mp4")
		if (kind == "feed") != ok {
			t.Errorf("%s : vidéo acceptée = %v, attendu %v", kind, ok, kind == "feed")
		}
	}
}

// LA NATURE DE CHAQUE GENRE, DÉCLARÉE À LA MAIN.
//
// ⚠️⚠️ C'EST LA TABLE QUI FAIT TOUT LE TRAVAIL DE CES ÉPREUVES, et elle est
// écrite à la main EXPRÈS. Un genre ajouté sans y être classé fait tomber
// `TestEveryKindHasADeclaredNature` : on ne peut pas ajouter un genre sans dire
// de quelle nature il est. Un motif large (« tout ce qui n'est pas un
// pictogramme est une photo ») aurait accepté en silence le premier genre d'une
// nature nouvelle — c'est arrivé deux fois, avec `sound` puis avec `voice`.
type nature string

const (
	photo nature = "photo"
	picto nature = "pictogramme"
	video nature = "photo+vidéo"
	proof nature = "preuve"
	audio nature = "audio"
)

var natures = map[string]nature{
	"dish": photo, "store": photo, "brand": photo, "vehicle": photo,
	"avatar": photo, "banner": photo, "equipment": photo,
	"class": picto, "marker": picto,
	"feed":  video,
	"sound": audio, "voice": audio, "speech": audio,
	"receipt": proof, "document": proof,
}

func TestEveryKindHasADeclaredNature(t *testing.T) {
	for kind := range Kinds {
		if _, ok := natures[kind]; !ok {
			t.Errorf("le genre `%s` n'a pas de nature déclarée : classez-le dans `natures`, "+
				"c'est ce qui dit s'il prend une image, un son ou un PDF", kind)
		}
	}
	for kind := range natures {
		if _, ok := Kinds[kind]; !ok {
			t.Errorf("`%s` est classé mais n'existe plus : retirez-le de `natures`", kind)
		}
	}
}

// Le JPEG passe partout sauf sur un pictogramme — une icône en JPEG est une
// icône floue sur un fond blanc — et sauf sur de l'AUDIO, qui n'est pas une
// image.
func TestEveryPhotoKindTakesJPEG(t *testing.T) {
	for kind, n := range natures {
		_, ok := Accepts(kind, "image/jpeg")
		want := n == photo || n == video || n == proof
		if want != ok {
			t.Errorf("%s (%s) : jpeg accepté = %v, attendu %v", kind, n, ok, want)
		}
	}
}

// ⚠️⚠️ L'AUDIO NE PASSE QUE PAR LES GENRES AUDIO, ET JAMAIS EN WAV.
//
// Trente secondes de WAV pèsent cinq mégaoctets ; en AAC, trois cents
// kilooctets. Sur les réseaux d'Afrique de l'Ouest, le premier ne se
// télécharge pas pendant qu'on fait défiler un feed — autant ne pas avoir de
// son du tout. Et les plafonds serrés (deux mégaoctets) sont ce qui empêche
// qu'un album entier, ou une heure d'enregistrement, entre par cette porte.
func TestOnlyTheAudioKindsTakeAudioAndNeverWav(t *testing.T) {
	for kind, n := range natures {
		for _, mime := range []string{"audio/mpeg", "audio/mp4", "audio/aac", "audio/ogg"} {
			_, ok := Accepts(kind, mime)
			if (n == audio) != ok {
				t.Errorf("%s (%s) : %s accepté = %v, attendu %v", kind, n, mime, ok, n == audio)
			}
		}
		if _, ok := Accepts(kind, "audio/wav"); ok {
			t.Errorf("%s accepte le WAV : cinq mégaoctets pour trente secondes", kind)
		}
		if n != audio {
			continue
		}
		if max := Kinds[kind].Max; max > 4<<20 {
			t.Errorf("le plafond de `%s` est %d : serré exprès — ni un album, ni une heure "+
				"d'enregistrement n'entrent par cette porte", kind, max)
		}
	}
}

// ⚠️⚠️ UN VOCAL ACCEPTE CE QU'UN TÉLÉPHONE PRODUIT, ET `sound` NON.
//
// Ce n'est pas nous qui encodons : Android rend du 3GP ou du WebM/Opus, iOS du
// M4A. Refuser ces formats, c'est refuser le vocal à la moitié du marché — et
// les accepter sur `sound` aurait rendu un message vocal sélectionnable comme
// musique de fond d'un carrousel, donc la voix d'un client dans le feed public.
func TestOnlyTheVoiceKindTakesWhatAPhoneRecords(t *testing.T) {
	for _, mime := range []string{"audio/opus", "audio/webm", "audio/3gpp", "audio/x-m4a"} {
		if _, ok := Accepts("voice", mime); !ok {
			t.Errorf("un vocal doit accepter %s : c'est ce que les téléphones enregistrent", mime)
		}
		if _, ok := Accepts("sound", mime); ok {
			t.Errorf("`sound` accepte %s : un vocal deviendrait une musique de feed", mime)
		}
	}
	if Kinds["voice"].Max != MaxVoiceBytes {
		t.Error("le plafond du vocal doit être celui de la transcription : un fichier gardé " +
			"que le transcripteur refuse est un fichier dont personne ne saura ce qu'il dit")
	}
}

// ⚠️ UN GENRE INCONNU N'ACCEPTE RIEN. Sans ce garde-fou, un genre mal
// orthographié dans une application rangeait des fichiers sous un préfixe
// inventé, et personne ne les retrouvait.
func TestAnUnknownKindAcceptsNothing(t *testing.T) {
	if Known("passeport") {
		t.Fatal("genre inventé déclaré connu")
	}
	for _, ct := range []string{"image/jpeg", "application/pdf", "video/mp4", "image/svg+xml"} {
		if _, ok := Accepts("passeport", ct); ok {
			t.Errorf("un genre inconnu a accepté %s", ct)
		}
	}
}

// ⚠️ LES PLAFONDS DIFFÈRENT, ET DANS LE BON SENS : un pictogramme est plus
// serré qu'une photo, une preuve plus large (un scan est multipage), une vidéo
// plus large encore.
func TestTheCeilingsFollowWhatTheKindHolds(t *testing.T) {
	if !(MaxBytesFor("class") < MaxBytesFor("avatar") &&
		MaxBytesFor("avatar") < MaxBytesFor("receipt") &&
		MaxBytesFor("receipt") < MaxBytesFor("feed")) {
		t.Fatalf("plafonds incohérents : picto=%d photo=%d preuve=%d vidéo=%d",
			MaxBytesFor("class"), MaxBytesFor("avatar"),
			MaxBytesFor("receipt"), MaxBytesFor("feed"))
	}
	// Un genre inconnu retombe sur le plafond d'une photo, jamais sur zéro :
	// zéro refuserait tout corps, y compris celui d'un appel légitime dont le
	// genre vient d'être ajouté ailleurs.
	if MaxBytesFor("passeport") != MaxUploadBytes {
		t.Fatal("un genre inconnu doit retomber sur le plafond d'une photo")
	}
}

// ⚠️ ET CHAQUE GENRE DIT SES FORMATS EN CLAIR : c'est cette phrase que lit
// quelqu'un dont le fichier vient d'être refusé.
func TestEveryKindDescribesItsFormats(t *testing.T) {
	for kind := range Kinds {
		if LabelFor(kind) == "" {
			t.Errorf("%s ne décrit pas ses formats", kind)
		}
	}
}
