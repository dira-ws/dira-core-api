package payment

import "time"

// InitiatePaymentRequest is the body of POST /payments/initiate.
type InitiatePaymentRequest struct {
	Purpose string `json:"purpose" validate:"required,oneof=order ride ride_subscription token_purchase wallet_topup"`
	Amount  int    `json:"amount" validate:"required,gt=0"`
	// RefID is the order id (purpose "order") or the wallet owner id
	// (purpose "token_purchase").
	RefID    string `json:"ref_id" validate:"required"`
	Provider string `json:"provider" validate:"omitempty"` // defaults to "mock"
	// Tokens is required (>0) for purpose "token_purchase".
	Tokens int `json:"tokens" validate:"omitempty,gt=0"`
	// IdempotencyKey rend une initiation RÉPÉTABLE sans risque.
	//
	// ⚠️ ELLE SE LIT AUSSI DANS L'EN-TÊTE `Idempotency-Key`, qui l'emporte.
	// L'en-tête est la convention, et c'est ce que les applications
	// demandaient ; le champ existe parce que `httpx.Decode` refuse les
	// champs inconnus, et qu'une application qui l'aurait deviné dans le
	// corps aurait récolté un `422` au lieu d'un paiement.
	IdempotencyKey string `json:"idempotency_key" validate:"omitempty,max=128"`
}

// PaymentResponse is the public representation of a payment.
type PaymentResponse struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	Purpose     string         `json:"purpose"`
	RefID       string         `json:"ref_id"`
	Provider    string         `json:"provider"`
	Amount      int            `json:"amount"`
	Currency    string         `json:"currency"`
	Status      string         `json:"status"`
	ProviderRef string         `json:"provider_ref"`
	Meta        map[string]any `json:"meta,omitempty"`
	// PaymentURL est l'adresse où le payeur termine chez l'opérateur. Servie
	// ICI AUSSI, et pas seulement à l'initiation : c'est elle qui permet de
	// reprendre un paiement dont la réponse s'est perdue.
	PaymentURL string `json:"payment_url,omitempty"`
	// OperatorReached dit si l'opérateur a répondu à la demande.
	//
	// ⚠️ `false` SUR UN PAIEMENT `pending` NE VEUT PAS DIRE « RIEN N'EST
	// PARTI ». Il veut dire « on a demandé et on ne sait pas » : une demande
	// de paiement peut dormir sur le téléphone du client. L'application
	// propose de REPRENDRE (même `Idempotency-Key`), jamais de repayer à neuf.
	OperatorReached bool `json:"operator_reached"`
	// ExpiresAt borne l'attente d'un paiement `pending` — après quoi il ne
	// vaut plus la peine d'être attendu. ⚠️ Rien ne le marque « échoué » à
	// cette date : un rappel tardif doit encore pouvoir le faire aboutir.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// PaymentListResponse is returned by GET /payments.
type PaymentListResponse struct {
	Items []PaymentResponse `json:"items"`
	// NextCursor est l'identifiant à repasser en `?cursor=` — vide quand la
	// page est la dernière.
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// InitiatePaymentResponse is returned by POST /payments/initiate.
type InitiatePaymentResponse struct {
	Payment PaymentResponse `json:"payment"`
	// PaymentURL is where the payer completes the mobile-money flow.
	PaymentURL string `json:"payment_url"`
}

// WebhookAck is the body returned to providers on accepted webhooks.
type WebhookAck struct {
	Received bool `json:"received"`
}

func toPaymentResponse(p *Payment) PaymentResponse {
	resp := PaymentResponse{
		ID:          p.ID.Hex(),
		UserID:      p.UserID.Hex(),
		Purpose:     p.Purpose,
		RefID:       p.RefID.Hex(),
		Provider:    p.Provider,
		Amount:      p.Amount,
		Currency:    p.Currency,
		Status:      p.Status,
		ProviderRef: p.ProviderRef,
		Meta:        p.Meta,
		PaymentURL:  p.PaymentURL,
		// ⚠️ La référence d'ATTENTE ne sort pas : elle n'est pas la référence
		// de l'opérateur, et une application qui l'afficherait au support
		// enverrait chercher chez l'opérateur une référence qu'il ne connaît
		// pas.
		OperatorReached: p.Reached,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
	if IsReservedRef(resp.ProviderRef) {
		resp.ProviderRef = ""
	}
	if !p.ExpiresAt.IsZero() {
		expires := p.ExpiresAt
		resp.ExpiresAt = &expires
	}
	return resp
}
