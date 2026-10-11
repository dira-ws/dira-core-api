// LES SEUILS DE DÉTECTION DU BOUTON D'ALERTE — à partir de QUAND ça déclenche.
//
// Le pays décidait déjà SI une détection est allumée (`internal/country`). Ce
// paquet décide À PARTIR DE QUAND elle se déclenche, et il existe parce que ce
// nombre était codé en dur dans chaque application.
//
// ⚠️⚠️ ET ILS NE CONCORDAIENT PAS. Le choc partait à 8 g chez le chauffeur et à
// 6 g chez le livreur. Personne ne l'avait décidé : chaque équipe avait estimé.
// Pire, aucun n'avait été mesuré sur le terrain — et régler un seuil demandait
// une version sur le Play Store, puis d'attendre que les gens mettent à jour.
//
// ⚠️⚠️ POURQUOI CE PAQUET EST DANS `pkg/` ET PAS DANS UN `internal/`.
//
// Deux modules ont besoin de cette table : `internal/country` la STOCKE et la
// valide, `internal/sos` la SERT aux applications. Les lois de ce dépôt
// voudraient que chacun déclare sa propre forme — c'est ce que fait `Settings`,
// délibérément, pour que le module d'alerte ne dépende pas de la forme qu'un
// réglage de console a prise.
//
// Ici, ce serait un piège. Recopier trente nombres dans deux paquets, c'est la
// règle écrite deux fois — donc perdue une fois : le jour où une borne bouge
// d'un côté, l'autre continue d'accepter ce que le premier refuse, et personne
// ne le voit avant qu'un pays entier ne se retrouve avec un seuil que le
// serveur remplace en silence. La TABLE est le contrat ; elle vit en un seul
// endroit.
//
// ⚠️ DES DURÉES, PAS DES TRAMES. Les applications lisent le micro par trames de
// 100 ms, mais ce découpage leur appartient. On sert des millisecondes ; le
// contrat ne dépend donc pas de la taille d'une trame, et une application qui
// changerait la sienne n'aurait rien à renégocier.
package sosdetect

import (
	"fmt"
	"math"
	"sort"
)

// Les trois applications qui détectent. ⚠️ `courier` et `driver` sont DEUX
// applications bien qu'elles partagent le rôle `driver` au socle : un livreur
// à moto ne subit pas les mêmes secousses qu'un chauffeur en voiture, et
// c'était déjà la seule chose sur laquelle les deux équipes avaient divergé.
const (
	AppClient  = "client"
	AppDriver  = "driver"
	AppCourier = "courier"
)

// Apps est la liste admise, dans l'ordre où la console montre les colonnes.
var Apps = []string{AppClient, AppDriver, AppCourier}

// ValidApp dit si ce mot désigne une application qui détecte.
func ValidApp(a string) bool {
	for _, v := range Apps {
		if v == a {
			return true
		}
	}
	return false
}

// Les clés de la table, nommées. ⚠️ Des constantes et non des chaînes écrites
// à la main : ces clés sortent en JSON vers la console (les bornes d'un champ
// de saisie) et reviennent dans les refus (`fields`). Une faute de frappe
// ferait un refus qui ne désigne aucun champ.
const (
	KeyShakeMinG     = "shake.min_g"
	KeyShakeCount    = "shake.count"
	KeyShakeWindow   = "shake.window_ms"
	KeyShakeMinGap   = "shake.min_gap_ms"
	KeyShakeCooldown = "shake.cooldown_s"
	KeyCrashMinG     = "crash.min_g"
	KeyCrashCooldown = "crash.cooldown_s"
	KeyNoiseLoud     = "noise.loud_dbfs"
	KeyNoiseMinLoud  = "noise.min_loud_ms"
	KeyNoiseWindow   = "noise.window_ms"
	KeyNoiseCooldown = "noise.cooldown_s"
)

// Bound est l'intervalle admis d'un champ.
//
// ⚠️⚠️ LES BORNES PROTÈGENT CONTRE DEUX ERREURS OPPOSÉES, et la seconde est la
// dangereuse. Un seuil trop BAS remplit la file de l'exploitation de fausses
// alertes, et les opérateurs apprennent à les ignorer — ça se voit. Un seuil
// trop HAUT ne se déclenche plus jamais, et personne ne s'en aperçoit : ça ne
// laisse aucune trace. C'est pour ce deuxième cas qu'existe la mesure
// (`POST /sos/detections`).
type Bound struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Bounds est LA table. Elle sert quatre choses, et c'est pour cela qu'elle est
// unique : le refus de la console, le rabotage côté serveur, les bornes des
// champs de saisie, et la documentation du contrat.
var Bounds = map[string]Bound{
	// La secousse : trois pics francs dans une seconde et demie. En dessous de
	// 1,8 g, marcher avec le téléphone à la main suffirait.
	KeyShakeMinG:   {1.8, 5.0},
	KeyShakeCount:  {2, 6},
	KeyShakeWindow: {800, 4000},
	// Deux pics plus proches que ça sont le MÊME pic — le rebond du capteur.
	KeyShakeMinGap:   {100, 1000},
	KeyShakeCooldown: {3, 120},
	// Le choc : un accident. ⚠️ 4 g EST DÉJÀ BAS — un téléphone qui tombe d'un
	// support de tableau de bord y arrive. C'est la borne basse, pas une
	// suggestion.
	KeyCrashMinG:     {4, 16},
	KeyCrashCooldown: {10, 300},
	// Le grand bruit. ⚠️ 0 dBFS EST LE MAXIMUM ABSOLU : un seuil à 0 ne se
	// déclencherait jamais, et c'est exactement la panne silencieuse décrite
	// plus haut. D'où une borne haute à −3.
	KeyNoiseLoud:     {-30, -3},
	KeyNoiseMinLoud:  {300, 3000},
	KeyNoiseWindow:   {500, 5000},
	KeyNoiseCooldown: {10, 600},
}

// Keys rend les clés de la table, triées — pour une console et des tests qui
// ne dépendent pas de l'ordre d'itération d'une map.
func Keys() []string {
	out := make([]string, 0, len(Bounds))
	for k := range Bounds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (b Bound) holdsF(v float64) bool { return v >= b.Min && v <= b.Max }
func (b Bound) clampF(v float64) float64 {
	return math.Min(math.Max(v, b.Min), b.Max)
}
func (b Bound) clampI(v int) int { return int(b.clampF(float64(v))) }

// Shake : secouer l'appareil. Trois pics francs dans une fenêtre.
type Shake struct {
	MinG      float64 `json:"min_g" bson:"min_g,omitempty"`
	Count     int     `json:"count" bson:"count,omitempty"`
	WindowMS  int     `json:"window_ms" bson:"window_ms,omitempty"`
	MinGapMS  int     `json:"min_gap_ms" bson:"min_gap_ms,omitempty"`
	CooldownS int     `json:"cooldown_s" bson:"cooldown_s,omitempty"`
}

// Crash : un choc. ⚠️ Pas proposé au passager — il n'est pas au volant, et un
// téléphone qui tombe du canapé ressemble à un choc.
type Crash struct {
	MinG      float64 `json:"min_g" bson:"min_g,omitempty"`
	CooldownS int     `json:"cooldown_s" bson:"cooldown_s,omitempty"`
}

// Noise : le GRAND BRUIT. Un niveau sonore, sans reconnaissance de mots.
//
// ⚠️⚠️ dBFS N'EST PAS UN NIVEAU ABSOLU, et c'est ce qui rend ce seuil le plus
// incertain des trois. −12 dBFS, c'est 12 dB sous le maximum que le micro DE CE
// TÉLÉPHONE sait coder : le même cri donne un autre chiffre sur un autre
// modèle, et un téléphone au fond d'une poche en perd beaucoup. C'est
// précisément pourquoi ce nombre doit pouvoir bouger sans nouvelle version, et
// pourquoi la mesure transmet le modèle de l'appareil.
type Noise struct {
	LoudDBFS  float64 `json:"loud_dbfs" bson:"loud_dbfs,omitempty"`
	MinLoudMS int     `json:"min_loud_ms" bson:"min_loud_ms,omitempty"`
	WindowMS  int     `json:"window_ms" bson:"window_ms,omitempty"`
	CooldownS int     `json:"cooldown_s" bson:"cooldown_s,omitempty"`
}

// Thresholds est le bloc RÉSOLU, tel qu'une application le reçoit.
//
// ⚠️ TOUJOURS COMPLET. Chaque champ porte une valeur après résolution :
// l'application n'a pas à refaire la cascade, et n'a donc aucune occasion de
// la refaire autrement que nous.
type Thresholds struct {
	// Revision augmente à chaque enregistrement console QUI CHANGE une valeur.
	//
	// ⚠️ ELLE VOYAGE JUSQUE DANS LA MESURE. Sans elle, on lirait « 12 % de ces
	// propositions sont annulées » sans savoir sous quel seuil — c'est-à-dire
	// qu'on ne pourrait rien conclure après avoir réglé quoi que ce soit.
	Revision int    `json:"revision"`
	Shake    Shake  `json:"shake"`
	Crash    *Crash `json:"crash,omitempty"`
	Noise    Noise  `json:"noise"`
}

// Defaults rend les valeurs que les applications appliquent AUJOURD'HUI.
//
// ⚠️ CE SONT LES VALEURS EN VIGUEUR, PAS DES VALEURS CHOISIES. Les servir
// telles quelles au premier déploiement ne change rien pour personne — c'est la
// condition pour que ce contrat sorte sans rien casser, et c'est pour ça qu'on
// garde la divergence 8 g / 6 g du choc plutôt que de la « corriger » ici. Elle
// se corrigera sur des mesures, depuis la console, pas dans un commit.
func Defaults(app string) Thresholds {
	t := Thresholds{
		Shake: Shake{MinG: 2.7, Count: 3, WindowMS: 1500, MinGapMS: 250, CooldownS: 8},
		Noise: Noise{LoudDBFS: -12, MinLoudMS: 800, WindowMS: 1500, CooldownS: 60},
	}
	switch app {
	case AppDriver:
		t.Crash = &Crash{MinG: 8, CooldownS: 30}
	case AppCourier:
		t.Crash = &Crash{MinG: 6, CooldownS: 30}
	}
	// ⚠️ AUCUN `default:` QUI DONNERAIT UN CHOC. Une application inconnue — ou
	// le passager — n'en reçoit pas, et c'est voulu : la spec passager l'exclut,
	// et inventer un seuil pour une application qu'on ne connaît pas serait
	// inventer un comportement.
	return t
}

// Patch est une surcharge : tout y est facultatif.
//
// ⚠️ DES POINTEURS, PARCE QUE « NON RÉGLÉ » ET « RÉGLÉ À ZÉRO » NE SONT PAS LA
// MÊME CHOSE. Avec des valeurs nues, un pays qui ne règle que la secousse
// remettrait le bruit et le choc à zéro — c'est-à-dire, après rabotage, à leur
// borne basse : des détections qui partent en continu.
type Patch struct {
	ShakeMinG     *float64 `bson:"shake_min_g,omitempty" json:"shake_min_g,omitempty"`
	ShakeCount    *int     `bson:"shake_count,omitempty" json:"shake_count,omitempty"`
	ShakeWindow   *int     `bson:"shake_window_ms,omitempty" json:"shake_window_ms,omitempty"`
	ShakeMinGap   *int     `bson:"shake_min_gap_ms,omitempty" json:"shake_min_gap_ms,omitempty"`
	ShakeCooldown *int     `bson:"shake_cooldown_s,omitempty" json:"shake_cooldown_s,omitempty"`
	CrashMinG     *float64 `bson:"crash_min_g,omitempty" json:"crash_min_g,omitempty"`
	CrashCooldown *int     `bson:"crash_cooldown_s,omitempty" json:"crash_cooldown_s,omitempty"`
	NoiseLoud     *float64 `bson:"noise_loud_dbfs,omitempty" json:"noise_loud_dbfs,omitempty"`
	NoiseMinLoud  *int     `bson:"noise_min_loud_ms,omitempty" json:"noise_min_loud_ms,omitempty"`
	NoiseWindow   *int     `bson:"noise_window_ms,omitempty" json:"noise_window_ms,omitempty"`
	NoiseCooldown *int     `bson:"noise_cooldown_s,omitempty" json:"noise_cooldown_s,omitempty"`
}

// Empty dit que cette surcharge ne règle rien.
func (p Patch) Empty() bool { return p == Patch{} }

// Fields nomme les champs que cette surcharge règle — pour le journal des
// modifications, qui doit dire QUOI a changé et pas seulement « les seuils ».
func (p Patch) Fields() []string {
	var out []string
	for _, c := range []struct {
		key string
		set bool
	}{
		{KeyShakeMinG, p.ShakeMinG != nil},
		{KeyShakeCount, p.ShakeCount != nil},
		{KeyShakeWindow, p.ShakeWindow != nil},
		{KeyShakeMinGap, p.ShakeMinGap != nil},
		{KeyShakeCooldown, p.ShakeCooldown != nil},
		{KeyCrashMinG, p.CrashMinG != nil},
		{KeyCrashCooldown, p.CrashCooldown != nil},
		{KeyNoiseLoud, p.NoiseLoud != nil},
		{KeyNoiseMinLoud, p.NoiseMinLoud != nil},
		{KeyNoiseWindow, p.NoiseWindow != nil},
		{KeyNoiseCooldown, p.NoiseCooldown != nil},
	} {
		if c.set {
			out = append(out, c.key)
		}
	}
	return out
}

// Apply pose une surcharge sur un bloc résolu.
//
// ⚠️ UNE SURCHARGE DE CHOC SUR UNE APPLICATION QUI N'EN A PAS N'EN CRÉE PAS.
// Régler `crash.min_g` pour le passager ne doit pas lui faire pousser une
// détection que sa spec exclut : la valeur est retenue (elle redeviendra utile
// si la décision change), elle n'est simplement pas servie.
func (t Thresholds) Apply(p Patch) Thresholds {
	if p.ShakeMinG != nil {
		t.Shake.MinG = *p.ShakeMinG
	}
	if p.ShakeCount != nil {
		t.Shake.Count = *p.ShakeCount
	}
	if p.ShakeWindow != nil {
		t.Shake.WindowMS = *p.ShakeWindow
	}
	if p.ShakeMinGap != nil {
		t.Shake.MinGapMS = *p.ShakeMinGap
	}
	if p.ShakeCooldown != nil {
		t.Shake.CooldownS = *p.ShakeCooldown
	}
	if t.Crash != nil {
		c := *t.Crash
		if p.CrashMinG != nil {
			c.MinG = *p.CrashMinG
		}
		if p.CrashCooldown != nil {
			c.CooldownS = *p.CrashCooldown
		}
		t.Crash = &c
	}
	if p.NoiseLoud != nil {
		t.Noise.LoudDBFS = *p.NoiseLoud
	}
	if p.NoiseMinLoud != nil {
		t.Noise.MinLoudMS = *p.NoiseMinLoud
	}
	if p.NoiseWindow != nil {
		t.Noise.WindowMS = *p.NoiseWindow
	}
	if p.NoiseCooldown != nil {
		t.Noise.CooldownS = *p.NoiseCooldown
	}
	return t
}

// Resolve applique la cascade et rabote : défauts → pays → pays × application.
//
// ⚠️⚠️ LE SERVEUR NE SERT JAMAIS UNE VALEUR HORS BORNES. Une valeur stockée
// peut l'être — une migration, une saisie directe en base, une borne qu'on a
// resserrée après coup. Dans ce cas on sert le DÉFAUT à sa place et on le
// nomme, pour qu'il parte au journal : un seuil silencieusement remplacé est
// exactement le genre de chose qu'on cherche pendant trois jours le soir où
// une alerte n'est pas partie.
//
// Rend le bloc, et les clés qu'il a fallu remplacer.
func Resolve(app string, country, perApp Patch) (Thresholds, []string) {
	t := Defaults(app).Apply(country).Apply(perApp)
	return clamp(app, t)
}

func clamp(app string, t Thresholds) (Thresholds, []string) {
	var fixed []string
	def := Defaults(app)

	fixF := func(key string, v *float64, fallback float64) {
		if !Bounds[key].holdsF(*v) {
			*v, fixed = fallback, append(fixed, key)
		}
	}
	fixI := func(key string, v *int, fallback int) {
		if !Bounds[key].holdsF(float64(*v)) {
			*v, fixed = fallback, append(fixed, key)
		}
	}

	fixF(KeyShakeMinG, &t.Shake.MinG, def.Shake.MinG)
	fixI(KeyShakeCount, &t.Shake.Count, def.Shake.Count)
	fixI(KeyShakeWindow, &t.Shake.WindowMS, def.Shake.WindowMS)
	fixI(KeyShakeMinGap, &t.Shake.MinGapMS, def.Shake.MinGapMS)
	fixI(KeyShakeCooldown, &t.Shake.CooldownS, def.Shake.CooldownS)
	if t.Crash != nil && def.Crash != nil {
		fixF(KeyCrashMinG, &t.Crash.MinG, def.Crash.MinG)
		fixI(KeyCrashCooldown, &t.Crash.CooldownS, def.Crash.CooldownS)
	}
	fixF(KeyNoiseLoud, &t.Noise.LoudDBFS, def.Noise.LoudDBFS)
	fixI(KeyNoiseMinLoud, &t.Noise.MinLoudMS, def.Noise.MinLoudMS)
	fixI(KeyNoiseWindow, &t.Noise.WindowMS, def.Noise.WindowMS)
	fixI(KeyNoiseCooldown, &t.Noise.CooldownS, def.Noise.CooldownS)

	// ⚠️ LA RÈGLE QUI LIE DEUX CHAMPS, et elle ne se voit dans aucune borne :
	// une fenêtre plus courte que la durée forte qu'elle doit contenir ne peut
	// JAMAIS se remplir. C'est une détection éteinte sans que rien ne le dise —
	// la panne silencieuse, encore. On élargit la fenêtre plutôt que de
	// raccourcir la durée : élargir rend la détection possible, raccourcir la
	// rendrait plus bavarde que ce que quelqu'un a demandé.
	if t.Noise.WindowMS < t.Noise.MinLoudMS {
		t.Noise.WindowMS = Bounds[KeyNoiseWindow].clampI(t.Noise.MinLoudMS)
		fixed = append(fixed, KeyNoiseWindow)
	}
	return t, fixed
}

// Check refuse une surcharge hors bornes, et rend la clé fautive.
//
// ⚠️ C'EST LA CONSOLE QUI APPELLE ÇA, et c'est la différence entre les deux
// mécanismes : le rabotage protège le SERVICE (on sert toujours quelque chose
// de sensé), le refus protège la PERSONNE qui saisit (elle apprend qu'elle
// s'est trompée). Un rabotage silencieux à la saisie aurait laissé quelqu'un
// taper 20 g, voir « enregistré », et croire que la détection de choc est
// désormais très tolérante — alors qu'elle serait à 16.
func Check(p Patch) (string, bool) {
	for _, c := range []struct {
		key string
		val *float64
	}{
		{KeyShakeMinG, p.ShakeMinG},
		{KeyCrashMinG, p.CrashMinG},
		{KeyNoiseLoud, p.NoiseLoud},
	} {
		if c.val != nil && !Bounds[c.key].holdsF(*c.val) {
			return c.key, false
		}
	}
	for _, c := range []struct {
		key string
		val *int
	}{
		{KeyShakeCount, p.ShakeCount},
		{KeyShakeWindow, p.ShakeWindow},
		{KeyShakeMinGap, p.ShakeMinGap},
		{KeyShakeCooldown, p.ShakeCooldown},
		{KeyCrashCooldown, p.CrashCooldown},
		{KeyNoiseMinLoud, p.NoiseMinLoud},
		{KeyNoiseWindow, p.NoiseWindow},
		{KeyNoiseCooldown, p.NoiseCooldown},
	} {
		if c.val != nil && !Bounds[c.key].holdsF(float64(*c.val)) {
			return c.key, false
		}
	}
	return "", true
}

// Describe nomme une borne en clair — « entre 1.8 et 5 ». Sert les refus.
func Describe(key string) string {
	b := Bounds[key]
	return fmt.Sprintf("%g – %g", b.Min, b.Max)
}

// CheckPair refuse la règle qui LIE deux champs, sur les valeurs EFFECTIVES.
//
// ⚠️⚠️ ELLE NE PEUT PAS SE VÉRIFIER SUR LA SURCHARGE SEULE, et c'est le piège.
// Quelqu'un qui ne règle QUE `min_loud_ms` à 2 000 ms, en laissant la fenêtre
// à son défaut de 1 500, écrit une détection qui ne peut jamais se déclencher —
// et sa saisie, prise isolément, est parfaitement dans les bornes. Il faut
// résoudre d'abord, vérifier ensuite.
func CheckPair(t Thresholds) (string, bool) {
	if t.Noise.WindowMS < t.Noise.MinLoudMS {
		return KeyNoiseWindow, false
	}
	return "", true
}
