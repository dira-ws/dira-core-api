package diramaps

// L'ENCODAGE DES TRACÉS — l'algorithme « polyline » de Google, précision 1e-5.
//
// ⚠️ ICI PLUTÔT QUE DANS CHAQUE VERTICALE, parce que c'est déjà la deuxième
// fois qu'il s'écrit sur cette plateforme (la livraison en a une copie privée
// dans `internal/delivery/geo.go`). Ce n'est pas une règle métier — un
// algorithme recopié ne divergera pas tout seul —, mais c'est le PAQUET DES
// CARTES, et c'est là qu'on vient chercher de quoi manipuler une géométrie.
//
// ⚠️ ET POURQUOI UNE POLYLINE PLUTÔT QUE DU GeoJSON : les deux tracés qu'une
// course porte déjà (`traveled_polyline`, `approach_polyline`) sont encodés
// ainsi, et la console sait déjà les décoder. Servir un troisième tracé dans
// une autre forme aurait obligé chaque lecteur à deux chemins de code pour la
// même chose. Accessoirement, un itinéraire urbain de cinq cents points pèse
// ~3 Ko encodé contre ~12 Ko en GeoJSON — et il voyage à chaque tour du suivi
// en direct.

import (
	"math"
	"strings"
)

// EncodePolyline encode des points `[lng, lat]` en polyline Google.
//
// ⚠️ L'ENTRÉE EST EN `[lng, lat]` (l'ordre de GeoJSON, celui de toute cette
// plateforme) et la SORTIE encode `lat` d'abord (l'ordre de l'algorithme).
// C'est le seul endroit où cette inversion a lieu ; la faire deux fois rend un
// tracé au large du golfe de Guinée, et la faire zéro fois aussi.
func EncodePolyline(points [][2]float64) string {
	var sb strings.Builder
	var prevLat, prevLng int64
	for _, p := range points {
		lat := int64(math.Round(p[1] * 1e5))
		lng := int64(math.Round(p[0] * 1e5))
		encodeValue(&sb, lat-prevLat)
		encodeValue(&sb, lng-prevLng)
		prevLat, prevLng = lat, lng
	}
	return sb.String()
}

// DecodePolyline rend les points `[lng, lat]` d'une polyline Google.
//
// ⚠️ TOLÉRANTE À UNE CHAÎNE TRONQUÉE : elle rend ce qu'elle a pu lire plutôt
// que de paniquer. Un tracé arrive d'un service tiers et traverse une base ;
// le jour où il est coupé, mieux vaut une carte avec un trait incomplet qu'un
// service qui s'arrête.
func DecodePolyline(encoded string) [][2]float64 {
	out := make([][2]float64, 0, len(encoded)/4)
	var lat, lng int64
	for i := 0; i < len(encoded); {
		dLat, n1 := decodeValue(encoded, i)
		dLng, n2 := decodeValue(encoded, i+n1)
		// ⚠️ UN POINT EST DEUX NOMBRES : s'il en manque un, le point n'existe
		// pas, et il ne faut surtout pas l'inventer. Sans ce garde-fou, une
		// chaîne coupée au milieu de sa dernière longitude rendait un point de
		// plus, à la bonne latitude et à une longitude inchangée — donc un
		// trajet qui finit par un trait horizontal vers nulle part.
		//
		// UN SEUL GARDE POUR LES DEUX MOITIÉS, et non deux : un garde par
		// nombre laissait le second rattraper le premier, et la ligne du
		// premier devenait inatteignable — une protection contre un cas qui ne
		// pouvait pas arriver.
		if n1 == 0 || n2 == 0 {
			break
		}
		i += n1 + n2
		lat += dLat
		lng += dLng
		out = append(out, [2]float64{float64(lng) / 1e5, float64(lat) / 1e5})
	}
	return out
}

func encodeValue(sb *strings.Builder, v int64) {
	u := v << 1
	if v < 0 {
		u = ^u
	}
	for u >= 0x20 {
		sb.WriteByte(byte(0x20|(u&0x1f)) + 63)
		u >>= 5
	}
	sb.WriteByte(byte(u) + 63)
}

// decodeValue rend la valeur et le nombre d'octets lus ; 0 si la chaîne se
// termine au milieu d'un nombre.
func decodeValue(s string, i int) (int64, int) {
	var shift uint
	var result int64
	start := i
	for i < len(s) {
		b := int64(s[i]) - 63
		i++
		result |= (b & 0x1f) << shift
		shift += 5
		if b < 0x20 {
			if result&1 != 0 {
				return ^(result >> 1), i - start
			}
			return result >> 1, i - start
		}
	}
	return 0, 0
}
