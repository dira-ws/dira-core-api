package notify

// UNE APPLICATION QUI NE FAIT PLUS PARTIE DE DIRA — ce que le socle en garde.
//
// Une application cliente distincte a enregistré ici ses jetons de
// notification, marqués à part (`push_devices.app`), et lu sa boîte avec
// `?app=`. Elle ne fait plus partie de Dira. Deux traces restent, et elles
// demandent chacune une règle — pas un oubli.
//
// ⚠️⚠️ SES JETONS NE REÇOIVENT PLUS RIEN. Ils restent en base, et une course,
// une commande ou une campagne de Dira n'a rien à faire dans une application
// qui n'est plus la sienne : elle ne saurait pas l'ouvrir. C'est le DÉPÔT qui
// les écarte (`activeDevices`), pour que tous les chemins d'envoi — une
// notification, un appel, une campagne — en héritent sans qu'aucun ne puisse
// l'oublier.
//
// ⚠️ ET LA BOÎTE NE SE TRIE PLUS PAR APPLICATION, mais `?app=` N'EST PAS
// IGNORÉ : il est refusé. L'ignorer rendrait la boîte ENTIÈRE à qui demandait
// la sienne — les commandes et les courses du compte, dans une autre
// application.

import (
	"strings"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// servedDevices écarte les jetons d'une application que Dira ne sert plus.
//
// ⚠️ TOUT `app` NON VIDE EN EST UN : aucune application de Dira n'en a jamais
// porté — la cliente, Lite, celles des agents et des marchands s'enregistrent
// sans. Un jeton sans `app` (le champ absent, ou vide) est servi, comme avant.
func servedDevices(devices []Device) []Device {
	out := devices[:0]
	for _, d := range devices {
		if strings.TrimSpace(d.App) == "" {
			out = append(out, d)
		}
	}
	return out
}

// refuseAppParam refuse un `?app=` sur la boîte : il n'y a plus d'application à
// trier. Vide = la boîte entière, comme toujours.
func refuseAppParam(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return apperr.Validation("unknown app: " + raw).
		WithMeta(map[string]any{"fields": []string{"app"}, "reason": raw})
}
