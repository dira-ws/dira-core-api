package payout

// L'AGRÉGATEUR DE PAIEMENT — le sens SORTANT.
//
// ⚠️⚠️ UN AGRÉGATEUR, PAS SIX OPÉRATEURS. Le sens ENTRANT (`internal/payment`)
// déclare déjà Orange Money, MTN, Moov et Wave derrière une abstraction, et
// c'est le bon modèle pour encaisser : chaque opérateur a sa page de paiement.
//
// Pour VERSER, c'est l'inverse. Brancher quatre opérateurs en direct veut dire
// quatre contrats marchands, quatre bacs à sable, quatre formats de signature,
// quatre comptes à approvisionner — et quatre endroits où l'argent peut se
// bloquer un vendredi soir. Un agrégateur en expose UN seul, décide lui-même
// vers quel opérateur router d'après le préfixe du numéro, et porte le
// flottant. C'est ce que ce contrat demande, et c'est le choix qui coûte le
// moins cher à exploiter.
//
// ⚠️ L'INTERFACE NE NOMME AUCUN OPÉRATEUR. Si l'on devait un jour revenir à des
// branchements directs, ce serait une implémentation de plus derrière cette
// interface — pas une réécriture du module.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// SendRequest est l'ordre de versement, tel qu'un agrégateur le comprend.
type SendRequest struct {
	// PayoutID est NOTRE identifiant, et il part chez l'agrégateur.
	//
	// ⚠️⚠️ C'EST LA CLÉ D'IDEMPOTENCE, et c'est ce qui évite de verser deux
	// fois. Un réseau qui coupe après l'envoi mais avant la réponse laisse une
	// demande dans un état inconnu ; la seule façon de reprendre sans risque
	// est de renvoyer LE MÊME identifiant et de laisser l'agrégateur dire
	// « déjà traité ». Un agrégateur qui ne le supporte pas n'est pas
	// utilisable pour verser de l'argent.
	PayoutID string
	// Phone est le numéro QUI REÇOIT, en E.164.
	Phone string
	// Amount en unité entière de la monnaie du pays.
	Amount   int
	Currency string
	// Reference est ce que le bénéficiaire verra sur son relevé d'opérateur.
	Reference string
}

// SendResult est ce que l'agrégateur rend.
type SendResult struct {
	// ProviderRef identifie le versement chez lui — c'est ce qu'on cite au
	// support quand l'argent n'est pas arrivé.
	ProviderRef string
	// Status : `sent` quand l'ordre est pris, `paid` quand l'agrégateur
	// confirme immédiatement (rare), `failed` quand il refuse tout de suite.
	Status string
	// FailureReason : la phrase de l'agrégateur, telle quelle.
	//
	// ⚠️ ON LA GARDE SANS LA TRADUIRE. « numéro non enregistré chez
	// l'opérateur » et « compte bénéficiaire plafonné » demandent deux gestes
	// différents, et les fondre en « échec du versement » oblige l'opérateur
	// du support à rappeler l'agrégateur pour apprendre ce qu'on savait déjà.
	FailureReason string
}

// Les états d'un versement chez l'agrégateur.
const (
	StatusSent   = "sent"
	StatusPaid   = "paid"
	StatusFailed = "failed"
)

// Event est une notification vérifiée de l'agrégateur.
type Event struct {
	ProviderRef string
	// PayoutID : notre identifiant, quand l'agrégateur le renvoie. ⚠️ On le
	// préfère à `ProviderRef` pour retrouver la demande : c'est NOTRE clé, et
	// elle existe même si l'ordre n'a jamais reçu de référence.
	PayoutID      string
	Status        string
	FailureReason string
}

// Provider abstrait l'agrégateur. Le concret dépend du pays et du contrat ;
// seuls l'abstraction et le bouchon sont livrés ici.
type Provider interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
	VerifyWebhook(payload []byte, signature string) (Event, error)
}

// ErrNoProvider : aucun agrégateur câblé.
//
// ⚠️ DIT CLAIREMENT PLUTÔT QUE RENDU COMME UNE PANNE : c'est un réglage
// manquant, et la personne qui le lit doit comprendre qu'il n'y a rien à
// réessayer.
var ErrNoProvider = errors.New("payout: no aggregator configured")

// MockProvider verse « dans le vide » — le développement et la recette.
//
// ⚠️ IL NE RÉUSSIT PAS TOUJOURS, ET C'EST VOLONTAIRE. Un bouchon qui réussit
// toujours laisse écrire un chemin d'échec que personne n'exécute jamais : le
// jour où un vrai versement échoue, c'est en production qu'on découvre que la
// remise au solde ne marchait pas. Un numéro se terminant par `00` échoue.
type MockProvider struct{ secret []byte }

const DefaultMockSecret = "mock-payout-secret"

func NewMockProvider(secret string) *MockProvider {
	if secret == "" {
		secret = DefaultMockSecret
	}
	return &MockProvider{secret: []byte(secret)}
}

func (p *MockProvider) Send(_ context.Context, req SendRequest) (SendResult, error) {
	if len(req.Phone) >= 2 && req.Phone[len(req.Phone)-2:] == "00" {
		return SendResult{
			Status:        StatusFailed,
			FailureReason: "numéro non enregistré chez l'opérateur (bouchon)",
		}, nil
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return SendResult{}, err
	}
	return SendResult{ProviderRef: "mock-" + hex.EncodeToString(b), Status: StatusSent}, nil
}

func (p *MockProvider) VerifyWebhook(payload []byte, signature string) (Event, error) {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write(payload)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(signature)) {
		return Event{}, fmt.Errorf("payout: bad webhook signature")
	}
	return decodeMockEvent(payload)
}
