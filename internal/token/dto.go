package token

import "time"

// WalletResponse is the public representation of a wallet.
type WalletResponse struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id"`
	Type    string `json:"type"`
	Balance int    `json:"balance"`
	// TokenPriceXOF est le prix unitaire d'un jeton, rendu avec le solde pour
	// qu'un client puisse afficher le montant AVANT d'acheter. Sans lui,
	// chaque application recopierait la valeur et dériverait à la première
	// décision commerciale.
	TokenPriceXOF int `json:"token_price_xof"`
	// BalanceXOF est le solde en ARGENT : produit des ventes pour un marchand,
	// commissions pour un livreur. Un SECOND compteur, jamais mélangé aux
	// jetons — « solde 42 » ne dirait plus si l'on peut propulser un plat ou
	// retirer 42 francs.
	BalanceXOF int `json:"balance_xof"`
	// DebtXOF : ce que ce portefeuille DOIT à la plateforme (commission sur
	// espèces, paiement refusé). Remboursé d'office sur le prochain crédit.
	DebtXOF int `json:"debt_xof"`
	// PromoXOF est ce que la PLATEFORME a offert. Servi À PART et jamais
	// additionné : l'écran doit pouvoir dire « dont X offerts », et les deux
	// ne se remboursent pas pareil.
	PromoXOF int `json:"promo_xof,omitempty"`
	// SpendableXOF est ce dont le titulaire dispose RÉELLEMENT — la somme des
	// deux. Calculé par le serveur plutôt que laissé à l'application : c'est
	// le nombre sur lequel se prend la décision d'acheter, et deux clients
	// qui l'additionnent différemment afficheraient deux soldes.
	SpendableXOF int `json:"spendable_xof"`
	// Topup : DANS QUELLES BORNES on recharge ce portefeuille, et les montants
	// proposés d'un geste.
	//
	// ⚠️⚠️ SERVIE, PARCE QUE L'APPLICATION LES CODAIT EN DUR. Dira Lite
	// proposait 1 000 / 2 000 / 5 000 / 10 000 F et un montant libre de sept
	// chiffres — des nombres choisis pour le Togo, recopiés dans trois
	// applications, et dérisoires en Guinée où l'unité est quinze fois plus
	// petite. Le contrat ne fixait que `amount > 0`.
	//
	// ⚠️ LES MONTANTS SONT DANS LA MONNAIE DU PAYS, pas en francs CFA : la
	// réponse porte `currency` et `decimals` pour que l'écran n'ait rien à
	// deviner. Voir `internal/country/topup.go`.
	//
	// Absente quand ce portefeuille ne se recharge pas en argent (celui d'un
	// marchand s'alimente par ses ventes).
	Topup     *TopupResponse `json:"topup,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// TopupResponse recopie la politique du pays — déclarée ici pour que le
// portefeuille ne dépende pas du paquet des pays, et remplie au câblage.
type TopupResponse struct {
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Presets  []int  `json:"presets"`
	Currency string `json:"currency"`
	Decimals int    `json:"decimals"`
}

func newWalletResponse(w *Wallet, tokenPriceXOF int) WalletResponse {
	return WalletResponse{
		ID:            w.ID.Hex(),
		OwnerID:       w.OwnerID.Hex(),
		Type:          w.Type,
		Balance:       w.Balance,
		TokenPriceXOF: tokenPriceXOF,
		BalanceXOF:    w.BalanceXOF,
		DebtXOF:       w.DebtXOF,
		PromoXOF:      w.PromoXOF,
		SpendableXOF:  w.BalanceXOF + w.PromoXOF,
		UpdatedAt:     w.UpdatedAt,
	}
}

// TransactionResponse is the public representation of a wallet transaction.
type TransactionResponse struct {
	ID       string `json:"id"`
	WalletID string `json:"wallet_id"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Amount   int    `json:"amount"`
	// Unit dit QUELLE unité le mouvement déplace : `token` (absent en base
	// pour l'historique) ou `xof`. Sans elle, un relevé additionnerait des
	// jetons et des francs.
	Unit      string         `json:"unit"`
	RefID     string         `json:"ref_id,omitempty"`
	RefKind   string         `json:"ref_kind,omitempty"`
	Ref       map[string]any `json:"ref,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

func newTransactionResponse(t Transaction) TransactionResponse {
	resp := TransactionResponse{
		ID:        t.ID.Hex(),
		WalletID:  t.WalletID.Hex(),
		Kind:      t.Kind,
		Reason:    t.Reason,
		Amount:    t.Amount,
		Unit:      t.Unit,
		Ref:       t.Ref,
		CreatedAt: t.CreatedAt,
	}
	if resp.Unit == "" {
		resp.Unit = "token"
	}
	resp.RefKind = t.RefKind
	if t.RefID != nil {
		resp.RefID = t.RefID.Hex()
	}
	return resp
}

// PurchaseRequest buys tokens for the caller's wallet (driver) or for a store
// wallet the merchant owns (store_id required for merchants).
type PurchaseRequest struct {
	Tokens  int    `json:"tokens" validate:"required,gt=0"`
	StoreID string `json:"store_id,omitempty" validate:"omitempty,len=24,hexadecimal"`
}

// PurchaseResponse returns the payment initiated for a token purchase.
type PurchaseResponse struct {
	PaymentID string `json:"payment_id"`
	Tokens    int    `json:"tokens"`
	AmountXOF int    `json:"amount_xof"`
}

// OperatorCreditRequest grants tokens without payment. Le justificatif est
// obligatoire : c'est lui qui rend le geste défendable en revue de comptes.
type OperatorCreditRequest struct {
	Amount        int    `json:"amount" validate:"required,gt=0"`
	Justification string `json:"justification" validate:"required,min=3,max=500"`
}

// OperatorPromoRequest offre un crédit PROMOTIONNEL (XOF) à un client —
// geste commercial, dédommagement. Justificatif obligatoire, comme pour les
// jetons : il part au journal d'audit.
type OperatorPromoRequest struct {
	AmountXOF     int    `json:"amount_xof" validate:"required,gt=0"`
	Justification string `json:"justification" validate:"required,min=3,max=500"`
}

// BuyOptionRequest purchases a store option paid in tokens.
type BuyOptionRequest struct {
	Option string `json:"option" validate:"required"`
}

// BuyOptionResponse confirms an option purchase.
type BuyOptionResponse struct {
	Option string `json:"option"`
	Cost   int    `json:"cost"`
}

// BoostResponse confirms a dish boost.
type BoostResponse struct {
	DishID string `json:"dish_id"`
	Cost   int    `json:"cost"`
}
