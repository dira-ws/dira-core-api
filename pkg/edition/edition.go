// Package edition porte la COUCHE ÉDITION de la plateforme : laquelle des
// applications passager parle au serveur.
//
// Dira Lite est une seconde application passager, pour les zones de faible
// alphabétisation (le Tchad d'abord). Deux applications distinctes sur le
// téléphone (`llc.dira.client` et `llc.dira.lite`), MÊMES comptes, MÊME rôle :
// rien ne distinguait donc leurs appels. Le serveur en a besoin pour trois
// choses — servir à Lite une configuration réduite, compter son usage à part,
// et pousser une notification vers l'application qui a commandé.
//
// ⚠️⚠️ DÉCLARATIVE, JAMAIS AUTORISANTE. C'est la règle la plus importante de
// ce paquet, et elle vient de la demande elle-même : l'édition est ce que
// l'application DIT d'elle-même, et n'importe qui peut l'écrire. Elle sert à
// ADAPTER (une liste plus courte, un texte plus simple) et à COMPTER. Elle ne
// doit jamais décider d'un droit : « cette action est réservée à Lite » se
// contournerait avec un en-tête. Le jour où une règle de sécurité devra en
// dépendre, il faudra une clé d'API propre à Lite, pas cet en-tête.
//
// ⚠️ ET SON DÉFAUT EST `full`, TOUJOURS. Les applications déjà installées
// n'envoient rien ; les lire comme une édition inconnue, ou refuser, aurait
// cassé tout le parc existant au déploiement. Une valeur inconnue retombe
// aussi sur `full` : une faute de frappe dans une version future doit dégrader
// vers l'application complète, jamais vers une application amputée.
package edition

import (
	"context"
	"strings"
)

// Header est l'en-tête par lequel une application dit quelle édition elle est.
//
// Le même chemin que `X-Dira-Country` : posé par l'application sur CHAQUE
// requête, lu par un intergiciel, et republié dans la réponse pour que
// l'application voie ce que le serveur a retenu.
const Header = "X-Dira-Edition"

// QueryParam remplace l'en-tête là où l'on ne peut pas en poser : l'ouverture
// d'un WebSocket depuis un navigateur, un lien.
const QueryParam = "edition"

// VersionHeader et PlatformHeader : de quelle VERSION et de quel système
// l'appel vient.
//
// ⚠️ GARDÉS AVEC L'ÉDITION, et pas ailleurs, parce qu'ils répondent à la même
// question — « qui appelle ? » — et servent au même usage : un réglage qui
// vise « Lite à partir de telle version », et un décompte d'usage qui
// distingue Android d'iOS. Sans la version, retirer une fonction de Lite la
// retire aussi aux versions qui ne savent pas s'en passer.
const (
	VersionHeader  = "X-Dira-App-Version"
	PlatformHeader = "X-Dira-Platform"
)

// Les éditions connues.
const (
	// Full est l'application passager ordinaire — et le DÉFAUT de tout appel
	// qui ne dit rien.
	Full = "full"
	// Lite est l'application des zones de faible alphabétisation : tout y est
	// écoutable, et sa configuration est volontairement plus courte.
	Lite = "lite"
)

// Normalize rend une édition connue, ou `full`.
//
// ⚠️ JAMAIS D'ERREUR, JAMAIS DE REFUS. Une édition est une information, pas
// une autorisation : un en-tête absent, mal écrit, ou venu d'une version
// future doit laisser passer l'appel et retomber sur l'application complète.
// Refuser ici aurait fait d'une faute de frappe une panne.
func Normalize(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case Lite:
		return Lite
	default:
		return Full
	}
}

// Known dit si cette chaîne nomme une édition que nous servons.
//
// ⚠️ DISTINCTE DE `Normalize`, et les deux sont utiles : `Normalize` sert au
// chemin d'un appel (où l'on ne refuse jamais), `Known` sert à l'ÉCRITURE d'un
// réglage depuis la console (où l'on refuse, parce qu'un réglage posé sur une
// édition inventée ne s'appliquerait jamais et personne ne saurait pourquoi).
func Known(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	return s == Full || s == Lite
}

// All liste les éditions, dans l'ordre où la console les présente.
func All() []string { return []string{Full, Lite} }

type contextKey struct{}

type carried struct {
	edition  string
	version  string
	platform string
}

// WithEdition inscrit l'édition de l'appel dans le contexte.
func WithEdition(ctx context.Context, ed, version, platform string) context.Context {
	return context.WithValue(ctx, contextKey{}, carried{
		edition:  Normalize(ed),
		version:  strings.TrimSpace(version),
		platform: strings.ToLower(strings.TrimSpace(platform)),
	})
}

// FromContext rend l'édition de l'appel — `full` à défaut.
//
// ⚠️ `full` MÊME SANS INTERGICIEL : un service qui n'a pas encore branché la
// couche édition sert l'application complète, c'est-à-dire se comporte comme
// avant. Rendre `""` aurait obligé chaque lecteur à un garde-fou, et l'un
// d'eux l'aurait oublié — en servant à tout le monde une configuration de
// Lite.
func FromContext(ctx context.Context) string {
	if c, ok := ctx.Value(contextKey{}).(carried); ok && c.edition != "" {
		return c.edition
	}
	return Full
}

// IsLite est le raccourci lisible des points d'appel.
func IsLite(ctx context.Context) bool { return FromContext(ctx) == Lite }

// AppFromContext rend la version et la plateforme déclarées, vides à défaut.
func AppFromContext(ctx context.Context) (version, platform string) {
	if c, ok := ctx.Value(contextKey{}).(carried); ok {
		return c.version, c.platform
	}
	return "", ""
}
