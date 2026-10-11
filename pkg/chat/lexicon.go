package chat

// LE LEXIQUE EMBARQUÉ — français et anglais.
//
// ⚠️⚠️ AUCUNE LISTE COMPILÉE N'EST JAMAIS LA BONNE, et c'est la première chose
// à dire. Les mots qui blessent changent d'un pays à l'autre, d'une langue à
// l'autre, et d'une année à l'autre ; un lexique figé dans un binaire vieillit
// mal et se corrige par un déploiement. Celui-ci est donc un POINT DE DÉPART,
// pas une référence : l'exploitation ajoute et retire depuis la console
// (`PUT /admin/chat/lexicon`), et ses termes s'ajoutent à ceux-ci.
//
// ⚠️ IL EST EMBARQUÉ POUR QUE LA MODÉRATION MARCHE AU PREMIER DÉMARRAGE. Un
// filtre qui attend qu'on le remplisse est un filtre éteint, et personne ne
// s'en aperçoit — exactement la panne silencieuse qu'on cherche à éviter
// ailleurs dans cette plateforme.
//
// ⚠️ DEUX GRAVITÉS, ET LA FRONTIÈRE EST UNE DÉCISION DE PRODUIT :
//
//   - `insult` : grossier, blessant. **Le message part**, et il est signalé.
//     Deux personnes qui s'énervent sur un prix s'insultent ; les bloquer ne
//     calme personne et fait croire à une panne de réseau.
//   - `threat` : menace sur la personne, ou injure discriminatoire. **Le
//     message ne part pas.** Ce qui est en jeu n'est plus le confort de la
//     conversation.
//
// ⚠️⚠️ LES TERMES PASSENT PAR `Normalise`, COMME LES MESSAGES — et ce n'est
// pas un détail d'implémentation, c'est la seule façon que les deux côtés se
// retrouvent.
//
// Ils étaient d'abord écrits « déjà normalisés », à la main. Le jour où la
// règle des lettres répétées est passée de deux à une, `kill` est resté `kill`
// dans la liste pendant que « kill » devenait `kil` dans les messages : la
// menace la plus évidente de tout le lexique ne déclenchait plus rien, et rien
// ne le signalait. Une liste qu'il faut tenir à jour à la main en suivant les
// règles d'une fonction est une liste qui se désynchronise.

// baseLexicon est le point de départ, par langue.
//
// La liste est volontairement COURTE et sans ambiguïté. Un lexique long se
// trompe : chaque terme discutable ajouté est un faux positif qui apprend aux
// gens que la modération est bête — et le jour où elle a raison, personne ne
// la croit.
var baseLexicon = map[string]Lexicon{
	"fr": {
		// Insultes — passent, signalées.
		"connard": SeverityInsult, "connasse": SeverityInsult,
		"encule": SeverityInsult, "enfoire": SeverityInsult,
		"salope": SeverityInsult, "salaud": SeverityInsult,
		"pute": SeverityInsult, "putain": SeverityInsult,
		"merde": SeverityInsult, "batard": SeverityInsult,
		"abruti": SeverityInsult, "cretin": SeverityInsult,
		"imbecile": SeverityInsult, "debile": SeverityInsult,
		// ⚠️ « con » EST DANS LA LISTE ET C'EST SANS DANGER, parce qu'on ne
		// compare que des MOTS ENTIERS : *second*, *contact*, *Conakry* ne le
		// déclenchent pas. C'est précisément ce que prouve le test du même nom.
		"con": SeverityInsult,
		// Menaces — ne passent pas.
		"tuer": SeverityThreat, "egorger": SeverityThreat,
		"frapper": SeverityThreat, "violer": SeverityThreat,
		"bruler": SeverityThreat, "crever": SeverityThreat,
	},
	"en": {
		"asshole": SeverityInsult, "bastard": SeverityInsult,
		"bitch": SeverityInsult, "bullshit": SeverityInsult,
		"dickhead": SeverityInsult, "fuck": SeverityInsult,
		"fucker": SeverityInsult, "idiot": SeverityInsult,
		"moron": SeverityInsult, "shit": SeverityInsult,
		"slut": SeverityInsult, "whore": SeverityInsult,
		"kill": SeverityThreat, "rape": SeverityThreat,
		"stab": SeverityThreat, "burn": SeverityThreat,
		"murder": SeverityThreat,
	},
}

// BaseLexicon rend le lexique embarqué des deux langues, fusionné.
//
// ⚠️ LES DEUX LANGUES SONT APPLIQUÉES ENSEMBLE, sans tenir compte de la langue
// du message. Un chauffeur togolais écrit en français, un passager ghanéen en
// anglais, et la même conversation mêle les deux — demander « quelle langue
// est-ce ? » avant de filtrer ajouterait une devinette là où l'union des deux
// listes suffit. Deux langues n'ont aucun mot commun dans ces listes-ci ; le
// jour où ce ne sera plus vrai, ce commentaire est l'endroit où le relire.
func BaseLexicon() Lexicon {
	out := make(Lexicon, 64)
	for _, l := range baseLexicon {
		for term, sev := range l {
			// ⚠️ NORMALISÉ ICI, pas à la main dans la liste : voir l'en-tête.
			if n := Normalise(term); n != "" {
				out[n] = sev
			}
		}
	}
	return out
}

// MergeLexicon ajoute les termes de l'exploitation à une base.
//
// ⚠️ LES TERMES AJOUTÉS SONT NORMALISÉS À L'ENTRÉE, parce que personne ne
// saisit « encule » dans un champ de console : on tape « enculé », avec son
// accent, et on attend que ça marche.
//
// ⚠️ ET UN TERME PEUT ÊTRE RETIRÉ (gravité vide). C'est la soupape du lexique
// embarqué : le jour où un mot de la liste est un nom de famille courant dans
// un pays, l'exploitation doit pouvoir l'éteindre sans attendre un
// déploiement.
func MergeLexicon(base Lexicon, extra map[string]Severity) Lexicon {
	out := make(Lexicon, len(base)+len(extra))
	for t, s := range base {
		out[t] = s
	}
	for t, s := range extra {
		n := Normalise(t)
		if n == "" {
			continue
		}
		if s == "" {
			delete(out, n)
			continue
		}
		out[n] = s
	}
	return out
}
