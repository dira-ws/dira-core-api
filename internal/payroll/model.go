// Package payroll porte LA PAIE DES SALARIÉS : les gens que l'entreprise
// paie, à quelle cadence, avec quelles primes et quelles retenues — et la
// trace de ce qui a été versé.
//
// ⚠️ CE N'EST PAS `internal/staff`, ET LA CONFUSION COÛTERAIT CHER. Le staff
// porte des COMPTES D'EXPLOITATION et leurs HABILITATIONS : qui a le droit de
// voir quoi. Ici, on parle d'ARGENT DÛ À UNE PERSONNE. Les deux populations se
// recouvrent en partie et ne se confondent nulle part : la gardienne de nuit
// est payée et n'ouvre aucune console, le directeur technique ouvre tout et
// n'est pas forcément sur cette paie-ci. Fondre les deux aurait fait d'une
// fiche de paie une habilitation — on aurait ouvert la console à tout le
// personnel pour pouvoir le payer.
//
// ⚠️ LE RATTACHEMENT À UN COMPTE EXISTE, MAIS IL N'ACCORDE RIEN. `UserID` est
// FACULTATIF : il sert à ne pas tenir deux vérités sur le contact d'une même
// personne, et à pouvoir la prévenir qu'elle a été payée. Écrire une fiche de
// paie ne crée aucun compte, n'ajoute aucune portée, et ne touche pas à
// `staff_members`.
//
// ⚠️ ET LA PLATEFORME N'EXÉCUTE PAS LE VERSEMENT. Elle l'ENREGISTRE : qui, pour
// quelle période, combien, par quel moyen, et sur l'ordre de qui. Imposer le
// portefeuille Dira (`internal/token`) à un salarié aurait obligé un gardien à
// retirer son salaire par le canal des agents de livraison ; le moyen est donc
// une DÉCLARATION (`cash`, virement, mobile money), avec sa référence.
//
// Les MONTANTS sont des entiers dans la plus petite unité de la monnaie du
// PAYS (XOF, GNF, XAF, GHS, NGN) : la paie porte sa monnaie, figée au
// versement, et aucun montant ne se convertit.
package payroll

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	collectionEmployees   = "payroll_employees"
	collectionAdjustments = "payroll_adjustments"
	collectionRuns        = "payroll_runs"
	collectionSettings    = "payroll_settings"
)

// Fréquences de paiement.
//
// ⚠️ `biweekly` est QUINZOMADAIRE — tous les quatorze jours —, et non « deux
// fois par mois ». Les deux se confondent dans la conversation et jamais dans
// un calendrier : vingt-six paies par an d'un côté, vingt-quatre de l'autre.
// Les périodes quinzomadaires sont donc comptées depuis une ORIGINE fixe
// (voir `period.go`), ce qui les rend identiques pour tout le pays — sans quoi
// deux salariés embauchés à deux semaines d'écart n'auraient jamais la même
// période, et une paie collective n'aurait pas de sens.
const (
	FrequencyMonthly  = "monthly"
	FrequencyBiweekly = "biweekly"
	FrequencyWeekly   = "weekly"
)

// Frequencies est la liste EXPORTÉE, pour que la validation, le vocabulaire
// servi à la console et les tests lisent la même — deux listes divergeraient.
var Frequencies = []string{FrequencyMonthly, FrequencyBiweekly, FrequencyWeekly}

// Statuts d'un salarié.
//
// ⚠️ `suspended` et `left` ne sont pas la même chose, et c'est l'argent qui
// les sépare : un salarié suspendu n'entre plus dans une paie mais garde sa
// fiche et ses retenues en cours ; un salarié sorti a une DATE DE SORTIE, et
// cette date borne les périodes dans lesquelles il peut encore apparaître.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusLeft      = "left"
)

var Statuses = []string{StatusActive, StatusSuspended, StatusLeft}

// Natures d'un ajustement : ce qui s'ajoute, ce qui se retire.
const (
	KindBonus     = "bonus"
	KindDeduction = "deduction"
)

var Kinds = []string{KindBonus, KindDeduction}

// Statuts d'un ajustement.
const (
	AdjustmentOpen      = "open"      // reste à passer sur une paie
	AdjustmentSettled   = "settled"   // entièrement passé
	AdjustmentCancelled = "cancelled" // annulé avant d'avoir tout passé
)

// Statuts d'une paie — le cycle : préparer, vérifier, verser.
//
// ⚠️ `paid` EST TERMINAL. Rien ne le quitte, et aucune route ne modifie une
// paie versée — voir `Run.Status` et `service.go`.
const (
	RunDraft     = "draft"     // préparée : les montants sont calculés, rien n'est dû
	RunApproved  = "approved"  // vérifiée : relue et bonne à verser
	RunPaid      = "paid"      // versée : figée pour toujours
	RunCancelled = "cancelled" // abandonnée avant versement
)

// Moyens de versement — ce que l'exploitation DÉCLARE avoir fait.
//
// ⚠️ AUCUN DE CES MOYENS N'EST EXÉCUTÉ PAR LA PLATEFORME, et c'est délibéré
// (voir l'en-tête du paquet). Les nommer sert à savoir où chercher la preuve
// quand quelqu'un dit n'avoir rien reçu : un reçu d'espèces, un avis de
// virement, une référence d'opérateur.
const (
	MethodCash        = "cash"
	MethodBank        = "bank_transfer"
	MethodMobileMoney = "mobile_money"
)

var Methods = []string{MethodCash, MethodBank, MethodMobileMoney}

// Employee est un salarié : une personne que l'entreprise paie.
type Employee struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Country string             `bson:"country"`
	// UserID est le compte d'exploitation de la personne, S'IL EXISTE.
	//
	// ⚠️ UN POINTEUR, et il est le plus souvent NUL. La majorité des salariés
	// n'ouvrent aucune application : exiger un compte aurait transformé la
	// déclaration d'un salaire en création d'accès. Quand il est posé, il est
	// UNIQUE (index) : deux fiches de paie sur un même compte auraient payé
	// deux fois la même personne sans que rien ne le signale.
	UserID *primitive.ObjectID `bson:"user_id,omitempty"`
	// FullName, Phone, Email : l'identité portée ICI.
	//
	// ⚠️ ÉCRITE SUR LA FICHE, pas lue au compte, et ce n'est pas une
	// duplication par paresse : un salarié sans compte n'a pas d'autre endroit
	// où exister. Quand le compte existe, la console affiche les deux et
	// l'écart se voit — ce qui vaut mieux qu'une fiche de paie dont le nom
	// change parce que quelqu'un a corrigé son profil.
	FullName string `bson:"full_name"`
	Phone    string `bson:"phone,omitempty"`
	Email    string `bson:"email,omitempty"`
	// Position : le poste, en texte libre. « Chargée de recouvrement Lomé Nord »
	// n'entre dans aucune énumération, et forcer un choix aurait rangé tout le
	// monde sous « autre ».
	Position string    `bson:"position"`
	HiredAt  time.Time `bson:"hired_at"`
	// LeftAt : la date de sortie. Elle BORNE les périodes : une paie dont la
	// période commence après cette date n'inclut plus la personne.
	LeftAt *time.Time `bson:"left_at,omitempty"`
	Status string     `bson:"status"`
	// BaseSalary est la rémunération de base PAR PÉRIODE DE PAIE, dans la
	// monnaie du pays.
	//
	// ⚠️ PAR PÉRIODE, ET NON PAR MOIS. Un salaire mensuel divisé par la
	// fréquence demande une règle cachée — quatre semaines, ou 4,33 ? — qui
	// n'est écrite nulle part et qui fait deux montants différents selon qui
	// calcule. Déclarer ce qui est versé à chaque échéance rend la fiche de
	// paie exacte en entiers, et rend le passage du mensuel à l'hebdomadaire
	// une décision explicite plutôt qu'une division.
	BaseSalary int    `bson:"base_salary"`
	Frequency  string `bson:"frequency"`
	// PayDay : le jour du mois du versement (1..28), pour une paie MENSUELLE.
	//
	// ⚠️ BORNÉ À 28, et c'est une borne de calendrier, pas une préférence. Un
	// « versement le 31 » glisse au 1er du mois suivant en février, et une paie
	// qui change de mois toute seule casse la période à laquelle elle appartient.
	PayDay int `bson:"pay_day,omitempty"`
	// PayWeekday : le jour de la semaine du versement (1 = lundi … 7 =
	// dimanche), pour une paie HEBDOMADAIRE ou QUINZOMADAIRE.
	//
	// ⚠️ DEUX CHAMPS PLUTÔT QU'UN, délibérément. Un seul « jour de versement »
	// qui vaut 1..28 pour le mensuel et 1..7 pour l'hebdomadaire se lit faux
	// une fois sur deux : « 5 » voudrait dire le 5 du mois ou le vendredi selon
	// un autre champ, et la validation ne pourrait pas distinguer un oubli
	// d'une valeur légitime.
	PayWeekday int       `bson:"pay_weekday,omitempty"`
	Notes      string    `bson:"notes,omitempty"`
	CreatedBy  string    `bson:"created_by,omitempty"`
	CreatedAt  time.Time `bson:"created_at"`
	UpdatedAt  time.Time `bson:"updated_at"`
}

// PayableOn dit si ce salarié entre dans une paie couvrant [start, end[.
//
// ⚠️ IL N'Y A PAS DE PRORATA, et c'est une décision assumée. Découper une
// période au nombre de jours travaillés demande de choisir une base (jours
// ouvrés ? calendaires ? trente par mois ?) : cette règle appartient au droit
// du travail du pays, et l'inventer ici aurait produit des bulletins faux avec
// l'air d'être justes. Une entrée ou une sortie en cours de période s'écrit
// donc comme une PRIME ou une RETENUE nommée, qui dit son montant et sa
// raison — et que quelqu'un a signée.
func (e *Employee) PayableOn(start, end time.Time) bool {
	if e.Status != StatusActive {
		return false
	}
	// Embauché après la fin de la période : rien à payer.
	if !e.HiredAt.Before(end) {
		return false
	}
	// Parti avant le début de la période : plus rien à payer.
	if e.LeftAt != nil && !e.LeftAt.After(start) {
		return false
	}
	return true
}

// Adjustment est une PRIME ou une RETENUE : un montant qui s'ajoute ou se
// retire, avec son motif et son auteur.
//
// ⚠️ UN OBJET POUR LES DEUX, et non deux collections. Une prime et une retenue
// posent exactement les mêmes questions — combien, pourquoi, qui l'a accordée,
// sur combien de paies, est-ce passé ? — et deux objets jumeaux auraient fait
// deux chemins à relire, deux fois les mêmes bornes à écrire, et un seul des
// deux corrigé le jour où une règle change.
type Adjustment struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	Country    string             `bson:"country"`
	EmployeeID primitive.ObjectID `bson:"employee_id"`
	Kind       string             `bson:"kind"`
	// Amount : le montant TOTAL, dans la monnaie du pays. Percent : une part,
	// en pourcentage.
	//
	// ⚠️ L'UN OU L'AUTRE, JAMAIS LES DEUX — la validation le refuse. « 10 % et
	// 5 000 F » n'a pas de sens unique : ni la somme, ni le plus grand, ni le
	// plus petit ne s'imposent, et chacun aurait été implémenté une fois.
	//
	// ⚠️ ET LES DEUX POURCENTAGES NE PORTENT PAS SUR LA MÊME BASE : une PRIME
	// en pourcentage se calcule sur le SALAIRE DE BASE (c'est une part de
	// rémunération), une RETENUE sur le BRUT effectivement dû (sinon elle
	// pourrait dépasser ce qu'il y a à retenir). Voir `payslip.go`.
	Amount  int    `bson:"amount,omitempty"`
	Percent int    `bson:"percent,omitempty"`
	Reason  string `bson:"reason"`
	// Recurring : à repasser sur CHAQUE paie, sans fin — une prime de
	// transport, une cotisation. Exclusif de `Installments`.
	Recurring bool `bson:"recurring"`
	// Installments : sur combien de paies étaler le montant total (1 = en une
	// fois). C'est l'ÉCHÉANCIER d'une retenue.
	Installments int `bson:"installments,omitempty"`
	// Runs : combien de paies l'ont déjà passé ; Taken : combien a déjà été
	// passé. Les deux, parce qu'une paie peut avoir passé MOINS que prévu
	// (plancher atteint) : compter les paies sans compter l'argent ferait
	// croire une retenue soldée alors qu'il reste à prendre.
	Runs  int `bson:"runs"`
	Taken int `bson:"taken"`
	// EffectiveFrom : ne pas passer avant cette date. Vide = tout de suite.
	EffectiveFrom time.Time           `bson:"effective_from,omitempty"`
	Status        string              `bson:"status"`
	RunID         *primitive.ObjectID `bson:"run_id,omitempty"`
	// CorrectsRun : la paie VERSÉE que cet ajustement régularise.
	//
	// ⚠️ C'EST TOUTE LA RÉPONSE À « CORRIGER UNE PAIE ». Une paie versée ne se
	// modifie pas (voir `Run`) : la correction est une pièce NOUVELLE, qui
	// nomme celle qu'elle rattrape et qui passe sur la paie suivante. On garde
	// ainsi les deux faits — ce qui a été versé, et ce qu'on a corrigé — au
	// lieu d'en perdre un en réécrivant l'autre.
	CorrectsRun *primitive.ObjectID `bson:"corrects_run,omitempty"`
	GrantedBy   string              `bson:"granted_by,omitempty"`
	SettledAt   *time.Time          `bson:"settled_at,omitempty"`
	CancelledAt *time.Time          `bson:"cancelled_at,omitempty"`
	CreatedAt   time.Time           `bson:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at"`
}

// Remaining rend ce qu'il reste à passer sur un ajustement à montant total.
// Un ajustement récurrent n'a pas de reste : il n'a pas de fin.
func (a *Adjustment) Remaining() int {
	if a.Recurring || a.Amount <= 0 {
		return 0
	}
	if left := a.Amount - a.Taken; left > 0 {
		return left
	}
	return 0
}

// SlipLine est une prime ou une retenue TELLE QU'ELLE A ÉTÉ PASSÉE sur une
// fiche de paie.
type SlipLine struct {
	AdjustmentID primitive.ObjectID `bson:"adjustment_id"`
	Kind         string             `bson:"kind"`
	Reason       string             `bson:"reason"`
	// Planned : ce que l'ajustement VISAIT ; Amount : ce qui est réellement
	// passé.
	//
	// ⚠️ LES DEUX SUR LA LIGNE, et c'est ce qui rend une retenue bornée
	// LISIBLE. N'écrire que le montant retenu aurait fait disparaître la borne :
	// la personne aurait vu « retenue : 15 000 » sans savoir qu'on en visait
	// 60 000, et le reste serait réapparu le mois suivant sans explication.
	Planned   int    `bson:"planned"`
	Amount    int    `bson:"amount"`
	GrantedBy string `bson:"granted_by,omitempty"`
}

// Payslip est la fiche de paie d'UNE personne sur UNE période.
type Payslip struct {
	EmployeeID primitive.ObjectID `bson:"employee_id"`
	// Name et Position sont FIGÉS à la préparation : la fiche reste lisible
	// quand la personne change de poste ou quitte l'entreprise.
	Name     string `bson:"name"`
	Position string `bson:"position,omitempty"`
	Base     int    `bson:"base"`
	// PayDate est la date de versement DE CETTE PERSONNE — son jour de paie, pas
	// celui du pays.
	//
	// ⚠️ SUR LA FICHE ET NON SEULEMENT SUR LA PAIE : le jour de versement est
	// réglable par salarié, et une paie collective qui n'afficherait que la date
	// du pays aurait fait mentir ce réglage pour tous ceux qui en ont un autre.
	PayDate time.Time  `bson:"pay_date"`
	Lines   []SlipLine `bson:"lines"`
	Bonuses int        `bson:"bonuses"`
	// Gross = Base + Bonuses ; Deductions = ce qui a été RETENU (jamais ce qui
	// était visé) ; Net = Gross - Deductions.
	Gross      int `bson:"gross"`
	Deductions int `bson:"deductions"`
	Net        int `bson:"net"`
	// Carried : ce que les bornes ont EMPÊCHÉ de retenir, et qui reste dû.
	//
	// ⚠️ REPORTÉ, JAMAIS EFFACÉ. Une retenue qui dépasse le plancher n'est pas
	// annulée — elle attend la paie suivante. L'effacer aurait fait d'une borne
	// de protection un cadeau, et personne n'aurait plus osé poser de plancher.
	Carried int `bson:"carried"`
	// Floor : le plancher appliqué — ce en dessous de quoi on a refusé de
	// descendre. Écrit sur la fiche pour que la borne se relise des mois plus
	// tard, quand les réglages du pays auront changé.
	Floor int `bson:"floor"`
}

// Run est une PAIE : une période, une fréquence, et une fiche par salarié.
//
// ⚠️ UNE PAIE VERSÉE NE SE MODIFIE PAS. Aucune route ne touche un `Run` en
// `paid` : ni le montant, ni la liste, ni le moyen. C'est la seule façon de
// pouvoir répondre « voilà ce qui a été versé le 28 » des mois plus tard — un
// document modifiable ne dit pas ce qui a été versé, il dit ce qu'on en pense
// aujourd'hui. Corriger s'écrit donc ailleurs : un ajustement qui NOMME la
// paie corrigée et passe sur la suivante (`Adjustment.CorrectsRun`).
type Run struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Country string             `bson:"country"`
	// Currency est figée à la préparation : la monnaie du pays ce jour-là. Un
	// montant relu dix-huit mois plus tard doit porter sa monnaie avec lui.
	Currency  string `bson:"currency"`
	Frequency string `bson:"frequency"`
	// PeriodStart est INCLUSIVE, PeriodEnd EXCLUSIVE (le premier instant de la
	// période suivante).
	//
	// ⚠️ UNE BORNE OUVERTE, et c'est ce qui empêche les paies de se chevaucher.
	// Avec deux bornes inclusives, chaque comparaison choisit entre `<=` et
	// `<` — et c'est exactement là qu'on laisse un jour en double ou un jour
	// de trou, une fois par an, sans que personne ne sache lequel.
	PeriodStart time.Time `bson:"period_start"`
	PeriodEnd   time.Time `bson:"period_end"`
	PayDate     time.Time `bson:"pay_date"`
	Status      string    `bson:"status"`
	Slips       []Payslip `bson:"slips"`
	// Les totaux, recalculés à la préparation et figés avec elle.
	Gross      int `bson:"gross"`
	Bonuses    int `bson:"bonuses"`
	Deductions int `bson:"deductions"`
	Net        int `bson:"net"`
	Carried    int `bson:"carried"`
	Headcount  int `bson:"headcount"`

	PreparedBy string     `bson:"prepared_by,omitempty"`
	ApprovedBy string     `bson:"approved_by,omitempty"`
	ApprovedAt *time.Time `bson:"approved_at,omitempty"`
	PaidBy     string     `bson:"paid_by,omitempty"`
	PaidAt     *time.Time `bson:"paid_at,omitempty"`
	Method     string     `bson:"method,omitempty"`
	// Reference : l'avis de virement, le bordereau, le lot mobile money.
	Reference    string     `bson:"reference,omitempty"`
	CancelledAt  *time.Time `bson:"cancelled_at,omitempty"`
	CancelReason string     `bson:"cancel_reason,omitempty"`
	Notes        string     `bson:"notes,omitempty"`
	CreatedAt    time.Time  `bson:"created_at"`
	UpdatedAt    time.Time  `bson:"updated_at"`
}

// Settings sont les réglages de la paie, PAR PAYS.
type Settings struct {
	Country string `bson:"_id"`
	// Les défauts proposés à la création d'un salarié. Ils ne contraignent
	// rien : chaque salarié porte sa propre fréquence et son propre jour.
	DefaultFrequency  string `bson:"default_frequency"`
	DefaultPayDay     int    `bson:"default_pay_day"`
	DefaultPayWeekday int    `bson:"default_pay_weekday"`
	// MaxDeductionPercent : la part MAXIMALE du brut qu'on peut retenir sur UNE
	// paie, toutes retenues confondues.
	//
	// ⚠️ C'EST LA BORNE QUI EMPÊCHE UNE PAIE VIDE, et elle est de la même
	// famille que le « minimum laissé à l'agent » du matériel
	// (`equipment.Plan.MinLeftXOF`) : une retenue légitime, appliquée en entier,
	// peut laisser quelqu'un sans rien — et quelqu'un sans rien ne vient pas
	// travailler le mois suivant.
	//
	// ⚠️ LE DÉFAUT DE 30 % EST UNE PRUDENCE DE PLATEFORME, PAS UNE RÈGLE DE
	// DROIT. La quotité saisissable d'un salaire est fixée par le droit du
	// travail de chaque pays, et elle n'est pas la même partout ; ce réglage
	// existe précisément pour que l'exploitation inscrive la valeur légale du
	// pays au lieu de subir une constante écrite ici.
	MaxDeductionPercent int `bson:"max_deduction_percent"`
	// MinNet : le plancher ABSOLU laissé à la personne, dans la monnaie du
	// pays. Zéro = seul le pourcentage borne.
	//
	// ⚠️ LES DEUX BORNES SE CUMULENT, et la plus protectrice gagne. Un
	// pourcentage seul laisse 70 % de presque rien ; un montant seul ne dit
	// rien des hauts salaires.
	MinNet    int       `bson:"min_net"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// DefaultSettings est le plancher : ce qui vaut tant qu'un pays n'a rien
// réglé. Prudent — mensuel, versé le 28, un tiers du brut retenable au plus.
func DefaultSettings(code string) Settings {
	return Settings{
		Country:          code,
		DefaultFrequency: FrequencyMonthly,
		// Le 28 : le dernier jour qui existe dans tous les mois. Voir
		// `Employee.PayDay`.
		DefaultPayDay: 28,
		// Vendredi : le jour de paie le plus courant dans la région, et celui
		// qui laisse un jour ouvré avant le week-end pour rattraper un virement
		// qui n'est pas parti.
		DefaultPayWeekday:   5,
		MaxDeductionPercent: 30,
		MinNet:              0,
	}
}
