// Index des FLOTTES PRIVÉES (collection fleets).
// Run with: mongosh <uri> indexes/fleet.mongodb.js
//
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/indexes/indexes.go et créés au démarrage de l'API ;
// un test refuse tout écart entre les deux.
//
// Une flotte est une société qui possède des véhicules conduits par d'autres.
// Elle vit au SOCLE parce qu'une même société possède des motos qui livrent et
// des voitures qui font des courses : la loger dans une verticale aurait
// obligé l'autre à lire la base de sa voisine pour afficher un propriétaire.

// UN nom, UNE flotte.
//
// ⚠️ C'est cet index qui empêche deux exploitants traitant le même contrat
// d'enregistrer deux fois la même société. Un contrôle applicatif « ce nom
// existe-t-il ? » laisse passer deux écritures concurrentes, et la base porte
// alors deux flottes que rien ne distingue — les véhicules se répartissent
// entre les deux, et aucun total n'est juste.
db.fleets.createIndex({ name: 1 }, { unique: true });

// La liste de l'administration : les plus récentes d'abord, filtrées par état.
db.fleets.createIndex({ status: 1, _id: -1 });
db.fleets.createIndex({ country: 1, _id: -1 });

// « Quelle flotte gère ce compte ? » — la question que pose la connexion d'un
// gérant.
//
// PARTIEL : une flotte enregistrée sur un contrat papier n'a pas encore de
// gérant déclaré, et indexer toutes ces absences coûterait la taille de la
// collection pour ne rien répondre.
db.fleets.createIndex(
  { owner_user_id: 1 },
  { partialFilterExpression: { owner_user_id: { $exists: true } } },
);

// UNE SEULE FLOTTE INTERNE PAR PAYS — la flotte de Dira (`internal/ownfleet`).
//
// ⚠️⚠️ C'est cet index qui le garantit, et non une lecture « existe-t-elle ? »
// suivie d'une création : deux attributions traitées en même temps le jour de
// l'ouverture d'un pays passeraient toutes les deux, et la plateforme porterait
// deux flottes internes. Les véhicules de l'une seraient invisibles depuis
// l'autre, et personne ne saurait qu'il manque la moitié du parc.
//
// PARTIEL SUR `internal: true`, et le champ est bien ÉCRIT quand il vaut vrai :
// c'est la condition pour qu'un index partiel verrouille réellement quelque
// chose — un filtre qui ne correspond à aucun document indexe le vide et
// laisse tout passer. Les flottes ORDINAIRES n'y entrent pas, sans quoi deux
// sociétés d'un même pays entreraient en collision.
db.fleets.createIndex(
  { country: 1 },
  { unique: true, partialFilterExpression: { internal: true } },
);
