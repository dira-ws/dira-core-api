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
	"strings"
)

// LiveRule est ce que l'exploitation règle pour UN pays.
//
// ⚠️⚠️ ELLE NE DIT PLUS LES LANGUES. Un pays décide si le micro existe chez lui
// et combien de temps il reste ouvert ; ce qu'un modèle sait faire d'une langue
// ne dépend pas de l'endroit où on la parle, et se mesure donc UNE fois, dans
// le catalogue (`Language.Live`). La liste par pays obligeait à ouvrir le wolof
// pays par pays, et laissait quelqu'un qui le parle à Lomé devant un refus.
//
// ⚠️ ELLE NE DIT PAS LES FORMATS AUDIO. Les encodages et la fréquence
// d'échantillonnage sont des CAPACITÉS DU RELAIS, pas des réglages : annoncer
// un encodage que le relais ne sait pas faire entendre à un fournisseur
// donnerait un micro qui part et un serveur qui n'entend rien — une panne
// muette, du côté de l'app, pour un réglage posé du côté du serveur. Ils
// viennent donc du code (`LiveInputEncodings`), et une épreuve du relais
// (`dira-analytics`, `live/contract_test.go`) vérifie qu'il accepte tout ce qui
// est annoncé ici, et qu'un fournisseur au moins l'entend.
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
	// ⚠️ C'EST CE QUE TOUT FOURNISSEUR ENTEND, et donc ce que le relais passe
	// sans y toucher — et le repli de tous les autres encodages. 16 kHz × 16
	// bits = 256 kbit/s : utilisable en 4G et en Wi-Fi, pas sur la 3G de
	// N'Djamena.
	LiveEncodingPCM16 = "pcm16"
	// LiveEncodingOpus : un paquet Opus brut par trame binaire, sans
	// conteneur — ≈ 20 kbit/s en montée au lieu de 256 (#267 §3,
	// dira-ws/dira-llm#20).
	//
	// ⚠️⚠️ ANNONCÉ NE VEUT PAS DIRE RETENU. Le relais ne décode pas l'Opus : il
	// le passe tel quel au seul fournisseur qui l'entend (dira-llm), et répond
	// `pcm16` dans `session.ready.input` partout ailleurs (Gemini, OpenAI).
	// L'application propose, le relais décide par session, et le dit — c'est
	// ce qui rend sûr de l'annoncer dans tous les pays, quel que soit leur
	// fournisseur.
	LiveEncodingOpus = "opus"
	// LiveInputSampleRate : la fréquence attendue de l'entrée.
	LiveInputSampleRate = 16000
	// LiveOutputSampleRate : celle de la sortie du fournisseur, en PCM16.
	LiveOutputSampleRate = 24000
	// LiveOpusOutputSampleRate : celle de la sortie quand l'entrée retenue est
	// l'Opus — des paquets à 48 kHz, ce que rend tout décodeur Opus.
	LiveOpusOutputSampleRate = 48000
)

// LiveInputEncodings : ce qu'une application peut PROPOSER au relais.
//
// ⚠️ UNE SEULE LISTE, LUE PAR LES DEUX CÔTÉS. Le socle l'annonce aux
// applications, le relais s'y mesure dans une épreuve de parité : c'est ce qui
// empêche d'annoncer un format que personne ne fait entendre. Cette plateforme
// a déjà écrit cinq fois une promesse dans un contrat sans l'exécuter.
//
// ⚠️⚠️ `pcm16` EN TÊTE, ET TOUJOURS LÀ : c'est le repli de tous les
// fournisseurs, et une application qui ne sait rien d'autre doit le trouver.
var LiveInputEncodings = []string{LiveEncodingPCM16, LiveEncodingOpus}

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
	langs := r.LiveLanguages()
	if len(langs) == 0 {
		// ⚠️ PAS DE LANGUE, PAS DE DIRECT : un drapeau allumé sans langue
		// ouvrirait un micro que le relais refuserait à la première phrase.
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
