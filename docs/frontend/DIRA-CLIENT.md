# App CLIENT UNIFIÉE — LIVRAISON **et** COURSES — contrat d'API

> **Version 4.95.0** · 29 octobre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Livraison : `…/api/v1/food` · Courses : `…/api/v1/vtc` · Combiné : `…/api/v1/analytics` · Suivi : `wss://tracking-staging.dira.llc`

---

> ## Ce que cette application est
>
> **Une seule application pour un client Dira**, qui commande à manger **et**
> prend des taxis. Un compte, un portefeuille, une boîte de notifications, un
> historique — et deux métiers à l'intérieur.
>
> Jusqu'ici c'étaient deux applications (`VTC-CLIENT`, `FOOD-CLIENT`), parce que
> le serveur sert deux API. Ce document décrit **l'application qui les réunit** :
> ce qui est partagé, ce qui est nouveau parce qu'on les réunit, et ce qui ne
> change pas.
>
> ### ⚠️ Ce que ce document contient, et ce qu'il ne contient pas
>
> | | |
> |---|---|
> | **En entier, ici** | les bases d'URL, les conventions, le pays, le fil de requête, le fond de carte, l'identité et l'argent, **l'assistant unifié**, **le fil d'activité**, les deux canaux temps réel, la boîte de notifications des deux métiers, les codes d'erreur |
> | **Inchangé, dans la spec du métier** | commander un repas (catalogue, panier, livraison) → `FOOD-CLIENT` · commander une course (devis, modes, suivi du chauffeur) → `VTC-CLIENT` |
>
> ⚠️ **ET C'EST UN CHOIX, PAS UN OUBLI.** Recopier ici les deux mille cinq cents
> lignes des deux parcours métier aurait fait **deux sources de vérité pour les
> mêmes routes** — et elles auraient divergé au premier changement, sans que
> personne ne sache laquelle croire. Le §10 donne l'inventaire **route par
> route** de ce qui vit où, pour qu'on n'ait jamais à chercher.
>
> Tout ce qui est **transverse ou combiné** est écrit ici **en entier** : c'est
> précisément ce qu'une application unifiée doit tenir, et ce qu'aucune des deux
> specs métier ne peut dire à votre place.

---

## 1. ⚠️ QUATRE BASES D'URL, et se tromper est SILENCIEUX

| Ce que vous appelez | Service | Base |
|---|---|---|
| connexion, profil, adresses, **portefeuille**, paiements, notifications, fichiers, pays | **socle** | `…/api/v1/…` |
| catalogue, panier, commandes, nutrition, tombola | **livraison** | `…/api/v1/food/…` |
| devis, courses, classes de véhicule, abonnements | **courses** | `…/api/v1/vtc/…` |
| **l'assistant unifié et le fil d'activité** (§8, §9) | **combiné** | `…/api/v1/analytics/…` |

**Le jeton est le même partout** : même secret, même session. On ne se connecte
pas quatre fois.

⚠️ **UN APPEL SUR LA MAUVAISE BASE COMPILE, PASSE LES TESTS, ET REND `404`
DEVANT UN CLIENT.** `GET /api/v1/orders` n'existe pas ; c'est
`GET /api/v1/food/orders`. Prévoyez **quatre** variables de configuration, et
une règle vérifiable à la construction : d'où vient cette route ? Une
application qui dérive la base d'une table de chemins finira par en oublier un.

⚠️ **POURQUOI LE COMBINÉ EST SOUS `/analytics`.** C'est le service qui détient
le modèle de langage de la plateforme **et** qui interroge déjà les deux
métiers ; le socle, lui, a pour règle de ne rien demander à une verticale. Le
nom du préfixe décrit le service, pas votre écran : lisez-le comme « le service
qui compose ».

---

## 2. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next": "…" }` |
| Montants | **entiers**, dans la monnaie du pays. Jamais de flottant. |
| Dates | ISO 8601 UTC. Une **date seule** s'écrit `YYYY-MM-DD`. |
| Langue | `Accept-Language` |
| Pays | `X-Dira-Country: TG` — sur chaque requête (§4) |
| Édition | `X-Dira-Edition: full` \| `lite` — sur chaque requête (§4 bis) |
| Fil | `X-Request-ID` — sur chaque réponse (§3) |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat.

**Un `422 validation_failed` nomme ses champs.** `fields` liste les **clés
JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le
formulaire. `reason` précise quand ce n'est pas la valeur d'un champ :
`unknown_field` (une clé que la route ne connaît pas — **refusée, pas
ignorée** : c'est un bug de l'application) ou `invalid_json`.

⚠️⚠️ **IL N'Y A PAS DE `meta` SUR LE FIL.** L'enveloppe rend `code`,
`message`, et trois champs **à plat** : `fields`, `reason`, `feature`.

La règle qui décide de ce qui voyage en clair : **ce qu'une personne LIT reste
dans la phrase ; ce que l'application doit FAIRE voyage à plat.**

- Un refus qui porte des **chiffres** (« ce trajet fait 2 100 m, la course
  partagée commence à 3 000 m ») les a **dans la phrase**, déjà traduite.
  Affichez-la telle quelle, ou recomposez-la depuis les réglages déjà lus.
- `fields` dit **quelles cases** souligner. `feature` dit **quel menu** cacher.
  Ni l'un ni l'autre ne se devine d'une phrase — il faudrait l'analyser, en
  douze langues.

⚠️ Si un passage d'un contrat vous promet un champ sous `meta.…`, **c'est une
erreur de rédaction** : le champ n'existe pas, et il n'a jamais existé. Un
audit est en cours (core#252) ; d'ici là, fiez-vous à cette liste.

---

## 3. 🧵 LE FIL D'UNE REQUÊTE — `X-Request-ID`

Chaque réponse porte `X-Request-ID`. À l'aller il est facultatif : si vous en
envoyez un, il est **repris** ; sinon le serveur en fabrique un.

**Ce qu'on vous demande : le montrer, et le joindre.** Un identifiant affiché
en petit sur un écran d'erreur, et joint à un rapport de bogue, permet de
retrouver la requête exacte dans les journaux des **quatre** services —
y compris les appels que le service combiné a faits aux deux métiers en votre
nom.

⚠️ **CELA COMPTE DOUBLE POUR UNE APPLICATION UNIFIÉE.** Un « ça ne marche
pas » sur l'assistant peut venir de trois services ; sans le fil, il faut
chercher dans trois journaux sans savoir dans lequel regarder.

---

## 4. ⚠️ Le PAYS — `X-Dira-Country`

Chaque compte, commande et course porte un pays. **Envoyez l'en-tête sur
chaque requête** ; la réponse porte le pays retenu.

**Les pays ouverts** : `GET /countries` (socle, public). Chaque entrée porte
`code`, `name`, `currency`, `phone_prefix`, `locale`, `timezone`, `basemap`
(§5) et `testing` (un pays d'essai, à ne pas montrer).

⚠️ **LA MONNAIE VIENT DU PAYS, PAS DU COMPTE.** Un montant est un entier dans
la monnaie de son pays : un Togolais qui commande à Dakar voit des francs CFA
du Sénégal, et à Conakry des francs guinéens. Une application qui formaterait
avec la monnaie du **compte** écrirait le bon nombre derrière le mauvais
symbole.

⚠️ **UNE OPÉRATION SE FAIT LÀ OÙ ELLE SE FAIT.** Le pays d'une course vient de
son **point de départ**, celui d'une commande de l'enseigne. Un client en
voyage commande normalement — et son numéro garde son indicatif.

⚠️ **LE PORTEFEUILLE NE TRAVERSE PAS UNE MONNAIE.** Un solde est un entier dans
la monnaie de son pays : payer au solde une course facturée en francs guinéens
depuis un solde en francs CFA est **refusé**. Les espèces et le paiement en
ligne, eux, restent ouverts. Entre deux pays de même monnaie (Togo et Sénégal,
Gabon et Tchad), le solde marche comme chez soi.

---

## 4 bis. 🆕 📱 L'ÉDITION — `X-Dira-Edition` (v4.86.0)

**Dira Lite** est une **seconde application passager**, pour les zones de
faible alphabétisation (le Tchad d'abord) : tout y est écoutable, et sa
configuration est volontairement plus courte.

Deux applications distinctes sur le téléphone (`llc.dira.client` et
`llc.dira.lite`), **mêmes comptes**, **même rôle** — `app: "client"` à la
connexion, inchangé. Rien ne permettait donc au serveur de savoir laquelle
l'appelait.

```
X-Dira-Edition: lite        # ou `full`
X-Dira-App-Version: 0.1.0 (26102901)     # facultatif, utile
X-Dira-Platform: android | ios           # facultatif, utile
```

Posez-le sur **chaque** requête, comme `X-Dira-Country` — WebSocket du suivi
compris, où le paramètre d'URL `?edition=lite` le remplace (un navigateur ne
pose pas d'en-tête sur l'ouverture d'une socket).

### ⚠️⚠️ Il est DÉCLARATIF, jamais autorisant

N'importe qui peut écrire `X-Dira-Edition: lite`. Il sert donc à **adapter**
(une liste plus courte, un texte plus simple) et à **compter**. Il ne décide
**jamais** d'un droit : nous ne construirons pas de règle de sécurité dessus,
et vous ne devez pas supposer qu'il en protège une.

### ⚠️ Son défaut est `full`, et rien ne vous refuse

| Ce que vous envoyez | Ce que le serveur retient |
|---|---|
| `lite` (toute casse) | `lite` |
| `full` | `full` |
| **rien** | `full` |
| une valeur inconnue (`léger`, `lite2`) | `full` |

Une faute de frappe dégrade vers l'application **complète**, jamais vers une
application amputée — et aucune requête n'est refusée pour cet en-tête.

### ⚠️ Le serveur republie l'édition retenue

La réponse porte `X-Dira-Edition`. **Lisez-le au moins une fois** : une
application Lite qui reçoit `full` sait immédiatement que son en-tête
n'arrive pas (une passerelle qui le filtre, un cache qui le mange), au lieu de
chercher deux jours pourquoi sa configuration n'est pas réduite.

### L'appareil enregistré dit son édition

```jsonc
POST /me/devices
{ "token": "…", "platform": "android", "locale": "fr",
  "edition": "lite" }           // absent = `full`
```

⚠️⚠️ **UNE MÊME PERSONNE PEUT AVOIR LES DEUX APPLICATIONS**, et c'est tout
l'objet de ce champ. Sans lui, une notification de course partait vers **tous**
ses jetons : elle arrivait **deux fois**, et dans l'application qui n'avait
rien commandé.

⚠️ **L'édition est une propriété de l'INSTALLATION, pas du compte.** C'est le
jeton qui la porte : la rattacher au compte rendrait impossible de distinguer
vos deux applications sur le même téléphone.

⚠️ **Un jeton déjà enregistré sans édition compte comme `full`** — le parc
installé continue de recevoir ses notifications sans rien changer.

> Si votre corps ne porte pas encore `edition`, le serveur retient celle de
> l'en-tête de la requête. Vous n'avez donc pas à sortir les deux applications
> dans le même ordre.

---

## 4 ter. 🆕 ⚙️ CE QUE VOTRE APPLICATION VOIT — `GET /app/config` (v4.86.0)

```
GET /app/config          # SANS jeton — lu AVANT l'écran de connexion
  →
{ "edition": "lite", "country": "TD",
  "features": {
    "scheduled_rides": false, "rental": false, "pool": false,
    "multi_car": false, "trip_share": true, "subscriptions": false,
    "promo_codes": false, "challenges": false, "raffle": false,
    "banners": false, "feed": true, "assistant": true,
    "assistant_auto_order": false, "voice_messages": true, "wallet": false
  },
  "lists": {
    "payment_methods": ["cash", "mobile_money"],
    "vehicle_modes": ["comfort", "eco", "moto"]
  } }
```

**Lisez-la une fois à l'ouverture**, gardez-la en mémoire, et **cachez vos
entrées de menu** sur ce qu'elle dit. C'est ce qui évite de proposer un bouton
qui échoue — et, pour Lite, d'avoir à lire à voix haute un refus qu'on pouvait
éviter.

### ⚠️ Pourquoi elle est publique, sans jeton

Vous la lisez **avant** l'écran de connexion : c'est là que vous décidez quels
menus dessiner. L'enfermer derrière un jeton vous obligerait à afficher
l'application complète puis à la réduire après connexion — la personne verrait
des entrées de menu **apparaître puis disparaître**.

Rien ici n'est secret : ce sont les fonctions d'un pays, que n'importe quelle
application découvre en s'en servant.

### `features` — toutes les clés, y compris celles à `true`

⚠️⚠️ **NE SUPPOSEZ RIEN D'UNE CLÉ ABSENTE.** Nous servons le catalogue
**entier** à chaque appel, précisément pour que vous n'ayez pas à le connaître.
Si une clé que vous attendez manque, c'est que votre serveur est plus ancien
que votre application : traitez-la comme **disponible** (`true`), jamais comme
coupée — sinon une fonction nouvelle disparaîtrait de vos écrans le jour où le
serveur l'annonce.

### `lists` — des listes blanches, et une liste absente veut dire « tout »

⚠️ `lists` ne contient que les listes **restreintes**. Une liste absente veut
dire « tout ce que le métier sert » — ce n'est **jamais** « rien ».

⚠️ **Croisez, ne remplacez pas.** `vehicle_modes` dit ce que vous avez le droit
de montrer, pas ce qui existe : la liste réelle reste celle de
`GET /settings/modes` et `GET /classes`. Afficher les clés de `lists`
telles quelles montrerait des modes que ce pays ne sert pas.

### ⚠️ Les routes existantes appliquent le MÊME filtre

Une application qui ne lit pas `/app/config` ne voit pas pour autant une
fonction exclue : **les routes concernées la refusent**. Cette lecture ne
remplace donc aucune vérification — elle vous évite de **proposer** ce qui sera
refusé.

⚠️ **Mais toutes les fonctions du catalogue n'ont pas de route à refuser**, et
il faut savoir laquelle est laquelle :

| Fonction | Ce qui l'applique |
|---|---|
| `wallet` | `GET /wallet`, `/wallet/transactions`, `POST /wallet/purchase` refusent |
| `challenges` | `GET /me/challenges` refuse |
| `promo_codes` | `GET /me/referral` refuse |
| `rental`, `pool`, `multi_car` | `POST /rides` refuse ; `GET /classes` et `GET /settings/modes` filtrent |
| `trip_share` | `POST /rides/{id}/share` refuse |
| `subscriptions` | `POST /subscriptions` refuse |
| `feed`, `banners` | 🚧 **pas encore appliqué côté serveur** — fiez-vous à `/app/config` |
| `raffle`, `voice_messages`, `assistant*` | **écrans d'application** : aucune route à refuser, `/app/config` est la seule source |

⚠️⚠️ **Pour les deux dernières lignes, `/app/config` n'est pas un confort,
c'est la SEULE chose qui coupe la fonction.** Une application qui ne la lit pas
affichera un écran que l'exploitation croit avoir retiré — et personne ne s'en
apercevra, puisque rien n'échoue.

### Le refus, quand vous le provoquez quand même

```jsonc
// 403
{ "error": { "code": "feature_unavailable_in_edition",
             "message": "Cette fonction n'est pas disponible dans votre application",
             "feature": "scheduled_rides" } }
```

⚠️ **Ce n'est PAS un problème de droits** — ne proposez pas de se reconnecter.
`error.feature` nomme la fonction : **cachez-la** au prochain démarrage plutôt
que de reproposer un bouton qui échoue.

⚠️ **La phrase est courte exprès** : Lite la lit à voix haute. Elle ne parle ni
d'édition ni de pays, délibérément — la personne n'a qu'une application, elle
ne sait pas qu'il en existe deux, et elle ne peut rien faire de cette
information.

### ⚠️ Rien n'est coupé par défaut

Un déploiement neuf sert `true` partout et aucune liste : la plateforme se
comporte **exactement comme avant**. Les restrictions sont posées pays par pays
et édition par édition depuis la console.

---

## 5. 🗺️ LE FOND DE CARTE — `basemap`

Les deux métiers affichent des cartes. Le serveur ne répond qu'à **une**
question : dans ce pays, quel fond montre-t-on d'abord ?

```jsonc
// à la connexion, à l'inscription, au rafraîchissement
"maps": { "basemap": "dira" | "google" }
// et dans le catalogue PUBLIC des pays
GET /countries → items[].basemap
```

**La règle du fond effectif, dans cet ordre** : le **choix de la personne**,
sinon le `basemap` du pays où elle **opère**, sinon `dira`.

⚠️ **LISEZ LES DEUX ENDROITS, ET IL FAUT LES DEUX.** Le bloc `maps` de
l'authentification voyage avec le jeton : il dit le fond du pays où l'on s'est
**connecté**, une fois. Une application qui ne lirait que lui garderait le fond
de la veille après un changement de pays.

⚠️ **AUCUNE CLÉ GOOGLE NE VIENT DU SERVEUR.** Une clé se restreint par ce qui
l'utilise (empreinte SHA-1 + paquet sur Android, bundle sur iOS) : elle
appartient à l'application. Proposez **toujours les deux** fonds.

⚠️ **LE FOND CHANGE ⇒ LA CARTE DOIT RENAÎTRE.** Les SDK lisent leur style à la
création ; changer la valeur sans reconstruire la vue ne se voit pas. C'est le
défaut qu'a eu la console : le logo d'un fournisseur posé sur les tuiles de
l'autre.

---

### ⚠️ LE SDK DE CARTE CHANGE DE NOM — `@dira-ws/dira-maps-react-native` (v4.70.0)

Le paquet `@kgtech-org/dira-maps-react-native` devient
**`@dira-ws/dira-maps-react-native`**, en version **0.5.0**.

**Aucune ligne du SDK ne change** : son API est celle de 0.4.7, au caractère
près. Seul le **nom** du paquet change.

**Pourquoi** : les dépôts de la plateforme ont quitté l'organisation
`kgtech-org` pour **`dira-ws`** le 12 octobre 2026, et le registre de paquets
de GitHub impose que le scope d'un paquet soit le **propriétaire de son
dépôt**. Sous `@kgtech-org`, plus aucune version ne peut être publiée — il n'y
a pas de contournement.

**Ce que l'application unifiée doit changer** — la dépendance est épinglée sur
un **tag git**, pas sur le registre :

```diff
 // package.json
-"@kgtech-org/dira-maps-react-native": "github:kgtech-org/dira-maps-react-native-sdk#v0.3.0"
+"@dira-ws/dira-maps-react-native": "github:dira-ws/dira-maps-react-native-sdk#v0.5.0"
```

puis les **17 imports** (17 fichiers) :

```diff
-import { useRoute } from '@kgtech-org/dira-maps-react-native'
+import { useRoute } from '@dira-ws/dira-maps-react-native'
```

⚠️ **LA CLÉ ET LES IMPORTS SE CHANGENT ENSEMBLE.** Avec un spécificateur git,
npm range le paquet dans le dossier portant la **clé du `package.json`**, et non
le nom que le paquet déclare lui-même. Changer l'une sans les autres casse la
résolution des modules.

⚠️ **ET NPM NE SIGNALE PAS LE DÉSACCORD** — vérifié. Si vous montez le tag à
`#v0.5.0` en gardant la clé `@kgtech-org/…`, l'installation **réussit** et
l'application **compile sans un mot** : le paquet renommé est rangé sous
l'ancien dossier. Rien ne casse, et rien ne vous apprend que le nom a changé.

⚠️ **ET LE TAG ÉPINGLÉ ICI EST `v0.3.0`** — quatre versions en arrière (0.4.0 à
0.4.7 : fraîcheur des réponses de géocodage réglable, une icône par catégorie de
lieu, gabarits de tuiles). Le renommage est l'occasion de monter en 0.5.0, qui
contient tout cela.

⚠️ **RIEN NE CASSE LE JOUR OÙ VOUS NE FAITES RIEN.** Les versions publiées sous
l'ancien nom restent installables. Mais plus aucune n'y sera publiée : une
application qui garde l'ancien nom est **figée sans que rien ne le signale** —
la même panne silencieuse qu'un serveur qui tire son image d'un registre que
personne n'alimente plus, et elle a coûté six jours de préproduction.

Il n'y a **pas de `.npmrc` à toucher** : l'application n'installe pas depuis le
registre de paquets. (Si vous y passez un jour, la ligne devient
`@dira-ws:registry=https://npm.pkg.github.com`, avec un jeton `read:packages`.)

---

> ### 🔢 Sur iPhone, les notifications d'un même objet se REMPLACENT (v4.71.0)
>
> Une course qui change cinq fois d'état laissait **cinq lignes** sur l'écran
> verrouillé, et la seule qui compte — la dernière — se lisait au milieu de
> quatre périmées. Android regroupe de lui-même ; iOS empile.
>
> Le serveur pose donc un identifiant de regroupement dérivé de **l'objet**
> dont parle le message (`ride-<id>`, `order-<id>`, `delivery-<id>`,
> `ticket-<id>`). **Rien à faire côté application** : c'est transparent, et
> aucun contrat ne change. À savoir seulement si vous vous demandez pourquoi
> une notification en a remplacé une autre.
>
> ⚠️ **Deux objets différents ne se remplacent jamais**, même si leurs
> identifiants se ressemblent — et un message qui ne parle d'aucun objet ne se
> regroupe pas : il s'affiche à part, comme avant.

## 5 bis. 🔒 LE VERROU DE L'APPLICATION — `app_lock` (v4.46.0)

Biométrie ou code secret devant l'application : qui ouvre le téléphone de
quelqu'un n'ouvre pas pour autant son compte Dira. **Le verrou vit chez vous** —
le serveur ne peut ni le poser, ni vérifier qu'il y est. Ce qu'il dit, c'est la
**politique du pays**, réglée depuis la console :

```jsonc
// à la connexion, à l'inscription, à la vérification d'un code, au rafraîchissement
"app_lock": {
  "mode": "optional",      // "off" · "optional" · "required"
  "biometrics": true,      // false = CODE SEUL
  "pin_length": 4,         // 4 ou 6
  "grace_seconds": 120,    // temps hors de l'app avant de redemander ; 0 = à chaque retour
  "max_attempts": 5        // échecs avant DÉCONNEXION
}
```

Et dans `GET /countries`, pour l'application qui change de pays sans se
reconnecter.

**Ce que chaque mode demande**

| `mode` | Ce que l'application fait |
|---|---|
| `off` | ne rien proposer — pas de case dans les réglages ; une case qui ne fait rien est pire que pas de case |
| `optional` | proposer dans les réglages, **éteint par défaut** |
| `required` | **exiger** la pose d'un verrou avant le premier écran, et ne pas offrir de le retirer |

⚠️ **LE CODE SECRET EST TOUJOURS POSSIBLE, MÊME AVEC LA BIOMÉTRIE.** Un capteur
cassé, un doigt mouillé, un visage dans le noir : sans code de secours, le
verrou enferme dehors quelqu'un qui n'a rien fait. `biometrics: false` veut dire
« code **seul** », jamais l'inverse.

⚠️ **AU-DELÀ DE `max_attempts`, ON DÉCONNECTE — ON NE BLOQUE PAS.** Jetez le
jeton de rafraîchissement (`POST /auth/logout`), effacez l'état local, revenez à
l'écran de connexion. Un téléphone volé qui se *bloque* garde un jeton valide
**trente jours** ; déconnecté, il ne garde rien. Et la personne légitime
retrouve son compte avec un code à usage unique (client) ou son mot de passe.

⚠️ **NE VERROUILLEZ JAMAIS UN APPEL DE COURSE NI UN BOUTON D'URGENCE.** Un appel
dure trente secondes : derrière un code, c'est un appel manqué, et le chauffeur
désactivera le verrou le jour même. Le verrou garde l'application, pas l'écran
qui sonne.

⚠️ **`grace_seconds` DÉCIDE SI LE VERROU SERA SUPPORTÉ OU CONTOURNÉ.** Compter
depuis le passage en arrière-plan, pas depuis la dernière saisie : redemander le
code parce que quelqu'un est allé lire le SMS de son opérateur fait désinstaller
l'application. `0` existe et se choisit — pour un parc de téléphones partagés.

⚠️ **CE QUE LE VERROU NE PROTÈGE PAS.** Il arrête l'ami curieux et le téléphone
laissé sur une table. Il n'arrête pas qui extrait le stockage d'un appareil
débridé : le jeton est là. Ce qui protège la donnée, c'est **le stockage
sécurisé** (Keychain `AfterFirstUnlockThisDeviceOnly`, Keystore) et la durée de
vie du jeton — jamais ce réglage.

⚠️ **NE L'ENVOYEZ PAS AU SERVEUR, NE LE RANGEZ PAS EN CLAIR.** Le code ne quitte
pas le téléphone ; il se dérive (PBKDF2, Argon2) avec un sel dans le stockage
sécurisé, ou mieux, il déverrouille le trousseau du système. Le serveur ne le
connaît pas, et ne doit pas le connaître : il n'aurait aucun moyen de le
vérifier sans devenir le point de panne de l'ouverture de l'application.

**La politique change sans reconnexion** : relisez-la à chaque réponse qui la
porte (connexion, inscription, vérification de code, **rafraîchissement**) et
appliquez-la à chaud. Un pays qui passe à `required` ne doit pas attendre
l'expiration d'un jeton de trente jours.

---

### 📱 COMBIEN D'APPAREILS — et lequel se déconnecte (v4.46.0)

Votre compte tient **plusieurs appareils** : le téléphone, la tablette, celui
qu'on vient de changer. Le nombre est réglé **par pays** (trois par défaut) et
se lit dans `GET /countries` → `max_devices`.

⚠️ **AU-DELÀ, LA SESSION LA PLUS SILENCIEUSE PART** — pas la première ouverte.
Une session est datée de son **dernier rafraîchissement** : le téléphone dont on
se sert tous les jours se redate seul, celui qui dort dans un tiroir s'en va le
premier. L'appareil évincé ne reçoit rien sur le moment : il l'apprend à son
prochain rafraîchissement, qui répond **`401`**. Traitez-le comme une session
expirée ordinaire — ramenez à l'écran de connexion, n'affichez pas « erreur ».

⚠️ **ENVOYEZ `device_id` À LA CONNEXION, MÊME SI VOUS ÊTES UNE APPLICATION DE
CLIENT.** Il ne va pas dans votre jeton et ne vous soumet à aucune règle d'agent ;
il sert à une seule chose : **reconnaître le même téléphone qui revient**. Sans
lui, une réinstallation compte pour un appareil de plus et pousse dehors un
autre de vos appareils. Avec lui, votre session précédente est simplement
remplacée. Il doit **survivre aux redémarrages** et vivre aussi longtemps que le
jeton de rafraîchissement, à côté de lui.

---

## 6. L'identité et l'argent — **PARTAGÉS** (socle, sans préfixe)

C'est tout l'intérêt d'une application unifiée : **une** inscription, **un**
solde, **une** boîte.

```
POST /auth/otp · POST /auth/otp/verify          ← la porte par CODE (v4.46.0)
POST /auth/register · POST /auth/login · POST /auth/refresh · POST /auth/logout
GET /me · GET /me/addresses · POST /me/devices
GET /wallet · GET /wallet/transactions
POST /payments/initiate · GET /payments/{id} · GET /payments/providers
GET /notifications · GET /countries · POST /uploads?kind=avatar   ⚠️ jpeg, png, webp — 5 Mio (v4.73.0)
GET /me/referral                                ← mon code de PARRAINAGE (v4.51.0)
```

### 🆕 6 bis. S'INSCRIRE ET SE CONNECTER PAR CODE — le téléphone, et rien d'autre (v4.46.0)

Deux routes, un écran de six chiffres, **pas de mot de passe à choisir ni à
retrouver** :

```
POST /auth/otp          { phone, channel?: "whatsapp"|"sms", app: "client", locale? }
   → 200 { sent, channel, expires_at, resend_after, dev_code? }

POST /auth/otp/verify   { phone, code, name?, first_name?, last_name?,
                          app: "client", device_id?, device_name? }
   → 201 { …, created: true }   le compte VIENT DE NAÎTRE
   → 200 { …, created: false }  connexion
```

Le corps de la réponse de `verify` est **exactement celui de
`POST /auth/login`** (`user`, `access_token`, `refresh_token`, `session?`,
`maps?`), augmenté de `created`.

⚠️ **LA PASSERELLE N'EST PAS ENCORE CÂBLÉE — LE CODE REVIENT DANS LA RÉPONSE.**
Tant que le serveur répond `channel: "echo"`, **rien n'est envoyé** : le code
est dans **`dev_code`**. C'est ce qui vous permet de câbler l'écran dès
maintenant. ⚠️ **`dev_code` disparaîtra sans préavis** le jour où WhatsApp ou
le SMS sera branché : traitez-le comme un bonus de développement — pré-remplir
le champ si vous voulez —, **jamais** comme la source du code. L'écran doit
fonctionner à l'identique quand il n'y est plus.

⚠️ **LE COMPTE À REBOURS SE REND DEPUIS `expires_at`**, jamais depuis l'horloge
du téléphone, et le bouton « renvoyer » se rouvre après `resend_after`
secondes. Un code vit **5 minutes** et meurt après **5 essais**.

⚠️ **NE DEMANDEZ LE NOM QU'APRÈS LA VÉRIFICATION, ET SEULEMENT SI
`created: true`.** La demande de code **ne dit jamais** si le numéro est déjà
client — ce serait un annuaire : qui veut savoir si quelqu'un utilise Dira
n'aurait qu'à poster son numéro. Sans nom, le serveur affiche le **numéro**
comme nom ; la personne le changera depuis son profil (`PATCH /me`).

⚠️ **CE COMPTE N'A PAS DE MOT DE PASSE.** `POST /auth/login` lui répondra
toujours `401 invalid_credentials` : ne proposez pas « se connecter avec un mot
de passe » à quelqu'un qui s'est inscrit par code, et ne gardez pas d'écran de
mot de passe oublié pour lui.

⚠️ **RÉSERVÉE AUX CLIENTS.** Un compte chauffeur, livreur, marchand ou de
direction reçoit `403 otp_not_available` et **aucun code** — six chiffres ne
remplacent un mot de passe que pour un client. Les applications d'agent gardent
`POST /auth/login`.

| Refus | Quand | Ce que l'écran fait |
|---|---|---|
| `422 validation_failed` (`fields: ["phone"]`) | le `+` manque | souligner le champ ; pré-remplir l'indicatif du pays |
| `429 otp_too_soon` | un code vient de partir | garder le bouton « renvoyer » grisé jusqu'à `resend_after` |
| `429 otp_too_many_requests` | plafond horaire de ce numéro | proposer le support, pas un nouvel essai |
| `503 otp_delivery_failed` | la passerelle n'a rien pris | **redemander tout de suite est légitime** : rien n'a été consommé |
| `401 otp_invalid` | code faux **ou** aucun code en cours | « code incorrect », garder la saisie |
| `401 otp_expired` | plus de 5 minutes | « demandez un nouveau code » |
| `401 otp_too_many_attempts` | 5 essais | le code est **mort** : revenir à l'écran du numéro |
| `403 otp_not_available` | compte qui se connecte par mot de passe | « ouvrez l'application correspondante » |
| `403 account_suspended` | compte suspendu | le dire tel quel, ne pas renvoyer vers « mot de passe oublié » |

⚠️ **DITES QUELLE APPLICATION SE CONNECTE — `app`.** À la connexion, envoyez
`app: "client"`. Le serveur refuse `403 wrong_app` quand le compte n'a pas le
droit d'entrer ici, et **`error.reason` nomme l'application à ouvrir**
(`client` · `driver` · `courier` · `merchant` · `partner` · `console`). Sans ce mot, un
chauffeur se connectait dans l'application cliente, recevait un jeton
parfaitement valide, puis voyait chaque écran répondre `403` — et croyait
l'application cassée.

> ⚠️⚠️ **UNE VALEUR DE `reason` QUE VOUS NE CONNAISSEZ PAS N'EST PAS UNE
> ERREUR — ET SURTOUT PAS UN BLANC.** La liste s'allonge : `courier` est arrivé
> en v4.37.0, **`partner` en v4.60.0** (la console web d'un propriétaire de
> flotte), et le prochain rôle arrivera sans vous prévenir. Une application qui
> traduit `reason` en nom d'application affiche alors « Ouvrez l'application  »
> — phrase coupée, sans nom.
>
> **La règle : valeur inconnue → affichez `error.message` tel quel.** Il est
> déjà traduit par le serveur et il nomme l'application.
>
> ⚠️ **ET `partner` NE S'OUVRE PAS DEPUIS UN TÉLÉPHONE** : ni bouton « ouvrir
> l'application », ni lien vers un magasin — il n'y en a pas. « Ce compte est
> un compte de partenaire : il s'ouvre depuis la console web de Dira. »


⚠️ **UN SEUL CARNET D'ADRESSES, ET C'EST VOULU.** « Maison » sert au repas
comme au taxi. N'entretenez pas deux listes.

⚠️ **UN SEUL SOLDE, DEUX MÉTIERS QUI LE DÉBITENT.** Un écran de solde qui ne
montrerait que les opérations d'un métier laisserait un client chercher où sont
passés ses 2 000 F. `GET /wallet/transactions` les rend toutes, des deux
métiers.

⚠️ **NE CONCLUEZ JAMAIS UN PAIEMENT SUR LA SEULE RÉPONSE HTTP.** Suivez
`GET /payments/{id}` jusqu'à `succeeded`, ou attendez le passage de l'opération
à son statut payé. Un écran qui annonce « payé » sur un `202` fait réclamer un
remboursement pour un paiement qui n'a jamais abouti.

### 🆕 Recharger : les bornes du pays, et retrouver une recharge perdue (v4.91.0)

```
GET /wallet
  → "topup": { "min": 500, "max": 2000000,
               "presets": [1000, 2000, 5000, 10000],
               "currency": "XOF", "decimals": 0 }
```

⚠️⚠️ **CES NOMBRES NE SONT PAS DES FRANCS CFA — LISEZ `currency`.** Vous codiez
1 000 / 2 000 / 5 000 / 10 000 et un montant libre de sept chiffres : ce sont
les nombres du **Togo**. Le franc guinéen vaut ~15 fois moins — « 1 000 FG »
affiché à Conakry propose sept centimes d'euro. Le cedi et le naira portent en
plus **deux décimales** : `decimals: 2` veut dire que l'entier est en
centièmes, et `2000` s'affiche **20,00 GH₵**. N'écrivez plus aucun montant en
dur.

- `presets` sont les montants proposés d'un geste, **triés**. Gardez le montant
  libre à côté, borné par `min`..`max`.
- ⚠️ **Les bornes sont APPLIQUÉES, pas suggérées.** Hors bornes,
  `POST /payments/initiate` refuse en `422` sous un code **nommé** :

| Code | Ce que l'écran dit |
|---|---|
| `topup_below_minimum` | le `message` traduit porte déjà le minimum et la monnaie — affichez-le tel quel |
| `topup_above_maximum` | idem avec le maximum |

Les deux portent `fields: ["amount"]`. ⚠️ Les **chiffres sont dans la phrase
traduite**, pas dans un champ à part : l'enveloppe d'erreur ne sert que `code`,
`message`, `fields`, `reason` et `feature`.

#### La route de recharge d'un passager

```
POST /payments/initiate
  Idempotency-Key: <une clé que VOUS choisissez>        ← v4.91.0
  { "purpose": "wallet_topup", "ref_id": "<votre propre id>",
    "amount": 5000, "provider": "tmoney" }

→ 201 { "payment": { "id", "status": "pending", "amount", "currency",
                     "operator_reached": true, "expires_at": "…",
                     "payment_url": "…" },
        "payment_url": "…" }
```

⚠️ **`POST /wallet/purchase` N'EST PAS LA RECHARGE D'UN PASSAGER.** Elle achète
des **jetons** — le droit d'entrée d'un livreur, l'outil de promotion d'un
marchand — et le socle la **refuse à un client** (`403`). Le solde `balance`
d'un client reste à zéro, c'est normal.

#### ⚠️⚠️ Quand la réponse se perd : trois champs et une clé

Vous reteniez « Payer » **dix minutes** à l'aveugle, parce que rien ne disait si
une demande était partie chez l'opérateur. Trois choses changent.

**1. `Idempotency-Key` — réessayez sans risque.** Une clé par *intention* de
paiement (pas par tentative), que vous gardez tant que l'écran est ouvert. La
même clé rend **le même paiement**, et ne déclenche **aucune seconde demande**
chez l'opérateur. Elle se lit aussi dans le corps (`idempotency_key`) si
l'en-tête vous est plus difficile à poser.

**2. `GET /payments` — retrouvez un paiement sans son `id`.**

```
GET /payments?purpose=wallet_topup&ref_id=<votre id>&status=pending&limit=5
→ 200 { "items": [ { "id", "status", "amount", "operator_reached",
                      "expires_at", "payment_url" } ],
        "next_cursor": "…", "has_more": false }
```

Le plus récent d'abord, **vos paiements seulement**. ⚠️ `purpose` **est** le
`ref_kind` que vous demandiez : `?purpose=order&ref_id=<order_id>` donne les
paiements d'une commande, `?purpose=ride&ref_id=<ride_id>` ceux d'une course.
Un `purpose`, `status` ou `ref_id` illisible est **refusé** (`422`, le champ
nommé) — jamais ignoré, parce qu'un filtre ignoré vous rendrait *tous* vos
paiements et vous feriez croire à un paiement en cours là où il n'y en a aucun.

**3. `operator_reached` — la seule chose honnête.**

| `status` | `operator_reached` | Ce que ça veut dire | Ce que l'écran fait |
|---|---|---|---|
| `pending` | `true` | la demande est **partie**, le client doit valider chez l'opérateur | « validez sur votre téléphone », ouvrez `payment_url` |
| `pending` | `false` | ⚠️ **on a demandé et on ne sait pas** | « un paiement est peut-être en cours » → **Reprendre** (même `Idempotency-Key`), jamais « Payer » à neuf |
| `succeeded` | — | encaissé et crédité | — |
| `failed` | — | refusé par l'opérateur | proposez de recommencer, **nouvelle** clé |

⚠️ **`operator_reached: false` ne veut PAS dire « rien n'est parti ».** Un délai
dépassé, une coupure, un `5xx` : une demande de paiement peut dormir sur le
téléphone du client. Proposer « Payer » comme si rien ne s'était passé peut
déclencher une **seconde** demande.

⚠️ **`expires_at` remplace vos dix minutes codées en dur.** Après cet instant,
un paiement `pending` ne vaut plus la peine d'être attendu — rendez la main.
**Rien ne le marque `failed` à cette date** : un rappel tardif de l'opérateur
peut encore le faire aboutir, donc gardez `GET /payments/{id}` accessible depuis
l'historique.

⚠️ Et une initiation qui **échoue** (`5xx`) laisse désormais le paiement
**retrouvable** — c'est l'inverse d'avant, où il n'existait aucune trace. Si
votre appel n'aboutit pas, interrogez `GET /payments` avant de proposer quoi que
ce soit.

---

## 🗑️ 6 ter. SUPPRIMER SON COMPTE — et ce qui reste des courses passées (v4.47.0)

```
DELETE /me
{ "password": "…" }      ← compte avec mot de passe
{ "code": "483920" }     ← compte né par code (demandez-en un par POST /auth/otp)
   → 200 { "status": "closed", "erase_at": "2026-11-06T18:40:00Z" }
```

C'est un **droit**, et l'écran qui le porte doit être trouvable depuis le
profil — pas enterré derrière le support.

**EN DEUX TEMPS, ET IL FAUT DIRE LES DEUX.**

1. **Tout de suite** — le compte est **fermé**. Toutes les sessions tombent, sur
   **tous** les appareils ; les notifications cessent ; la connexion répond
   ensuite `403 account_closed`, par mot de passe **comme par code**. Videz le
   jeton, le stockage sécurisé, les caches et les écrans, et revenez à la
   connexion.
2. **À `erase_at`** (trente jours plus tard par défaut) — l'**identité** part :
   le nom devient « Compte supprimé », le téléphone est brouillé puis
   **libéré**, l'e-mail, la photo, l'état civil, les préférences et le **carnet
   d'adresses** sont effacés, la boîte de notifications est purgée, et les
   messages écrits au chauffeur, au livreur ou au support sont jetés.

⚠️ **LES COURSES ET LES COMMANDES PASSÉES RESTENT — dites-le AVANT le bouton,
pas après.** Ce sont des **écritures comptables** : prix, commission, date, déjà
déclarés dans une facturation par pays. Les effacer trouerait un journal en
partie double. Mais elles ne **désignent plus personne** — aucun service ne
stocke de nom ni de téléphone, il les demande au compte au moment d'afficher —,
donc anonymiser le compte anonymise **tout** l'historique d'un seul coup,
partout, console et support compris. Un écran qui promet « tout sera effacé »
promet ce qui ne peut pas l'être, et c'est la réclamation qui suivra.

⚠️ **AFFICHEZ `erase_at`.** « Votre compte est fermé. Vos données seront
effacées le 6 novembre. » se comprend. « Compte supprimé » tout seul ne se
comprend pas — et la première réclamation arrive le jour où la personne appelle
le support, qui lit encore son nom à l'écran.

⚠️ **IL FAUT PROUVER QUI ON EST**, et c'est tout le corps de la requête :
`password` pour un compte qui en a un, `code` pour un compte né par code
(demandez-en un par `POST /auth/otp` ; il est **consommé** ici). L'opération est
irréversible depuis l'application : sans cette preuve, un téléphone déverrouillé
posé sur une table suffirait à faire disparaître le compte de quelqu'un.

⚠️ **VIDEZ LE PORTEFEUILLE D'ABORD.** Un solde, des jetons ou une **dette**
font refuser `409 wallet_not_empty`, et `error.reason` dit lequel
(`money` · `tokens` · `debt`). On ne détruit pas de l'argent en silence :
emmenez la personne vers son solde, ou vers le support pour un remboursement.

⚠️ **PENDANT LES TRENTE JOURS, LE SUPPORT PEUT ANNULER.** C'est le seul recours
de qui a touché le bouton par erreur, et c'est la raison même du délai : dites-le
sur l'écran de confirmation, avec le moyen d'écrire au support. Se **réinscrire**
avec le même numéro pendant ce délai répond `403 account_closed` — pas
`phone_taken` : le numéro n'est rendu qu'à `erase_at`.

⚠️ **NE SUPPRIMEZ JAMAIS AU PREMIER APPUI.** Un écran de conséquences (ce qui
part, ce qui reste, la date), puis la preuve d'identité. Deux gestes pour une
action qu'aucun bouton ne défera.

| Refus | Quand | Ce que l'écran fait |
|---|---|---|
| `401 invalid_credentials` | mot de passe faux | souligner le champ, ne pas quitter l'écran |
| `401 otp_invalid` · `401 otp_expired` | code faux ou périmé | redemander un code |
| `409 wallet_not_empty` | solde, jetons ou dette (`error.reason`) | envoyer vers le solde, ou le support |
| `409 deletion_already_requested` | demande déjà en cours | afficher `erase_at` et proposer le support pour annuler |
| `403 erasure_not_self_serve` | compte d'agent, de marchand ou de direction | renvoyer vers le support (il porte des versements à solder) |
| `403 account_closed` | à la connexion, au rafraîchissement ou à l'inscription | « ce compte a été supprimé » + « se réinscrire » ou support |

---

---

## 6 quater. 🎟️ LES CODES PROMO ET LE PARRAINAGE — un champ, deux métiers (v4.51.0)

**Un code est au SOCLE, et il vaut des deux côtés.** Le même mot ne peut pas
valoir une chose sur une course et une autre sur une commande : une enveloppe,
un suivi, une vérité. C'est exactement ce qu'une application unifiée doit
montrer — **un seul champ « code promo »**, placé au même endroit dans les deux
tunnels.

| Où on le saisit | Ce qu'on envoie | Ce qui revient |
|---|---|---|
| devis d'une course | `POST /rides/quote { …, "code": "DIRA2000" }` | `promo_code`, `promo_code_xof` **par classe**, `promo_code_ignored` |
| commande de repas | `POST /orders { …, "code": "DIRA2000" }` | `promo_code`, `promo_code_xof`, `total` déjà remisé |

> ⚠️ **LE PRIX SERVI EST DÉJÀ REMISÉ** des deux côtés — `fare_xof`, `total`. Ne
> soustrayez **rien** : le montant du code est là pour être **affiché**.

> ⚠️ **MAIS LES DEUX MÉTIERS NE REFUSENT PAS AU MÊME MOMENT, et l'écran doit en
> tenir compte.** Sur une course, le refus arrive **au devis** : le passager
> corrige son code avant de commander, rien n'est perdu. Sur une commande, il
> arrive **à la création** — la commande n'est pas créée. Un panier composé
> pendant dix minutes ne doit pas disparaître sur une faute de frappe : gardez-le
> à l'écran, et laissez réessayer ou commander sans code.

> ⚠️ **UN CODE PEUT VALOIR POUR UN SEUL MÉTIER.** `promo_code_wrong_service`
> arrive avec `meta.valid_for` : dites **lequel** (« ce code est pour les
> commandes »), et, puisque vous portez les deux onglets, proposez d'y aller.
> C'est l'avantage que seule une application unifiée peut offrir ; ne le gaspillez
> pas en affichant « code invalide ».

> ⚠️ **`promo_code_ignored` N'EXISTE QUE SUR UNE COURSE** : il dit qu'un code
> valable a été battu par une offre automatique. Ne le traitez pas comme un refus
> — « gardez-le, une meilleure offre s'applique déjà ». Côté commande, ce cas
> n'existe pas.

**Le parrainage**, lui, est un écran unique du compte — il ne dépend d'aucun
métier :

```
GET /me/referral
→ { "enabled": true, "code": "AWA7K2M",
    "invitee_xof": 1000, "sponsor_xof": 500, "max_sponsored": 10, "uses": 3 }
```

> ⚠️ **`enabled` EST EXPLICITE : NE LE DÉDUISEZ PAS DE L'ABSENCE DE CODE.** Le
> parrainage se règle **pays par pays** et il est **éteint par défaut** : à faux,
> **cachez l'entrée** « inviter un ami » plutôt que d'afficher un écran vide.

> ⚠️ **DEUX MONTANTS, DEUX BÉNÉFICIAIRES.** `invitee_xof` est une **remise** pour
> celui qui arrive, `sponsor_xof` un **crédit** pour celui qui invite. « Gagnez
> 1 500 F » fait attendre 1 500 F à tout le monde et produit deux déceptions.

> ⚠️ **LE PARRAIN EST PAYÉ QUAND LA REMISE EST CONSOMMÉE** — course terminée,
> commande livrée —, jamais à la saisie. Dites-le : sinon quelqu'un qui voit son
> filleul commander et son solde ne pas bouger croira que rien ne marche. Et
> comme le filleul peut dépenser son code **dans l'un ou l'autre métier**, le
> crédit peut arriver d'un onglet que le parrain ne regardait pas — votre fil
> d'activité (§9) est le bon endroit pour le dire.

Détail des neuf refus et de leurs suites : `VTC-CLIENT` §3 et `FOOD-CLIENT` §4.

## 7. ⚠️ La navigation : un onglet par métier, et ce que ça change

Trois décisions qui n'existaient pas quand il y avait deux applications :

1. **L'onglet est un CONTEXTE, et il vaut de l'or.** Quand la personne vient de
   toucher « manger », dites-le à l'assistant (`vertical: "food"`, §8) : c'est
   le meilleur signal qui existe, il est gratuit, et il évite une question.
2. **Le pays suit l'OPÉRATION, pas l'onglet.** Un client à Dakar commande un
   repas dakarois et une course dakaroise ; ne gardez pas un pays par onglet.
3. **L'accueil n'a pas d'onglet.** Sur un écran où les deux métiers cohabitent,
   **n'envoyez pas** `vertical` : c'est au serveur de trancher.

---

## 8. 🆕 L'ASSISTANT UNIFIÉ — un champ de saisie, deux métiers (v4.43.0)

```
POST /analytics/ai/chat
{ "message": "emmène-moi à l'aéroport",
  "origin": [9.45, 0.41],        // facultatif — la position du téléphone
  "vertical": "vtc" | "food" }   // facultatif — voir plus bas
  →
{ "vertical": "vtc",             // QUI a répondu
  "reply": "Voici deux options depuis votre position.",
  "plan": { … },                 // le plan du métier, VERBATIM
  "routed_by": "lexical" }
```

« J'ai faim » et « emmène-moi à l'aéroport » arrivent par le **même** champ. Le
serveur tranche de quel métier il s'agit, délègue à l'assistant de ce métier-là,
et vous rend sa réponse **telle quelle**.

### `plan` est celui du métier qui a répondu

⚠️ **DEUX FORMES, ET `vertical` DIT LAQUELLE.** Nous ne retouchons pas le plan :
le réécrire aurait fait deux sources de vérité pour un prix.

```jsonc
// vertical: "food" — des lignes VÉRIFIÉES contre le catalogue
"plan": { "items": [ { "dish_id": "…", "store_id": "…", "name": "Poulet braisé",
                       "qty": 2, "price": 2700 } ],
          "unresolved": ["pastels"] }

// vertical: "vtc" — des lieux trouvés et des devis calculés
"plan": { "stops": [ { "kind": "pickup", "label": "Tokoin", "geo": [1.21, 6.17] },
                     { "kind": "dest", "label": "Aéroport", "geo": [1.25, 6.16] } ],
          "quotes": [ { "id": "…", "class_key": "eco", "fare_xof": 2500 } ],
          "unresolved": [], "when": "" }
```

⚠️ **UN PLAN NE COMMANDE RIEN.** Il se confirme par la route du métier —
`POST /food/orders` avec les lignes, `POST /vtc/rides { quote_id }` avec un
devis — exactement comme s'il avait été composé à la main. **Jamais** de texte
libre qui devient une commande.

⚠️ **`unresolved` SE DIT, IL NE SE TAIT PAS.** Ce que le catalogue ou la carte
n'a pas trouvé est rendu à part : proposez une recherche. Le taire ferait une
commande qui ne contient pas ce qu'on a demandé.

### ⚠️ Quand le serveur NE SAIT PAS — et il a le droit

```jsonc
{ "reply": "Vous voulez une course, ou quelque chose à manger ?",
  "choices": ["vtc", "food"] }      // et PAS de `vertical`
```

**`vertical` absent = personne n'a répondu.** Posez la question : deux boutons
sur `choices`, et renvoyez le **même message** avec `vertical` renseigné.

⚠️ **ON NE DEVINE PAS, ET C'EST DÉLIBÉRÉ.** Envoyer « commande-moi quelque
chose » au hasard ferait livrer un repas à quelqu'un qui attend un taxi — et il
le découvrirait au prix. Une application qui choisirait elle-même « au pif »
pour éviter une question referait exactement cette erreur.

⚠️ **`reply` EST TOUJOURS REMPLI**, même sans `vertical` : si vous n'avez pas
prévu `choices`, affichez-le et vous aurez déjà une question sensée.

### `routed_by` — comment on a tranché

| Valeur | Ce qui a décidé |
|---|---|
| `app` | **vous** l'avez dit (`vertical` dans la requête) |
| `lexical` | le **vocabulaire** du message : un seul métier y était nommé |
| `model` | le **modèle**, parce que le vocabulaire ne suffisait pas |

⚠️ **SERVI POUR QU'UN ACHEMINEMENT FAUTIF SOIT EXPLICABLE.** Joignez-le à un
rapport de bogue avec `X-Request-ID` : « j'ai été envoyé au mauvais métier » ne
se diagnostique pas autrement.

⚠️ **ENVOYEZ `vertical` DÈS QUE VOUS LE SAVEZ.** C'est gratuit, c'est exact, et
cela évite un appel de modèle — donc une dépense, une latence, et un risque
d'erreur. Ne l'envoyez **pas** depuis un accueil où les deux métiers cohabitent.

### `origin`

La position du téléphone. Elle sert de **départ** aux courses quand le passager
n'en nomme pas — « je vais à l'aéroport » part d'ici. La livraison l'ignore :
elle a les adresses du compte.

### Les refus

| Refus | Ce que ça veut dire | Ce que vous affichez |
|---|---|---|
| `403 ai_disabled` | l'exploitation a coupé l'assistant | **masquez la boîte de dialogue** — ne la laissez pas échouer |
| `429 ai_quota_reached` | cette personne a épuisé son quota du jour | « réessayez demain », et proposez la composition à la main |
| `429 ai_budget_reached` | la plateforme a atteint son plafond du jour | idem, sans accuser la personne |
| `503 assistant_unavailable` | aucun métier joignable | masquez la boîte |

⚠️ **UN QUOTA ATTEINT N'EST PAS UNE PANNE.** Le refus du métier est reporté
**tel quel**, code et phrase déjà traduite : affichez la phrase, et laissez
toujours un chemin manuel.

### 🎙️ 🆕 LE VOCAL COMBINÉ — gardé, réécoutable, parfois exécuté (v4.84.0)

```
POST /analytics/ai/voice
    multipart :  file     = <audio>              (obligatoire, 2 Mio au plus)
                 seconds  = 5                    (⚠️ ENVOYEZ-LA — voir plus bas)
                 language = <code>               (facultatif)
                 origin   = "9.45,0.41"          (facultatif)
                 vertical = "vtc" | "food"       (facultatif)
  →
{ "voice_id": "68f0…",
  "audio_url": "https://files.dira.llc/voice/…/a1b2.m4a",   // RÉÉCOUTABLE
  "seconds": 7,
  "text": "emmène-moi à l'aéroport",
  "language": "fr",
  "vertical": "vtc",
  "reply": "Voici deux options depuis votre position.",
  "plan": { … },                  // la même forme qu'au chat, verbatim
  "confidence": 0.91,
  "threshold": 0.85,              // 🆕 v4.89.0 — le seuil DE CE PAYS
  "routed_by": "lexical",
  "state": "understood" }         // ⚠️ CE QUI A ÉTÉ FAIT — voir le tableau
```

⚠️ **🆕 `threshold` SE LIT AVEC `confidence`, JAMAIS SEUL** (v4.89.0). Il était
annoncé et jamais servi. « J'ai compris à 72 % » ne veut rien dire sans « il en
faut 85 » : c'est la paire qui dit si la commande partira toute seule.
`threshold: 0` veut dire que ce pays ne commande **jamais** sans confirmation.

Un seul appel fait tout : l'audio est **gardé**, transcrit, porté à l'assistant
du métier comme un message écrit, et le tour entier est **écrit dans le fil**.

> ⚠️ **L'AUDIO EST LA SEULE PIÈCE QUI NE MENTE PAS, et c'est pour cela qu'on le
> garde.** La transcription se trompe — « riz sauce » devient « riz gras » — et
> le plan aussi. Sans le fichier, un litige se tranche sur la parole de l'un
> contre celle de l'autre ; avec lui, le support **réécoute** et tranche en
> dix secondes.

#### ⚠️⚠️ 🆕 UN ÉCHEC D'ÉCOUTE ARRIVE EN `200`, PAS EN `502` (v4.89.0)

Quand la transcription échoue, vous recevez un **`200`** portant
`state: "failed"`, `voice_id` et **`audio_url`** — le texte est vide.

```jsonc
{ "voice_id": "68f0…", "audio_url": "https://files.dira.llc/voice/…", 
  "seconds": 7, "text": "", "state": "failed", "confidence": 0 }
```

⚠️ **Proposez la RÉÉCOUTE, pas « nous n'avons pas entendu ».** Le `502` sans
corps vous faisait annoncer un échec sec alors que **l'audio était gardé** :
vous aviez de quoi le rejouer, et le vocal restait orphelin dans le stockage.

⚠️ **Et ce n'est pas une panne de notre service** : c'est un enregistrement
qu'on n'a pas su lire. Le compter en `5xx` le faisait aussi entrer dans nos
pannes, ce qui noyait les vraies.

#### ⚠️⚠️ `state` DIT CE QUI A ÉTÉ FAIT, et c'est lui qui choisit votre écran

| `state` | Ce qui s'est passé | Ce que vous montrez |
|---|---|---|
| `understood` | transcrit, un `plan` proposé, **la main vous reste** | le plan à **confirmer** — c'est le comportement de toujours |
| `ordered` | **la commande est PARTIE** | « c'est parti », et `ordered.kind` / `ordered.id` ouvrent le suivi |
| `needs_support` | l'IA n'a pas été assez sûre pour décider | « **nous vous rappelons** » — un humain va écouter |
| `failed` | la transcription a échoué | « nous n'avons pas entendu, réessayez » — l'audio est **conservé** |

```jsonc
// state: "ordered"
{ "state": "ordered", "ordered": { "kind": "ride", "id": "68f0aa…" } }
// kind = "ride" → suivez la course ; kind = "order" → suivez la commande
```

⚠️ **NE DÉDUISEZ JAMAIS « c'est commandé » DE LA PRÉSENCE D'UN `plan`.** Un
`plan` arrive dans **les quatre** cas. Seul `state` tranche, et seul `ordered`
porte un identifiant. Afficher « c'est parti » sur un `needs_support` ferait
attendre une voiture que personne n'a commandée.

#### ⚠️⚠️ LE RENVERSEMENT, DIT EN TOUTES LETTRES

Cette spécification disait, depuis la v4.43.0 : « **un vocal n'est jamais
exécuté directement** », et « **un plan ne commande rien** ». C'était une règle
de sûreté, et elle avait une bonne raison : les accents d'ici ne pardonnent
pas, et une commande partie d'un vocal mal transcrit se découvre **au prix**.

Elle reste vraie **par défaut**, et elle ne s'ouvre que si l'exploitation pose
un **seuil de confiance** dans la console. Ce qu'il faut en retenir côté
application :

- **Ne changez rien à votre écran de confirmation.** `understood` est ce que
  vous recevrez tant qu'aucun pays n'a posé de seuil, c'est-à-dire aujourd'hui
  partout. Une application qui ne sait traiter que `understood` continue de
  marcher — elle affichera simplement un plan là où un autre pays verrait
  « c'est parti ».
- **Mais `ordered` peut arriver du jour au lendemain**, sans nouvelle version
  d'application : le seuil est un réglage, pas un déploiement. Traitez les
  quatre états **maintenant**.
- **Une commande née d'un vocal est toujours EN ESPÈCES.** Jamais le
  portefeuille, jamais en ligne : un vocal ne déplace pas d'argent. La personne
  paie à l'arrivée et peut refuser à la porte si ce n'est pas ce qu'elle
  voulait ; au portefeuille, il faudrait un remboursement pour réparer ce qu'un
  « non merci » règle.
- **Elle est passée en son nom, avec son jeton.** La verticale lui applique
  exactement ce qu'elle lui appliquerait s'il avait appuyé lui-même : son
  solde, les bornes de son pays, le nombre de courses qu'il a déjà en cours.
  Un refus du métier ramène donc `state: "needs_support"`, jamais `ordered`.

#### La langue parlée

Ajoutez `language=<code>` quand la personne a **choisi** sa langue ; **n'envoyez
rien** sinon — le serveur prend alors la première langue du pays, réglée par
l'exploitation (au Sénégal ce peut être le wolof, au Togo le français).

⚠️ **CE N'EST PAS `Accept-Language`** : celui-ci dit la langue de l'interface,
et un téléphone réglé en français n'empêche pas son propriétaire de parler
wolof. Douze codes sont admis, tout autre est refusé (`422 unknown_language`) :

| Code | Langue | Pays |
|---|---|---|
| `fr` · `en` | français · anglais | tous |
| `ee` · `kbp` | éwé · kabiyè | Togo |
| `wo` · `ff` · `srr` | wolof · peul (pulaar) · sérère | Sénégal |
| `man` · `sus` · `ff` | malinké · soussou · peul (pular) | Guinée |
| `shu` · `sba` · `ff` | arabe tchadien · ngambay (sara) · peul (fulfulde) | Tchad |
| `fan` | fang | Gabon |

#### Le fichier

| | |
|---|---|
| Plafond | **2 Mio** — une minute large en opus ou en AAC mono |
| Types acceptés | `audio/mp4`, `audio/m4a`, `audio/x-m4a`, `audio/aac`, `audio/mpeg`, `audio/ogg`, `audio/opus`, `audio/webm`, `audio/3gpp` |
| Refusé | **le WAV** — trente secondes pèsent cinq mégaoctets |

⚠️ **ENCODEZ, N'ENVOYEZ PAS DU BRUT.** C'est ce qu'Android (3GP, WebM/Opus) et
iOS (M4A) produisent nativement : il n'y a rien à faire de plus. Un
enregistrement non compressé dépasse le plafond en vingt secondes, et c'est
**l'envoi** qui punirait la personne sur un réseau d'ici, pas nous.

#### ⚠️⚠️ `seconds` — ENVOYEZ LA DURÉE QUE VOUS AVEZ MESURÉE

Votre enregistreur l'a **chronométrée**. Envoyez-la : un entier, en secondes,
dans le même multipart.

Sans elle, le serveur la déduit du **poids du fichier**, ce qui suppose un
débit — et le débit varie du simple au quintuple d'un encodeur à l'autre. Nous
l'avons mesuré sur la préprod : **cinq secondes de parole à 32 kbit/s
s'affichaient « 0:01 »**. À l'autre bout, une minute à 16 kbit/s s'afficherait
« 0:15 ».

C'est bénin — ce chiffre ne sert qu'à dessiner la largeur d'une bulle et à
écrire « 0:07 », rien ne se facture ni ne se décide dessus. Mais une bulle qui
annonce « 0:01 » et qui joue pendant cinq secondes se lit comme un bogue, et
c'est vous qui le recevrez.

La valeur rendue est **celle que vous avez envoyée** (bornée à 600). À défaut,
c'est l'estimation, et elle est fausse par construction.

#### Les refus

| Refus | Ce que ça veut dire | Ce que vous affichez |
|---|---|---|
| `413 audio_too_large` | plus de 2 Mio | « message trop long », et proposez de recommencer plus court |
| `422 unknown_language` | code de langue inconnu | un bogue d'application : corrigez le code envoyé |
| `429 ai_quota_reached` | quota du jour épuisé | « réessayez demain », **et laissez écrire** |
| `429 ai_budget_reached` | plafond de la plateforme | idem, sans accuser la personne |
| ~~`502 transcription_failed`~~ | 🆕 v4.89.0 — **ce n'est plus un refus** : voir `state: "failed"` ci-dessous | |
| `503 transcription_unavailable` | aucun fournisseur audio sur ce déploiement | **masquez le bouton micro** |
| `503 language_not_served` | rien ne transcrit cette langue (`reason` = la langue) | proposez d'écrire, ou une autre langue |

⚠️ **`audio_url` PEUT ÊTRE ABSENT SUR UNE RÉPONSE PAR AILLEURS NORMALE.** Cela
veut dire que l'archivage n'a pas répondu : la transcription, le plan et l'état
sont justes, mais la bulle n'aura pas de lecteur. **N'échouez pas** pour cela —
affichez la bulle avec son texte seul.

---

## 8 ter. 🆕 🎯 LE FIL D'UNE DEMANDE — 6 objectifs, 4 étapes au plus (v4.88.0)

L'assistant **garde le fil d'une demande en cours** et vous mène au résultat en
**quatre étapes au plus**. Il ne pose **qu'une** question par tour : celle de
l'étape manquante suivante.

### Les six objectifs, et leurs étapes dans l'ordre

| `goal.key` | Étapes (`slot`) | Ce qui agit |
|---|---|---|
| `profile` | `profile_edit` | enregistrer — après confirmation |
| `orders` | `reorder` | un plan **aux prix du jour**, à confirmer |
| `wallet` | `topup` **ou** `wallet_history` | recharger — après confirmation |
| `ride` | `pickup` → `dropoff` → `vehicle_class` → `payment` | le plan de course, à confirmer |
| `food` | `dishes` → `delivery_address` → `payment` | le plan de commande, à confirmer |
| `track` | `which` *(sautée s'il n'y en a qu'une)* | lire l'état ; appeler, écrire ; annuler après confirmation |

### Ce que chaque réponse porte

Sur `POST /analytics/ai/chat`, `POST /analytics/ai/voice`, et sur
`GET /analytics/ai/thread` :

```jsonc
"goal": {
  "key": "ride",
  "step": 3, "steps": 4,
  "filled": {
    "pickup":  { "label": "Ma position — Moursal", "point": [15.05, 12.11] },
    "dropoff": { "label": "Marché central" }
  },
  "asking": "vehicle_class",
  "missing": ["vehicle_class", "payment"],
  "options": [
    { "value": "moto", "label": "Moto", "price_xof": 700 },
    { "value": "eco",  "label": "Éco",  "price_xof": 1000 }
  ]
}
```

⚠️ **`goal` est ABSENT quand aucune demande n'est en cours** (`goal: null`).
Une application qui l'ignore se comporte **exactement comme avant** : c'est ce
qui permet de le servir sans attendre que vos deux applications l'adoptent.

⚠️ **`step` compte ce qui est SU, pas le rang de la question.** Trois choses
connues et une seule manquante donnent « étape 4 sur 4 », même si c'est la
**première** étape de la liste qu'on vous demande. Dire « 1 sur 4 » à quelqu'un
qui a presque fini est exactement ce qui fait abandonner.

⚠️ **`options` n'arrive QUE pendant qu'on demande quelque chose.** Sur une
demande complète, il n'y a plus de boutons : un appui aurait rouvert une étape
déjà réglée.

⚠️ **Et `options` n'est jamais servi sur `GET /thread`** — ces choix portent des
**prix**, et un fil se relit des heures plus tard. Un tarif d'avant la
majoration, lu à voix haute, se découvrirait au moment de payer. Redemandez
l'état par un message si vous avez besoin des prix.

### Un appui : `choice`

```jsonc
POST /analytics/ai/chat
{ "message": "Moto", "choice": { "slot": "vehicle_class", "value": "moto" } }
```

⚠️ **Envoyez `choice` quand la personne APPUIE sur un bouton**, pour que nous
n'ayons pas à re-deviner le mot. Un appui est une certitude ; le repasser au
modèle rendrait incertain ce qui était sûr — et pour Dira Lite, une étape mal
comprise se repose à voix haute.

⚠️ **Un `choice` périmé est IGNORÉ, pas refusé.** Si vos boutons sont restés à
l'écran après l'abandon d'une demande, l'appui ne provoque pas d'erreur : le
tour se poursuit comme un message ordinaire.

### Repartir de zéro : `reset_goal`

```jsonc
{ "message": "", "reset_goal": true }
```

⚠️ **Mettez-le derrière un bouton « Nouvelle demande ».** Les trois autres
sorties — confirmer, « laisse tomber », demander autre chose — passent par le
modèle ; celle-ci n'a pas besoin d'être **comprise**, et c'est la seule dont
dispose une personne coincée.

### Ce que vous n'avez PAS à faire

⚠️⚠️ **Le contexte vit sur le SERVEUR.** Ne le renvoyez pas, ne le stockez pas :
une application relancée, ou le passage du widget à l'application, **reprend**
la demande. À l'ouverture, lisez `GET /analytics/ai/thread` et proposez
« On reprend : un taxi vers le marché central, il manque le mode ».

⚠️ **Une demande est abandonnée après 30 minutes d'inactivité**, et `thread` ne
la propose plus. Reprendre un taxi de ce matin ce soir ferait croire que
l'assistant n'écoute pas.

⚠️ **Une question à côté ne fait pas perdre le fil.** « Combien j'ai sur mon
portefeuille ? » au milieu d'un taxi : on répond, et la demande reste entière.

⚠️ **Une correction ne change qu'une étape.** « Plutôt une moto » garde le
départ et l'arrivée — et le paiement déjà choisi.

### 🚧 Ce qui n'est pas encore là

⚠️ **`register=simple` n'est PAS accepté** (#239). Il doit agir sur la réponse,
qui vient du métier ; l'envoyer aujourd'hui ferait `422 unknown field` sur
chaque message. **Ne l'envoyez pas** : il arrivera quand les deux métiers
sauront le lire, et ce document le dira.

⚠️ **`reply_audio_url` et `POST /analytics/ai/speech` n'existent pas** (#239).
Nous transcrivons chez nous (`dira-stt`), nous ne **parlons** pas : aucune
synthèse n'est installée, et pour `shu`, `sba`, `ff`, `ewe`, `wo` aucune voix
grand public n'existe à ce jour. Continuez de lire avec la voix du téléphone
(`fr`, `en`, `ar` en repli pour `shu`).

---

## 8 bis. 🆕 🧵 LE FIL DE CONVERSATION — il ne s'évapore plus (v4.84.0)

```
GET /analytics/ai/thread
  →
{ "country": "GA",
  "message_count": 42,            // TOUT ce qui a été dit, même hors fenêtre
  "updated_at": "2026-10-28T09:12:04Z",   // ⚠️ ABSENT si rien n'a jamais été dit
  "messages": [
    { "id": "68f0…", "role": "client", "text": "emmène-moi à l'aéroport",
      "voice_id": "68f0…",
      "audio_url": "https://files.dira.llc/voice/…/a1b2.m4a",  // RÉÉCOUTABLE
      "seconds": 7, "at": "2026-10-28T09:12:01Z" },
    { "id": "68f1…", "role": "assistant", "text": "Voici deux options.",
      "vertical": "vtc", "plan": { … }, "at": "2026-10-28T09:12:03Z" },
    { "id": "68f2…", "role": "support", "text": "Course commandée depuis votre message.",
      "ref_kind": "ride", "ref_id": "68f3…", "at": "2026-10-28T09:19:40Z" }
  ] }
```

**Les échanges avec l'assistant étaient volatils** : la réponse partait, rien
n'était gardé, et fermer l'application perdait tout. Ils sont désormais
**écrits**, par compte et **par pays**. Appelez cette route à l'ouverture de la
boîte de dialogue : c'est l'historique que la personne s'attend à retrouver.

### Les trois rôles

| `role` | Qui parle | Ce que vous montrez |
|---|---|---|
| `client` | la personne | sa bulle. ⚠️ Avec un **lecteur audio** quand `audio_url` est là |
| `assistant` | l'IA | sa bulle, et le `plan` s'il y en a un |
| `support` | **un être humain** de chez nous | ⚠️ une bulle **visiblement différente** |

⚠️⚠️ **LE RÔLE `support` N'EST PAS DÉGUISÉ EN ASSISTANT, ET VOUS NE DEVEZ PAS LE
DÉGUISER NON PLUS.** Quand quelqu'un a repris la main — parce que l'IA n'avait
pas compris —, la personne doit le **voir**. Lui laisser croire que « l'IA » a
commandé pour elle rendrait inexplicable le jour où l'IA ne le fait pas, et
transformerait un service rendu en comportement erratique.

### ⚠️ UNE FENÊTRE, PAS UN HISTORIQUE

La route rend les **200 derniers** messages ; `message_count` dit combien il y
en a eu **en tout**. Quand les deux diffèrent, écrivez-le (« les 200 derniers
messages ») plutôt que de laisser croire que c'est tout.

> La borne n'est pas une économie : un document de base de données plafonne, et
> un fil sans limite finit par refuser l'écriture qui l'aurait sauvé — la
> personne perdrait alors **tout**, au lieu de perdre le plus ancien.

### ⚠️ UN FIL PAR PAYS

Un voyageur qui commande à Lomé puis à Dakar a **deux** fils, et cette route
rend celui du pays de la requête (`X-Dira-Country`). Ses deux histoires ne se
mêlent pas, et l'exploitation d'un pays ne lit que la sienne. Ne mettez donc
pas en cache un fil au-delà d'un changement de pays.

### ⚠️ UN FIL VIDE EST UNE RÉPONSE NORMALE, ET IL N'INVENTE RIEN

Un compte qui n'a jamais rien dit reçoit `200` avec `messages: []` et
`message_count: 0` — jamais un `404`. Vous pouvez donc itérer sans garde.

⚠️ **ET `updated_at` EST ALORS ABSENT**, délibérément : il n'y a pas de date à
formater. Nous servions auparavant `0001-01-01T00:00:00Z`, la date nulle du
langage, et une application qui la formate affiche « 31/12/0000 » sous une
conversation vide. Il n'y a pas non plus d'`id` de fil : les deux routes le
désignent par la personne, et l'identifiant nul que nous servions avait
l'apparence d'un vrai.

### Ce que la route N'EST PAS

- **Elle n'envoie rien.** On écrit avec `POST /analytics/ai/chat` (texte) ou
  `POST /analytics/ai/voice` (vocal) ; chacun ajoute les deux tours au fil tout
  seul, et vous n'avez **rien** à pousser vous-même.
- **Elle ne pagine pas**, et n'a pas de curseur : la fenêtre est la réponse.
- **Elle ne porte aucune liste de courses ni de commandes.** Un message peut
  nommer celle qui en est née (`ref_kind` / `ref_id`) ; l'historique des
  activités est une autre route.

---

## 9. 🆕 LE FIL D'ACTIVITÉ — courses et commandes, par semaine (v4.43.0)

```
GET /analytics/activity?weeks=4&before=2026-10-05T00:00:00Z
```

```jsonc
{
  "timezone": "Africa/Libreville",
  "sources": { "vtc": "ok", "food": "ok" },
  "weeks": [
    { "start": "2026-10-05T00:00:00+01:00",
      "end":   "2026-10-11T23:59:59+01:00",
      "iso_year": 2026, "iso_week": 41,
      "totals": { "vtc":  { "count": 2, "amount": 4500 },
                  "food": { "count": 1, "amount": 5400 } },
      "items": [
        { "vertical": "vtc", "id": "…", "at": "2026-10-07T18:00:00Z",
          "status": "completed", "amount": 2500, "currency": "XAF",
          "details": { "mode": "pool", "pickup": "Tokoin",
                       "dropoff": "Aéroport", "distance_m": 9800 } },
        { "vertical": "food", "id": "…", "at": "2026-10-06T12:00:00Z",
          "status": "completed", "amount": 5400, "currency": "XAF",
          "details": { "first_item": "Poulet braisé", "lines": 2,
                       "items_count": 3, "payment_method": "wallet" } }
      ] }
  ],
  "next_before": "2026-10-05T00:00:00+01:00",
  "has_more": true
}
```

### ⚠️ LE PAS DE PAGINATION EST LA SEMAINE, jamais la ligne

Redonnez **`next_before`** pour la page suivante. `weeks` dit combien de
semaines vous voulez (défaut **4**, plafond **12** — au-delà, plafonné **en
silence**, et `has_more` vous dit de continuer).

⚠️ **UNE SEMAINE N'EST JAMAIS COUPÉE ENTRE DEUX PAGES**, et c'est pour ça que
le pas n'est pas la ligne : un en-tête « 3 courses · 12 000 F » servi sur la
moitié d'une semaine mentirait.

⚠️ **LES SEMAINES VIDES SONT SAUTÉES.** Entre deux commandes espacées d'un
mois, vous recevez **deux** semaines, pas les cinq qui les séparent. Une
application qui compterait les semaines pour afficher un calendrier doit lire
`start` et `iso_week`, pas l'index dans la liste.

### ⚠️ LES SEMAINES SONT DÉCOUPÉES DANS LE FUSEAU DU PAYS

Lundi 00:00 → dimanche 23:59:59, **dans `timezone`** — pas en UTC.

Le Gabon et le Tchad sont à UTC+1 : une course prise **lundi 00 h 30** à
Libreville a eu lieu dimanche 23 h 30 en UTC, et un découpage en UTC la
rangerait dans la semaine **précédente**. `start` et `end` sont servis **avec
leur décalage** ; `timezone` est là pour que vous affichiez les mêmes bornes
que nous.

⚠️ **`iso_year` N'EST PAS TOUJOURS L'ANNÉE DE LA DATE.** Le 29 décembre 2025
appartient à la **semaine 1 de 2026**. Afficher l'année du calendrier à côté du
numéro ISO donnerait « semaine 1 de 2025 ».

### ⚠️ `sources` — ce qui a répondu, et ce qui n'a pas répondu

`"ok"` ou `"unavailable"`, par métier.

Un métier muet **ne vide pas** l'historique de l'autre : vous recevez ce qui
existe. Mais **dites-le** — « vos commandes n'ont pas pu être chargées » — car
un fil où la moitié manque sans le dire se lit comme un fil **complet**, et le
client croit avoir perdu ses commandes.

⚠️ **UN JETON REFUSÉ REFUSE LE FIL ENTIER** (`401`), et c'est voulu : servir la
moitié d'un historique à quelqu'un qu'on n'a pas pu identifier serait pire que
de refuser.

### ⚠️ C'est un FIL, pas l'objet

Une ligne porte de quoi **dessiner une ligne** : le métier, l'instant, le
statut **du métier** (verbatim), le montant, et des **faits** dans `details`.

| `vertical` | `details` |
|---|---|
| `vtc` | `mode`, `pickup`, `dropoff`, `distance_m` |
| `food` | `first_item`, `lines`, `items_count`, `payment_method` |

⚠️ **DES FAITS, PAS UNE PHRASE, ET C'EST À VOUS D'ÉCRIRE LA PHRASE.** Fabriquer
« Course vers l'aéroport » côté serveur aurait demandé de traduire, de choisir
une casse et une longueur — trois décisions d'écran prises loin de l'écran.

⚠️ **`details` PEUT ÊTRE ABSENT OU INCOMPLET.** Une course sans adresse tapée
n'a pas de `pickup`. N'affichez pas un tiret là où il n'y a rien à dire.

⚠️ **POUR LE DÉTAIL, ALLEZ CHEZ LE MÉTIER** — `GET /vtc/rides/{id}`,
`GET /food/orders/{id}`. Ils sont la **seule** source de vérité : le fil ne
porte ni la répartition de l'argent, ni le parcours, ni la conversation.

---

---

## 9 bis. 🧾 LES REÇUS ET LES RELEVÉS, EN PDF (v4.52.0)

Quatre routes, deux de chaque côté, et une seule entrée dans l'application :

| | Reçu d'une opération | Relevé d'une période |
|---|---|---|
| courses | `GET /rides/{id}/receipt.pdf` | `GET /rides/statement.pdf?from=&to=` |
| commandes | `GET /orders/{id}/receipt.pdf` | `GET /orders/statement.pdf?from=&to=` |

`application/pdf`, `attachment`, `no-store`, nom de fichier déjà daté. **Ne les
ouvrez pas dans une WebView** : laissez le système les enregistrer ou les
partager.

Les deux documents portent la **distance réellement parcourue** — mesurée sur le
trajet de l'agent — et **nomment leur source** : `tracked` mesurée, `planned`
estimée quand le suivi n'a rien gardé. Le second cas est **courant**, pas rare.

> ⚠️ **LE DÉTAIL S'ADDITIONNE JUSQU'AU TOTAL, ou le serveur ne sert rien** (500).
> Un reçu dont les lignes ne tombent pas juste prouve une erreur, sous notre nom,
> devant le comptable du client.

> ⚠️ **ET DEUX RELEVÉS, PAS UN.** C'est le seul endroit où l'application unifiée
> ne peut pas réunir les deux métiers, et il vaut mieux le savoir avant de dessiner
> l'écran : un relevé mêlé exigerait un document unique calculé par un service qui
> voit les deux, et il n'existe pas. Proposez donc **deux exports** dans l'écran
> d'activité (§9) — « mes courses » et « mes commandes » — plutôt qu'un bouton qui
> promettrait un seul fichier.

> ⚠️ **`from` ET `to` OBLIGATOIRES, SANS DÉFAUT**, des deux côtés : un défaut
> implicite produirait une somme sur une autre période que celle demandée. Et les
> deux **refusent au lieu de tronquer** — `409 statement_too_large` au delà de 500
> opérations ou de 366 jours, avec `meta.max_rows`. Proposez de découper la
> période.

Détail de ce que chaque document contient, et des pièges propres à chaque
métier : `VTC-CLIENT` et `FOOD-CLIENT`, section « Le reçu et le relevé ».

## 10. Les deux métiers — l'inventaire, et où chacun est décrit

Rien ne change dans ces parcours : les routes, les formes et les refus sont
ceux des deux specs métier, qui restent à jour.

### Livraison → **`FOOD-CLIENT.md`** (base `…/api/v1/food`)

| Ce que vous faites | Routes |
|---|---|
| Découvrir | `GET /stores` · `GET /stores/{id}` · `GET /dishes/{id}` · `GET /feed` |
| Commander | `POST /orders` · `GET /orders` · `GET /orders/{id}` · `POST /orders/{id}/cancel` |
| | 🎟️ **LE CODE PROMO SE SAISIT À LA COMMANDE (v4.51.0)** — `{ "code": "…" }`. ⚠️ **Un code refusé fait ÉCHOUER la commande** plutôt que de la créer au prix plein : gardez le panier à l'écran. `promo_code_xof` est **distinct de `discount`**, qui porte un crédit de tombola. Détail : §6 quater et `FOOD-CLIENT` §4 |
| Suivre | `GET /deliveries/{id}` · socket du suivi (§11) |
| Reçu, relevé | 🧾 `GET /orders/{id}/receipt.pdf` · `GET /orders/statement.pdf?from=&to=` **(v4.52.0)** — et `delivery.actual_distance_m` + `distance_source`, enfin servis au client ; `country` aussi, qui **dit la monnaie** des montants |
| Parler | `GET/POST /orders/{id}/messages` |
| Noter | `POST /orders/{id}/rating` |
| Nutrition, tombola | `GET /nutrition/profile` · `GET /nutrition/plan` · `GET /tombola/draws` · `GET /tombola/me` |

### Courses → **`VTC-CLIENT.md`** (base `…/api/v1/vtc`)

| Ce que vous faites | Routes |
|---|---|
| Choisir | `GET /classes?near=` · `GET /settings/modes?near=` |
| | 🛵 **Le MOTO-TAXI est un mode du catalogue (v4.48.0)**, ouvert dans tous les pays et **en tête** de la liste : `seats: 1`, pas de course partagée (`modes` n'a pas de clé `pool`), `icon_url` `null` le temps que l'exploitation pose son image — dessinez la silhouette `map_icon` (`moto`). ⚠️ **Aucune voiture ne répond à un appel de moto** : ne promettez pas « une voiture si aucune moto n'est libre ». Détail : `VTC-CLIENT` §2 |
| Chiffrer | `POST /rides/quote` · `POST /rides/pool/quote` · `POST /rides/rental/quote` |
| | 🎟️ **LE CODE PROMO SE SAISIT AU DEVIS (v4.51.0)** — `{ "code": "…" }`, et la remise revient **par classe** : 20 % d'une moto et 20 % d'un van ne font pas le même nombre de francs. ⚠️ `promo_code_ignored` dit qu'un code **valable** a été battu par une offre automatique : ce n'est **pas** un refus, et le dire évite l'appel au support. Détail : §6 quater et `VTC-CLIENT` §3 |
| | ⚠️ **La course PARTAGÉE cherche le CO-PASSAGER *avant* le chauffeur** — c'est le seul mode qui le fasse, et l'écran d'attente en dépend : pendant la première étape (`dispatch_state: "pooling"`, jusqu'à 5 min) **aucun chauffeur n'est appelé**. Principe complet, chronologie des deux passagers, conditions d'appariement, ordre de route et les quatre fins possibles : `VTC-CLIENT` §4 quater, **à lire avant de câbler un écran** |
| Commander | `POST /rides` · `GET /rides` · `GET /rides/{id}` · `POST /rides/{id}/cancel` · `POST /rides/{id}/relaunch` |
| | 💸 **ANNULER PEUT COÛTER (v4.50.0)** — `cancellation` voyage avec la course : `fee_xof`, `step`, `why`, `grace_left_s`. ⚠️ **Retenir de l'argent à quelqu'un qui n'a pas pu le lire AVANT est indéfendable** : l'écran de confirmation dit le montant, ou dit que c'est gratuit **et jusqu'à quand**. Jamais de bouton « Annuler » nu quand `fee_xof > 0`. Détail : `VTC-CLIENT` §5 |
| | 🏷️ **LE MOTIF D'ANNULATION EST NOMMÉ (v4.55.0)** — `POST /rides/{id}/cancel { "reason_code": "…", "reason": "…" }`, et la liste se **demande** : `GET /rides/cancel-reasons` → `{ by, items: [{ code, grave }] }`. ⚠️ **N'écrivez pas les codes en dur** : la liste dépend du rôle du jeton, et le serveur refuse un code de l'autre rôle (`422`, `fields: ["reason_code"]`). ⚠️ **Traduisez-les chez vous** : la route sert des codes, pas des phrases. ⚠️ **Le code s'AJOUTE à la phrase libre**, il ne la remplace pas — le code se compte, la phrase explique le cas ; `other` existe pour ce qu'aucun code ne couvre. 🔎 **`driver_asked` (« le chauffeur m'a demandé d'annuler ») est un signal de FRAUDE** : proposez-le en clair, sinon le geste est indistinguable d'un renoncement. Détail : `VTC-CLIENT` §5 |
| En route | `PATCH /rides/{id}/stops` · socket du suivi (§11) |
| | 📡 **SUIVRE L'APPEL, ET CHOISIR UNE VOITURE (v4.75.0)** — pendant `dispatch_state: "calling"`, la course porte **`dispatch_call`** : `center` (⚠️ **servi** : en partage, vous ne connaissez pas l'autre départ), **`radius_m` de la tentative EN COURS déjà élargie** (⚠️ le rayon de départ au 4ᵉ essai dessinerait un cercle où plus personne n'est appelé), `attempt` / `max_attempts`, `attempt_ends_at` (un **instant**), et `ringing[]` — les points des voitures qu'on sonne, **sans nommer personne et sans aucun compte de refus** (⚠️ « 2 ont refusé » n'apprend qu'une chose au passager, et c'est faux : un chauffeur qui ne répond pas est au volant). ⚠️ **La mise à jour arrive par une trame dont le statut NE CHANGE PAS** — un écran qui l'ignore ne verra jamais le rayon s'élargir. 🆕 **`POST /vtc/rides/{id}/pick { ref }`** sonne **en premier** la voiture choisie sur la carte (⚠️ **pas « seulement elle »** : la recherche continue seule). ⚠️ `409 pick_driver_gone` = « cette voiture vient d'être prise », pas une panne. ⚠️ **Le chauffeur reste anonyme jusqu'à son acceptation** : n'inventez pas de carte vide. Détail : `VTC-CLIENT` |
| | 🛰️ **LE RADAR — les voitures libres autour de vous (v4.74.0)** — `GET /vtc/nearby-drivers?lng=&lat=` : les véhicules **libres** du vivier d'appel, **avant** de commander. ⚠️ **`enabled: false` → l'écran n'existe pas** (pas de carte vide, pas de bouton grisé) : c'est le **défaut**, chaque pays l'ouvre. ⚠️ **Aucun chauffeur n'est identifié** — un point porte `position`, `type`, `distance_m` et **rien d'autre**, pas même un identifiant d'affichage : deux balayages ne se recollent pas, donc dessinez un **état**, jamais un mouvement. ⚠️ `type` sert au **pictogramme**, pas à promettre un prix ; `distance_m` est à **vol d'oiseau**, jamais une durée ; `count` est le nombre **servi** (borné), jamais « 12 voitures disponibles ». ⚠️ **Une liste vide n'est pas « personne ne viendra »** : le dispatch cherche plus loin et plus longtemps — **laissez commander**. ⚠️ `mode: "auto"` → balayez en respectant `refresh_s` et **suspendez** quand la carte n'est pas à l'écran ; `on_demand` → **aucun** balayage spontané. Détail : `VTC-CLIENT` |
| | 🔗 **PARTAGER SON TRAJET (v4.66.0)** — `POST /rides/{id}/share` et `POST /scheduled-rides/{id}/share` : un proche ouvre une page **sans compte** et voit la voiture avancer. ⚠️ **`token` n'apparaît qu'une fois, à la création** — le serveur ne le garde que haché et ne peut plus le relire : **partagez immédiatement**, ne le stockez pas. ⚠️ **`url` est composée par le serveur**, ne la fabriquez pas — et depuis la **v4.73.0 elle est TOUJOURS servie**, avec `deep_link` à côté. ⚠️⚠️ **Ce contrat demandait, quand `url` manquait, de partager le jeton brut** : un passager a reçu « Code de suivi de mon trajet Dira (l'adresse du lien n'est pas disponible) : fDj3MQoA… », quarante-trois caractères de hasard envoyés à quelqu'un qui n'a rien pour les ouvrir. **Retirez ce repli.** `url` (`https://…`) va dans la conversation et s'ouvre chez tout le monde ; `deep_link` (`dira://trip/…`) va dans le **QR code**, dessiné par l'application — jamais l'inverse : un `https` scanné ouvre un navigateur par-dessus l'application, et un `dira://` collé dans une conversation n'ouvre rien chez qui n'a pas l'application. ⚠️ **`minutes: 0` = « jusqu'à mon arrivée », et c'est le défaut à proposer en premier** ; `expires_at` est toujours servi et c'est un **plafond**, pas une promesse. ⚠️ `POST /trip-shares/{id}/revoke` l'éteint tout de suite — mettez ce bouton **sur l'écran de la course**. ⚠️ Lisez `GET /settings/share?near=` : `enabled: false` → **pas de bouton**, et `choices_min` sont les durées à dessiner. Détail : `VTC-CLIENT` §6 bis |
| Reçu, relevé | 🧾 `GET /rides/{id}/receipt.pdf` · `GET /rides/statement.pdf?from=&to=` **(v4.52.0)** — distance **réellement parcourue** et sa source ; une course annulée porte désormais `cancel_fee_xof` |
| Parler, noter | `GET/POST /rides/{id}/messages` · `POST /rides/{id}/rating` |
| | 🛡️ **CHAQUE MESSAGE PASSE PAR UN FILTRE (v4.66.0)** — une **insulte part** et est signalée à l'exploitation ; une **menace ne part pas** (`422`, `message_blocked`). ⚠️ **Gardez le texte dans la zone de saisie sur un refus** : le vider fait perdre ce qu'on venait d'écrire, et la personne recommence en pire. ⚠️ **N'affichez pas le message refusé dans le fil** — une bulle « non envoyé » laisse croire à un problème de réseau et fait retenter dix fois. ⚠️ `hidden: true` = retiré par un modérateur : `body` vide, **la ligne reste**, affichez « message retiré » — l'autre partie a vu qu'il y avait quelque chose là. ⚠️ **N'embarquez aucune liste de mots** : l'exploitation la change sans redéploiement. Détail : `VTC-CLIENT` §7 et `FOOD-CLIENT` §6 |
| | 🎯 **DES OBJECTIFS À GAGNER (v4.59.0)** — `GET /me/challenges`, **au socle** et **commun aux deux métiers** : « 5 commandes ce mois-ci » compte vos commandes, « dépensez 20 000 F » compte les deux. ⚠️ `percent` **déjà borné à 100** ; `value` **et** `target` ensemble (« 3 sur 5 », pas « 60 % ») ; `reached` affiché **avant** `paid_xof`, et `pending` dit en clair « bonus en cours de versement ». ⚠️ **Ni budget ni nombre de gagnants servis** — les afficher ferait une course aux places. ⚠️ Une commande multi-boutiques compte pour **une**. Détail : §15 et `VTC-CLIENT` §🎯 |
| | 🔒 **CE QUE VOUS AVEZ LE DROIT D'AFFICHER du chauffeur ou du livreur est réglé par l'exploitation, pays par pays et métier par métier (v4.49.0).** Le serveur **n'envoie pas** ce qu'on n'a pas le droit de montrer : un champ absent n'est pas une panne, c'est un champ fermé. ⚠️ `phone` présent ne veut **pas** dire affichable — lisez `show_phone` ; `direct_call` sans `show_phone` veut dire « un bouton qui appelle, et le numéro nulle part ». ⚠️ **N'écrivez aucune règle en dur** : ces décisions changent sans redéploiement. Détail : `VTC-CLIENT` et `FOOD-CLIENT`, section Confidentialité |
| Programmer, s'abonner | `POST /scheduled-rides` · `POST /subscriptions` |

⚠️ **`?near=` SUR LES DEUX ROUTES DE RÉGLAGE DES COURSES.** Passez le point de
départ : sans lui, vous lisez les modes et le catalogue du pays du **compte**,
et vous recevez les prix du pays où la course se fera. Deux pays dans un seul
écran.

---

## 11. ⚠️ Temps réel : DEUX sockets, et il faut les deux

| Canal | Base | Ce qui arrive | Portée |
|---|---|---|---|
| **Socket des commandes** | `wss://api…/api/v1/food/ws/orders?token=` | `order_created`, `order_status { from, status }`, `order_message` | application ouverte, **toutes** vos commandes |
| **Socket du suivi** | `wss://tracking…/track/subscribe/{id}?token=` | `position`, et `status` à chaque passage — **les deux portent leur `mission_id`** (v4.95.0) | application ouverte, **une** opération à la fois — `{id}` est l'identifiant de la **course** ou de la **livraison** |
| **Push** | socle, `POST /me/devices` | un gabarit **et** des données (`type`, l'identifiant, `status`) | application fermée ou en arrière-plan |

### 🆕 ⚠️⚠️ UN REFUS DU SOCKET DU SUIVI DIT LAQUELLE DES DEUX (v4.95.0)

Jusqu'ici le suivi refusait **`401`** pour deux choses qui n'ont rien à voir :
un jeton expiré — qu'il faut rafraîchir, après quoi ça marche — et une opération
qui n'est pas la vôtre, qu'aucun rafraîchissement ne rendra suivable. La boucle
« `401` → rafraîchir → redemander → `401` » tournait indéfiniment.

| statut | corps | ce qu'il faut faire |
|---|---|---|
| `401` | `{"error":"unauthorized"}` | **rafraîchir** (`POST /auth/refresh`) puis rouvrir |
| `403` | `{"error":"mission_forbidden","mission_id":"…"}` | **cesser d'essayer** : cette course ou cette commande n'est pas la vôtre |

⚠️ **C'est le STATUT qui porte la décision, pas le corps** : une poignée de main
échouée ne rend pas toujours son corps au client. Et ces refus tombent **avant**
que la socket n'existe — à ne pas confondre avec le code de fermeture
`4401 token_expired`, qui arrive sur une connexion déjà ouverte.

⚠️ **Et les trames `position` et `status` portent leur `mission_id`.** Toujours,
même sur la route à une seule opération : aiguillez dessus plutôt que sur
l'ordre d'arrivée.

⚠️ **DEUX SOCKETS, DEUX SERVICES, DEUX CYCLES DE VIE. NE LES FACTORISEZ PAS**
sous prétexte que ce sont deux WebSockets. Celui des commandes est global et
vit tant que l'application est ouverte ; celui du suivi est **par opération**
et se referme quand l'écran se ferme. Une couche unique « le socket » finit par
fermer le mauvais.

⚠️ **LES COURSES N'ONT PAS DE SOCKET GLOBAL.** Il n'existe rien qui annonce
« une de vos courses a changé » : pour une course, c'est le socket du suivi de
**cette** course, ou le push. Une application unifiée qui attendrait la
symétrie des deux métiers attendrait une trame qui n'arrive jamais.

### Le flux, toujours le même — **un signal, un `GET`**

1. **À l'ouverture d'un écran** : `GET` la ressource. C'est l'état de
   référence — jamais ce que dit le socket.
2. **Trame d'état reçue** : comparez à ce que vous affichez ; si ça diffère,
   `GET` et redessinez. Un `from` qui n'est pas votre état signale une trame
   manquée — relisez.
3. **Push reçu** (arrière-plan) : ouvrir l'écran, `GET`.
4. **Reconnexion** (back-off 1 s → 2 s → 4 s … 30 s) : `GET` **immédiatement**,
   avant d'appliquer la moindre trame.
5. **Sans socket** : sondez toutes les **10 s** tant que l'opération n'est ni
   terminée ni annulée, 30 s au bout de cinq minutes sans changement. Jamais
   sur une opération terminée.

⚠️ **AUCUN CANAL NE GARANTIT L'ORDRE, L'UNICITÉ NI LA LIVRAISON.** Deux trames
pour le même passage (socket **et** push) sont normales : le second `GET` répond
la même chose. Une application qui ferait du socket sa source de vérité verrait,
un jour, une course « en route » qu'un `GET` dit terminée.

### ⚠️ CE QUE CETTE RÈGLE A ÉVITÉ, POUR DE VRAI (v4.65.0)

Le socket du suivi envoyait `{"type":"status","status":"completed"}` **à
l'arrivée du chauffeur au point de départ**, et sur une course **annulée** :
un effet de bord du gel du parcours côté suivi, pas un état de l'opération.
Corrigé — mais c'est **le point 2 ci-dessus** (une trame ⇒ un `GET`) qui
distinguait les applications qui n'ont rien vu de celles qui affichaient une
course terminée pendant que la voiture se garait devant la porte.

**Et sur une course partagée, la carte du second passager restait vide du
départ à l'arrivée** : le chauffeur ne pousse ses positions que sous une
seule des deux courses. Le serveur les recopie maintenant aux deux —
abonnez-vous simplement à **votre** `ride_id`. Les deux courses d'un partage
ne se terminent **pas** ensemble, et c'est normal : voir `VTC-CLIENT.md` §6.

---

## 🆘 LE BOUTON D'ALERTE — SOS (v4.56.0) — **SOCLE** (sans `/vtc` ni `/food`)

Un bouton, les **deux** métiers, **une seule** file côté exploitation. C'est
l'un des rares endroits où l'application unifiée n'a rien à réunir : le socle
sert déjà le même SOS pour une course et pour une livraison.

```
GET  /sos/settings          → ce que ce pays décide (détections, délai, numéros)
POST /sos                   → DÉCLENCHER
GET  /sos/me                → mon alerte en cours (200 avec `alert: null`)
POST /sos/{id}/position     → où je suis MAINTENANT
POST /sos/{id}/cancel       → « fausse alerte »
```

```jsonc
POST /sos
{ "source": "button", "confirmed": true,
  "lat": 14.6928, "lng": -17.4467, "accuracy_m": 15, "battery": 7,
  "vertical": "vtc", "ride_id": "…",       // ou "food" + delivery_id
  "note": "un homme me suit" }
→ 201 { "id": "…", "status": "open", "trigger": "bouton pressé", … }
```

⚠️⚠️ **LA RÈGLE QUI GOUVERNE TOUT : UNE ALERTE NE SE PERD JAMAIS.** Aucun champ
n'est obligatoire — **pas même la position**. N'attendez pas un point GPS pour
envoyer : « on ne sait pas où il est » est une alarme qu'un opérateur traite en
premier, pas une requête à compléter. Envoyez d'abord, poussez la position
ensuite. Et **réessayez en boucle jusqu'à un `2xx`**, en tête de votre file
hors-ligne et sans attendre la fenêtre de synchronisation : une alerte remise
trois minutes plus tard ne sert plus à personne.

⚠️ **N'AJOUTEZ AUCUNE VALIDATION DE VOTRE CÔTÉ.** Pas de position obligatoire,
pas de motif à choisir, pas de formulaire. Tout ce qui est bancal est **corrigé
par le serveur**, jamais rejeté.

⚠️ **LE DOUBLE APPUI N'EST PAS UNE ERREUR** : le serveur rend la même alerte et
y ajoute la position. Ne désactivez donc **pas** le bouton après le premier
appui, et n'affichez pas « déjà envoyé » comme une erreur — appuyer encore
*améliore* l'alerte et rassure.

⚠️ **APPELEZ `GET /sos/me` AU DÉMARRAGE, TOUJOURS.** Un téléphone qui redémarre
après un choc, une application tuée par le système : sans cela, la personne
croit avoir appelé alors que non. `200` avec `alert: null` est le cas **normal**,
pas une erreur.

⚠️ **ANNULER NE SUPPRIME PAS L'ALERTE**, et l'exploitation la voit encore
quinze minutes. Dites-le (« l'exploitation a été prévenue et vous rappellera
peut-être ») : une annulation peut être **contrainte**, et promettre que « tout
est effacé » serait un mensonge. Pas de deuxième confirmation pour annuler — un
appui suffit.

### ⚠️⚠️ IL N'Y A AUCUN NUMÉRO À COMPOSER, ET CE N'EST PAS UN OUBLI (v4.58.0)

**Le téléphone de la personne en danger ne compose rien.** L'alerte part au
**service client** : un opérateur l'appelle, et c'est **lui** qui appelle les
secours s'il le faut.

`GET /sos/settings` **ne sert aucun numéro** — il n'y a pas de champ `numbers` à
lire. **N'en inventez aucun** : pas de 112, pas de numéro codé dans
l'application, pas de repli « au cas où ». Un écran qui proposerait « appeler la
police » enverrait quelqu'un composer un numéro que nous ne lui avons pas donné.

⚠️ **CE QUE VOUS AFFICHEZ À LA PLACE** : « **Le service client a été prévenu et
va vous appeler.** » C'est `calls_back`, et c'est la seule chose que la personne
cherche à savoir après avoir appuyé.

⚠️ **ET AUCUN BOUTON « APPELER DIRA » NON PLUS.** Il ferait patienter quelqu'un
en danger dans une file d'attente téléphonique pendant qu'un opérateur essaie de
le joindre sur la même ligne.

### ⚠️ LES DÉTECTIONS PROPOSENT, ELLES N'ENVOIENT PAS

`shake`, `crash`, `voice` ouvrent un **compte à rebours** de
`countdown_seconds` (défaut 10 s) avec **un** bouton « Annuler ». À
l'expiration, l'alerte part avec `confirmed: false`.

Un envoi direct remplirait la file de dos-d'âne, l'opérateur apprendrait à les
ignorer, et la vraie alerte se noierait. Et le compte à rebours part quand
personne n'annule parce que c'est tout l'intérêt : après un choc violent,
**personne n'annule parce que personne ne peut**.

⚠️ **`confirmed: false` EST PLUS GRAVE, PAS MOINS** — l'intuition dit l'inverse.
Le serveur le traite comme tel. Ne le présentez jamais comme « envoyé par
erreur ».

⚠️ **PENDANT LE COMPTE À REBOURS, PRÉPAREZ TOUT** (GPS, batterie, corps de la
requête) : un compte à rebours qui finit sur « recherche du GPS… » a gaspillé
dix secondes.

⚠️ **CÔTÉ CLIENT, SEULE `shake` A DU SENS.** Vous n'êtes pas au volant :
`crash` mesurerait un téléphone qui tombe du canapé. Lisez `GET /sos/settings`
et n'activez que celle-là.

### Où vit le bouton, dans une application qui fait les deux métiers

> - Sur l'écran de **suivi d'une course** : pendant toute la course, en deux
>   appuis au plus. Le passager est la partie la plus exposée — seul, dans le
>   véhicule de quelqu'un d'autre, sur un trajet qu'il ne choisit pas.
> - Sur l'écran de **suivi d'une livraison** : un livreur se présente à une
>   porte, parfois le soir, parfois à quelqu'un qui est seul.
> - **Pas sur l'accueil** : hors opération, il n'a pas de contexte à donner — et
>   le serveur l'accepte quand même, si vous choisissez de l'y mettre.
>
> ⚠️ **ET PAS DE VERROU D'APPLICATION SUR CET ÉCRAN** (`app_lock`) : un code
> secret entre quelqu'un et son bouton d'alerte serait indéfendable.

⚠️ **NE LE CONFONDEZ PAS AVEC LE SUPPORT.** Le support est une conversation
qu'on relit le lendemain ; le SOS est une alarme qu'un opérateur prend dans la
minute. Deux portes qui se ressemblent font perdre le temps qu'on n'a pas.

⚠️ **ET LE SOS N'ANNULE RIEN.** On peut déclencher l'alerte et rester dans le
véhicule — c'est même le cas le plus fréquent. N'enchaînez pas sur une
annulation.

Détail complet, écran par écran : `VTC-CLIENT` et `FOOD-CLIENT`, section 🆘.

---

## 12. ⚠️ Une seule boîte, deux vocabulaires

`GET /notifications` (socle) rend **tout** : les commandes et les courses, dans
une seule liste. Chaque entrée porte une **clé de gabarit** et des **données**.

| Métier | Clés | Données |
|---|---|---|
| Livraison | `order_confirmed` · `order_preparing` · `order_ready` · `order_assigned` · `order_delivered` · `order_cancelled` | `type: "order_status"`, `order_id`, `status` |
| Courses | `ride_accepted` · `ride_driver_on_the_way` · `ride_driver_arrived` · `ride_driver_honked` · `ride_cancelled` · `ride_search_exhausted` · `ride_fare_adjusted` · `ride_rate_prompt` | `type: "ride_status"`, `ride_id`, `status` |
| Courses partagées | `ride_pool_matched` · `ride_pool_no_match` · `ride_pool_partner_left` | `type: "ride_status"`, `ride_id`, `dispatch_reason` |
| Transverse | `ticket_reply` · `ticket_resolved` · `session_superseded` | selon le gabarit |

⚠️ **ROUTEZ SUR `data.type`, PAS SUR LA CLÉ.** `type` dit quel écran ouvrir,
l'identifiant dit lequel. La clé du gabarit est du **texte** : elle peut être
renommée par l'exploitation, et une application qui décide d'après elle ouvrira
un jour le mauvais écran. ⚠️ Et une clé **inconnue** doit s'afficher quand même,
dans la boîte, sans rien ouvrir : une application qui ignore ce qu'elle ne
connaît pas fait disparaître des messages.

⚠️ **UNE CLÉ NE DIT PAS SON MÉTIER.** `ticket_reply` peut concerner une
commande ou une course. Ne devinez pas le métier d'après la clé ; lisez
`data.type`.

---

## 13. Codes d'erreur à traiter nommément

**Transverses (socle)**

| Code | Geste |
|---|---|
| `401 invalid_token` | rafraîchir, puis reconnecter |
| `401 session_superseded` | le compte a été ouvert ailleurs — **déconnecter et le dire** |
| `403 wrong_app` | `error.reason` nomme l'application à ouvrir |
| `429 rate_limited` | réessayer plus tard, sans boucler |
| `422 validation_failed` | souligner `fields` |
| `403 account_closed` | ce compte a été **supprimé** — « se réinscrire », ou le support si c'est une erreur |

**La porte par code (§6 bis)**

| Code | Geste |
|---|---|
| `401 otp_invalid` | « code incorrect » — garder la saisie |
| `401 otp_expired` | « demandez un nouveau code » |
| `401 otp_too_many_attempts` | le code est **mort** : revenir à l'écran du numéro |
| `429 otp_too_soon` · `429 otp_too_many_requests` | « renvoyer » grisé jusqu'à `resend_after` ; au plafond horaire, proposer le support |
| `503 otp_delivery_failed` | **rien n'a été consommé** : redemander tout de suite est légitime |
| `403 otp_not_available` | ce compte se connecte par mot de passe — nommer l'application à ouvrir |

**Le combiné (§8, §8 bis, §9)**

| Code | Geste |
|---|---|
| `403 ai_disabled` · `503 assistant_unavailable` | masquer la boîte de dialogue |
| `429 ai_quota_reached` · `429 ai_budget_reached` | proposer la composition à la main |
| `503 verticals_unavailable` | aucun métier joignable — réessayer |
| `502 vertical_unreachable` | ce métier ne répond pas — réessayer |
| `413 audio_too_large` | **le vocal** dépasse 2 Mio — recommencer plus court |
| `422 unknown_language` | code de langue inconnu : un bogue d'application |
| ~~`502 transcription_failed`~~ | 🆕 v4.89.0 — n'est plus émis : un échec d'écoute arrive en `200` avec `state: "failed"` et l'`audio_url` |
| `503 transcription_unavailable` | masquer le **bouton micro** (le texte marche) |
| `503 language_not_served` | `reason` = la langue — proposer d'écrire, ou une autre langue |

**Les métiers** : leurs refus sont décrits dans leurs specs, et ils arrivent
**tels quels** à travers l'assistant combiné — traitez-les comme s'ils venaient
du métier.

---

## 14. Liste de contrôle d'intégration

- [ ] **Quatre** bases configurées, et une vérification à la construction que chaque route part de la bonne.
- [ ] `app: "client"` à la connexion ; `403 wrong_app` traité avec `error.reason`.
- [ ] Porte par code : compte à rebours rendu depuis `expires_at`, « renvoyer » gouverné par `resend_after`, nom demandé **seulement** si `created: true`, `dev_code` jamais nécessaire au fonctionnement.
- [ ] Verrou : `app_lock` relu à **chaque** réponse qui le porte (y compris le rafraîchissement) et appliqué à chaud ; code de secours toujours possible ; au-delà de `max_attempts`, **déconnexion** — jamais blocage.
- [ ] `X-Dira-Country` sur **chaque** requête ; monnaie formatée depuis le pays de l'**opération**.
- [ ] **`X-Dira-Edition`** sur **chaque** requête (§4 bis), et `?edition=` sur le WebSocket du suivi.
- [ ] L'édition **republiée** par le serveur est lue au moins une fois : recevoir `full` en se croyant `lite` veut dire que l'en-tête n'arrive pas.
- [ ] `POST /me/devices` porte `edition` : sans elle, une notification arrive **deux fois**, dont une dans l'application qui n'a rien commandé.
- [ ] **`GET /app/config`** lu à l'ouverture (§4 ter), gardé en mémoire, et les menus cachés dessus.
- [ ] Une clé **absente** de `features` est traitée comme **disponible**, jamais comme coupée.
- [ ] Les listes de `lists` sont **croisées** avec les listes réelles du métier, jamais affichées telles quelles.
- [ ] `403 feature_unavailable_in_edition` ne propose **pas** de se reconnecter ; `error.feature` dit quoi cacher.
- [ ] **Le vocal** (§8) : les **quatre** `state` traités — `understood`, `ordered`, `needs_support`, `failed` —, et jamais « c'est commandé » déduit de la présence d'un `plan`.
- [ ] `ordered.kind` ouvre le bon suivi (`ride` → la course, `order` → la commande).
- [ ] 2 Mio au plus, en opus/AAC/M4A/3GP — **pas de WAV** ; `audio_url` absent n'est pas une erreur.
- [ ] **`seconds` envoyé** avec le vocal : la durée mesurée par votre enregistreur, sans quoi la bulle annonce « 0:01 » pour cinq secondes de parole.
- [ ] **Le fil** (§8 bis) rechargé à l'ouverture de la boîte de dialogue, et **vidé au changement de pays**.
- [ ] Une bulle `client` avec `audio_url` porte un **lecteur**, et une bulle `support` est **visiblement humaine**.
- [ ] `message_count > messages.length` écrit « les 200 derniers messages », et ne laisse pas croire que c'est tout.
- [ ] `X-Request-ID` affiché sur les écrans d'erreur et joint aux rapports.
- [ ] Fond de carte : choix de la personne → pays où elle opère → `dira` ; la carte **renaît** au changement.
- [ ] `vertical` envoyé depuis un onglet, **omis** depuis l'accueil.
- [ ] `choices` traité : deux boutons, puis le même message avec `vertical`.
- [ ] Un plan ne commande rien : confirmation par la route du métier.
- [ ] `unresolved` affiché, pas tu.
- [ ] Fil d'activité : `next_before` redonné, semaines vides sautées, `timezone` utilisé, `sources` affiché quand un métier manque.
- [ ] Deux sockets distincts, cycles de vie séparés ; `GET` à chaque reconnexion.
- [ ] Notifications routées sur `data.type` ; clé inconnue affichée sans ouvrir.
- [ ] Un seul carnet d'adresses, un seul solde, une seule boîte.
- [ ] Suppression de compte : écran de conséquences **avant** la preuve d'identité, `erase_at` affiché, « les courses et commandes passées restent » dit **avant** le bouton, `409 wallet_not_empty` renvoyé vers le solde, `403 account_closed` traité à la connexion **et** à l'inscription.

---

## 15. Journal

### 4.84.0 — 28 octobre 2026

🎙️ **LE VOCAL EST GARDÉ, LE FIL NE S'ÉVAPORE PLUS, ET LE SUPPORT PEUT REPRENDRE
LA MAIN** (§8, §8 bis).

```
POST /analytics/ai/voice    multipart : file, language?, origin?, vertical?
    → { voice_id, audio_url, seconds, text, language, vertical, reply, plan,
        confidence, routed_by, state, ordered? }

GET  /analytics/ai/thread
    → { country, message_count, updated_at, messages[] }
```

Trois choses qui n'en font qu'une :

1. **Les échanges étaient volatils.** `POST /analytics/ai/chat` répondait et
   n'écrivait rien : fermer l'application perdait tout, et le support qui
   rappelait redemandait ce que la personne venait d'expliquer. Les deux tours
   sont désormais écrits, par compte et par pays.
2. **Le vocal était transcrit puis jeté.** L'audio est maintenant **gardé** et
   **réécoutable** (`audio_url`, porté par la bulle elle-même) : la
   transcription se trompe — « riz sauce » devient « riz gras » — et sans le
   fichier, un litige se tranche sur la parole de l'un contre celle de l'autre.
3. **Un humain peut reprendre la main.** Un message de rôle `support` dit qu'il
   l'a fait, et ⚠️ **il ne doit pas être déguisé en assistant** : laisser croire
   que « l'IA » a commandé rendrait inexplicable le jour où elle ne le fait pas.

⚠️⚠️ **ET UN RENVERSEMENT, DIT EN TOUTES LETTRES.** Cette spécification disait
depuis la v4.43.0 : « un vocal n'est jamais exécuté directement ». Elle reste
vraie **par défaut** — rien ne change si l'exploitation ne touche à rien — mais
un **seuil de confiance** peut désormais être posé dans la console, et alors la
commande part toute seule. `state` dit ce qui a été fait : `understood`
(confirmez, comme toujours), `ordered` (c'est parti — `ordered.kind` / `.id`),
`needs_support` (« nous vous rappelons »), `failed`.

⚠️ **TRAITEZ LES QUATRE ÉTATS MAINTENANT**, même si vous ne verrez
qu'`understood` : le seuil est un **réglage**, pas un déploiement, et `ordered`
peut arriver du jour au lendemain sans nouvelle version d'application.

⚠️ **NE DÉDUISEZ JAMAIS « c'est commandé » DE LA PRÉSENCE D'UN `plan`** : un
`plan` arrive dans les quatre cas. Seul `state` tranche.

⚠️ **UNE COMMANDE NÉE D'UN VOCAL EST TOUJOURS EN ESPÈCES**, jamais le
portefeuille : la personne paie à l'arrivée et peut refuser à la porte. Au
portefeuille, il faudrait un remboursement pour réparer ce qu'un « non merci »
règle.

⚠️ **ENVOYEZ `seconds`**, la durée que votre enregistreur a mesurée. Sans
elle, le serveur la déduit du poids, ce qui suppose un débit : cinq secondes de
parole à 32 kbit/s s'affichaient « 0:01 » (mesuré sur la préprod). Et
`updated_at` du fil est **absent** quand rien n'a jamais été dit — non
`0001-01-01`, qui s'affiche « 31/12/0000 » sous une conversation vide.

Le fichier : **2 Mio** au plus, en `m4a` / `aac` / `mp3` / `ogg` / `opus` /
`webm` / `3gp` — **le WAV est refusé** (trente secondes pèsent cinq mégaoctets).
C'est déjà ce qu'Android et iOS produisent nativement.

### 4.59.0 — 9 octobre 2026

🎯 **DES OBJECTIFS À ATTEINDRE, AVEC UN BONUS À GAGNER** — et ils mêlent les deux
métiers, ce qui en fait l'un des rares endroits où cette application n'a rien à
réunir : le compte est déjà commun au socle.

```
GET /me/challenges   → { items: [ { title, target, value, percent, reward_xof, ends_at, reached, pending } ] }
```

⚠️ **UNE SEULE ROUTE, LE PUBLIC VIENT DU JETON.** Pas de paramètre à passer.

⚠️ **`percent` EST DÉJÀ BORNÉ À 100** — une barre à 140 % a l'air d'un bug. Et
affichez **`value` et `target` ensemble** : « 3 sur 5 » dit qu'il en reste deux,
« 60 % » ne dit rien.

⚠️ **`money: true` CHANGE L'AFFICHAGE** : « 15 000 F sur 20 000 F », pas
« 15 000 fois ». Le serveur le dit pour que vous n'ayez pas à deviner d'après le
nom de la mesure.

⚠️ **`reached` S'AFFICHE AVANT `paid_xof`** (deux moments), et `pending: true` se
dit **en clair** : « bonus en cours de versement ». Le silence se lit comme un
refus.

⚠️ **UNE COMMANDE MULTI-BOUTIQUES COMPTE POUR UNE**, même si elle donne trois
livraisons. Et « dépensez 20 000 F » compte vos **commandes ET vos courses** :
c'est le même compte, au socle.

⚠️ **UN OBJECTIF PÉRIODIQUE CHANGE D'`id`** et remet son compteur à zéro chaque
lundi : ne gardez pas l'avancement de la semaine passée en cache.

⚠️ **ET VOUS NE RECEVEZ NI BUDGET NI NOMBRE DE GAGNANTS** : les afficher
transformerait un objectif en course aux places, où la moitié des gens
abandonnent en se croyant trop tard. Aucune route ne les sert.

`challenge_reached` arrive en notification, **catégorie `support` donc non
coupable** : quelqu'un qui a coupé les offres commerciales doit tout de même
apprendre qu'il vient d'être payé.

Détail : `VTC-CLIENT` et `FOOD-CLIENT`, section 🎯.


### 4.58.0 — 9 octobre 2026

☎️ **LE SERVICE CLIENT APPELLE, PAS VOTRE TÉLÉPHONE.** Le bouton d'alerte
(v4.56.0) ne propose plus d'appeler les secours : l'alerte part au service
client, un opérateur **appelle la personne**, et c'est lui qui appelle les
secours s'il le faut.

⚠️⚠️ **`GET /sos/settings` N'A PLUS DE CHAMP `numbers`** — il a disparu, ce n'est
pas un oubli. **Retirez les boutons d'appel de l'écran d'urgence**, et n'en
inventez aucun : ni 112, ni numéro codé dans l'application, ni repli « au cas
où ». Un écran qui proposerait « appeler la police » enverrait quelqu'un composer
un numéro que nous ne lui avons pas donné.

⚠️ **AFFICHEZ `calls_back`** : « le service client a été prévenu et va vous
appeler ». C'est la seule chose que la personne cherche à savoir.

⚠️ **ET PAS DE BOUTON « APPELER DIRA » NON PLUS** : il ferait patienter quelqu'un
en danger dans une file d'attente téléphonique pendant qu'un opérateur essaie de
le joindre sur la même ligne.

⚠️ **CE QUI COMPTE ENCORE PLUS MAINTENANT** : le **numéro du compte** doit être
juste (c'est par lui qu'on rappelle — un compte sans numéro joignable est un
compte qu'on ne peut pas secourir), la **note facultative** est lue par
l'opérateur *avant* qu'il appelle, et la **position poussée toutes les 5–10 s**
est ce qu'il transmet aux secours.

Détail : section 🆘, et `VTC-CLIENT` / `FOOD-CLIENT`.

### 4.56.0 — 8 octobre 2026

🆘 **LE BOUTON D'ALERTE**, au socle (`…/api/v1/sos`) et commun aux deux
métiers — l'un des rares endroits où cette application n'a rien à réunir.
Section 🆘 pour le détail ; l'essentiel :

⚠️⚠️ **UNE ALERTE NE SE PERD JAMAIS.** Aucun champ obligatoire, **pas même la
position** : n'attendez pas un point GPS pour envoyer. Réessayez en boucle
jusqu'à un `2xx`, en tête de la file hors-ligne. **N'ajoutez aucune validation
de votre côté.**

⚠️ **LE DOUBLE APPUI ENRICHIT LA MÊME ALERTE** : laissez le bouton actif.

⚠️ **`GET /sos/me` AU DÉMARRAGE** — `200` avec `alert: null` est le cas normal.

⚠️ **LES DÉTECTIONS PROPOSENT** via un compte à rebours de `countdown_seconds`
avec un seul bouton « Annuler ». À l'expiration, l'alerte part avec
`confirmed: false` — **plus grave, pas moins** : personne n'a annulé parce que
personne ne pouvait. Côté client, **seule `shake` a du sens**.

⚠️ **AUCUN NUMÉRO À COMPOSER (v4.58.0)** : le téléphone ne compose rien, le
**service client** rappelle. Pas de champ `numbers`, pas de bouton d'appel — ni
vers les secours, ni vers nous. Affichez `calls_back` : « le service client a été
prévenu et va vous appeler ».

⚠️ **ANNULER NE SUPPRIME PAS L'ALERTE** (quinze minutes à l'écran de
l'exploitation) : dites « l'exploitation a été prévenue », pas « effacé ». Une
annulation peut être contrainte.

Le bouton vit sur le **suivi d'une course** et sur le **suivi d'une livraison**,
en deux appuis au plus, **sans verrou d'application**. Il n'annule rien, et ce
n'est pas le support.


### 4.55.0 — 8 octobre 2026

🏷️ **LE MOTIF D'ANNULATION D'UNE COURSE EST NOMMÉ.** Il était un texte libre et
ne se comptait pas : « changé d'avis », « Changé d'avis », « chg avis » — trois
façons d'écrire un fait.

```
GET /rides/cancel-reasons   → { "by": "rider", "items": [ { "code": "changed_mind", "grave": false }, … ] }
POST /rides/{id}/cancel       { "reason_code": "wait_too_long", "reason": "…" }
```

⚠️ **DEMANDEZ LA LISTE, N'ÉCRIVEZ PAS LES CODES EN DUR.** Elle dépend du rôle du
jeton, et le serveur refuse un code de l'autre rôle (`422`, `fields:
["reason_code"]`). Les sept motifs du passager ne sont pas ceux du chauffeur.

⚠️ **TRADUISEZ-LES CHEZ VOUS** : la route sert des codes, pas des phrases.

⚠️ **LE CODE S'AJOUTE À LA PHRASE, IL NE LA REMPLACE PAS.** Le code se compte, la
phrase explique le cas précis. Les deux restent facultatifs — forcer un choix
fait cocher le premier élément de la liste, et `other` existe pour le reste.

🔎 **`driver_asked` EST LE MOTIF QU'IL FAUT VRAIMENT PROPOSER.** « Le chauffeur
m'a demandé d'annuler » est un **signal de fraude** : il évite ainsi les frais
d'annulation **et** garde sa place dans le vivier. Sans ce choix, le geste est
indistinguable d'un passager qui renonce, et il ne se compte nulle part.

⚠️ **AUCUN MOTIF DU PASSAGER N'EST `grave`**, et `grave` vous est pourtant
servi : lisez-le plutôt que de supposer `false`. Une agression passe par le
**support**, pas par une case à cocher.

⚠️ **ET LES COURSES ANNULÉES AVANT CETTE VERSION N'ONT QUE LA PHRASE** :
`cancelled_reason_code` est absent sur elles. Affichez `cancelled_reason` telle
quelle, ne traduisez pas l'absence par `other`.

> **Rien ne change côté commandes** : `POST /orders/{id}/cancel` n'a pas de
> taxonomie, et ce n'est pas un oubli — une commande s'annule avant d'être
> préparée, où le seul motif utile est « je n'en veux plus ».


### 4.52.0 — 8 octobre 2026

🧾 **REÇUS ET RELEVÉS EN PDF**, deux routes par métier (§9 bis), avec la
**distance réellement parcourue** et sa source — `tracked` mesurée, `planned`
estimée, et le second cas est courant. ⚠️ `attachment` + `no-store` : **pas de
WebView, pas de cache disque** — ces documents portent une adresse, un trajet et
un montant. ⚠️ **Le détail s'additionne jusqu'au total ou rien n'est servi**
(500). ⚠️ `from`/`to` **obligatoires sans défaut**, et les relevés **refusent au
lieu de tronquer** (`409 statement_too_large`, 500 opérations / 366 jours).
⚠️ **Deux relevés, pas un** : c'est le seul endroit où l'application unifiée ne
peut pas réunir les deux métiers — proposez deux exports. Nouveaux champs :
`cancel_fee_xof` sur une course annulée (enfin servi — le bloc `cancellation`
n'est qu'un devis et disparaît), `delivery.actual_distance_m` +
`distance_source` au client, et `country` sur une commande, **qui dit la
monnaie**.


### 4.51.0 — 8 octobre 2026

🎟️ **UN CHAMP « CODE PROMO », DANS LES DEUX TUNNELS.** Un code est au **socle** :
le même mot ne peut pas valoir une chose sur une course et une autre sur une
commande. `POST /rides/quote { …, "code" }` et `POST /orders { …, "code" }` ; le
prix servi est **déjà remisé**. ⚠️ **Les deux métiers ne refusent pas au même
moment** — au devis pour une course (on corrige), à la création pour une commande
(elle n'est **pas** créée) : gardez le panier à l'écran. ⚠️ `promo_code_ignored`
(courses seulement) dit qu'un code **valable** a été battu par une offre
automatique : ce n'est pas un refus. ⚠️ `promo_code_wrong_service` arrive avec
`meta.valid_for` — vous portez les deux onglets, **proposez d'y aller** plutôt
que d'écrire « code invalide ». 🎁 **`GET /me/referral`** : un écran de compte,
sans métier. ⚠️ `enabled` à faux ⇒ **cachez l'entrée**. ⚠️ Deux montants, deux
bénéficiaires ; le parrain est payé quand la remise est **consommée**, et comme
le filleul peut la dépenser dans l'un ou l'autre onglet, le crédit arrive parfois
d'un métier que le parrain ne regardait pas — le fil d'activité (§9) est le bon
endroit pour le dire. Détail : §6 quater.


### 4.50.0 — 8 octobre 2026

💸 **ANNULER UNE COURSE PEUT COÛTER.** Un barème se règle par pays et par
véhicule, et il voyage **avec la course** dans `cancellation` : `fee_xof`,
`pct`, `step`, `why`, `grace_left_s`. ⚠️ **L'écran de confirmation doit le
dire** — retenir de l'argent à quelqu'un qui n'a pas pu le lire avant est
indéfendable. Gratuit tant qu'aucun chauffeur n'a répondu, puis gratuit pendant
`grace_left_s` secondes, puis une part du tarif — plus haute quand le chauffeur
**attend** sur place. ⚠️ Sur une course payée, le remboursement est **amputé**
des frais ; en espèces, ils deviennent une **dette** réglée à la prochaine
recharge. Détail : `VTC-CLIENT` §5.

### 4.49.0 — 8 octobre 2026

🔒 **QUI VOIT QUOI DE QUI** — ce que vous avez le droit d'afficher de l'autre
partie est désormais réglé par l'exploitation, **pays par pays et métier par
métier**. Le serveur **n'envoie pas** ce qu'on n'a pas le droit de montrer : un
champ absent n'est pas une panne. ⚠️ `phone` présent ne veut **pas** dire
affichable — lisez **`show_phone`** ; `direct_call` sans `show_phone` veut dire
« un bouton qui appelle, et le numéro nulle part ». ⚠️ **N'écrivez aucune règle
en dur** (« si c'est une course, pas de téléphone ») : elle contredirait le
serveur sans que personne ne sache laquelle croire. Détail : `VTC-CLIENT` §
Confidentialité et `FOOD-CLIENT` § Confidentialité.

### 4.48.0 — 8 octobre 2026

🛵 **LE MOTO-TAXI** — `moto` arrive dans `GET /classes`, **en tête** du
catalogue et dans **tous les pays** : `seats: 1` (ni 2 passagers, ni bagage),
pas de course partagée (`modes` n'a pas de clé `pool`), `icon_url` `null` le
temps que l'exploitation pose son image — dessinez la silhouette `map_icon`
(`moto`). ⚠️ **Aucune voiture ne répond à un appel de moto** : ne promettez pas
« une voiture si aucune moto n'est libre ». Détail : `VTC-CLIENT` §2.

### 4.47.0 — 7 octobre 2026

🗑️ **SUPPRIMER SON COMPTE** — `DELETE /me` (§6 ter), en **deux temps** : le
compte est **fermé** tout de suite (sessions coupées partout, `403
account_closed` ensuite, par mot de passe **comme par code**) et son identité
part à **`erase_at`**, trente jours plus tard. ⚠️ **Les courses et les commandes
passées restent** — ce sont des écritures comptables — mais elles ne désignent
plus personne : le nom devient « Compte supprimé » partout, d'un coup. ⚠️ Il
faut **prouver qui on est** (`password` ou `code`), le portefeuille doit être
**vide** (`409 wallet_not_empty`, `error.reason`), et le numéro n'est **libéré**
qu'à `erase_at` — se réinscrire avant répond `403 account_closed`, pas
`phone_taken`. Pendant le délai, le support peut **annuler**.

### 4.46.0 — 7 octobre 2026

🆕 **LA PORTE PAR CODE** — `POST /auth/otp` et `POST /auth/otp/verify` (§6 bis) :
le téléphone, six chiffres, pas de mot de passe. `verify` rend le corps de
`POST /auth/login` plus **`created`**. ⚠️ Tant que `channel` vaut `echo`, **rien
n'est envoyé** et le code revient dans **`dev_code`** — un bonus de
développement qui disparaîtra sans préavis. ⚠️ Le nom ne se demande qu'après,
et seulement si `created: true` : la demande de code ne dit jamais si le numéro
est connu. ⚠️ Un compte né par code **n'a pas de mot de passe** : pas d'écran
« mot de passe oublié » pour lui.

### 4.43.0 — 6 octobre 2026

🆕 **Première version de ce document** : l'application cliente **unifiée**, qui
fait la livraison **et** les courses sur un compte, un solde et un historique.

- **L'assistant unifié** `POST /analytics/ai/chat` — un champ de saisie, deux
  métiers. `vertical` dit qui a répondu, `plan` est celui du métier **verbatim**,
  `routed_by` dit comment on a tranché. ⚠️ **`vertical` absent = on n'a pas su**,
  et `choices` demande : on ne devine pas.
- **Le fil d'activité** `GET /analytics/activity` — courses et commandes dans
  une seule liste, par **semaine**, dans le **fuseau du pays**. Le pas de
  pagination est la semaine ; les semaines vides sont sautées ; `sources` dit ce
  qui a répondu.
- **`?before=`** sur `GET /vtc/rides` et `GET /food/orders` : la fenêtre de date
  qui rend le fil combiné possible.
- ⚠️ Les **deux sockets** et la **boîte unique** : les deux pièges propres à une
  application qui réunit les deux métiers, écrits ici parce qu'aucune des deux
  specs métier ne pouvait les dire.
