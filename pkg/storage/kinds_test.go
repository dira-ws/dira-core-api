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

// Le JPEG passe partout sauf sur un pictogramme — une icône en JPEG est une
// icône floue sur un fond blanc — et sauf sur un SON, qui n'est pas une image.
//
// ⚠️ LES TROIS FAMILLES SONT NOMMÉES, ET C'EST VOLONTAIRE : un genre ajouté
// sans être classé ici fait TOMBER cette épreuve. Un motif large (« tout ce qui
// n'est pas un pictogramme est une photo ») aurait accepté en silence le
// premier genre d'une quatrième nature — et c'est exactement ce qui est arrivé
// en ajoutant `sound`.
func TestEveryPhotoKindTakesJPEG(t *testing.T) {
	pictos := map[string]bool{"class": true, "marker": true}
	sounds := map[string]bool{"sound": true}
	for kind := range Kinds {
		_, ok := Accepts(kind, "image/jpeg")
		image := !pictos[kind] && !sounds[kind]
		if image != ok {
			t.Errorf("%s : jpeg accepté = %v, attendu %v (pictogramme = %v, son = %v)",
				kind, ok, image, pictos[kind], sounds[kind])
		}
	}
}

// ⚠️⚠️ UN SON NE PASSE QUE PAR LE GENRE `sound`, ET JAMAIS EN WAV.
//
// Trente secondes de WAV pèsent cinq mégaoctets ; en AAC, trois cents
// kilooctets. Sur les réseaux d'Afrique de l'Ouest, le premier ne se
// télécharge pas pendant qu'on fait défiler un feed — autant ne pas avoir de
// son du tout. Et le plafond serré (deux mégaoctets) est ce qui empêche qu'un
// album entier entre par cette porte.
func TestOnlyTheSoundKindTakesAudioAndNeverWav(t *testing.T) {
	for kind := range Kinds {
		for _, audio := range []string{"audio/mpeg", "audio/mp4", "audio/aac", "audio/ogg"} {
			_, ok := Accepts(kind, audio)
			if (kind == "sound") != ok {
				t.Errorf("%s : %s accepté = %v, attendu %v", kind, audio, ok, kind == "sound")
			}
		}
		if _, ok := Accepts(kind, "audio/wav"); ok {
			t.Errorf("%s accepte le WAV : cinq mégaoctets pour trente secondes", kind)
		}
	}
	if max := Kinds["sound"].Max; max != MaxSoundBytes || max > 4<<20 {
		t.Errorf("le plafond du son est %d : serré exprès, un son de fond n'est pas un album", max)
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
