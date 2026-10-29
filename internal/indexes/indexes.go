// Package indexes déclare les index MongoDB du socle.
//
// La LISTE vit ici, le mécanisme de pose vit dans `pkg/db` : chaque service a
// ses collections, et aucun n'a à réimplémenter la pose, le repli ni le compte
// rendu.
//
// ⚠️ Ces déclarations DOUBLENT `indexes/*.mongodb.js`. Les fichiers .js
// documentent le schéma ; ce sont ces lignes-ci qui sont réellement appliquées,
// au démarrage. Un index qu'il faut penser à poser à la main finit toujours par
// manquer quelque part.
package indexes

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var applicationIndexes = []db.Index{
	// --- user ---
	{Collection: "users", Keys: db.K("phone", 1), Unique: true},
	{Collection: "users", Keys: db.K("role", 1)},
	// La borne PAYS des listes de la console — voir `pkg/country`. Le pays
	// en tête, le rôle ensuite : c'est l'ordre des filtres de `/admin/users`
	// et des audiences de campagne (« les clients du Togo »).
	{Collection: "users", Keys: db.K("country", 1, "role", 1, "_id", 1)},
	// L'email est facultatif : unique ET sparse, sinon deux comptes sans
	// email entreraient en collision sur la valeur nulle.
	// Unicité de l'e-mail SEULEMENT parmi ceux qui en ont un. On s'inscrit par
	// téléphone : la plupart des comptes n'ont pas d'e-mail, et `sparse` ne les
	// écarte que si le champ est absent — pas s'il vaut la chaîne vide. Deux
	// comptes sans e-mail suffisaient donc à empêcher l'index de se construire,
	// et le service démarrait sans lui.
	//
	// `$gt: ""` sélectionne les chaînes non vides : `$ne` n'est pas admis dans
	// une expression de filtre partiel.
	{
		Collection:    "users",
		Keys:          db.K("email", 1),
		Unique:        true,
		PartialFilter: bson.D{{Key: "email", Value: bson.D{{Key: "$gt", Value: ""}}}},
	},
	// --- LES CODES PROMO (internal/promocode) ---
	//
	// ⚠️ LE CODE EST UNIQUE SUR TOUTE LA PLATEFORME, et c'est cet index qui le
	// garantit — pas une vérification applicative. Lire « ce code existe-t-il ? »
	// puis écrire laisse une fenêtre entre les deux : deux créations
	// simultanées passeraient le test, et la seconde échouerait sur l'écran de
	// quelqu'un. C'est aussi ce qui permet de TIRER un code en réessayant sur
	// collision plutôt qu'en interrogeant d'abord.
	{Collection: "promo_codes", Keys: db.K("code", 1), Unique: true},
	// Les codes d'un influenceur, et la liste de la console par pays.
	{Collection: "promo_codes", Keys: db.K("owner_id", 1, "_id", -1), Sparse: true},
	{Collection: "promo_codes", Keys: db.K("country", 1, "kind", 1, "_id", -1)},
	// ⚠️ UN USAGE PAR (CODE, RÉFÉRENCE), et c'est ce qui rend l'engagement
	// IDEMPOTENT : un rappel de paiement rejoué, un redémarrage au mauvais
	// moment ou un double clic ne consomment pas l'enveloppe deux fois. Sans
	// cette contrainte, un budget se vide de moitié sur un incident réseau.
	{Collection: "promo_code_uses", Keys: db.K("promo_id", 1, "ref_id", 1), Unique: true},
	// La limite PAR PERSONNE se lit ici — c'est elle qui protège de l'abus,
	// pas le budget.
	{Collection: "promo_code_uses", Keys: db.K("user_id", 1, "state", 1)},
	// Le suivi d'un code, et la clôture par référence.
	{Collection: "promo_code_uses", Keys: db.K("promo_id", 1, "_id", -1)},
	{Collection: "promo_code_uses", Keys: db.K("ref_id", 1)},
	// LES INFLUENCEURS : une fiche par compte, un pseudonyme unique.
	{Collection: "influencers", Keys: db.K("user_id", 1), Unique: true},
	{Collection: "influencers", Keys: db.K("handle", 1), Unique: true},
	{Collection: "influencers", Keys: db.K("country", 1, "_id", -1)},

	// LES COMPTES À EFFACER — le balayage de la suppression (voir
	// `internal/user/erasure.go`). Il tourne toutes les heures sur la
	// collection la plus lue de la plateforme ; sans index, il la parcourt
	// entièrement vingt-quatre fois par jour pour trouver, le plus souvent,
	// zéro ligne.
	//
	// ⚠️ PARTIEL SUR `deletion_requested_at`, pas sur `anonymised_at`. Les
	// comptes fermés sont une poignée face à des centaines de milliers de
	// comptes actifs : un index complet aurait indexé toute la base pour
	// servir une requête qui ne regarde qu'elle. Le second critère
	// (`anonymised_at` absent) se filtre après, sur les quelques lignes que
	// l'index rend.
	{
		Collection:    "users",
		Keys:          db.K("deletion_requested_at", 1),
		PartialFilter: bson.D{{Key: "deletion_requested_at", Value: bson.D{{Key: "$exists", Value: true}}}},
	},
	// LA PORTE PAR CODE — un seul code vivant par numéro (voir
	// `internal/user/otp.go`). L'unicité n'est pas décorative : c'est elle
	// qui fait qu'une demande REMPLACE la précédente au lieu d'empiler des
	// codes valides, et donc de multiplier les chances d'en deviner un.
	{Collection: "auth_otp_codes", Keys: db.K("phone", 1), Unique: true},
	// La purge suit la FENÊTRE de cadence, pas l'échéance du code : effacer
	// à l'expiration du code remettrait le compteur horaire à zéro toutes
	// les cinq minutes, et le plafond de demandes ne plafonnerait rien.
	{Collection: "auth_otp_codes", Keys: db.K("purge_at", 1), TTLSeconds: db.TTL(0)},
	{Collection: "refresh_tokens", Keys: db.K("token_hash", 1), Unique: true},
	// Les sessions d'un compte, la plus récemment active en tête : c'est
	// l'ordre exact dans lequel la borne d'appareils les lit pour évincer la
	// plus silencieuse (`internal/user/devices.go`). Sans le `created_at`,
	// chaque connexion triait en mémoire toutes les sessions du compte.
	{Collection: "refresh_tokens", Keys: db.K("user_id", 1, "created_at", -1)},
	{Collection: "refresh_tokens", Keys: db.K("expires_at", 1), TTLSeconds: db.TTL(0)},
	// --- user (carnet d'adresses) ---
	// Le carnet d'un compte, l'adresse par défaut en tête — c'est celle que
	// l'application présélectionne.
	{Collection: "user_addresses", Keys: db.K("user_id", 1, "is_default", -1, "created_at", 1)},

	// --- token ---
	{Collection: "token_wallets", Keys: db.K("owner_id", 1), Unique: true},
	{Collection: "token_wallets", Keys: db.K("country", 1, "_id", -1)},
	{Collection: "token_transactions", Keys: db.K("wallet_id", 1)},
	{Collection: "token_transactions", Keys: db.K("created_at", 1)},
	// La référence d'un mouvement : le COUPLE, pas l'identifiant seul. Deux
	// verticales peuvent porter le même identifiant — deux collections, deux
	// compteurs d'ObjectID, aucune garantie d'unicité entre elles.
	{Collection: "token_transactions", Keys: db.K("ref_kind", 1, "ref_id", 1), Sparse: true},
	// ⚠️ C'EST CET INDEX qui rend un mouvement d'argent rejouable sans danger.
	// La clé est le `_id` du marqueur — unique par construction — et la
	// réservation se fait DANS la transaction qui débite. Sans lui, une réponse
	// perdue et un réessai débitent deux fois le même client.
	//
	// TTL de 30 jours : ces marqueurs ne servent que le temps où un réessai est
	// plausible. Les garder indéfiniment ferait grossir une collection que
	// personne ne lit.
	{Collection: "wallet_operations", Keys: db.K("at", 1), TTLSeconds: db.TTL(30 * 24 * 3600)},

	// --- paiements ---
	{Collection: "payments", Keys: db.K("provider_ref", 1), Unique: true},
	{Collection: "payments", Keys: db.K("user_id", 1)},
	{Collection: "payments", Keys: db.K("status", 1)},
	{Collection: "payments", Keys: db.K("country", 1, "_id", -1)},
	// ⚠️ UNE CLÉ D'IDEMPOTENCE PAR COMPTE, et c'est CET index qui le garantit
	// — pas une relecture applicative. Lire « cette clé existe-t-elle ? » puis
	// écrire laisse une fenêtre entre les deux : deux initiations simultanées
	// de la même intention passeraient le test, et DEUX demandes de paiement
	// partiraient sur le téléphone du client. C'est aussi ce qui permet au
	// perdant de relire le gagnant plutôt que de rendre une erreur.
	//
	// PARTIEL, parce que la plupart des paiements n'ont pas de clé : `sparse`
	// ne les écarterait que si le champ est ABSENT, et deux paiements sans clé
	// suffiraient à empêcher l'index de se construire — le service démarrerait
	// alors sans lui, et le journal serait le seul à le dire. `$gt: ""`
	// sélectionne les chaînes non vides.
	{
		Collection:    "payments",
		Keys:          db.K("user_id", 1, "idempotency_key", 1),
		Unique:        true,
		PartialFilter: bson.D{{Key: "idempotency_key", Value: bson.D{{Key: "$gt", Value: ""}}}},
	},
	// RETROUVER UN PAIEMENT PERDU : `GET /payments?purpose=&ref_id=`, le plus
	// récent d'abord. L'ordre des clés suit l'ordre des filtres, et
	// `created_at` avant `_id` suit `db.NewestFirst`.
	{Collection: "payments", Keys: db.K("user_id", 1, "purpose", 1, "ref_id", 1, "created_at", -1, "_id", -1)},
	// ⚠️ LE REMBOURSEMENT D'UNE COMMANDE CHERCHAIT SANS INDEX. Son commentaire
	// affirmait pourtant que « l'index sur (purpose, ref_id) rend l'écriture
	// atomique » — cet index n'existait pas, et le `FindOneAndUpdate` balayait
	// la collection. L'atomicité, elle, venait du filtre ; la lenteur, de
	// l'index manquant. Le voici.
	{Collection: "payments", Keys: db.K("purpose", 1, "ref_id", 1, "status", 1)},

	// --- notifications ---
	{Collection: "push_devices", Keys: db.K("token", 1), Unique: true},
	{Collection: "push_devices", Keys: db.K("user_id", 1, "disabled_at", 1)},
	{Collection: "message_templates", Keys: db.K("key", 1), Unique: true},
	{Collection: "notifications", Keys: db.K("user_id", 1, "_id", -1)},
	{Collection: "notifications", Keys: db.K("user_id", 1, "read_at", 1)},
	// Les campagnes dues : le tic ne relit que celles qui attendent.
	{Collection: "campaigns", Keys: db.K("status", 1, "send_at", 1)},
	{Collection: "campaigns", Keys: db.K("country", 1, "_id", -1)},

	// --- notes ---
	// UNE note par (commande, cible). L'unicité est portée par l'INDEX et non
	// par une lecture préalable : deux envois simultanés passeraient tous les
	// deux un contrôle applicatif et compteraient double.
	{Collection: "ratings", Keys: db.K("order_id", 1, "target_type", 1, "target_id", 1), Unique: true},
	// La liste des avis d'une cible, du plus récent au plus ancien.
	{Collection: "ratings", Keys: db.K("target_type", 1, "target_id", 1, "_id", -1)},

	// --- flottes privées ---
	//
	// ⚠️ Le NOM est UNIQUE, et c'est ce qui empêche deux exploitants traitant
	// le même contrat d'enregistrer deux fois la même société. Un contrôle
	// applicatif « ce nom existe-t-il ? » laisse passer les deux écritures
	// concurrentes, et la base porte alors deux flottes que rien ne distingue.
	{Collection: "fleets", Keys: db.K("name", 1), Unique: true},
	// La liste de l'administration, filtrée par état.
	{Collection: "fleets", Keys: db.K("status", 1, "_id", -1)},
	{Collection: "fleets", Keys: db.K("country", 1, "_id", -1)},
	// ⚠️⚠️ UNE SEULE FLOTTE INTERNE PAR PAYS — LA FLOTTE DE DIRA. C'est cet
	// index qui le garantit, et non une lecture « existe-t-elle ? » suivie d'une
	// création : deux attributions traitées en même temps le jour de l'ouverture
	// d'un pays passeraient toutes les deux, et la plateforme porterait deux
	// flottes internes. Les véhicules de l'une seraient invisibles depuis
	// l'autre, et personne ne saurait qu'il manque la moitié du parc.
	//
	// ⚠️ LE FILTRE PARTIEL PORTE SUR `internal: true`, ET LE CHAMP EST BIEN
	// ÉCRIT quand il vaut `true` (`bson:"internal,omitempty"`) : c'est la
	// condition pour qu'un index partiel verrouille réellement quelque chose —
	// un filtre qui ne correspond à aucun document indexe le vide et laisse tout
	// passer. Les flottes ORDINAIRES, elles, n'y entrent pas : sans quoi deux
	// sociétés d'un même pays entreraient en collision.
	{
		Collection:    "fleets",
		Keys:          db.K("country", 1),
		Unique:        true,
		PartialFilter: bson.D{{Key: "internal", Value: true}},
	},
	// --- LA FLOTTE INTERNE (internal/ownfleet) : les véhicules de Dira ---
	//
	// ⚠️ LA PLAQUE EST UNIQUE PAR PAYS, et c'est cet index qui le tient. Deux
	// exploitants qui enregistrent la même livraison passeraient tous les deux
	// un contrôle applicatif, et le parc porterait deux fois la même voiture :
	// l'une confiée, l'autre « disponible » pour toujours.
	{Collection: "own_fleet_assets", Keys: db.K("country", 1, "plate", 1), Unique: true},
	{Collection: "own_fleet_assets", Keys: db.K("country", 1, "status", 1, "_id", -1)},
	// Le véhicule d'une personne — la lecture de l'application de l'agent, qui
	// n'est pas bornée par pays (c'est le compte connecté qui désigne).
	{Collection: "own_fleet_assets", Keys: db.K("holder_user_id", 1)},
	// La libération d'un véhicule est conditionnée par l'identifiant de son
	// contrat : c'est le filtre du chemin INVERSE.
	{Collection: "own_fleet_assets", Keys: db.K("contract_id", 1)},
	// « Quelle flotte gère ce compte ? » — la question que pose la connexion
	// d'un gérant. PARTIEL : une flotte enregistrée sur contrat papier n'a pas
	// encore de gérant, et indexer ces absences coûterait sans rien répondre.
	{Collection: "fleets", Keys: db.K("owner_user_id", 1),
		PartialFilter: bson.D{{Key: "owner_user_id", Value: bson.D{{Key: "$exists", Value: true}}}}},

	// --- staff ---
	//
	// ⚠️ UNE fiche par compte. Deux fiches contradictoires décideraient des
	// droits d'une personne selon celle qu'on lit en premier — et un contrôle
	// applicatif « a-t-il déjà une fiche ? » laisse passer deux créations
	// concurrentes.
	{Collection: "staff_members", Keys: db.K("user_id", 1), Unique: true},
	// La liste de l'administration, filtrée par fonction ou par état.
	{Collection: "staff_members", Keys: db.K("function", 1, "_id", -1)},
	{Collection: "staff_members", Keys: db.K("status", 1, "_id", -1)},

	// --- le journal d'audit de TOUTE la plateforme ---
	//
	// Lu du plus récent au plus ancien, filtré par service, par acteur, par
	// action ou par objet. Les verticales y écrivent par la surface de
	// service ; personne d'autre ne le lit que la console.
	//
	// ⚠️ LE PAYS EST EN TÊTE de chacun : toute lecture du journal est bornée
	// par le pays de la requête (`pkg/audit.Search`), donc aucune n'attaque
	// ces index sans lui.
	{Collection: "audit_logs", Keys: db.K("country", 1, "_id", -1)},
	{Collection: "audit_logs", Keys: db.K("country", 1, "service", 1, "_id", -1)},
	{Collection: "audit_logs", Keys: db.K("country", 1, "actor_id", 1, "_id", -1)},
	{Collection: "audit_logs", Keys: db.K("country", 1, "action", 1, "_id", -1)},
	{Collection: "audit_logs", Keys: db.K("country", 1, "resource.id", 1, "_id", -1)},
	{Collection: "audit_logs", Keys: db.K("created_at", -1)},

	// --- LES PANNES (internal/faults) ---
	//
	// ⚠️ UNE LIGNE PAR EMPREINTE, et l'unicité le garantit. Sans elle, deux
	// occurrences simultanées de la même panne — ce qui est exactement ce qui
	// arrive quand une route casse — créeraient deux lignes, et le
	// groupement, qui fait tout l'intérêt de ce registre, s'effondrerait au
	// pire moment.
	{Collection: "platform_faults", Keys: db.K("fingerprint", 1), Unique: true},
	{Collection: "platform_faults", Keys: db.K("last_seen", -1)},
	{Collection: "platform_faults", Keys: db.K("service", 1, "last_seen", -1)},
	{Collection: "platform_faults", Keys: db.K("resolved_at", 1, "last_seen", -1)},
	// TRENTE JOURS : garder pour toujours remplit la base de pannes corrigées
	// il y a un an ; garder deux jours fait disparaître celle qui n'arrive
	// qu'au moment de la paie.
	{Collection: "platform_faults", Keys: db.K("last_seen", 1), TTLSeconds: db.TTL(30 * 24 * 3600)},

	// --- le matériel (internal/equipment) ---
	{Collection: "equipment_items", Keys: db.K("country", 1, "active", 1, "_id", -1)},
	{Collection: "equipment_contracts", Keys: db.K("user_id", 1, "status", 1, "_id", 1)},
	{Collection: "equipment_contracts", Keys: db.K("country", 1, "status", 1, "_id", -1)},
	{Collection: "equipment_contracts", Keys: db.K("status", 1, "next_period_at", 1)},
	{Collection: "equipment_contracts", Keys: db.K("item_id", 1, "_id", -1)},
	// Les contrats d'un BIEN UNIQUE — la redevance d'un véhicule de la flotte
	// interne. ⚠️ C'est la lecture qui décore une page de parc en UNE requête :
	// sans index, l'écran du parc balaye tous les contrats du pays.
	{Collection: "equipment_contracts", Keys: db.K("asset_id", 1, "_id", -1)},

	// --- la paie des salariés (internal/payroll) ---
	{Collection: "payroll_employees", Keys: db.K("country", 1, "status", 1, "_id", -1)},
	{Collection: "payroll_employees", Keys: db.K("country", 1, "frequency", 1, "status", 1)},
	// Un COMPTE ne porte qu'une fiche de paie : deux fiches sur un même compte
	// auraient payé deux fois la même personne. Partiel plutôt que `sparse` :
	// la plupart des salariés n'ont pas de compte, et `sparse` n'écarte que les
	// champs ABSENTS — un `user_id: null` écrit par une mise à jour entrerait en
	// collision avec le suivant.
	{
		Collection:    "payroll_employees",
		Keys:          db.K("user_id", 1),
		Unique:        true,
		PartialFilter: bson.D{{Key: "user_id", Value: bson.D{{Key: "$type", Value: "objectId"}}}},
	},
	{Collection: "payroll_adjustments", Keys: db.K("employee_id", 1, "status", 1, "created_at", 1)},
	{Collection: "payroll_adjustments", Keys: db.K("country", 1, "status", 1, "_id", -1)},
	{Collection: "payroll_adjustments", Keys: db.K("run_id", 1)},
	// ⚠️ LE VERROU DE PÉRIODE : une seule paie par pays, fréquence et période.
	//
	// ⚠️ ET IL COUVRE L'ÉTAT DE NAISSANCE (`draft`). Un index unique partiel qui
	// ne couvrirait que `paid` ne verrouillerait RIEN : le second brouillon
	// s'insérerait sans heurt, et le doublon n'éclaterait qu'au versement —
	// l'argent déjà parti. C'est la leçon du verrou des rapports, qui réservait
	// en `sending` hors de son propre index.
	//
	// `cancelled` en sort exprès : une paie abandonnée doit pouvoir être
	// repréparée.
	{
		Collection: "payroll_runs",
		Keys:       db.K("country", 1, "frequency", 1, "period_start", 1),
		Unique:     true,
		PartialFilter: bson.D{{Key: "status", Value: bson.D{{Key: "$in",
			Value: bson.A{"draft", "approved", "paid"}}}}},
	},
	{Collection: "payroll_runs", Keys: db.K("country", 1, "status", 1, "_id", -1)},
	{Collection: "payroll_runs", Keys: db.K("slips.employee_id", 1, "_id", -1)},
}

// Ensure pose les index du socle. Idempotent : Mongo ignore un index déjà
// présent, et un index qui échoue est journalisé sans arrêter le démarrage.
func Ensure(ctx context.Context, database *mongo.Database, logger *slog.Logger) int {
	return db.EnsureIndexes(ctx, database, logger, applicationIndexes)
}
