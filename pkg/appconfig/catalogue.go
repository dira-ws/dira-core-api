// Package appconfig porte LES RÉGLAGES QUI DISENT CE QU'UNE APPLICATION VOIT.
//
// Dira Lite est une seconde application passager, pour les zones de faible
// alphabétisation. Le propriétaire veut pouvoir en RETIRER des fonctions et
// des valeurs qui restent disponibles dans l'application normale — sans
// nouvelle version des applications, comme les réglages par pays aujourd'hui.
//
// ⚠️⚠️ DEUX DIMENSIONS, PAYS *ET* ÉDITION, et jamais l'une sans l'autre. Un
// réglage « pour Lite » qui ignorerait le pays retirerait la fonction au Tchad
// ET au Togo ; un réglage « pour le Tchad » qui ignorerait l'édition la
// retirerait aussi à l'application normale. Les deux questions se posent
// ensemble parce qu'on y répond ensemble : « que voit Lite au Tchad ? »
//
// ⚠️ LE DÉFAUT EST « DISPONIBLE PARTOUT ». Rien ne change pour personne tant
// que quelqu'un n'a pas explicitement restreint. Un registre vide se comporte
// exactement comme la plateforme d'avant — c'est la seule façon d'ajouter
// cette couche sans réviser chaque pays.
//
// ⚠️ ET LE SOCLE NE DEMANDE RIEN AUX VERTICALES. Il ne sait pas quels modes de
// véhicule existent, ni quels moyens de paiement un pays sert : ce sont leurs
// catalogues. Il tient donc des LISTES BLANCHES — « Lite n'a droit qu'à
// ceux-ci » — et chaque verticale croise la sienne avec. Composer ici en les
// interrogeant aurait créé la dépendance que `internal/callback` interdit en
// toutes lettres.
package appconfig

import "sort"

// Feature nomme une fonction qu'on peut retirer d'une édition ou d'un pays.
//
// ⚠️⚠️ DÉCLARÉES, JAMAIS LIBRES, et c'est la décision centrale de ce fichier.
// Une carte de chaînes libres laisse écrire `scheduled_ride` au singulier : le
// réglage s'enregistre, personne ne le lit, la fonction reste visible dans
// Lite, et on cherche la panne dans l'application. Une clé inconnue est donc
// REFUSÉE à l'écriture (voir `Known`).
type Feature struct {
	Key string `json:"key"`
	// Label : ce que la console affiche. En français, la langue de
	// l'exploitation.
	Label string `json:"label"`
	// Hiding dit CE QUE RETIRER CETTE FONCTION FAIT VOIR À LA PERSONNE. C'est
	// la seule phrase qui compte sur l'écran de réglage : « désactiver
	// scheduled_rides » ne dit rien, « le client ne peut plus programmer une
	// course pour plus tard » dit tout.
	Hiding string `json:"hiding"`
	// Owner : le service qui sert cette fonction. Informatif, pour que la
	// console groupe l'écran et qu'on sache qui applique le filtre.
	Owner string `json:"owner"`
}

// Les services qui servent des fonctions.
const (
	OwnerCore      = "core"
	OwnerVTC       = "vtc"
	OwnerFood      = "food"
	OwnerAnalytics = "analytics"
)

// Features est le catalogue des fonctions qu'on peut couper.
//
// ⚠️ UNE FONCTION N'ENTRE ICI QUE SI LA COUPER A UN SENS POUR LA PERSONNE. On
// ne met pas « les courses » ni « la connexion » : une application passager
// sans course n'est pas une édition réduite, c'est une application cassée.
// Tout ce qui est listé ici peut disparaître d'un menu sans rendre le reste
// incohérent.
var Features = []Feature{
	{FeatureScheduledRides, "Courses programmées", "Le client ne peut plus commander une course pour plus tard — seulement tout de suite.", OwnerVTC},
	{FeatureRental, "Location", "Le mode location disparaît du choix des véhicules.", OwnerVTC},
	{FeaturePool, "Course partagée", "Le client ne peut plus partager sa course avec un autre passager.", OwnerVTC},
	{FeatureMultiCar, "Plusieurs voitures", "Le client ne peut plus demander deux voitures ou plus en une fois.", OwnerVTC},
	{FeatureTripShare, "Partage de trajet", "Le bouton « partager mon trajet » disparaît : plus de lien à envoyer à un proche.", OwnerVTC},
	{FeatureSubscriptions, "Abonnements de courses", "Le client ne peut plus souscrire d'abonnement.", OwnerVTC},
	{FeaturePromoCodes, "Codes promo", "Le champ « code promo » disparaît de la commande.", OwnerCore},
	{FeatureChallenges, "Objectifs et bonus", "L'écran des objectifs à atteindre disparaît.", OwnerCore},
	{FeatureRaffle, "Tombola", "L'écran de la tombola disparaît.", OwnerCore},
	{FeatureBanners, "Bannières et publicités", "Les emplacements publicitaires ne sont plus servis — les formats chargés d'images.", OwnerCore},
	{FeatureFeed, "Fil des plats (carrousel)", "Le carrousel de plats et de vidéos disparaît de l'accueil.", OwnerFood},
	{FeatureAssistant, "Assistant", "La boîte de dialogue de l'assistant disparaît.", OwnerAnalytics},
	{FeatureAssistantLive, "Assistant en direct", "Le micro en continu disparaît : la personne garde l'assistant tour par tour (un vocal, une réponse).", OwnerAnalytics},
	{FeatureAssistantAutoOrder, "Commande automatique de l'assistant", "Un message vocal ne déclenche plus de commande tout seul : le client confirme toujours.", OwnerAnalytics},
	{FeatureVoiceMessages, "Messages vocaux", "Le bouton micro disparaît : le client écrit.", OwnerAnalytics},
	{FeatureWallet, "Portefeuille", "Le solde et la recharge disparaissent ; restent les espèces et le mobile money.", OwnerCore},
}

// Les clés du catalogue.
const (
	FeatureScheduledRides = "scheduled_rides"
	FeatureRental         = "rental"
	FeaturePool           = "pool"
	FeatureMultiCar       = "multi_car"
	FeatureTripShare      = "trip_share"
	FeatureSubscriptions  = "subscriptions"
	FeaturePromoCodes     = "promo_codes"
	FeatureChallenges     = "challenges"
	FeatureRaffle         = "raffle"
	FeatureBanners        = "banners"
	FeatureFeed           = "feed"
	FeatureAssistant      = "assistant"
	// ⚠️ CELUI-LÀ S'ALLUME PAR LA PRÉSENCE D'UN BLOC `live`, pays par pays —
	// voir `live.go`. La règle de catalogue ne fait que le RETIRER ensuite
	// d'une édition.
	FeatureAssistantLive      = "assistant_live"
	FeatureAssistantAutoOrder = "assistant_auto_order"
	FeatureVoiceMessages      = "voice_messages"
	FeatureWallet             = "wallet"
)

// Known dit si cette clé nomme une fonction du catalogue.
//
// ⚠️ C'EST CE QUI REFUSE UNE FAUTE DE FRAPPE À L'ÉCRITURE. Sans elle,
// `scheduled_ride` au singulier s'enregistrait, personne ne le lisait, et la
// fonction restait visible dans Lite — on aurait cherché la panne dans
// l'application.
func Known(key string) bool {
	for _, f := range Features {
		if f.Key == key {
			return true
		}
	}
	return false
}

// Lists nomme les listes dont on peut restreindre le contenu.
//
// ⚠️ DES LISTES BLANCHES, PAS DES CATALOGUES. Le socle ne sait pas quels modes
// de véhicule existent — c'est le catalogue des courses — ni quels moyens de
// paiement un pays sert. Il dit seulement « Lite n'a droit qu'à ceux-ci », et
// la verticale croise avec ce qu'elle a. Une liste blanche ABSENTE veut dire
// « tout ce que la verticale sert », jamais « rien ».
var Lists = []Feature{
	{ListPaymentMethods, "Moyens de paiement", "Lite ne propose que les moyens cochés ici — par exemple espèces et mobile money.", OwnerCore},
	{ListVehicleModes, "Modes de véhicule", "Lite ne propose que les modes cochés ici — par exemple moto, éco, confort.", OwnerVTC},
	{ListVehicleClasses, "Classes de véhicule", "Lite ne propose que les classes cochées ici.", OwnerVTC},
	{ListAdPlacements, "Emplacements publicitaires", "Lite ne reçoit que les emplacements cochés ici.", OwnerCore},
}

// Les clés des listes.
const (
	ListPaymentMethods = "payment_methods"
	ListVehicleModes   = "vehicle_modes"
	ListVehicleClasses = "vehicle_classes"
	ListAdPlacements   = "ad_placements"
)

// KnownList dit si cette clé nomme une liste du catalogue.
func KnownList(key string) bool {
	for _, l := range Lists {
		if l.Key == key {
			return true
		}
	}
	return false
}

// FeatureKeys rend les clés des fonctions, triées — pour une réponse stable.
//
// ⚠️ TRIÉES, parce qu'une carte Go se parcourt dans un ordre ALÉATOIRE : une
// réponse dont l'ordre change à chaque appel casse la mise en cache côté
// application et rend illisible toute comparaison de deux réponses.
func FeatureKeys() []string {
	out := make([]string, 0, len(Features))
	for _, f := range Features {
		out = append(out, f.Key)
	}
	sort.Strings(out)
	return out
}
