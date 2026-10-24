// LA PAIE DES SALARIÉS — voir `internal/payroll`.
// Run with: mongosh <uri> indexes/payroll.mongodb.js
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et un
// test refuse tout écart entre les deux.
//
// ⚠️ CE N'EST PAS `staff_members`. Le staff porte des COMPTES et leurs
// HABILITATIONS — qui a le droit de voir quoi ; la paie porte de l'ARGENT DÛ À
// UNE PERSONNE. La plupart des salariés n'ont aucun compte, et le rattachement
// entre les deux est facultatif.

// Les listes de la console, bornées au PAYS.
db.payroll_employees.createIndex({ country: 1, status: 1, _id: -1 });
// La préparation d'une paie : les actifs d'une fréquence, dans un pays.
db.payroll_employees.createIndex({ country: 1, frequency: 1, status: 1 });

// UN COMPTE ne porte qu'une fiche de salarié.
//
// ⚠️ Deux fiches sur le même compte auraient payé deux fois la même personne,
// et un contrôle applicatif « a-t-il déjà une fiche ? » laisse passer deux
// créations concurrentes — deux exploitants enregistrant la même embauche au
// même moment.
//
// PARTIEL plutôt que `sparse` : la plupart des salariés n'ont pas de compte, et
// `sparse` n'écarte que les champs ABSENTS — un `user_id: null` écrit par une
// mise à jour entrerait en collision avec le suivant.
db.payroll_employees.createIndex(
  { user_id: 1 },
  { unique: true, partialFilterExpression: { user_id: { $type: "objectId" } } },
);

// Les primes et retenues d'une personne, les plus anciennes d'abord : c'est
// l'ordre dans lequel elles passent sur une paie.
db.payroll_adjustments.createIndex({ employee_id: 1, status: 1, created_at: 1 });
db.payroll_adjustments.createIndex({ country: 1, status: 1, _id: -1 });
// La RÉSERVATION par une paie en préparation — ce qui empêche deux paies de
// compter la même prime.
db.payroll_adjustments.createIndex({ run_id: 1 });

// ⚠️ LE VERROU DE PÉRIODE : une seule paie par pays, fréquence et période.
//
// ⚠️ ET IL COUVRE L'ÉTAT DE NAISSANCE (`draft`). Un index unique partiel qui ne
// couvrirait que `paid` ne verrouillerait RIEN : le second brouillon
// s'insérerait sans heurt, et le doublon n'éclaterait qu'au versement —
// l'argent déjà parti. C'est la leçon du verrou des rapports, qui réservait en
// `sending`, hors de son propre index.
//
// `cancelled` en sort exprès : une paie abandonnée doit pouvoir être repréparée,
// sans quoi une erreur de manipulation fermerait un mois pour toujours.
db.payroll_runs.createIndex(
  { country: 1, frequency: 1, period_start: 1 },
  { unique: true, partialFilterExpression: { status: { $in: ["draft", "approved", "paid"] } } },
);
db.payroll_runs.createIndex({ country: 1, status: 1, _id: -1 });
// Les paies où figure une personne — l'historique de sa fiche.
db.payroll_runs.createIndex({ "slips.employee_id": 1, _id: -1 });
