package chat

// LA MODÉRATION DU CHAT — automatique d'abord, humaine ensuite.
//
// Deux personnes qui ne se connaissent pas se parlent pendant une course ou une
// livraison. La plupart du temps c'est « je suis au portail bleu ». Parfois
// c'est une insulte, une menace, ou pire — et jusqu'ici rien ne le voyait, rien
// ne le signalait, et personne ne pouvait le relire.
//
// ⚠️⚠️ LE PIÈGE DE CE GENRE DE FILTRE, ET IL A UN NOM : le « problème de
// Scunthorpe ». Une liste de mots interdits appliquée en SOUS-CHAÎNE censure la
// ville anglaise de Scunthorpe, parce qu'un gros mot s'y cache au milieu. En
// français c'est pire : « con » est dans *second*, *contact*, *connaître*,
// *Conakry* — la capitale du pays que ce contrat vise. Un filtre en
// sous-chaîne bloquerait un livreur qui écrit « je suis à Conakry ».
//
// D'où la règle qui gouverne ce fichier : **ON NE COMPARE QUE DES MOTS
// ENTIERS.** Jamais une sous-chaîne, jamais un `strings.Contains`.
//
// ⚠️ ET LA NORMALISATION NE SERT PAS À ÊTRE MALIN, elle sert à ne pas être
// contourné par un accent. « enculé », « encule », « ENCULÉ », « e n c u l é »
// et « encul3 » sont le même mot pour celui qui le reçoit ; ils doivent l'être
// pour le filtre. Ce qui ne se normalise pas — l'ironie, le sous-entendu, la
// menace polie — n'est PAS l'affaire de l'automate : c'est celle de l'humain
// qui lit la file (voir `Flagged`).

import (
	"strings"
	"unicode"
)

// Les trois verdicts de l'automate.
const (
	// VerdictPass : rien à signaler. Le message part.
	VerdictPass = "pass"
	// VerdictFlag : le message PART QUAND MÊME, et il est signalé.
	//
	// ⚠️⚠️ C'EST LE VERDICT PAR DÉFAUT D'UNE INSULTE, ET C'EST DÉLIBÉRÉ. Un
	// chauffeur et un passager qui s'énervent sur un prix s'insultent parfois ;
	// bloquer le message ne calme personne et fait croire à une panne du
	// réseau — on le renvoie trois fois. Le laisser passer et le POSER DANS UNE
	// FILE permet à un humain de voir le motif réel, qui est presque toujours
	// ailleurs (un retard, une adresse fausse, un prix qui a bougé).
	VerdictFlag = "flag"
	// VerdictBlock : le message NE PART PAS.
	//
	// ⚠️ RÉSERVÉ À CE QUI NE SE DISCUTE PAS : les menaces et les insultes
	// discriminatoires. Pour tout le reste, bloquer coûte plus que signaler.
	VerdictBlock = "block"
)

// Severity d'un terme du lexique — ce que l'automate en fait.
type Severity string

const (
	// SeverityInsult : grossier. Passe, signalé.
	SeverityInsult Severity = "insult"
	// SeverityThreat : menace ou haine. Ne passe pas.
	SeverityThreat Severity = "threat"
)

// Verdict est ce que l'automate rend sur un message.
type Verdict struct {
	// Verdict : `pass`, `flag` ou `block`.
	Verdict string `json:"verdict"`
	// Terms : LES RÈGLES QUI ONT DÉCLENCHÉ, sous leur forme normalisée.
	//
	// ⚠️⚠️ CE N'EST PAS LE TEXTE TEL QU'IL A ÉTÉ ÉCRIT, et l'écran de
	// modération doit le dire. On y lit `conard`, `kil`, `salope` — des formes
	// repliées, pas des fautes d'orthographe du lexique. Le message, lui, est
	// juste à côté dans la file : le modérateur lit ce qui a été écrit, et
	// cette liste lui dit QUELLE RÈGLE a parlé.
	//
	// C'est ce dont on a besoin pour corriger un lexique trop large : si
	// `conard` revient sur dix messages anodins, c'est la règle qu'il faut
	// retirer, et il faut pouvoir la nommer.
	//
	// ⚠️ SERVI AU MODÉRATEUR, JAMAIS À L'AUTEUR. Dire « le mot X est interdit »
	// apprend à écrire « X. » ou « XX ».
	Terms []string `json:"terms,omitempty"`
	// Severity : la plus grave rencontrée.
	Severity Severity `json:"severity,omitempty"`
}

// Blocked dit que le message ne doit pas partir.
func (v Verdict) Blocked() bool { return v.Verdict == VerdictBlock }

// Flagged dit que le message part mais doit être relu.
func (v Verdict) Flagged() bool { return v.Verdict == VerdictFlag }

// Lexicon est la liste des termes surveillés, par langue.
//
// ⚠️ UNE CARTE DE MOT → GRAVITÉ, et les mots y sont déjà NORMALISÉS. Normaliser
// à chaque comparaison coûterait un passage par message et par terme ; le
// lexique se construit une fois.
type Lexicon map[string]Severity

// Check rend le verdict de l'automate sur un corps de message.
//
// ⚠️ MOTS ENTIERS UNIQUEMENT — voir l'en-tête du fichier. C'est la ligne qui
// empêche de censurer « Conakry ».
func Check(body string, lex Lexicon) Verdict {
	if len(lex) == 0 {
		return Verdict{Verdict: VerdictPass}
	}
	out := Verdict{Verdict: VerdictPass}
	seen := map[string]bool{}
	for _, w := range words(body) {
		sev, hit := lex[w]
		if !hit || seen[w] {
			continue
		}
		seen[w] = true
		out.Terms = append(out.Terms, w)
		// ⚠️ LA PLUS GRAVE L'EMPORTE, et une fois bloqué on reste bloqué : un
		// message qui contient une menace ET une insulte est une menace.
		if sev == SeverityThreat {
			out.Severity, out.Verdict = SeverityThreat, VerdictBlock
			continue
		}
		if out.Severity != SeverityThreat {
			out.Severity, out.Verdict = SeverityInsult, VerdictFlag
		}
	}
	return out
}

// words découpe un corps en mots NORMALISÉS.
//
// ⚠️ LE DÉCOUPAGE SE FAIT SUR TOUT CE QUI N'EST PAS UNE LETTRE OU UN CHIFFRE,
// apostrophes comprises : « t'es » donne « t » et « es », et c'est voulu — la
// liste porte des mots, pas des contractions.
func words(body string) []string {
	norm := Normalise(body)
	return strings.FieldsFunc(norm, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Normalise met un texte sous la forme où les mots se comparent.
//
// Quatre passes, et chacune ferme un contournement observé :
//
//  1. **minuscules** — « ENCULÉ » ;
//  2. **accents retirés** — « enculé » / « encule » sont le même mot pour qui
//     le reçoit ;
//  3. **chiffres qui imitent des lettres** — « encul3 », « s4lope » ;
//  4. **lettres répétées ramenées à UNE** — « salooooope », « sallope ».
//
// ⚠️ ON S'ARRÊTE LÀ, ET C'EST UNE DÉCISION. Retirer les espaces internes
// attraperait « e n c u l é » mais collerait aussi « ta gueule » en « tagueule »
// … et surtout transformerait « rue du con grès » en un seul mot. Un filtre qui
// gagne un contournement rare en créant un faux positif fréquent a perdu.
func Normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	var last rune
	var run int
	for _, r := range s {
		r = fold(r)
		if r == last {
			// ⚠️⚠️ UNE SEULE, ET C'EST UN ARBITRAGE QUI S'EST IMPOSÉ À L'USAGE.
			//
			// La règle gardait d'abord DEUX lettres, pour préserver « bonne »,
			// « all », « messe ». Elle ratait alors le contournement le plus
			// courant qui soit : « salooooope » donnait « saloope », qui ne
			// correspond à aucun terme du lexique. Un filtre que trois touches
			// de clavier contournent ne filtre rien.
			//
			// En ramenant à une seule lettre, « bonne » devient « bone » — et
			// ça ne coûte rien, parce que **le lexique passe par la MÊME
			// fonction** : les deux côtés se déforment pareil, donc ils se
			// retrouvent. Le seul risque réel serait qu'un mot ordinaire
			// s'effondre exactement sur un terme interdit ; c'est pour ce
			// cas-là qu'un terme se retire depuis la console.
			run++
			continue
		}
		run = 0
		last = r
		b.WriteRune(r)
	}
	return b.String()
}

// fold ramène une lettre accentuée à sa forme nue, et un chiffre qui imite une
// lettre à cette lettre.
//
// ⚠️ UNE TABLE EXPLICITE PLUTÔT QUE `unicode.Mn` ET UNE NORMALISATION NFD :
// cette liste couvre le français et l'anglais, se lit en entier, et n'ajoute
// aucune dépendance. Le jour où une langue de plus entre au lexique, c'est ici
// qu'on le voit — pas dans une bibliothèque.
func fold(r rune) rune {
	switch r {
	case 'à', 'á', 'â', 'ä', 'ã', 'å', '4', '@':
		return 'a'
	case 'ç':
		return 'c'
	case 'è', 'é', 'ê', 'ë', '3':
		return 'e'
	case 'ì', 'í', 'î', 'ï', '1', '!':
		return 'i'
	case 'ñ':
		return 'n'
	case 'ò', 'ó', 'ô', 'ö', 'õ', '0':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	case 'ý', 'ÿ':
		return 'y'
	case '5', '$':
		return 's'
	case '7':
		return 't'
	}
	return r
}
