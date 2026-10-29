package appconfig

// L'ASSISTANT EN DIRECT — ce qu'une application doit savoir avant d'ouvrir un
// micro en flux continu (dira-ws/dira-core-api#267).
//
// ⚠️⚠️ CELUI-LÀ S'ALLUME, IL NE SE COUPE PAS, et c'est l'exception de ce
// paquet. Toutes les autres fonctions du catalogue partent de « disponible
// partout » et une règle les RETIRE : c'est ce qui rend la couche sûre à poser
// sur une plateforme qui tourne. Le direct, lui, est facturé à la seconde dans
// les deux sens, et la prise en charge vocale des langues qui en ont besoin
// n'est pas acquise — l'arabe tchadien et le wolof restent à mesurer. Un
// défaut « disponible partout » aurait ouvert un micro facturé dans quatorze
// pays le jour du déploiement, sans que personne ne l'ait demandé.
//
// Il s'allume donc par la PRÉSENCE d'un bloc, pays par pays, et la fonction du
// catalogue ne sert qu'à le retirer ENSUITE d'une édition — « le direct au
// Tchad, mais pas dans Lite ».

import (
	"sort"
	"strings"
)

// LiveRule est ce que l'exploitation règle pour UN pays.
//
// ⚠️ ELLE NE DIT PAS LES FORMATS AUDIO. Les encodages et la fréquence
// d'échantillonnage sont des CAPACITÉS DU RELAIS, pas des réglages : annoncer
// `opus` à une application alors que le relais ne sait pas le décoder
// donnerait un micro qui part et un serveur qui n'entend rien — une panne
// muette, du côté de l'app, pour un réglage posé du côté du serveur. Ils
// viennent donc du code (`LiveInputEncodings`), et une épreuve du relais
// vérifie qu'il sait décoder tout ce qui est annoncé ici.
type LiveRule struct {
	// MaxSessionSeconds borne UNE session, pour le coût. Zéro = le défaut.
	//
	// ⚠️ CE N'EST PAS LA LIMITE DU FOURNISSEUR. Gemini Live ferme une session
	// audio au bout de quinze minutes ; le relais la reprend sans que
	// l'application le voie. Cette borne-ci dit ce qu'on accepte de PAYER
	// d'affilée.
	MaxSessionSeconds int `bson:"max_session_seconds,omitempty" json:"max_session_seconds,omitempty"`
	// IdleTimeoutSeconds coupe après un silence. Zéro = le défaut.
	//
	// ⚠️ C'EST LE ROBINET QUI COMPTE VRAIMENT : un téléphone posé sur une
	// table, micro ouvert, facture à la seconde sans que personne ne parle.
	IdleTimeoutSeconds int `bson:"idle_timeout_seconds,omitempty" json:"idle_timeout_seconds,omitempty"`
	// Languages : les langues OUVERTES au direct dans ce pays.
	//
	// ⚠️⚠️ VIDE VEUT DIRE « AUCUNE », ET DONC « LE DIRECT EST ÉTEINT ICI ».
	// C'est le contraire de la convention de `listed` dans ce paquet, où une
	// liste vide veut dire « tout est permis » — et c'est voulu : la prise en
	// charge vocale d'une langue par le fournisseur SE MESURE (banc de #266)
	// avant de s'ouvrir. « Vide = toutes » aurait ouvert l'arabe tchadien par
	// omission, dans le pays où il est la langue principale, sans que personne
	// n'ait écouté ce que le modèle en fait.
	Languages []string `bson:"languages,omitempty" json:"languages,omitempty"`
}

// LiveBlock est ce qu'une application LIT dans `/app/config`.
//
// ⚠️ TOUS LES CHAMPS SONT SERVIS, défauts appliqués : une application ne doit
// pas avoir à connaître nos défauts pour savoir quand couper son micro.
type LiveBlock struct {
	MaxSessionSeconds  int       `json:"max_session_seconds"`
	IdleTimeoutSeconds int       `json:"idle_timeout_seconds"`
	Languages          []string  `json:"languages"`
	Input              LiveInput `json:"input"`
}

// LiveInput dit ce que le relais sait RECEVOIR.
type LiveInput struct {
	Encodings  []string `json:"encodings"`
	SampleRate int      `json:"sample_rate"`
}

// Les encodages d'entrée, et la fréquence : des CAPACITÉS, pas des réglages.
const (
	// LiveEncodingPCM16 : PCM signé 16 bits, petit-boutien, mono.
	//
	// ⚠️ C'EST CE QUE LE FOURNISSEUR EXIGE, et donc ce que le relais passe
	// sans y toucher. 16 kHz × 16 bits = 256 kbit/s : utilisable en 4G et en
	// Wi-Fi, pas sur la 3G de N'Djamena. L'Opus demandé par #267 §3 exige un
	// décodeur dans le relais ; tant qu'il n'y est pas, il n'est pas annoncé.
	LiveEncodingPCM16 = "pcm16"
	// LiveInputSampleRate : la fréquence attendue de l'entrée.
	LiveInputSampleRate = 16000
	// LiveOutputSampleRate : celle de la sortie du fournisseur.
	LiveOutputSampleRate = 24000
)

// LiveInputEncodings : ce que le relais sait décoder AUJOURD'HUI.
//
// ⚠️ UNE SEULE LISTE, LUE PAR LES DEUX CÔTÉS. Le socle l'annonce aux
// applications, le relais s'y mesure dans une épreuve de parité : c'est ce qui
// empêche d'annoncer un format que personne ne décode. Cette plateforme a déjà
// écrit cinq fois une promesse dans un contrat sans l'exécuter.
var LiveInputEncodings = []string{LiveEncodingPCM16}

// Les défauts, quand l'exploitation ne dit rien.
const (
	LiveMaxSessionSecondsDefault = 600
	LiveIdleSecondsDefault       = 30
)

// Les bornes acceptées à l'écriture.
const (
	LiveMaxSessionSecondsFloor   = 60
	LiveMaxSessionSecondsCeiling = 1800
	LiveIdleSecondsFloor         = 5
	LiveIdleSecondsCeiling       = 300
)

// LiveFor rend le bloc servi à CE pays et CETTE édition — et dit s'il existe.
//
// ⚠️⚠️ LES DEUX PORTES SONT ICI, DANS UNE SEULE FONCTION, et c'est délibéré :
// la présence d'un bloc pour le pays, et la fonction du catalogue qui peut le
// retirer d'une édition. Écrite deux fois — une fois pour servir le bloc, une
// fois pour servir le drapeau —, cette règle aurait fini par se contredire :
// une application lirait `features.assistant_live: true` et aucun bloc, ou
// l'inverse, et c'est elle qui aurait tranché. Sur cette plateforme, une règle
// écrite deux fois est perdue une fois.
func (r *Registry) LiveFor(countryCode, ed string) (LiveBlock, bool) {
	if r == nil {
		return LiveBlock{}, false
	}
	rule, ok := r.Live[strings.ToUpper(strings.TrimSpace(countryCode))]
	if !ok {
		return LiveBlock{}, false
	}
	langs := cleanLanguages(rule.Languages)
	if len(langs) == 0 {
		// ⚠️ PAS DE LANGUE, PAS DE DIRECT. Voir `LiveRule.Languages` : ici,
		// vide veut dire « aucune ».
		return LiveBlock{}, false
	}
	if !r.Allows(FeatureAssistantLive, countryCode, ed) {
		return LiveBlock{}, false
	}
	return LiveBlock{
		MaxSessionSeconds:  orDefault(rule.MaxSessionSeconds, LiveMaxSessionSecondsDefault),
		IdleTimeoutSeconds: orDefault(rule.IdleTimeoutSeconds, LiveIdleSecondsDefault),
		Languages:          langs,
		Input: LiveInput{
			Encodings:  append([]string(nil), LiveInputEncodings...),
			SampleRate: LiveInputSampleRate,
		},
	}, true
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// cleanLanguages normalise et TRIE les langues, en jetant les doublons.
//
// ⚠️ TRIÉES, parce que la réponse de `/app/config` est mise en cache par les
// applications : un ordre qui change à chaque lecture invalide ce cache sans
// qu'aucun réglage ait bougé.
func cleanLanguages(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
