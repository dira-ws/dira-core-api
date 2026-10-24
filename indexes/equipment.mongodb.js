// Le MATÉRIEL loué ou vendu aux agents — voir `internal/equipment`.
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et
// un test refuse tout écart entre les deux.
db.equipment_items.createIndex({ country: 1, active: 1, _id: -1 });
db.equipment_contracts.createIndex({ user_id: 1, status: 1, _id: 1 });
db.equipment_contracts.createIndex({ country: 1, status: 1, _id: -1 });
db.equipment_contracts.createIndex({ status: 1, next_period_at: 1 });
db.equipment_contracts.createIndex({ item_id: 1, _id: -1 });
// Les contrats d'un BIEN UNIQUE — la redevance d'un véhicule de la flotte
// interne (`internal/ownfleet`).
//
// ⚠️ C'est la lecture qui décore une page de parc en UNE requête : sans cet
// index, l'écran du parc balaye tous les contrats du pays pour retrouver la
// redevance de cinquante lignes.
db.equipment_contracts.createIndex({ asset_id: 1, _id: -1 });
