package appconfig

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

// store est ce que le service demande à son dépôt.
type store interface {
	Load(ctx context.Context) (*appconfig.Registry, error)
	Save(ctx context.Context, reg appconfig.Registry, actor string) error
	Meta(ctx context.Context) (*time.Time, string)
}

// Service tient le registre, et le garde en mémoire un instant.
//
// ⚠️⚠️ UN CACHE TRÈS COURT, ET C'EST UN ARBITRAGE EXPLICITE. `/app/config` est
// lu à chaque ouverture d'application, et chaque route qui filtre une liste le
// lira aussi : sans cache, une plateforme à mille clients ferait mille
// lectures Mongo par minute pour un document qui change une fois par mois.
//
// Mais le cache est de QUINZE SECONDES, pas de dix minutes : retirer une
// fonction depuis la console doit se voir tout de suite. Un réglage qui met
// dix minutes à prendre effet se teste en croyant qu'il ne marche pas, et on
// finit par le poser deux fois.
type Service struct {
	repo store

	mu     sync.RWMutex
	cached *appconfig.Registry
	at     time.Time
}

// cacheFor borne la fraîcheur du registre en mémoire.
const cacheFor = 15 * time.Second

func NewService(repo store) *Service { return &Service{repo: repo} }

// Registry rend le registre, du cache ou de la base.
//
// ⚠️ UNE LECTURE QUI ÉCHOUE REND UN REGISTRE VIDE, PAS UNE ERREUR. Un registre
// vide ne restreint rien : si Mongo ne répond pas, les applications voient
// TOUT — c'est-à-dire se comportent comme avant cette fonctionnalité. L'autre
// sens aurait coupé des fonctions à cause d'une panne de base, et une panne de
// base ne doit pas devenir une panne de produit.
func (s *Service) Registry(ctx context.Context) *appconfig.Registry {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.at) < cacheFor {
		reg := s.cached
		s.mu.RUnlock()
		return reg
	}
	s.mu.RUnlock()

	reg, err := s.repo.Load(ctx)
	if err != nil || reg == nil {
		// Le dernier connu plutôt que rien ; rien plutôt qu'un refus.
		s.mu.RLock()
		last := s.cached
		s.mu.RUnlock()
		if last != nil {
			return last
		}
		return &appconfig.Registry{}
	}
	s.mu.Lock()
	s.cached, s.at = reg, time.Now()
	s.mu.Unlock()
	return reg
}

// Allows dit si une fonction est disponible pour CETTE requête.
//
// Le pays et l'édition viennent du contexte : c'est l'intergiciel qui les a
// posés, et les relire ailleurs ferait deux sources de vérité.
func (s *Service) Allows(ctx context.Context, feature string) bool {
	return s.Registry(ctx).Allows(feature, country.FromContext(ctx), edition.FromContext(ctx))
}

// Require refuse si la fonction est retirée de cette édition ou de ce pays.
//
// ⚠️ À POSER SUR L'ÉCRITURE, PAS SUR LA LECTURE. Une application d'une version
// antérieure, ou un appel direct, atteindra une route dont la fonction est
// coupée : c'est là qu'il faut refuser, nommément. Refuser aussi les lectures
// aurait cassé des écrans d'historique — une course programmée déjà créée
// reste une course qu'on doit pouvoir consulter après avoir coupé la fonction.
func (s *Service) Require(ctx context.Context, feature string) error {
	if s.Allows(ctx, feature) {
		return nil
	}
	return appconfig.Refuse(feature)
}

// AllowedIn réduit une liste servie par un module à ce que cette requête a le
// droit de voir.
func (s *Service) AllowedIn(ctx context.Context, list string, served []string) []string {
	return s.Registry(ctx).AllowedIn(list, country.FromContext(ctx), edition.FromContext(ctx), served)
}

// Resolve rend ce que cette requête voit — la réponse de `/app/config`.
func (s *Service) Resolve(ctx context.Context) appconfig.Resolved {
	return s.Registry(ctx).Resolve(country.FromContext(ctx), edition.FromContext(ctx))
}

// Save écrit le registre, après l'avoir validé.
//
// ⚠️⚠️ UNE CLÉ INCONNUE EST REFUSÉE, ET C'EST LE SEUL MOMENT OÙ ON PEUT LE
// FAIRE. Sans ce contrôle, `scheduled_ride` au singulier s'enregistrait,
// personne ne le lisait, la fonction restait visible dans Lite — et on
// cherchait la panne dans l'application pendant deux jours.
func (s *Service) Save(ctx context.Context, reg appconfig.Registry, actor string) error {
	clean := appconfig.Registry{}
	for key, rule := range reg.Features {
		if !appconfig.Known(key) {
			return apperr.Validation("unknown feature: " + key).
				WithMeta(map[string]any{"fields": []string{"features"}, "feature": key})
		}
		r, err := cleanRule(rule, false)
		if err != nil {
			return err
		}
		if clean.Features == nil {
			clean.Features = map[string]appconfig.Rule{}
		}
		clean.Features[key] = r
	}
	for key, rule := range reg.ListRules {
		if !appconfig.KnownList(key) {
			return apperr.Validation("unknown list: " + key).
				WithMeta(map[string]any{"fields": []string{"lists"}, "list": key})
		}
		r, err := cleanRule(rule, true)
		if err != nil {
			return err
		}
		if clean.ListRules == nil {
			clean.ListRules = map[string]appconfig.Rule{}
		}
		clean.ListRules[key] = r
	}
	if err := s.repo.Save(ctx, clean, actor); err != nil {
		return err
	}
	// ⚠️ LE CACHE EST VIDÉ TOUT DE SUITE : un opérateur qui vient de couper
	// une fonction et ne la voit pas disparaître la coupe une seconde fois.
	s.mu.Lock()
	s.cached, s.at = nil, time.Time{}
	s.mu.Unlock()
	return nil
}

// cleanRule normalise une règle et refuse ce qui ne s'appliquerait jamais.
//
// ⚠️ UNE ÉDITION INVENTÉE EST REFUSÉE À L'ÉCRITURE, là où un APPEL la pardonne
// (`edition.Normalize`). C'est la même distinction que `Known` / `Normalize` :
// sur le chemin d'un appel, une faute de frappe ne doit rien casser ; dans un
// réglage, elle produit une règle qui ne s'applique à rien et que personne ne
// pourra expliquer.
func cleanRule(rule appconfig.Rule, isList bool) (appconfig.Rule, error) {
	out := appconfig.Rule{}
	for _, e := range rule.Editions {
		if !edition.Known(e) {
			return out, apperr.Validation("unknown edition: " + e).
				WithMeta(map[string]any{"fields": []string{"editions"}})
		}
		out.Editions = append(out.Editions, strings.ToLower(strings.TrimSpace(e)))
	}
	for _, c := range rule.Countries {
		c = strings.ToUpper(strings.TrimSpace(c))
		if len(c) != 2 {
			return out, apperr.Validation("a country is two letters: " + c).
				WithMeta(map[string]any{"fields": []string{"countries"}})
		}
		out.Countries = append(out.Countries, c)
	}
	if !isList && len(rule.Allow) > 0 {
		// ⚠️ UNE LISTE BLANCHE SUR UNE FONCTION NE VEUT RIEN DIRE, et
		// l'accepter en silence aurait laissé croire qu'elle agit.
		return out, apperr.Validation("a feature has no whitelist — use a list for that").
			WithMeta(map[string]any{"fields": []string{"allow"}})
	}
	for _, a := range rule.Allow {
		if a = strings.TrimSpace(a); a != "" {
			out.Allow = append(out.Allow, a)
		}
	}
	// ⚠️⚠️ UNE RÈGLE QUI NE RETIRE RIEN EST REFUSÉE, PAS RANGÉE. Une règle
	// sans édition nommée ne mord sur personne ; une règle de liste sans
	// liste blanche ne restreint aucune valeur. Les deux s'enregistraient
	// sans broncher, et c'est ce qui fait tout #245 : la case était décochée
	// à l'écran, le serveur servait tout, et le réglage avait l'air posé.
	// Celui qui écrit l'apprend maintenant à l'écriture.
	if !out.Restricts(isList) {
		msg := "a rule must name at least one edition to take away"
		if isList && len(out.Editions) > 0 {
			msg = "a list rule needs its whitelist — without it nothing is restricted"
		}
		return appconfig.Rule{}, apperr.Validation(msg).
			WithMeta(map[string]any{"fields": []string{"editions", "allow"}})
	}
	return out, nil
}
