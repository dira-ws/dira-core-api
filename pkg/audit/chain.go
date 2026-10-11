package audit

// LE CHAÎNAGE DU JOURNAL — rendre une falsification VISIBLE.
//
// ⚠️⚠️ CE QUE CECI FAIT, ET CE QUE CECI NE FAIT PAS. Un journal « inviolable »
// n'existe pas tant que quelqu'un a accès à la base : il peut toujours
// supprimer une ligne. Ce que le chaînage apporte est autre chose, et c'est ce
// qui compte : il rend cette suppression **DÉTECTABLE**. Chaque entrée porte
// l'empreinte de la précédente ; retirer, réécrire ou réordonner une seule
// ligne casse la chaîne à cet endroit exact, et la vérification dit **où**.
//
// La différence tient en un mot : ce n'est pas « infalsifiable », c'est
// « falsification démontrable ». Promettre le premier serait mentir à un
// groupe de crédit-bail qui, lui, sait lire un rapport d'audit.
//
// ⚠️⚠️ LE SCELLEMENT EST ASYNCHRONE, ET CE N'EST PAS UN RACCOURCI.
//
// Une chaîne demande un ORDRE : pour calculer l'empreinte d'une entrée, il faut
// connaître la précédente. Le faire à l'écriture obligerait à sérialiser toutes
// les écritures du journal derrière un verrou — et ce paquet pose la règle
// inverse en toutes lettres : « une écriture d'audit ne doit pas casser
// l'opération qu'elle documente ». Un verrou global sur le journal ferait
// exactement ça, un soir de forte charge.
//
// Les entrées sont donc écrites comme avant, sans rien attendre, puis scellées
// par passes dans l'ordre de leur `_id` — qui est monotone et porte l'instant
// de création. Une entrée non encore scellée est une entrée **récente**, pas
// une entrée suspecte.
//
// ⚠️ ET L'EMPREINTE PORTE SUR CE QUE LA BASE CONTIENT, pas sur ce que
// l'appelant a passé. C'est la seule façon pour la vérification de la
// recalculer : elle relit les mêmes documents, par le même chemin, et retrouve
// les mêmes octets. Hacher l'objet d'origine aurait donné une chaîne
// invérifiable dès le premier aller-retour BSON.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GenesisHash ouvre la chaîne. ⚠️ Une constante nommée plutôt qu'une chaîne
// vide : « pas de précédent » et « précédent effacé » ne doivent pas se
// ressembler dans un rapport.
const GenesisHash = "genesis"

// Sealed dit si cette entrée est entrée dans la chaîne.
func (e Entry) Sealed() bool { return e.Hash != "" }

// chainable est l'entrée telle qu'elle est HACHÉE : les champs qui engagent,
// et rien d'autre.
//
// ⚠️ `seq` ET `prev` EN FONT PARTIE, sinon réordonner deux entrées ne casserait
// rien — or l'ordre EST l'information dans un journal d'audit : « il a
// suspendu le compte PUIS viré l'argent » ne dit pas la même chose que
// l'inverse.
type chainable struct {
	Seq       int64           `json:"seq"`
	Prev      string          `json:"prev"`
	Service   string          `json:"service"`
	Country   string          `json:"country"`
	ActorID   string          `json:"actor_id"`
	ActorRole string          `json:"actor_role"`
	Action    string          `json:"action"`
	Resource  Resource        `json:"resource"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
	IP        string          `json:"ip"`
	CreatedAt string          `json:"created_at"`
}

// hashOf calcule l'empreinte d'une entrée à sa place dans la chaîne.
//
// ⚠️ JSON CANONIQUE : `encoding/json` trie les clés d'une map, et une valeur
// relue de Mongo arrive en `primitive.M` — donc en map. Deux lectures du même
// document donnent les mêmes octets, ce qui est exactement ce qu'il faut.
func hashOf(seq int64, prev string, e Entry) (string, error) {
	before, err := json.Marshal(e.Before)
	if err != nil {
		return "", fmt.Errorf("audit: hash before: %w", err)
	}
	after, err := json.Marshal(e.After)
	if err != nil {
		return "", fmt.Errorf("audit: hash after: %w", err)
	}
	raw, err := json.Marshal(chainable{
		Seq: seq, Prev: prev,
		Service: e.Service, Country: e.Country,
		ActorID: e.ActorID, ActorRole: e.ActorRole,
		Action: e.Action, Resource: e.Resource,
		Before: before, After: after, IP: e.IP,
		// ⚠️ EN NANOSECONDES UTC : tronquer à la seconde ferait deux entrées
		// indiscernables dans une rafale, et une chaîne dont l'ordre ne se
		// démontre plus.
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf("audit: hash entry: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// head rend la dernière entrée scellée — le bout de la chaîne.
func (r *Recorder) head(ctx context.Context) (seq int64, hash string, err error) {
	var last Entry
	opts := options.FindOne().SetSort(bson.D{{Key: "seq", Value: -1}})
	switch e := r.col.FindOne(ctx, bson.M{"seq": bson.M{"$exists": true}}, opts).Decode(&last); {
	case e == mongo.ErrNoDocuments:
		return 0, GenesisHash, nil
	case e != nil:
		return 0, "", e
	}
	return last.Seq, last.Hash, nil
}

// SealResult dit ce qu'une passe a fait.
type SealResult struct {
	Sealed int   `json:"sealed"`
	From   int64 `json:"from_seq,omitempty"`
	To     int64 `json:"to_seq,omitempty"`
}

// Seal scelle les entrées en attente, dans l'ordre de leur `_id`.
//
// ⚠️ `_id` ET NON `created_at`. L'identifiant d'objet Mongo est monotone et
// porte l'instant de création ; `created_at` vient d'une verticale, donc d'une
// AUTRE horloge, et deux serveurs légèrement désynchronisés suffiraient à
// sceller dans un ordre que la vérification ne retrouverait pas.
//
// ⚠️ IDEMPOTENTE ET REPRENABLE : une passe interrompue laisse des entrées non
// scellées, que la suivante reprend. Rien n'est jamais re-scellé — le filtre
// porte sur l'absence de `seq`.
func (r *Recorder) Seal(ctx context.Context, limit int) (SealResult, error) {
	if r.col == nil {
		return SealResult{}, fmt.Errorf("audit: no collection to seal")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	seq, prev, err := r.head(ctx)
	if err != nil {
		return SealResult{}, fmt.Errorf("audit: read head: %w", err)
	}

	cur, err := r.col.Find(ctx,
		bson.M{"seq": bson.M{"$exists": false}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return SealResult{}, fmt.Errorf("audit: find unsealed: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	out := SealResult{From: seq + 1}
	for cur.Next(ctx) {
		var e Entry
		if err := cur.Decode(&e); err != nil {
			return out, fmt.Errorf("audit: decode: %w", err)
		}
		seq++
		// ⚠️ PAR `Link`, comme les tests : deux façons de poser un maillon
		// finiraient par en poser deux différents.
		linked, err := Link(seq, prev, e)
		if err != nil {
			return out, err
		}
		update := bson.M{"$set": bson.M{
			"seq": linked.Seq, "prev_hash": linked.PrevHash, "hash": linked.Hash,
			"sealed_at": time.Now().UTC(),
		}}
		// ⚠️ LA CONDITION `seq` ABSENT EST LE VERROU. Deux instances qui
		// scelleraient en même temps : la seconde ne trouve rien à écrire sur
		// les entrées déjà prises, et sa passe s'arrête proprement plutôt que
		// de produire deux chaînes concurrentes.
		res, err := r.col.UpdateOne(ctx,
			bson.M{"_id": e.ID, "seq": bson.M{"$exists": false}}, update)
		if err != nil {
			return out, fmt.Errorf("audit: seal %s: %w", e.ID.Hex(), err)
		}
		if res.MatchedCount == 0 {
			return out, nil
		}
		prev = linked.Hash
		out.Sealed++
		out.To = seq
	}
	return out, cur.Err()
}

// VerifyResult est le rapport d'une vérification.
//
// ⚠️ IL NOMME L'ENTRÉE QUI CASSE, ET LA PREMIÈRE SEULEMENT. Au-delà de la
// première rupture, tout le reste est faux par construction : lister mille
// entrées « cassées » noierait la seule qui compte.
type VerifyResult struct {
	OK      bool  `json:"ok"`
	Checked int   `json:"checked"`
	From    int64 `json:"from_seq,omitempty"`
	To      int64 `json:"to_seq,omitempty"`
	// Unsealed : les entrées pas encore entrées dans la chaîne. ⚠️ CE N'EST
	// PAS UNE ANOMALIE — ce sont les plus récentes, en attente de la prochaine
	// passe.
	Unsealed int `json:"unsealed"`
	// BrokenAt, BrokenID, Reason : là où ça casse, et pourquoi.
	BrokenAt int64  `json:"broken_at_seq,omitempty"`
	BrokenID string `json:"broken_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Les trois façons dont une chaîne casse, nommées.
const (
	// BreakMissing : un numéro de séquence manque — une entrée a été
	// SUPPRIMÉE.
	BreakMissing = "entry_missing"
	// BreakContent : l'empreinte ne correspond plus au contenu — une entrée a
	// été RÉÉCRITE.
	BreakContent = "entry_rewritten"
	// BreakLink : le maillon ne pointe pas sur le précédent — la chaîne a été
	// RECOUSUE.
	BreakLink = "chain_relinked"
)

// Verify relit la chaîne et dit où elle casse.
func (r *Recorder) Verify(ctx context.Context, limit int) (VerifyResult, error) {
	if r.col == nil {
		return VerifyResult{}, fmt.Errorf("audit: no collection to verify")
	}
	if limit <= 0 || limit > 100000 {
		limit = 100000
	}
	n, err := r.col.CountDocuments(ctx, bson.M{"seq": bson.M{"$exists": false}})
	if err != nil {
		return VerifyResult{}, err
	}
	out := VerifyResult{OK: true, Unsealed: int(n)}

	cur, err := r.col.Find(ctx,
		bson.M{"seq": bson.M{"$exists": true}},
		options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return out, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var (
		prev     = GenesisHash
		expected int64
		first    = true
	)
	for cur.Next(ctx) {
		var e Entry
		if err := cur.Decode(&e); err != nil {
			return out, err
		}
		if first {
			expected, out.From, first = e.Seq, e.Seq, false
		}
		// ⚠️ UN TROU DANS LES NUMÉROS EST UNE SUPPRESSION, et c'est le cas le
		// plus probable : effacer une ligne est plus simple que la réécrire.
		reason, detail, err := Step(prev, expected, e)
		if err != nil {
			return out, err
		}
		if reason != "" {
			return broken(out, e, reason, detail), nil
		}
		prev, expected, out.To = e.Hash, expected+1, e.Seq
		out.Checked++
	}
	return out, cur.Err()
}

func broken(out VerifyResult, e Entry, reason, detail string) VerifyResult {
	out.OK = false
	out.BrokenAt = e.Seq
	out.BrokenID = idHex(e.ID)
	out.Reason = reason + ": " + detail
	return out
}

func idHex(id primitive.ObjectID) string {
	if id.IsZero() {
		return ""
	}
	return id.Hex()
}

// EnsureChainIndexes pose ce que le scellement et la vérification demandent.
func (r *Recorder) EnsureChainIndexes(ctx context.Context) error {
	if r.col == nil {
		return nil
	}
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// ⚠️ UNIQUE ET PARTIEL : deux entrées ne peuvent pas porter le même
		// numéro, et les non scellées (sans `seq`) ne se gênent pas entre
		// elles. Sans l'unicité, deux scellements concurrents produiraient
		// deux chaînes valides et contradictoires.
		{
			Keys: bson.D{{Key: "seq", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("audit_seq").
				SetPartialFilterExpression(bson.M{"seq": bson.M{"$exists": true}}),
		},
		// La file des entrées à sceller.
		{Keys: bson.D{{Key: "_id", Value: 1}, {Key: "seq", Value: 1}}, Options: options.Index().SetName("audit_to_seal")},
	})
	return err
}

// Step vérifie UNE entrée à sa place dans la chaîne. Rend la rupture, ou "".
//
// ⚠️⚠️ EXTRAITE EXPRÈS DE LA BOUCLE, et c'est ce qui rend la garantie
// démontrable. La lecture de Mongo est mécanique ; ce qui mérite d'être prouvé,
// c'est que supprimer, réécrire ou réordonner une entrée est bien DÉTECTÉ. Une
// fonction pure se teste sans base, donc elle se teste vraiment.
//
// ⚠️ L'ORDRE DES TROIS CONTRÔLES COMPTE. Le trou de séquence se voit en
// premier parce que c'est le cas le plus probable : effacer une ligne est plus
// simple que la réécrire. Le signaler en « maillon recousu » enverrait chercher
// une réécriture là où il y a eu une suppression.
func Step(prev string, expected int64, e Entry) (reason, detail string, err error) {
	if e.Seq != expected {
		return BreakMissing, fmt.Sprintf(
			"séquence %d attendue, %d trouvée — une entrée a été supprimée", expected, e.Seq), nil
	}
	if e.PrevHash != prev {
		return BreakLink,
			"le maillon ne pointe pas sur l'entrée précédente — la chaîne a été recousue", nil
	}
	h, err := hashOf(e.Seq, e.PrevHash, e)
	if err != nil {
		return "", "", err
	}
	if h != e.Hash {
		return BreakContent,
			"l'empreinte ne correspond plus au contenu — l'entrée a été réécrite", nil
	}
	return "", "", nil
}

// Link scelle une entrée à la suite de `prev` — la pose d'un maillon, isolée
// pour que les tests puissent construire une vraie chaîne sans base.
func Link(seq int64, prev string, e Entry) (Entry, error) {
	h, err := hashOf(seq, prev, e)
	if err != nil {
		return e, err
	}
	e.Seq, e.PrevHash, e.Hash = seq, prev, h
	return e, nil
}
