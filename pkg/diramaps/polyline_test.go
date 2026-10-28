package diramaps

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ LE CAS DE RÉFÉRENCE DE GOOGLE, repris tel quel : c'est la seule façon de
// savoir qu'on parle le même dialecte qu'un décodeur écrit ailleurs — celui de
// la console, celui d'une application mobile, celui d'un outil de débogage.
// Une implémentation « cohérente avec elle-même » peut être fausse de bout en
// bout sans qu'aucun aller-retour ne le dise.
func TestTheGoogleReferenceVector(t *testing.T) {
	// Le vecteur documenté par Google : (38.5, -120.2), (40.7, -120.95),
	// (43.252, -126.453) donne `_p~iF~ps|U_ulLnnqC_mqNvxq`+"`"+`@`.
	const want = "_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	pts := [][2]float64{{-120.2, 38.5}, {-120.95, 40.7}, {-126.453, 43.252}}
	assert.Equal(t, want, EncodePolyline(pts))

	back := DecodePolyline(want)
	require.Len(t, back, 3)
	for i, want := range pts {
		assert.InDelta(t, want[0], back[i][0], 1e-5, "lng du point %d", i)
		assert.InDelta(t, want[1], back[i][1], 1e-5, "lat du point %d", i)
	}
}

// ⚠️⚠️ L'ORDRE DES COORDONNÉES EST LE PIÈGE DE CE FICHIER. L'entrée est en
// `[lng, lat]` — GeoJSON, et l'ordre de toute cette plateforme — alors que
// l'algorithme encode `lat` d'abord. Inverser une fois de trop, ou une fois de
// moins, place un trajet de Lomé au large du golfe de Guinée : les deux
// coordonnées y sont petites et de même signe, donc rien n'a l'air cassé.
func TestTheLngLatOrderSurvivesTheRoundTrip(t *testing.T) {
	// Lomé : lng 1.22, lat 6.13. Inversé, on tombe à lng 6.13, lat 1.22 —
	// en mer, à 550 km de là, et toujours « au Togo » pour un œil distrait.
	lome := [][2]float64{{1.2208, 6.1281}, {1.2549, 6.1656}}
	back := DecodePolyline(EncodePolyline(lome))
	require.Len(t, back, 2)
	assert.InDelta(t, 1.2208, back[0][0], 1e-5, "le lng doit rester le lng")
	assert.InDelta(t, 6.1281, back[0][1], 1e-5, "la lat doit rester la lat")
	// Et le point est bien à terre : la longitude d'un trajet togolais est
	// PLUS PETITE que sa latitude.
	assert.Less(t, back[0][0], back[0][1], "coordonnées inversées : ce trajet est en mer")
}

// ⚠️ UNE CHAÎNE TRONQUÉE REND CE QU'ELLE PEUT, elle ne panique pas. Un tracé
// vient d'un service tiers et traverse une base de données ; le jour où il est
// coupé, mieux vaut un trait incomplet qu'un service qui s'arrête.
func TestATruncatedPolylineDoesNotPanic(t *testing.T) {
	full := EncodePolyline([][2]float64{{1.22, 6.12}, {1.25, 6.16}, {1.28, 6.19}})
	for i := 0; i <= len(full); i++ {
		assert.NotPanics(t, func() { DecodePolyline(full[:i]) }, "coupée à %d", i)
	}
	assert.Empty(t, DecodePolyline(""))
	assert.Len(t, DecodePolyline(full), 3)
	// ⚠️ ET ELLE N'INVENTE PAS LE POINT QUI MANQUE. Coupée au milieu de sa
	// dernière longitude, elle rend DEUX points — pas trois dont le dernier
	// serait à la bonne latitude et à une longitude inchangée, c'est-à-dire un
	// trajet qui finit par un trait horizontal vers nulle part.
	assert.Len(t, DecodePolyline(full[:len(full)-1]), 2,
		"un point incomplet a été inventé")
	// Et du charabia ne rend pas des points inventés aux quatre coins du monde.
	for _, p := range DecodePolyline("!!!!!!") {
		assert.LessOrEqual(t, math.Abs(p[0]), 180.0)
		assert.LessOrEqual(t, math.Abs(p[1]), 90.0)
	}
}

// ⚠️ LES COORDONNÉES SONT ARRONDIES, PAS TRONQUÉES. La différence est d'un
// cent-millième de degré — environ un mètre ici — et elle serait invisible sur
// un point. Mais une troncature tire TOUJOURS dans le même sens : sur les cinq
// cents points d'un itinéraire urbain, le tracé dérive d'un mètre du côté du
// sud-ouest sur toute sa longueur, et se décolle visiblement de la rue.
func TestTheCoordinatesAreRoundedNotTruncated(t *testing.T) {
	// 6.128149 × 1e5 = 612814,9 : arrondi 612815, tronqué 612814.
	pts := [][2]float64{{1.220849, 6.128149}}
	back := DecodePolyline(EncodePolyline(pts))
	require.Len(t, back, 1)
	assert.Equal(t, 1.22085, back[0][0], "longitude tronquée au lieu d'être arrondie")
	assert.Equal(t, 6.12815, back[0][1], "latitude tronquée au lieu d'être arrondie")
}

// ⚠️ ET LE TRACÉ ENCODÉ EST BIEN PLUS COURT QUE SA FORME BRUTE : c'est la
// raison du format. Un itinéraire urbain voyage à CHAQUE tour du suivi en
// direct, pour chaque course en cours.
func TestTheEncodedFormIsCompact(t *testing.T) {
	// Cinq cents points d'un itinéraire urbain, espacés d'une dizaine de mètres.
	pts := make([][2]float64, 0, 500)
	for i := 0; i < 500; i++ {
		pts = append(pts, [2]float64{1.2208 + float64(i)*0.0001, 6.1281 + float64(i)*0.00008})
	}
	encoded := EncodePolyline(pts)
	// Le GeoJSON équivalent — `[1.2208,6.1281],` — tourne autour de 22 octets
	// par point ; la polyline doit tenir sous le tiers.
	assert.Less(t, len(encoded), 500*22/3,
		"le tracé encodé pèse %d octets pour 500 points : le format ne sert plus à rien", len(encoded))
	assert.Len(t, DecodePolyline(encoded), 500)
}
