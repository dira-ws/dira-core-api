// Package voice dit ce qu'un MESSAGE VOCAL peut être, pour toute la
// plateforme : sa durée, son poids, ses formats.
//
// ⚠️⚠️ UNE SEULE SOURCE, ET C'EST TOUT L'OBJET DE CE PAQUET (core#265). Ces
// bornes vivaient en trois copies — le stockage du socle, la transcription de
// l'analytique, et rien du tout côté applications, qui devaient deviner. Un
// vocal accepté par le stockage mais refusé par le transcripteur serait un
// fichier gardé dont personne ne saura jamais ce qu'il dit ; une application
// qui enregistre plus longtemps que la borne fait parler quelqu'un pour rien.
// Le stockage, `/app/config` et l'analytique lisent désormais ici.
//
// ⚠️ SANS DÉPENDANCE, et ce n'est pas un détail : `pkg/appconfig` l'importe,
// et tous les services importent `pkg/appconfig`. Un paquet de constantes ne
// doit pas faire entrer un client de stockage dans chacun d'eux.
package voice

import "sort"

// MaxSeconds est la durée maximale d'un vocal — celle de notre service de
// transcription (dira-stt, `STT_MAX_SECONDS`). Au-delà, la route vocale
// refuse AVANT tout moteur (`413 audio_too_long`).
const MaxSeconds = 60

// MaxBytes borne un vocal : deux mégaoctets, une minute large en opus ou en
// AAC mono. La borne qu'un client ne peut pas tromper.
const MaxBytes = 2 << 20

// Types : ce qu'un téléphone produit pour un vocal, et l'extension du
// fichier gardé.
//
// ⚠️ PAS DE WAV — une minute y pèse dix mégaoctets, des minutes d'envoi sur
// un réseau lent pour ce qu'opus dit en cent cinquante kilooctets. Et plus de
// formats qu'un son de feed, parce que ce n'est pas nous qui encodons :
// Android rend du 3GP ou du WebM/Opus, iOS du M4A.
var Types = map[string]string{
	"audio/mp4":   ".m4a",
	"audio/m4a":   ".m4a",
	"audio/x-m4a": ".m4a",
	"audio/aac":   ".aac",
	"audio/mpeg":  ".mp3",
	"audio/ogg":   ".ogg",
	"audio/opus":  ".opus",
	"audio/webm":  ".webm",
	"audio/3gpp":  ".3gp",
}

// Limits : ce qu'une application lit pour afficher la VRAIE limite.
type Limits struct {
	MaxSeconds int      `json:"max_audio_seconds"`
	MaxBytes   int      `json:"max_audio_bytes"`
	Formats    []string `json:"formats"`
}

// Served rend les limites telles que `/app/config` les sert : les types
// acceptés, triés — une liste stable d'un appel à l'autre.
func Served() Limits {
	formats := make([]string, 0, len(Types))
	for mime := range Types {
		formats = append(formats, mime)
	}
	sort.Strings(formats)
	return Limits{MaxSeconds: MaxSeconds, MaxBytes: MaxBytes, Formats: formats}
}
