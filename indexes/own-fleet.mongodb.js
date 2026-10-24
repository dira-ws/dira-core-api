// Index de LA FLOTTE INTERNE (collection own_fleet_assets).
// Run with: mongosh <uri> indexes/own-fleet.mongodb.js
//
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/indexes/indexes.go et créés au démarrage de l'API ;
// un test refuse tout écart entre les deux.
//
// Ce sont les véhicules que DIRA POSSÈDE et confie à un chauffeur ou à un
// livreur contre une redevance. La redevance elle-même n'est pas ici : c'est un
// contrat de LOCATION du module du matériel (`equipment_contracts`), parce
// qu'il ne peut exister qu'un seul moteur de retenue sur les gains.

// UNE plaque, UN véhicule, par PAYS.
//
// ⚠️ C'est cet index qui empêche deux exploitants enregistrant la même
// livraison d'inscrire deux fois la même voiture. Un contrôle applicatif
// « cette plaque existe-t-elle ? » laisse passer deux écritures concurrentes, et
// le parc porte alors deux lignes pour un véhicule : l'une confiée, l'autre
// « disponible » pour toujours.
db.own_fleet_assets.createIndex({ country: 1, plate: 1 }, { unique: true });

// L'écran du parc : par pays, filtré par état, les plus récents d'abord.
db.own_fleet_assets.createIndex({ country: 1, status: 1, _id: -1 });

// « Quel véhicule m'est confié ? » — la lecture de l'application de l'agent.
//
// ⚠️ Elle n'est PAS bornée par pays : c'est le compte connecté qui désigne ses
// véhicules, et un chauffeur en voyage doit pouvoir lire ce qu'il doit.
db.own_fleet_assets.createIndex({ holder_user_id: 1 });

// Le CHEMIN INVERSE : rendre un véhicule au parc est conditionné par
// l'identifiant de son contrat, pour qu'un retour enregistré en retard ne
// libère pas une voiture déjà reconfiée à quelqu'un d'autre.
db.own_fleet_assets.createIndex({ contract_id: 1 });
