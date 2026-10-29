package country

// LES BORNES DE RECHARGE DU PORTEFEUILLE D'UN PASSAGER.
//
// ⚠️⚠️ L'INVERSE DES RETRAITS : ouvert par défaut. Un retrait éteint ne gêne
// personne tant qu'on ne l'annonce pas ; une recharge éteinte coupe le moyen de
// payer une course. Un pays jamais réglé doit donc recharger, et ce fichier ne
// sert qu'à dire DANS QUELLES BORNES.
//
// ⚠️⚠️ ET LES NOMBRES NE SONT PAS DES FRANCS CFA. C'est le piège de cette
// couche, et il a déjà coûté une fois sur le catalogue des motos : 1 000 vaut
// un petit trajet au Togo et sept centimes d'euro en Guinée. Les montants sont
// dans la MONNAIE DU PAYS, en unité la plus petite — le franc pour XOF, XAF et
// GNF, le centième pour le cedi et le naira. Les champs ne portent donc PAS de
// suffixe `_xof`, contrairement aux réglages de retrait écrits avant : un nom
// qui annonce une monnaie que la valeur n'a pas est pire qu'un nom neutre, et
// `currency` part avec la réponse pour que l'écran n'ait rien à deviner.

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Topup est la politique de recharge d'un pays. Zéro ou vide = le défaut de la
// monnaie, jamais « pas de recharge ».
type Topup struct {
	Min int `bson:"min,omitempty"`
	Max int `bson:"max,omitempty"`
	// Presets : les montants proposés d'un geste. Vide = ceux de la monnaie.
	Presets []int `bson:"presets,omitempty"`
}

// topupDefaults : les bornes par MONNAIE, dans son unité la plus petite.
//
// ⚠️ PAR MONNAIE ET NON PAR PAYS, et ce n'est pas une conversion : ce sont des
// nombres RONDS dans chaque monnaie, choisis pour être plausibles à l'écran.
// Un taux de change figé dans le code serait faux dans l'année, et personne ne
// le relirait ; l'exploitation règle son pays depuis la console le jour où ces
// montants ne lui vont plus.
var topupDefaults = map[string]Topup{
	// Francs CFA : pas de décimale, l'unité est le franc.
	"XOF": {Min: 500, Max: 2_000_000, Presets: []int{1_000, 2_000, 5_000, 10_000}},
	"XAF": {Min: 500, Max: 2_000_000, Presets: []int{1_000, 2_000, 5_000, 10_000}},
	// Franc guinéen : pas de décimale non plus, mais une unité ~15 fois plus
	// petite. Les mêmes nombres y seraient dérisoires.
	"GNF": {Min: 5_000, Max: 20_000_000, Presets: []int{10_000, 25_000, 50_000, 100_000}},
	// Cedi : DEUX décimales — ces entiers sont des centièmes. 500 = 5 GH₵,
	// et les montants proposés valent 20, 50, 100 et 200 GH₵.
	"GHS": {Min: 500, Max: 2_000_000, Presets: []int{2_000, 5_000, 10_000, 20_000}},
	// Naira : deux décimales aussi. 50 000 = 500 ₦, et les montants proposés
	// valent 2 000, 5 000, 10 000 et 20 000 ₦.
	"NGN": {Min: 50_000, Max: 200_000_000, Presets: []int{200_000, 500_000, 1_000_000, 2_000_000}},
}

// topupFallback s'applique à une monnaie absente de la table.
//
// ⚠️ IL EXISTE POUR QUE L'AJOUT D'UNE MONNAIE AU CATALOGUE NE COUPE PAS LA
// RECHARGE. Une monnaie nouvelle sans ligne ici rendrait des bornes nulles, et
// `max = 0` se lit « aucun montant n'est accepté » : le pays ouvrirait sans
// pouvoir encaisser, et rien ne le dirait. Les nombres sont ceux des francs,
// faute de mieux — et `TestEveryCatalogueCurrencyHasItsTopupDefaults` fait
// rougir l'ajout pour qu'on les choisisse pour de vrai.
var topupFallback = Topup{Min: 500, Max: 2_000_000, Presets: []int{1_000, 2_000, 5_000, 10_000}}

// TopupResponse est la politique telle que les applications et la console la
// lisent — tous les champs posés, aucun à deviner.
type TopupResponse struct {
	Min     int   `json:"min"`
	Max     int   `json:"max"`
	Presets []int `json:"presets"`
	// Currency : la monnaie dans laquelle ces nombres sont écrits. ⚠️ Servie
	// avec eux, et non laissée à l'application : c'est la seule chose qui
	// empêche de lire des nairas comme des francs.
	Currency string `json:"currency"`
	// Decimals : combien de décimales la monnaie porte — zéro pour les francs,
	// deux pour le cedi et le naira. L'écran en a besoin pour AFFICHER 20,00
	// là où l'entier vaut 2 000.
	Decimals int `json:"decimals"`
}

// TopupUpdateRequest règle les bornes depuis la console. Tout est facultatif.
type TopupUpdateRequest struct {
	// ⚠️ `min` NE PEUT PAS ÊTRE ZÉRO. Une recharge d'un franc coûte à la
	// plateforme des frais d'agrégateur supérieurs à ce qu'elle transporte, et
	// c'est le client qui a raison : c'est son argent.
	Min *int `json:"min" validate:"omitempty,min=1,max=100000000"`
	Max *int `json:"max" validate:"omitempty,min=1,max=1000000000"`
	// Presets : liste complète, elle REMPLACE la précédente. Une liste vide
	// explicite rétablit les montants de la monnaie.
	Presets *[]int `json:"presets" validate:"omitempty,max=8,dive,min=1"`
}

var errNoTopupUpdate = apperr.Validation("nothing to update: send min, max or presets")

// LES RÉGLAGES QUI REFUSENT TOUT, ou qui affichent un bouton condamné.
//
// ⚠️⚠️ CHAQUE BORNE EST VALIDE SEULE ET LA PAIRE PEUT ÊTRE IMPOSSIBLE — le même
// piège que les retraits, et la même parade : on relit le résultat FUSIONNÉ.
// `{"min": 50000}` seul suffit à rendre impossible un `max: 10000` écrit trois
// mois plus tôt, sans que la requête le nomme.
//
// ⚠️⚠️ ET UN MONTANT PROPOSÉ HORS BORNES EST UN BOUTON QUI ÉCHOUE. C'est pire
// qu'un réglage mort : l'écran affiche « 10 000 », le client appuie, et le
// serveur refuse ce que le serveur lui-même venait de proposer. Personne ne
// soupçonne le réglage — on soupçonne l'opérateur mobile.
var (
	errTopupMaxUnderMin = apperr.New("topup_max_under_min",
		"max is below min: no amount could ever be topped up",
		http.StatusUnprocessableEntity)
	errTopupPresetOutOfBounds = apperr.New("topup_preset_out_of_bounds",
		"a suggested amount falls outside min..max: the button would be refused",
		http.StatusUnprocessableEntity)
)

// checkTopupCoherence relit les bornes et les montants proposés les uns contre
// les autres. Elle reçoit la politique DÉJÀ complétée par ses défauts.
func checkTopupCoherence(t TopupResponse) error {
	if t.Max < t.Min {
		return errTopupMaxUnderMin.WithMeta(map[string]any{
			// ⚠️ LES DEUX NOMBRES : celui qui lit vient d'en changer un seul, et
			// c'est souvent l'autre qu'il faut corriger.
			"reason": fmt.Sprintf("min=%d, max=%d", t.Min, t.Max),
		})
	}
	for _, p := range t.Presets {
		if p < t.Min || p > t.Max {
			return errTopupPresetOutOfBounds.WithMeta(map[string]any{
				"reason": fmt.Sprintf("preset=%d, min=%d, max=%d", p, t.Min, t.Max),
			})
		}
	}
	return nil
}

// topupResponse complète les trous avec les défauts de la monnaie.
func topupResponse(code string, t Topup) TopupResponse {
	devise := ""
	decimals := 0
	if info, ok := country.Lookup(code); ok {
		devise = info.Currency
		if cur, ok := country.LookupCurrency(info.Currency); ok {
			decimals = cur.Decimals
		}
	}
	défaut, ok := topupDefaults[devise]
	if !ok {
		défaut = topupFallback
	}
	out := TopupResponse{
		Min: t.Min, Max: t.Max, Presets: t.Presets,
		Currency: devise, Decimals: decimals,
	}
	if out.Min <= 0 {
		out.Min = défaut.Min
	}
	if out.Max <= 0 {
		out.Max = défaut.Max
	}
	if len(out.Presets) == 0 {
		out.Presets = défaut.Presets
	}
	// ⚠️ TRIÉS ET DÉDOUBLONNÉS ICI, pas à l'écriture : un pays réglé avant ce
	// tri porte peut-être déjà une liste en désordre, et l'écran afficherait
	// « 5 000 · 1 000 · 10 000 ». Ranger à la lecture répare aussi l'existant.
	out.Presets = rangés(out.Presets)
	return out
}

// rangés trie et dédoublonne une liste de montants, sans toucher l'original.
func rangés(in []int) []int {
	vus := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if !vus[v] {
			vus[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

// TopupPolicy rend les bornes d'un pays — la console et les applications.
func (s *Service) TopupPolicy(ctx context.Context, code string) (*TopupResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := topupResponse(info.Code, inst.Topup)
	return &out, nil
}

// TopupOf rend les bornes EFFECTIVES — l'adaptateur que le portefeuille et le
// paiement appellent.
//
// ⚠️ AU MIEUX, ET OUVERT EN CAS DE DOUTE — l'inverse de `PayoutOf`. Une base
// muette ne doit pas empêcher quelqu'un de recharger : le risque d'une borne
// par défaut est qu'un montant inhabituel passe, celui d'un refus est qu'une
// course ne soit pas payée.
func (s *Service) TopupOf(ctx context.Context, code string) TopupResponse {
	info, ok := country.Lookup(code)
	if !ok {
		return topupResponse("", Topup{})
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return topupResponse(info.Code, Topup{})
	}
	return topupResponse(info.Code, inst.Topup)
}

// UpdateTopup règle les bornes de recharge d'un pays.
func (s *Service) UpdateTopup(ctx context.Context, code string, req TopupUpdateRequest) (*TopupResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Min == nil && req.Max == nil && req.Presets == nil {
		return nil, errNoTopupUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Topup
	before := topupResponse(info.Code, cur)
	if req.Min != nil {
		cur.Min = *req.Min
	}
	if req.Max != nil {
		cur.Max = *req.Max
	}
	if req.Presets != nil {
		cur.Presets = rangés(*req.Presets)
	}
	// ⚠️ RELU FUSIONNÉ ET AVANT L'ÉCRITURE : un réglage impossible écrit puis
	// corrigé laisse une fenêtre où le pays ne peut plus recharger.
	merged := topupResponse(info.Code, cur)
	if err := checkTopupCoherence(merged); err != nil {
		return nil, err
	}
	if err := s.repo.SetTopup(ctx, info.Code, cur); err != nil {
		return nil, apperr.Internal(err)
	}
	// ⚠️ AU JOURNAL D'AUDIT : relever le plafond de recharge d'un pays est le
	// genre de geste dont on veut savoir qui l'a fait, le jour où un montant
	// inhabituel est entré.
	if s.audit != nil {
		s.audit.Record(ctx, "country.topup", "country", info.Code, before, merged)
	}
	return &merged, nil
}
