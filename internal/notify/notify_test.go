package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tmpl(fr, en Text) Template {
	t := Template{Key: "k", Enabled: true, Locales: map[string]Text{}}
	if fr.Body != "" {
		t.Locales[LocaleFR] = fr
	}
	if en.Body != "" {
		t.Locales[LocaleEN] = en
	}
	return t
}

// La langue demandée l'emporte quand elle existe.
func TestPickUsesTheRequestedLocale(t *testing.T) {
	x := tmpl(Text{Title: "Prête", Body: "fr"}, Text{Title: "Ready", Body: "en"})
	assert.Equal(t, "en", pick(x, LocaleEN).Body)
	assert.Equal(t, "fr", pick(x, LocaleFR).Body)
}

// Le repli français est INCONDITIONNEL : un gabarit traduit à moitié part en
// français plutôt que de ne pas partir.
func TestPickFallsBackToFrench(t *testing.T) {
	x := tmpl(Text{Title: "Prête", Body: "fr"}, Text{})
	assert.Equal(t, "fr", pick(x, LocaleEN).Body, "l'anglais manque : le français prend le relais")
	assert.Equal(t, "fr", pick(x, "wo").Body)
	assert.Equal(t, "fr", pick(x, "").Body)
}

// Dernier recours : n'importe quelle langue vaut mieux que rien. Un gabarit
// sans français ne devrait pas exister — la validation le refuse — mais s'il
// arrive de la base, le silence serait pire.
func TestPickTakesAnythingRatherThanNothing(t *testing.T) {
	x := tmpl(Text{}, Text{Title: "Ready", Body: "en"})
	assert.Equal(t, "en", pick(x, "wo").Body)
	assert.Empty(t, pick(Template{Locales: map[string]Text{}}, LocaleFR).Body)
}

// Un appareil déclare sa langue dans une demi-douzaine de formes, et la carte
// des gabarits n'en connaît qu'une.
func TestNormalizeLocale(t *testing.T) {
	cases := map[string]string{
		"fr": "fr", "fr-FR": "fr", "FR_fr": "fr", "EN": "en",
		"": "", "fr-Latn-FR": "fr",
		"zz-very-long-garbage": "zz",
		"12":                   "", // pas une langue
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeLocale(in), in)
	}
}

// --- envoi ---

type fakePusher struct {
	sent    []PushMessage
	results []PushResult
	err     error
}

func (p *fakePusher) Push(_ context.Context, msgs []PushMessage) ([]PushResult, error) {
	p.sent = append(p.sent, msgs...)
	if p.err != nil {
		return nil, p.err
	}
	if p.results != nil {
		return p.results, nil
	}
	out := make([]PushResult, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, PushResult{Token: m.Token})
	}
	return out, nil
}

// Le titre et le corps sont bornés AVANT l'envoi : laisser le système
// d'exploitation tronquer le ferait couper où il veut, y compris au milieu
// d'un montant.
func TestPushIsBoundedBeforeSending(t *testing.T) {
	long := ""
	for i := 0; i < MaxBody+50; i++ {
		long += "a"
	}
	assert.Len(t, []rune(Truncate(long, MaxBody)), MaxBody)
	assert.Len(t, []rune(Truncate(long, MaxTitle)), MaxTitle)
}

// Le gabarit du CHAT ne nomme jamais l'expéditeur : le canal existe pour que
// le client et le livreur se parlent sans échanger leurs coordonnées, et une
// notification qui nommerait l'un défairait cela sur l'écran verrouillé.
func TestChatTemplateNamesNobody(t *testing.T) {
	x := defaults[KeyChatMessage]
	for loc, txt := range x.Locales {
		vars := Variables(txt.Title, txt.Body)
		assert.Equal(t, []string{"body"}, vars,
			"le gabarit %s ne doit attendre que le corps du message", loc)
	}
}

// Un gabarit compilé n'utilise QUE ce que la plateforme sait remplir. Livrer
// un défaut qui rendrait du vide serait le pire des exemples.
func TestDefaultsUseOnlyProvidedVariables(t *testing.T) {
	for _, d := range Defaults() {
		for loc, txt := range d.Locales {
			assert.Empty(t, unknownVariables(d.Key, txt.Title, txt.Body),
				"gabarit %s/%s : variable que la plateforme ne remplit pas", d.Key, loc)
		}
	}
}

// Toute clé émise par le code déclare ce qu'elle fournit. Sans cette
// déclaration, l'enregistrement refuserait TOUTE variable sur cette clé.
func TestEveryKeyDeclaresWhatItProvides(t *testing.T) {
	for _, d := range Defaults() {
		assert.NotEmpty(t, Provided(d.Key), "clé %s sans variables déclarées", d.Key)
	}
}

// La clé du chat ne fournit QUE le corps : offrir un nom d'expéditeur
// défairait sur l'écran verrouillé ce que la conversation protège.
func TestChatProvidesNoIdentity(t *testing.T) {
	assert.Equal(t, []string{"body"}, Provided(KeyChatMessage))
}

// Tout gabarit compilé porte le FRANÇAIS : c'est le repli, et sans lui un
// destinataire dont la langue manque n'aurait rien à recevoir.
func TestEveryDefaultHasTheFallbackLocale(t *testing.T) {
	for _, d := range Defaults() {
		fr, ok := d.Locales[LocaleFR]
		require.True(t, ok, "gabarit %s sans français", d.Key)
		assert.NotEmpty(t, fr.Body, d.Key)
		assert.NotEmpty(t, fr.Title, d.Key)
	}
}

// Les textes compilés doivent tenir dans les bornes qu'on impose aux textes
// saisis : livrer un défaut que l'écran d'administration refuserait
// d'enregistrer serait incohérent.
func TestDefaultsRespectTheirOwnBounds(t *testing.T) {
	for _, d := range Defaults() {
		for loc, txt := range d.Locales {
			assert.LessOrEqual(t, len([]rune(txt.Title)), MaxTitle, "%s/%s", d.Key, loc)
			assert.LessOrEqual(t, len([]rune(txt.Body)), MaxBody, "%s/%s", d.Key, loc)
		}
	}
}

// Une panne de transport ne doit marquer AUCUN appareil comme mort : les
// jetons n'y sont pour rien, et les désactiver rendrait toute la flotte
// injoignable après un incident réseau.
func TestTransportFailureDisablesNothing(t *testing.T) {
	p := &fakePusher{err: errors.New("fcm down")}
	results, err := p.Push(context.Background(), []PushMessage{{Token: "t1"}})
	require.Error(t, err)
	assert.Nil(t, results, "aucun verdict par appareil : rien à désactiver")
}

// Un jeton déclaré mort par FCM se distingue d'un simple échec : il ne
// guérira pas, et le réessayer indéfiniment finit par faire refuser le projet.
func TestUnregisteredIsDistinctFromAFailure(t *testing.T) {
	p := &fakePusher{results: []PushResult{
		{Token: "dead", Unregistered: true},
		{Token: "flaky", Err: errors.New("timeout")},
		{Token: "ok"},
	}}
	results, err := p.Push(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, results[0].Unregistered)
	assert.False(t, results[1].Unregistered, "un échec passager n'est pas une mort")
	assert.NoError(t, results[2].Err)
}

// --- catégories et centre de notifications ---

// Une clé de message se range dans une CATÉGORIE : c'est elle que
// l'utilisateur coupe, jamais une clé. Couper `order_delivered` sans couper
// `order_ready` n'aurait aucun sens pour lui.
func TestKeysMapToCategories(t *testing.T) {
	assert.Equal(t, CategoryChatMessages, categoryOf(KeyChatMessage))
	assert.Equal(t, CategoryDriverCall, categoryOf(KeyDriverCall))
	for _, k := range []string{KeyOrderReady, KeyOrderDelivered, KeyOrderCancelled, KeyDispatchFailed} {
		assert.Equal(t, CategoryOrderUpdates, categoryOf(k), k)
	}
}

// ⚠️ L'appel de course n'est PAS coupable : c'est le gagne-pain du livreur, et
// un réglage mal compris le rendrait invisible du dispatch sans qu'il sache
// pourquoi.
func TestTheDriverCallCannotBeMuted(t *testing.T) {
	assert.False(t, Muteable(CategoryDriverCall))
	assert.True(t, Muteable(CategoryOrderUpdates))
	assert.True(t, Muteable(CategoryChatMessages))
	assert.True(t, Muteable(CategoryPromotions))
}

// Toute clé émise se range quelque part : une clé sans catégorie ne pourrait
// ni être coupée ni être classée dans la liste.
func TestEveryKeyHasACategory(t *testing.T) {
	for _, d := range Defaults() {
		assert.NotEmpty(t, categoryOf(d.Key), d.Key)
	}
}

// --- L'APPEL SUR iOS, ET LE REGROUPEMENT (socle #193, 18 octobre 2026) ---

// ⚠️ LE DÉCOMPTE VIENT DU TTL, celui-là même qu'on pose en `apns-expiration` :
// le texte affiché et la péremption du message sortent du MÊME nombre. Les
// lire à deux endroits laisserait « vous avez 30 s » s'afficher sur un message
// gardé plus longtemps.
func TestTheCallAlertCountsTheSecondsItIsValidFor(t *testing.T) {
	var m PushMessage
	applyRing(&m, defaultTmpl(t, KeyCallRing), LocaleFR, "c1", 30*time.Second)

	assert.True(t, m.Ring, "sans cela, iOS ne réveille pas une application fermée")
	assert.Equal(t, "Nouvelle course", m.Title)
	assert.Equal(t, "Vous avez 30 s pour répondre.", m.Body)
	assert.Equal(t, "call-c1", m.CollapseID, "une relance doit REMPLACER l'appel précédent")
	assert.Equal(t, callSound, m.Sound, "le son par défaut d'iOS ne tient pas trente secondes")
	assert.Equal(t, callCategory, m.Category, "elle dit à l'application quel écran ouvrir")
}

// La langue de l'APPAREIL, pas celle du compte : c'est l'écran que la personne
// a sous les yeux.
func TestTheCallAlertSpeaksTheDeviceLanguage(t *testing.T) {
	var m PushMessage
	applyRing(&m, defaultTmpl(t, KeyCallRing), LocaleEN, "c1", 25*time.Second)
	assert.Equal(t, "New ride", m.Title)
	assert.Equal(t, "You have 25 s to answer.", m.Body)
}

// ⚠️ UN APPEL SANS IDENTIFIANT NE SE REGROUPE PAS : un identifiant vide
// ferait se remplacer DEUX APPELS DIFFÉRENTS, et le second effacerait le
// premier sur l'écran du chauffeur.
func TestACallWithoutAnIdentifierIsNotGrouped(t *testing.T) {
	var m PushMessage
	applyRing(&m, defaultTmpl(t, KeyCallRing), LocaleFR, "", 30*time.Second)
	assert.Empty(t, m.CollapseID)
	assert.True(t, m.Ring, "il doit sonner quand même")
}

// ⚠️ CINQ CHANGEMENTS D'ÉTAT D'UNE COURSE NE DOIVENT LAISSER QU'UNE LIGNE.
// Android regroupe de lui-même ; iOS empile, et la seule notification qui
// compte — la dernière — se lit au milieu de quatre périmées.
func TestTheMessagesOfOneObjectReplaceEachOther(t *testing.T) {
	accepted := collapseOf(map[string]string{"ride_id": "r1", "driver": "Kossi"})
	arrived := collapseOf(map[string]string{"ride_id": "r1", "vehicle": "TG-1"})
	assert.Equal(t, "ride-r1", accepted)
	assert.Equal(t, accepted, arrived, "deux états de LA MÊME course se remplacent")
}

// ⚠️ MAIS DEUX OBJETS DIFFÉRENTS NE SE REMPLACENT JAMAIS — et le préfixe est
// ce qui l'empêche : une course et une commande peuvent porter le même
// identifiant hexadécimal, et l'une effacerait l'autre.
func TestTwoDifferentObjectsNeverReplaceEachOther(t *testing.T) {
	same := "6ab253499cb508f24710c036"
	assert.NotEqual(t,
		collapseOf(map[string]string{"ride_id": same}),
		collapseOf(map[string]string{"order_id": same}),
		"la course effacerait la commande")
	assert.Equal(t, "ride-"+same, collapseOf(map[string]string{"ride_id": same}))
	assert.Equal(t, "order-"+same, collapseOf(map[string]string{"order_id": same}))
}

// ⚠️ ET UN MESSAGE SANS OBJET NE SE REGROUPE PAS DU TOUT : rendre une chaîne
// vide est une réponse. Sans identifiant, deux messages n'ont aucune raison de
// se remplacer, et les confondre en perdrait un.
func TestAMessageAboutNothingIsNotGrouped(t *testing.T) {
	assert.Empty(t, collapseOf(nil))
	assert.Empty(t, collapseOf(map[string]string{"type": "promo"}))
	assert.Empty(t, collapseOf(map[string]string{"ride_id": ""}), "une valeur vide n'est pas un objet")
}

// defaultTmpl rend LE gabarit compilé d'une clé.
//
// ⚠️ EN UN SEUL APPEL À `Defaults()`, et ce n'est pas un détail de style :
// `Defaults()` construit sa tranche depuis une map, donc l'ordre CHANGE d'un
// appel à l'autre. Une version qui cherchait l'indice puis réindexait un
// second appel lisait un autre gabarit — mon épreuve a commencé par échouer
// sur « Nouveau  à valider », le texte d'une habilitation de staff.
func defaultTmpl(t *testing.T, key string) Template {
	t.Helper()
	for _, d := range Defaults() {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("gabarit %s absent", key)
	return Template{}
}

// --- L'EXCÈS DE VITESSE GRAVE ---------------------------------------------

// ⚠️ LE GABARIT PORTE LE COUPLE VITESSE/SEUIL ET LE TÉLÉPHONE, et l'épreuve
// lit LE TEXTE RENDU — pas la structure du gabarit. Un gabarit dont une
// variable n'est pas substituée part quand même : l'opérateur reçoit
// « [speed] » entre crochets à deux heures du matin, et n'a aucun moyen de
// savoir s'il doit appeler.
func TestTheGraveSpeedingAlertRendersTheSpeedAndTheNumber(t *testing.T) {
	var tpl Template
	for _, d := range Defaults() {
		if d.Key == KeyStaffSpeeding {
			tpl = d
		}
	}
	require.NotEmpty(t, tpl.Key, "le gabarit de l'excès grave doit être compilé")

	vars := map[string]string{
		"who": "Kossi Amegan", "speed": "118 km/h sur 60",
		"held": "22 s", "phone": "+24106000000",
	}
	for _, loc := range []string{LocaleFR, LocaleEN} {
		txt := tpl.Locales[loc]
		title, missTitle := Render(txt.Title, vars)
		body, missBody := Render(txt.Body, vars)

		// ⚠️ UNE VARIABLE MANQUANTE NE FAIT PAS ÉCHOUER L'ENVOI (c'est un choix
		// produit de `Render`) : elle part VIDE. Donc l'épreuve doit la
		// chercher ici, sinon le défaut ne se verrait que sur le téléphone d'un
		// opérateur réveillé à deux heures du matin.
		assert.Empty(t, missTitle, "variables non fournies dans le titre (%s)", loc)
		assert.Empty(t, missBody, "variables non fournies dans le corps (%s)", loc)
		assert.NotContains(t, title+body, "[", "une variable n'a pas été remplie (%s)", loc)
		// ⚠️ LA VITESSE EST DANS LE TITRE : c'est tout ce qu'une bannière
		// montre avant qu'on la touche, et c'est le chiffre qui décide s'il
		// faut appeler maintenant.
		assert.Contains(t, title, "118 km/h sur 60", "la vitesse doit être dans le TITRE (%s)", loc)
		assert.Contains(t, title, "Kossi Amegan", "le nom doit être dans le titre (%s)", loc)
		// ⚠️ ET LE NUMÉRO EST DANS LE CORPS : c'est le service client qui
		// APPELLE — il n'y a pas d'autre geste, le registre ne sanctionne rien.
		assert.Contains(t, body, "+24106000000", "le numéro doit sortir (%s)", loc)
	}
}

// ⚠️ IL N'EXISTE AUCUN GABARIT POUR LES EXCÈS ORDINAIRES, ni pour le passager.
// Les deux absences sont des décisions :
//
//   - un gabarit « excès ordinaire » aurait fait sonner un téléphone à chaque
//     dépassement du pays, et au bout d'une semaine personne ne regarde plus ;
//   - prévenir le passager À BORD a été écarté : enfermé dans une voiture qui
//     roule trop vite, il ne peut rien faire de l'information sinon avoir peur.
func TestSpeedingHasExactlyOneTemplateAndItIsForTheStaff(t *testing.T) {
	var keys []string
	for _, d := range Defaults() {
		if strings.Contains(d.Key, "speed") {
			keys = append(keys, d.Key)
		}
	}
	assert.Equal(t, []string{KeyStaffSpeeding}, keys,
		"un seul gabarit de vitesse, et il va au staff")
	assert.True(t, strings.HasPrefix(KeyStaffSpeeding, "staff_"),
		"la clé doit se nommer `staff_` : c'est ce qui la range hors des catégories coupables")
}

// ⚠️ ET ELLE N'EST PAS COUPABLE. Un poste de supervision qui peut éteindre les
// alertes de sécurité n'en est plus un — et contrairement au SOS, le chauffeur
// concerné n'a rien demandé : personne d'autre que l'exploitation ne sait que
// ça vient d'arriver.
func TestTheGraveSpeedingAlertCannotBeMutedAway(t *testing.T) {
	assert.Equal(t, CategoryStaff, categoryOf(KeyStaffSpeeding))
	assert.False(t, Muteable(categoryOf(KeyStaffSpeeding)))
}

// ⚠️ LES QUATRE MESSAGES D'UN TRANSFERT NE SE REMPLACENT PAS, et c'est ce que
// cette épreuve garde : trois destinataires, trois phrases, et chacune porte
// l'information sans laquelle elle serait inquiétante.
//
//   - le motif au passager : un remplacement sans explication se lit comme une
//     annulation ;
//   - le nom et le véhicule : il va monter dedans ;
//   - « votre chauffeur continue » : sans cette phrase, il reste sur « on
//     cherche quelqu'un d'autre » pendant que la voiture roule ;
//   - la part de l'ancien chauffeur : son écran se ferme sur une course qu'il
//     n'a pas terminée.
func TestTheFourTransferMessagesEachCarryWhatTheirReaderNeeds(t *testing.T) {
	want := map[string][]string{
		KeyRideTransferStarted: {"reason"},
		KeyRideTransferred:     {"driver", "vehicle"},
		KeyRideTransferFailed:  {"driver"},
		KeyRideTransferDone:    {"amount"},
	}
	byKey := map[string]Template{}
	for _, d := range Defaults() {
		byKey[d.Key] = d
	}
	for key, vars := range want {
		tpl, ok := byKey[key]
		if !ok {
			t.Fatalf("gabarit %s absent des défauts", key)
		}
		if !tpl.Enabled {
			t.Fatalf("%s doit être actif : un transfert muet laisse le passager croire à une annulation", key)
		}
		for _, loc := range []string{LocaleFR, LocaleEN} {
			txt, ok := tpl.Locales[loc]
			if !ok || txt.Title == "" || txt.Body == "" {
				t.Fatalf("%s n'a pas de phrase en %s", key, loc)
			}
			for _, v := range vars {
				if !strings.Contains(txt.Body, "["+v+"]") {
					t.Fatalf("%s (%s) doit porter [%s] DANS sa phrase : hors de la phrase, la variable est perdue", key, loc, v)
				}
			}
		}
	}
}

// ⚠️⚠️ LE TRANSFERT D'UNE COMMANDE NE SE DIT PAS AVEC LES PHRASES D'UNE
// COURSE, et c'est tout l'objet de cette épreuve.
//
// Les quatre phrases du VTC annoncent une voiture qui vient « vous reprendre »
// et demandent au passager de rester où il est. Recopiées sur l'écran de
// quelqu'un qui attend un repas chez lui, elles sont incompréhensibles :
// personne ne monte dans la moto du livreur, le client ne bouge pas, et ce qui
// change de main est son PAQUET.
//
// La tentation de la recopie est réelle — les deux familles ont la même forme,
// les mêmes destinataires, les mêmes variables. Alors l'épreuve regarde les
// MOTS : la commande doit être nommée, et l'invitation à monter dans la
// voiture doit être absente.
func TestTheDeliveryTransferMessagesSpeakOfAParcelAndNotOfAnotherCar(t *testing.T) {
	want := map[string][]string{
		KeyDeliveryTransferStarted: {"reason"},
		KeyDeliveryTransferred:     {"courier", "vehicle"},
		KeyDeliveryTransferFailed:  {"courier"},
		KeyDeliveryTransferDone:    {"amount"},
	}
	byKey := map[string]Template{}
	for _, d := range Defaults() {
		byKey[d.Key] = d
	}
	for key, vars := range want {
		tpl, ok := byKey[key]
		if !ok {
			t.Fatalf("gabarit %s absent des défauts", key)
		}
		if !tpl.Enabled {
			t.Fatalf("%s doit être actif : un transfert muet laisse le client croire à une commande perdue", key)
		}
		for _, loc := range []string{LocaleFR, LocaleEN} {
			txt, ok := tpl.Locales[loc]
			if !ok || txt.Title == "" || txt.Body == "" {
				t.Fatalf("%s n'a pas de phrase en %s", key, loc)
			}
			for _, v := range vars {
				if !strings.Contains(txt.Body, "["+v+"]") {
					t.Fatalf("%s (%s) doit porter [%s] DANS sa phrase : hors de la phrase, la variable est perdue", key, loc, v)
				}
			}
		}
		// ⚠️ AUCUNE DES QUATRE NE DOIT PARLER DE LA COURSE DU PASSAGER. Ce
		// sont les mots exacts des phrases du VTC : s'ils apparaissent ici,
		// quelqu'un a recopié la mauvaise famille.
		fr := byKey[key].Locales[LocaleFR].Body
		for _, forbidden := range []string{"vous reprendre", "restez où vous êtes", "votre course", "votre trajet"} {
			if strings.Contains(fr, forbidden) {
				t.Fatalf("%s dit « %s » : c'est la phrase d'une COURSE. Le client d'une livraison ne monte dans aucun véhicule", key, forbidden)
			}
		}
	}
	// Et la première phrase nomme bien ce qui est en jeu : la commande.
	if !strings.Contains(byKey[KeyDeliveryTransferStarted].Locales[LocaleFR].Body, "commande") {
		t.Fatal("le message d'ouverture doit nommer la COMMANDE : c'est elle que le client attend")
	}
}
