package appconfig

// LES LANGUES DE LA PLATEFORME — UN CATALOGUE, SANS PAYS.
//
// ⚠️⚠️ POURQUOI ICI, ET PAS DANS CHAQUE PAYS. Une langue se réglait à trois
// endroits : la liste écrite en dur dans chaque application, les « langues
// attendues » de chaque pays à l'analytique, et les langues du direct, encore
// par pays. Trois listes pour une seule question — « quelles langues Dira
// parle-t-elle ? » — et aucune ne répondait à celle qu'on se pose vraiment :
// quelqu'un qui parle wolof à Lomé doit pouvoir parler wolof à Lomé. Ce qu'un
// modèle sait faire d'une langue ne dépend pas du pays où on la parle.
//
// ⚠️ ET ICI, DANS LE REGISTRE, parce que c'est ce que les applications lisent
// déjà, sans jeton, avant l'écran de connexion — le choix de la langue s'y
// présente — et ce que l'analytique lit déjà pour le direct. Le MODÈLE de
// chaque langue reste dans la table d'attribution de l'analytique (`*/wo`) :
// c'est elle qui décide de la facture, et une application n'a pas à le savoir.

import "strings"

// ScriptArabic : l'écriture arabe, qui se lit de droite à gauche.
const ScriptArabic = "arab"

// Les bornes du catalogue, refusées à l'écriture.
const (
	// LanguagesMax : au-delà, ce n'est plus un sélecteur qu'on parcourt.
	LanguagesMax = 40
	// LanguageHintMax : l'indication part dans CHAQUE consigne de transcription.
	LanguageHintMax = 500
)

// fallbackLanguage : celle d'un catalogue qui n'en offrirait aucune aux écrans.
// L'écriture le refuse ; cette lecture-ci est la seconde porte, pour un
// document corrigé à la main.
const fallbackLanguage = "fr"

// Language est une langue du catalogue, telle que la console la règle.
type Language struct {
	// Code : ISO 639-1 quand il existe (`wo`, `ff`, `ee`, `bm`), ISO 639-3
	// sinon (`shu`, `man`). C'est ce qu'une application envoie à `language`.
	Code string `bson:"code" json:"code"`
	// Name en français, Native comme ses locuteurs l'écrivent — c'est lui
	// qu'une personne cherche dans une liste.
	Name   string `bson:"name" json:"name"`
	Native string `bson:"native" json:"native"`
	// App : on peut lire les écrans dans cette langue.
	//
	// ⚠️ UNE APPLICATION NE L'OFFRE QUE SI ELLE EN A LA TRADUCTION. Ce drapeau
	// dit « l'exploitation la propose », pas « les chaînes existent » : une
	// version installée avant la traduction doit l'ignorer, pas afficher un
	// écran en français sous une étiquette wolof.
	App bool `bson:"app" json:"app"`
	// Spoken : on peut parler à Dira dans cette langue.
	Spoken bool `bson:"spoken" json:"spoken"`
	// Live : ouverte à l'assistant EN DIRECT.
	//
	// ⚠️⚠️ OFF PAR DÉFAUT, COCHÉE LANGUE PAR LANGUE. La tenue d'un modèle dans
	// une langue SE MESURE avant de s'ouvrir (#267 §6). Le pays, lui, ne
	// décide plus que si le micro existe ici, et combien de temps.
	Live bool `bson:"live" json:"live"`
	// Script : `arab`, ou vide pour l'alphabet latin. C'est lui qui retourne
	// l'interface et qui dit au modèle dans quelle écriture rendre le texte.
	Script string `bson:"script,omitempty" json:"script,omitempty"`
	// Hint : ce qu'on dit au modèle pour transcrire juste — l'alphabet, les
	// lettres propres, le mélange avec le français. Vide = celle du catalogue
	// de l'analytique, ou le simple nom pour une langue qu'il ne connaît pas.
	Hint string `bson:"hint,omitempty" json:"hint,omitempty"`
	// Whisper : le code sous lequel Whisper la connaît. Vide = il l'ignore, et
	// seul un modèle multimodal l'entendra.
	Whisper string `bson:"whisper,omitempty" json:"whisper,omitempty"`
}

// startingLanguages : le catalogue tant que personne n'a rien enregistré.
//
// ⚠️ LES ÉCRANS GARDENT LES TROIS LANGUES QU'ILS ONT DÉJÀ, ET LE DIRECT LES DEUX
// QUI ONT ÉTÉ MESURÉES (`fr`, `shu`) : un déploiement ne doit ni promettre une
// traduction qui n'existe pas, ni ouvrir un micro facturé à la seconde dans une
// langue que personne n'a écoutée.
//
// ⚠️ PAS DE `Hint` NI DE `Whisper` : l'analytique les tient déjà pour ces
// langues, et les recopier ici ferait deux vérités qui divergeraient à la
// première retouche.
var startingLanguages = []Language{
	{Code: "fr", Name: "Français", Native: "Français", App: true, Spoken: true, Live: true},
	{Code: "en", Name: "Anglais", Native: "English", App: true, Spoken: true},
	{Code: "shu", Name: "Arabe tchadien", Native: "عربي تشادي", App: true, Spoken: true, Live: true, Script: ScriptArabic},
	{Code: "ee", Name: "Éwé", Native: "Eʋegbe", Spoken: true},
	{Code: "wo", Name: "Wolof", Native: "Wolof", Spoken: true},
	{Code: "ff", Name: "Peul", Native: "Pulaar · Pular · Fulfulde", Spoken: true},
	{Code: "man", Name: "Malinké", Native: "Maninka · Mandinka", Spoken: true},
	{Code: "bm", Name: "Bamanan", Native: "Bamanankan", Spoken: true},
}

// LanguageCatalogue rend les langues en vigueur : ce que la console a rangé,
// sinon la liste de départ.
//
// ⚠️ TOUT OU RIEN, SANS FUSION : retirer une langue doit la retirer. Une
// liste de départ mêlée à ce qui est rangé la ferait revenir à chaque lecture.
//
// ⚠️ UNE COPIE : la réponse part dans des mains qui peuvent l'écrire.
func (r *Registry) LanguageCatalogue() []Language {
	src := startingLanguages
	if r != nil && len(r.Languages) > 0 {
		src = r.Languages
	}
	return append([]Language(nil), src...)
}

// LanguagesBlock est ce qu'une application lit dans `/app/config`.
type LanguagesBlock struct {
	// Default : la langue de l'application tant que la personne n'a rien
	// choisi — la première qui s'offre aux écrans.
	Default string `json:"default"`
	// Items : ce qui se propose, dans l'ordre de la console.
	Items []LanguageItem `json:"items"`
}

// LanguageItem : une langue, telle qu'une application la propose.
//
// ⚠️⚠️ UNE STRUCTURE À PART, PAS `Language`. Cette réponse est PUBLIQUE, sans
// jeton : l'indication au modèle et le code Whisper sont notre cuisine, et une
// étiquette `json:"-"` oubliée sur un champ futur les aurait publiés. Ici, un
// champ ne sort que si quelqu'un l'ajoute exprès.
type LanguageItem struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Native string `json:"native"`
	// RTL : elle s'écrit de droite à gauche.
	RTL    bool `json:"rtl"`
	App    bool `json:"app"`
	Spoken bool `json:"spoken"`
}

// LanguagesFor rend le bloc servi aux applications.
//
// ⚠️ NI PAYS NI ÉDITION DANS SA SIGNATURE, et c'est la règle écrite dans le
// type : le jour où quelqu'un voudra la faire varier, il devra le décider ici,
// pas le glisser dans un filtre.
//
// ⚠️ UNE LANGUE QUI NE S'OFFRE NULLE PART N'EST PAS SERVIE, mais elle reste
// ENTENDUE : un téléphone qui l'a gardée continue d'envoyer son vocal.
func (r *Registry) LanguagesFor() LanguagesBlock {
	out := LanguagesBlock{Items: []LanguageItem{}}
	for _, l := range r.LanguageCatalogue() {
		if !l.App && !l.Spoken {
			continue
		}
		out.Items = append(out.Items, LanguageItem{
			Code: l.Code, Name: l.Name, Native: l.Native,
			RTL: l.Script == ScriptArabic, App: l.App, Spoken: l.Spoken,
		})
		if out.Default == "" && l.App {
			out.Default = l.Code
		}
	}
	if out.Default == "" {
		out.Default = fallbackLanguage
	}
	return out
}

// LiveLanguages : les langues ouvertes au direct, dans l'ordre du catalogue.
//
// ⚠️ IL FAUT AUSSI QU'ELLE SE PARLE : une langue ouverte au direct mais qu'aucun
// écran ne propose n'est jamais choisie. L'écriture le refuse ; cette lecture-ci
// est la seconde porte, parce qu'ouvrir un micro ne doit pas tenir à un seul
// contrôle.
func (r *Registry) LiveLanguages() []string {
	out := []string{}
	for _, l := range r.LanguageCatalogue() {
		if code := strings.ToLower(strings.TrimSpace(l.Code)); l.Live && l.Spoken && code != "" {
			out = append(out, code)
		}
	}
	return out
}
