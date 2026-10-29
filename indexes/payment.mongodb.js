// MongoDB indexes for the payment module.
// Run with mongosh against the application database.
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés
// sont déclarés dans internal/platform/db/indexes.go et créés au démarrage
// de l'API ; un test refuse tout écart entre les deux. Toute ligne ajoutée
// ici doit l'être aussi là-bas.

// payments
db.payments.createIndex({ provider_ref: 1 }, { unique: true }); // webhook idempotency
db.payments.createIndex({ user_id: 1 });
db.payments.createIndex({ status: 1 });
db.payments.createIndex({ country: 1, _id: -1 });

// RETROUVER UN PAIEMENT DONT LA RÉPONSE S'EST PERDUE (#248, #250).
//
// La clé d'idempotence est unique PAR COMPTE, et c'est cet index qui le
// garantit : deux initiations simultanées de la même intention passeraient
// une vérification applicative, et deux demandes de paiement partiraient sur
// le téléphone du client.
//
// PARTIEL : la plupart des paiements n'ont pas de clé. `sparse` n'écarterait
// que les documents où le champ est ABSENT — deux paiements portant la chaîne
// vide suffiraient alors à empêcher l'index de se construire.
db.payments.createIndex(
  { user_id: 1, idempotency_key: 1 },
  { unique: true, partialFilterExpression: { idempotency_key: { $gt: "" } } },
);
// GET /payments?purpose=&ref_id= — le plus récent d'abord (created_at avant
// _id, comme db.NewestFirst).
db.payments.createIndex({ user_id: 1, purpose: 1, ref_id: 1, created_at: -1, _id: -1 });
// Le remboursement d'une commande (RefundOrderPayment) cherchait sans index.
db.payments.createIndex({ purpose: 1, ref_id: 1, status: 1 });
