# App CLIENT — COURSES (VTC) — contrat d'API

> **Version 4.110.0** · 30 octobre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Courses : `https://api-staging.dira.llc/api/v1/vtc` · Suivi : `wss://tracking-staging.dira.llc`

---

> ## ⚠️ DEUX BASES D'URL
>
> | Ce que vous appelez | Service | Base |
> |---|---|---|
> | connexion, profil, adresses, **portefeuille**, paiements, notifications | **socle** | `…/api/v1/…` |
> | tout le reste de ce document | **courses** | `…/api/v1/vtc/…` |
>
> Le **jeton est le même** des deux côtés : même secret, même session. On ne se
> connecte pas deux fois.

---

## 0. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et pour les courses |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next_cursor": "…" }` — `next_cursor` absent = dernière page |
| Montants | **entiers**, en XOF (`…_xof`). Jamais de flottant. |
| Dates | ISO 8601 UTC (`2026-09-13T10:41:23Z`). Une **date seule** s'écrit `YYYY-MM-DD`. |
| Coordonnées | `[lng, lat]`, dans cet ordre, partout |
| Langue | `Accept-Language: fr` ou `en` — les messages d'erreur et les notifications suivent |
| Pays | `X-Dira-Country: TG` — sur chaque requête ; la réponse porte le pays **retenu** (voir la section *Le pays*) |
| Fil | `X-Request-ID` — **sur chaque réponse** ; à l'aller, facultatif mais recommandé (voir la section *Le fil d'une requête*) |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat.

**Un `422 validation_failed` nomme ses champs.** `fields` liste les **clés
JSON** en cause : soulignez **ces** cases, pas une bannière sous tout le
formulaire. `reason` précise, quand ce n'est pas la valeur d'un champ :
`unknown_field` (une clé que la route ne connaît pas — **refusée, pas
ignorée**, son nom est dans `fields` ; c'est un bug de l'application) ou
`invalid_json`.

**`401`** = jeton expiré ou invalide : `POST /auth/refresh`, puis rejouer la
requête ; si le refresh échoue, revenir à la connexion. **`403`** = ce rôle,
ou cette personne, n'a pas accès — ne pas réessayer.

---

## 🧵 LE FIL D'UNE REQUÊTE — `X-Request-ID` (v4.34.0)

**Chaque réponse porte un `X-Request-ID`.** C'est le nom sous lequel cette
requête est écrite dans nos journaux, et le même voyage **jusqu'au bout de la
chaîne** : un geste dans l'application traverse jusqu'à quatre services — le
métier, le socle (identité, portefeuille), le suivi (l'appel des chauffeurs) et
les cartes (l'itinéraire). Quatre journaux, un seul identifiant.

**Le minimum : lisez cet en-tête et gardez-le avec l'erreur que vous
enregistrez.** Un ticket qui cite le fil se lit en trente secondes. Sans lui, il
faut recouper quatre journaux à l'horodatage — ce qui marche jusqu'au jour où
deux personnes font le même geste la même seconde.

**Il est sur TOUTES les réponses, y compris un refus et y compris un `500`** —
précisément celles dont on parle. Un `5xx` est d'ailleurs rangé côté serveur
**avec ce fil dedans** : citez-le, et le défaut est retrouvé sans chercher.

**Mieux : envoyez le vôtre.** L'en-tête est accepté **à l'aller** aussi, et
repris tel quel.

⚠️ **C'EST LE SEUL MOYEN DE RETROUVER UN APPEL DONT LA RÉPONSE N'EST JAMAIS
ARRIVÉE.** Délai dépassé, tunnel, réseau qui tombe entre la question et la
réponse : il n'y a **rien à lire**, et ce sont exactement les appels qu'on
cherche à comprendre. L'identifiant que l'application a émis, lui, est dans nos
journaux — et s'il n'y est pas, cela répond aussi : la requête ne nous a jamais
atteints, le problème est en amont de nous.

⚠️ Un build qui tourne **dans un navigateur** ne peut lire un en-tête de réponse
que si la façade l'expose : une raison de plus d'envoyer le vôtre, qui marche
partout.

**La forme** : **64 caractères au plus**, pris dans `a-z A-Z 0-9 - _ .`

⚠️ **Hors de ces règles, l'identifiant est REFUSÉ — pas tronqué.** On tire le
nôtre, et la réponse dit lequel a été retenu : l'application n'est jamais
laissée sans fil. Tronquer aurait fait se confondre deux fils différents au
moment précis où on les cherche.

⚠️ **JAMAIS DE DONNÉE PERSONNELLE DEDANS.** Ni numéro de téléphone, ni nom, ni
jeton, ni identifiant de compte : ce mot est recopié dans les journaux de six
services, et une base de journaux ne se purge pas comme un compte se supprime.
Un identifiant d'installation plus un compteur suffit : `a1f9c2-1874`.

⚠️ **UN PAR REQUÊTE.** Le même sur tous les appels d'un écran regrouperait tout
et n'identifierait rien. Sur une **reprise du même appel**, en revanche, garder
le même est utile : les trois tentatives se relisent comme une seule histoire.

⚠️ **Le fil n'est pas une référence métier.** Il nomme un APPEL, pas une
commande ni une course : deux appels sur la même course ont deux fils, et un
appel rejoué à l'identique en a un troisième. Pour désigner l'objet, c'est son
`id` qu'on cite ; le fil dit *ce qui s'est passé cette fois-là*.

---

## 🗺️ LE FOND DE CARTE — `maps` (v4.40.0)

La plateforme sert **deux fonds de carte** : le nôtre, et Google. Le pays règle
celui qu'on voit **d'abord** ; la personne choisit ensuite celui qui lui va, et
son choix reste chez elle.

**Ce que le serveur rend**, dans la réponse de connexion, d'inscription et de
rafraîchissement :

```json
"maps": { "basemap": "dira" }
```

| | |
|---|---|
| `basemap` | `dira` ou `google` — le fond par **défaut** du pays, la case cochée d'avance |

C'est **tout** ce que le serveur a à dire de votre carte : quel fond montrer
d'abord ici. Un seul champ, une seule question.

### ⚠️ `google_key` ET `google_map_type` ONT DISPARU (v4.40.0)

Jusqu'à la 4.39.1, `maps` portait la clé Google du pays pour votre plateforme.
**Ces deux champs ne sont plus servis.** La raison est simple et elle vous
concerne : **une clé Google se restreint par ce qui l'utilise** — nom de paquet
+ empreinte SHA-1 sur Android, identifiant de bundle sur iOS, référent HTTP sur
le web. Elle appartient donc à **l'application**, pas au pays, et vous en avez
déjà une, correctement restreinte. Une clé servie par l'API arrivait trop tard,
pour quelqu'un qui n'en avait pas besoin.

Ce que ça change pour vous, dans l'ordre :

1. **Si vous lisiez `google_key`, cessez.** Elle sera absente, définitivement.
2. **Si vous conditionniez le CHOIX à sa présence, ne le conditionnez plus** :
   proposez **toujours** les deux fonds. C'était la règle inverse jusqu'ici, et
   c'est le seul point où votre code doit changer.
3. **Affichez Google avec VOTRE clé** — le SDK natif sur Android et iOS.
4. `platform` sur `/auth/login`, `/auth/register` et `/auth/refresh` est
   désormais **acceptée et ignorée**. ⚠️ **Ne la retirez pas** de vos requêtes :
   le serveur refuse les champs inconnus, mais celui-là reste déclaré
   exprès — l'envoyer ne coûte rien, et le retirer n'apporte rien.

### Où lire le défaut d'un pays — DEUX endroits, et il faut les deux (v4.41.0)

| | Quand | Ce qu'il dit |
|---|---|---|
| `maps.basemap` de `/auth/login`, `/auth/register`, `/auth/refresh` | à la connexion et à chaque rafraîchissement de jeton | le fond du pays où l'on **se connecte** |
| **`basemap` de chaque pays dans `GET /countries`** | à chaque ouverture de l'application, sans jeton | le fond de **n'importe quel** pays ouvert |

⚠️ **LE BLOC `maps` NE SUFFIT PAS, ET C'EST LE PIÈGE.** Il voyage avec le jeton :
il dit le fond du pays où l'on s'est connecté, **une fois**. Or l'application
rouvre sans se reconnecter (le jeton d'accès se rafraîchit, mais pas à chaque
lancement), et surtout **le pays peut changer sans reconnexion** — un passager
togolais qui ouvre l'application à Dakar opère au Sénégal. Une application qui ne
lirait que `maps` garderait le fond de la veille, dans le mauvais pays.

`GET /countries` est **public** et déjà appelé avant l'inscription : chaque pays
y porte désormais `basemap`.

```jsonc
{ "items": [
  { "code": "SN", "name": "Sénégal", "currency": "XOF", …, "basemap": "google" },
  { "code": "TG", "name": "Togo",    "currency": "XOF", …, "basemap": "dira" } ] }
```

### Ce que vous faites, concrètement

1. **Au lancement**, lisez `GET /countries` (vous l'appelez déjà) et gardez le
   `basemap` de chaque pays avec le reste de la fiche.
2. **Le fond effectif** se décide dans cet ordre, et il n'y en a pas d'autre :

   ```
   le choix de la personne (gardé chez vous)
     sinon  le basemap du pays où elle OPÈRE  (celui de l'en-tête X-Dira-Country)
       sinon  dira
   ```

3. **Tant que la personne n'a rien choisi, suivez le pays** — c'est exactement ce
   à quoi sert ce réglage. Dès qu'elle choisit, son choix **prime** et ne se
   perd plus. ⚠️ Il **survit au changement de pays** : quelqu'un qui passe du
   Togo au Sénégal a changé de pays, pas d'avis.
4. **Au changement de pays**, relisez le `basemap` du nouveau pays et
   **redessinez la carte** si le fond effectif change. Un fond lu à la première
   carte et jamais relu est un fond qui ment dès le second voyage.
5. `maps` **absent** (socle plus ancien) ou `basemap` inconnu = **notre fond**,
   et rien d'autre à faire.

> ⚠️ **Le fond change, la carte doit RENAÎTRE.** Les SDK lisent leur style à la
> création : changer la valeur sans reconstruire la vue ne se voit pas, et on
> croit le réglage sans effet. C'est le défaut que la console a eu le
> 30 septembre — le logo Google posé sur les tuiles de l'autre fond.

⚠️ **LE LOGO GOOGLE EST OBLIGATOIRE** dès que ses tuiles s'affichent : **16 dp de
haut au minimum**, **10 dp de dégagement**, **jamais recouvert** par un autre
logo — le vôtre compris. Ce n'est pas une politesse, c'est une clause du contrat
d'utilisation de l'API. Les SDK natifs le dessinent eux-mêmes ; ne le masquez pas
sous une barre ou un bouton flottant.

⚠️ **VOTRE clé se protège par sa RESTRICTION, pas par le secret.** Elle voyage en
clair sur chaque requête de tuile — Google l'exige. Restreignez-la (empreinte,
bundle, référent), plafonnez son quota, et ne la recopiez pas dans un journal ni
dans un rapport de plantage : ces fichiers partent chez un tiers et se gardent
des mois.


### ⚠️ LE SDK DE CARTE CHANGE DE NOM — `@dira-ws/dira-maps-react-native` (v4.70.0)

Le paquet `@kgtech-org/dira-maps-react-native` devient
**`@dira-ws/dira-maps-react-native`**, en version **0.5.0**.

**Aucune ligne du SDK ne change** : son API est celle de 0.4.7, au caractère
près. Il n'y a rien à réapprendre, rien à réécrire autour. Seul le **nom** du
paquet change.

**Pourquoi** : les dépôts de la plateforme ont quitté l'organisation
`kgtech-org` pour **`dira-ws`** le 12 octobre 2026, et le registre de paquets
de GitHub impose que le scope d'un paquet soit le **propriétaire de son
dépôt**. Sous `@kgtech-org`, plus aucune version ne peut être publiée. Ce n'est
pas un réglage : il n'y a pas de contournement.

**Ce que l'application cliente VTC doit changer** — la dépendance est épinglée sur un **tag git**,
pas sur le registre :

```diff
 // package.json
-"@kgtech-org/dira-maps-react-native": "github:kgtech-org/dira-maps-react-native-sdk#v0.4.4"
+"@dira-ws/dira-maps-react-native": "github:dira-ws/dira-maps-react-native-sdk#v0.5.0"
```

puis les **6 imports** (5 fichiers) :

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
l'application **compile sans un mot** : le paquet renommé est simplement rangé
sous l'ancien dossier. Rien ne casse, et rien ne vous apprend que le nom a
changé — jusqu'au jour où quelqu'un cherche pourquoi le paquet n'a plus de
nouvelles versions.

⚠️ **RIEN NE CASSE LE JOUR OÙ VOUS NE FAITES RIEN.** Les versions publiées sous
l'ancien nom restent installables, et le tag git que vous épinglez aujourd'hui
continuera de se télécharger. Mais plus aucune version n'y sera publiée : une
application qui garde l'ancien nom est **figée sans que rien ne le signale**.
C'est la même panne silencieuse qu'un serveur qui tire son image d'un registre
que personne n'alimente plus — et elle a coûté six jours de préproduction.

Il n'y a **pas de `.npmrc` à toucher** : l'application cliente VTC n'installe pas depuis le registre
de paquets. (Si vous y passez un jour, la ligne de registre devient
`@dira-ws:registry=https://npm.pkg.github.com`, avec un jeton `read:packages`.)

---

## ⚠️ Le PAYS — `X-Dira-Country` (v4.2.0)

Dira s'installe pays par pays, et **toute donnée est bornée par pays** :
un compte, une commande, une course, une enseigne, un chauffeur portent un
`country` (ISO 3166-1 alpha-2 : `TG`, `BJ`, `CI`…) et une application ne
voit que ceux de **son** pays. Ce que l'application doit faire tient en
trois gestes.

**1. Envoyer son pays dans l'en-tête `X-Dira-Country`, sur chaque
requête** — socle, métier, suivi. Le serveur répond avec le pays **retenu**
dans le même en-tête : lisez-le, et si votre valeur diffère, alignez-vous.
Pour un compte ordinaire, l'en-tête **informe** — le pays du compte,
inscrit dans le jeton, s'impose ; un client ne change pas de pays en
changeant un en-tête. Sans jeton (inscription, connexion, `GET /countries`),
l'en-tête est **admis** s'il nomme un pays ouvert, sinon le pays par défaut
du déploiement s'applique. Là où l'on ne peut pas poser d'en-tête (une
ouverture de WebSocket depuis un navigateur), `?country=TG` fait le même
travail.

**2. Trouver son pays** — avant l'inscription, puis à chaque démarrage :

```
POST /api/v1/me/country/resolve        (connecté)
{ "lng": 1.2255, "lat": 6.1319 }       — la position de l'appareil, si vous l'avez
{}                                     — sinon corps vide : l'adresse IP décide

→ 200 {
  "country":   "TG",        ← le pays RETENU : envoyez-le dans X-Dira-Country
  "source":    "geo",       ← geo | ip | account | default
  "detected":  "TG",        ← où la position / l'IP situe la personne, même hors zone
  "supported": true,        ← `detected` est un pays ouvert
  "updated":   false        ← le pays du compte a changé
}
```

Deux signaux, dans l'ordre : la **position** de l'appareil (`geo`, la
vérité à cent mètres près) ; sinon, ou si la position est hors zone,
l'**adresse IP** de la requête (`ip`, moins sûre — un opérateur mobile sort
parfois par un autre pays). Un signal qui désigne un pays **non ouvert** ne
vaut pas : `supported: false`, `detected` dit où la personne est, et
`country` reste celui du compte (`account`) ou, s'il n'en avait pas, le pays
par défaut (`default`). C'est le moment d'afficher « Dira n'est pas encore
disponible au Ghana » — sans bloquer : le compte reste utilisable dans son
pays.

**Avant l'inscription**, le compte n'existe pas : appelez `GET /countries`
(public) pour la liste des pays ouverts, avec leur **indicatif**, et
posez `X-Dira-Country` sur `POST /auth/register` avec le pays où la position
de l'appareil tombe (calculé côté appareil, ou après une première
connexion). Sans en-tête, le serveur déduit le pays de l'**indicatif** du
téléphone (`+228` → `TG`, `+229` → `BJ`), puis du pays par défaut. Le compte
porte le résultat dans `user.country`.

**3. Rafraîchir la session quand le pays change.** `updated: true` veut
dire que le compte a changé de pays — mais le jeton en cours porte encore
l'ancien (`cty`), et c'est **le jeton** qui borne les listes. Faites un
`POST /auth/refresh` tout de suite : le nouveau jeton porte le nouveau pays.
Un voyageur qui ouvre l'application à Cotonou devient béninois pour Dira —
c'est ce qu'il veut, commander à Cotonou. Son historique togolais ne bouge
pas, il est marqué de son pays d'alors.

```
GET /api/v1/countries                  (public)
→ 200 { "default": "TG", "items": [
  { "code": "TG", "name": "Togo", "currency": "XOF", "currency_name": "Franc CFA (UEMOA)",
    "currency_symbol": "F CFA", "currency_decimals": 0, "phone_prefix": "+228",
    "locale": "fr", "timezone": "Africa/Lome", "center": [1.2255, 6.1319],
    "enabled": true, "default": true }
]}
```

**La monnaie vient du pays.** Tout montant de la plateforme est un entier
dans la plus petite unité de la monnaie du pays où il a été créé — un prix
sous `GN` est en francs guinéens, sous `TG` en francs CFA. Rien n'est
converti : formatez avec `currency_symbol` et `currency_decimals` du pays
courant (« 2 500 F CFA », « 35 000 FG »). Les champs nommés `…_xof` sont
un héritage de nommage : ils portent la monnaie du pays.

Trois pays sont ouverts d'office : **Togo** (`TG`), **Sénégal** (`SN`),
**Guinée** (`GN`). Les autres s'ouvrent depuis la console.

`GET /me` porte `country`. **Ne le mettez pas en cache au-delà d'une
session** : la résolution du démarrage suivant peut le changer.

> ⚠️ **Ne pas envoyer d'en-tête n'est pas une erreur, mais c'est un
> silence** : le serveur retombe sur le pays du compte, puis sur celui du
> déploiement, et l'application ne saura jamais qu'elle a été rangée
> ailleurs que là où elle croit être. L'en-tête est ce qui rend l'écart
> visible — dans la réponse.

## 🆕 ⚙️ L'ÉDITION — `X-Dira-Edition` (v4.87.0)

**Dira Lite** est une seconde application passager, pour les zones de faible
alphabétisation. L'exploitation peut en **retirer** des fonctions ou des
valeurs qui restent disponibles dans l'application normale — sans nouvelle
version des applications, exactement comme les réglages par pays.

**Envoyez votre édition dans l'en-tête `X-Dira-Edition`, sur chaque
requête** — socle, courses, suivi (et `?edition=` sur le WebSocket) :

```
X-Dira-Edition: lite        # ou `full`
X-Dira-App-Version: 0.1.0 (26102901)     # facultatif, utile
X-Dira-Platform: android                 # facultatif
```

⚠️ **Absent ou inconnu = `full`**, l'application COMPLÈTE. Une faute de frappe
ne doit pas vous amputer : dégrader vers l'édition réduite ferait disparaître
des menus sur une erreur d'en-tête.

⚠️ **Le serveur republie l'édition retenue** dans le même en-tête. **Lisez-la
au moins une fois** : recevoir `full` en se croyant `lite` veut dire que votre
en-tête n'arrive pas — et vous afficheriez alors l'application entière.

⚠️ **Ce n'est pas une sécurité.** N'importe qui peut écrire
`X-Dira-Edition: lite` ; l'en-tête sert à **adapter** ce qu'on sert, pas à
protéger quoi que ce soit.

### Lisez `GET /app/config` au démarrage

```
GET /app/config          # SOCLE, sans jeton — AVANT l'écran de connexion
  →
{ "edition": "lite", "country": "TD",
  "features": { "rental": false, "pool": false, "multi_car": false,
                "trip_share": false, "subscriptions": false,
                "scheduled_rides": true, … },
  "lists": { "vehicle_modes": ["free"], "vehicle_classes": ["moto", "eco"] } }
```

Gardez-la en mémoire et **cachez vos entrées de menu dessus**. C'est ce qui
évite de proposer un bouton qui échoue — et, pour Lite, d'avoir à lire à voix
haute un refus qu'on pouvait éviter.

⚠️ **Ne supposez rien d'une clé absente de `features`** : le catalogue entier
est servi à chaque appel. Une clé qui manque veut dire que votre serveur est
plus ancien que votre application — traitez-la comme **disponible** (`true`),
jamais comme coupée.

⚠️ **`lists` ne contient que les listes RESTREINTES**, et une liste absente
veut dire « tout ce que le métier sert » — **jamais** « rien ».

### ⚠️⚠️ Ce que les routes des COURSES filtrent déjà pour vous

Vous n'avez **rien à croiser** : nos routes appliquent le filtre elles-mêmes.

⚠️ **Toutes les fonctions du catalogue n'ont pas de route à refuser.** Celles
des courses en ont (le tableau ci-dessous et les cinq refus plus bas) ; d'autres
sont de purs **écrans d'application** — la tombola, les vocaux — et pour
celles-là `/app/config` n'est pas un confort, c'est la **seule** chose qui les
coupe. Une application qui ne la lit pas affichera un écran que l'exploitation
croit avoir retiré, sans que rien n'échoue nulle part.

| Route | Ce qu'elle fait dans Lite |
|---|---|
| `GET /classes` | ne rend que les **classes** permises, et dans chaque classe, que les **modes** permis |
| `GET /settings/modes` | sert `enabled: false` sur un mode retiré |

⚠️ **Un mode retiré est ÉTEINT, pas absent.** `rental.enabled: false` est
exactement la forme que vous lisez déjà pour un pays qui ne loue pas : une
seule façon de dire la même absence. Un champ **manquant**, lui, veut dire
« serveur plus ancien », pas « mode coupé ».

⚠️ **L'ordre de nos listes ne change pas.** La classe la moins chère vient
toujours avant : ce que vous affichez d'abord reste ce que nous proposons
d'abord.

⚠️ **Et nous ne rendons JAMAIS une liste vide.** Si une liste blanche mal
saisie ne croisait aucune de nos classes, nous servons le catalogue **entier**
plutôt qu'une application sans aucune voiture à commander. Vous n'avez donc
jamais à gérer « zéro véhicule » à cause d'un réglage.

### Les refus, quand vous les provoquez quand même

```jsonc
// 403
{ "error": { "code": "feature_unavailable_in_edition",
             "message": "Cette fonction n'est pas disponible dans votre application",
             "feature": "rental" } }
```

Trois routes des courses peuvent le rendre :

| Route | Fonction |
|---|---|
| `POST /rides` sur un devis en **location** | `rental` |
| `POST /rides` sur un devis en **partage** | `pool` |
| `POST /rides` avec `cars > 1` | `multi_car` |
| `POST /rides/{id}/share` | `trip_share` |
| `POST /subscriptions` | `subscriptions` |

⚠️ **Ce n'est PAS un problème de droits** — ne proposez pas de se reconnecter.
`error.feature` nomme la fonction : **cachez-la** au prochain démarrage plutôt
que de reproposer un bouton qui échoue.

⚠️ **La phrase est courte exprès** : Lite la lit à voix haute. Elle ne parle ni
d'édition ni de pays, délibérément — la personne n'a qu'une application, elle
ne sait pas qu'il en existe deux, et elle ne peut rien faire de cette
information.

⚠️ **Et la route refuse même si vous n'avez pas lu `/app/config`.** Cette
lecture sert à ne pas **proposer** ce qui sera refusé ; elle ne remplace aucune
vérification. Une version ancienne de l'application, un écran en cache, un
appel direct : le refus arrive quand même.

⚠️ **Le refus du MODE se dit avant celui du nombre de voitures.** Si vous
demandez trois locations dans une édition qui n'a ni l'une ni l'autre, le refus
parle de la **location** — ce que la personne a choisi à l'écran. Ne la faites
pas repasser à une voiture pour découvrir ensuite que la location non plus
n'est pas là.

### ⚠️ Deux filtres qui se cumulent, et le plus restrictif gagne

Le **pays** dit ce que Dira vend ici ; l'**édition** dit ce que CETTE
application a le droit de montrer. Un mode que le pays n'ouvre pas reste fermé
quoi qu'on règle par édition — on ne rouvre pas un marché par un réglage
d'application.

### ⚠️ Rien n'est coupé par défaut

Un déploiement neuf sert tout, à tout le monde : l'application normale ne perd
**rien** tant que personne n'a explicitement restreint quelque chose.

---

## 🔒 LE VERROU DE L'APPLICATION — `app_lock` (v4.46.0)

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

## 1. Le modèle en une phrase

Un passager compose un trajet, **voit un prix avant de commander**, et la
plateforme cherche un chauffeur pendant qu'il attend.

Trois choses à comprendre avant de coder :

**Le prix ne vient jamais de vous.** Un devis est mémorisé côté serveur et
expire en deux minutes. Vous envoyez son identifiant, pas un montant — sans
quoi on commanderait une course à un franc.

**Les arrêts sont dans l'ordre choisi.** Contrairement aux collectes d'une
livraison, rien n'est réordonné : « d'abord chez ma sœur, puis l'aéroport » se
fait dans cet ordre.

**Une course à bord ne s'annule plus.** Interrompre demanderait de décider où
déposer quelqu'un, et ce n'est pas une décision qu'un bouton prend.

---

## 2. Les classes

```
GET /classes
```

```json
{ "items": [ { "key": "eco", "name": "Éco", "note": "Citadine · 1-4 passagers", "seats": 4,
              "icon_url": "https://files.dira.llc/class/…/eco.png",
              "map_icon_url": "https://files.dira.llc/class/…/eco-map.png",
              "map_icon": "voiture",
              "waiting_free_min": 5, "waiting_per_min_xof": 100,
              "bill_actual_time": true, "time_tolerance_min": 10, "per_min_xof": 50 } ] }
```

**Ce qui peut s'ajouter au prix en route (v4.14.0)** — quatre nombres et un
drapeau, par mode, à montrer **avant** que le passager ne choisisse :
`waiting_free_min` minutes d'attente offertes au départ puis
`waiting_per_min_xof` la minute ; et, si `bill_actual_time` est vrai, les
minutes roulées au-delà de la durée prévue du devis, tolérance
`time_tolerance_min` déduite, à `per_min_xof` chacune. Une ligne suffit :
« 5 min d'attente offertes puis 100 F/min · au-delà de 10 min de retard sur
la durée prévue, 50 F/min ». Ces termes sont **recopiés sur la course au
devis** : ce que le passager a lu en commandant est ce qui s'applique.

**Les modes se règlent depuis la console (v4.5.0)** : leur nom et leurs
icônes — des **images envoyées par l'exploitation** — peuvent changer, et
un mode peut s'ajouter. **N'écrivez ni le nom ni l'icône en dur** :
affichez `icon_url` devant `name`, tels que servis. `map_icon_url` est
l'image du marqueur sur une carte à plat et `nav_icon_url` la même sur une
carte de navigation (voir ci-dessous) ; à
défaut (`null`), `map_icon` nomme une silhouette de repli (`voiture` ·
`berline` · `suv` · `van` · `moto` · `tricycle` · `velo` · `pieton`) —
sans image ni silhouette connue, dessinez `voiture`. Mettez les images en
cache par URL : elles changent d'URL quand elles changent. La `key`
reste l'identifiant technique (devis, course) — jamais un libellé.

### 🛵 LE MOTO-TAXI — un mode à part, ouvert dans tous les pays (v4.48.0)

```json
{ "key": "moto", "name": "Moto", "note": "Moto-taxi · 1 passager", "seats": 1,
  "map_icon": "moto", "icon_url": null,
  "modes": { "free": { … }, "rental": { … } } }
```

Il arrive **en tête du catalogue** — c'est la course la moins chère et la plus
courante de la région. Il était jusqu'ici rangé dans `eco`, avec les
citadines.

⚠️ **`seats` VAUT 1, ET CE N'EST PAS DÉCORATIF.** Ne proposez pas « 2
passagers », ni bagage, ni siège enfant sur un mode à une place. Lisez `seats`
plutôt que de le déduire de la `key` : le jour où un tricycle arrive, il en
portera trois.

⚠️ **PAS DE COURSE PARTAGÉE SUR UNE MOTO**, et vous le savez **sans règle
spéciale** : `modes` ne porte pas la clé `pool`. C'est déjà la règle générale —
une clé absente = mode non vendu par ce véhicule —, et c'est pour cela qu'il
n'y a rien à coder de particulier. N'écrivez surtout pas « si key == moto
alors… » : c'est le serveur qui décide, mode par mode et pays par pays.

⚠️ **`icon_url` EST `null` AU DÉPART**, le temps que l'exploitation pose son
image. Dessinez alors la silhouette `map_icon` (`moto`) — **pas** l'icône d'une
voiture, et surtout pas une image en dur de votre côté : elle resterait là le
jour où la vraie arrive. C'est la même règle que pour tous les modes, et c'est
le premier mode qui la met vraiment à l'épreuve.

⚠️ **UNE MOTO N'EST JAMAIS ENVOYÉE SUR UNE COURSE DE VOITURE, NI L'INVERSE.**
Le reste du catalogue est une hiérarchie — une berline prend une course éco, le
passager monte dans mieux que ce qu'il a payé — mais la moto est **en dehors** :
elle ne sert qu'elle-même, et aucune voiture ne répond à un appel de moto. Rien
à faire de votre côté : c'est le serveur qui trie avant de sonner. Ne promettez
donc pas « une voiture si aucune moto n'est libre » — cela n'arrivera pas, et
l'écran d'attente doit dire la vérité.

### 🛺 LE TRICYCLE — le tuk-tuk, trois places (v4.67.0)

```json
{ "key": "tricycle", "name": "Tricycle", "note": "Tuk-tuk · 1-3 passagers",
  "seats": 3, "map_icon": "tricycle", "icon_url": null,
  "modes": { "free": { … }, "rental": { … } } }
```

**Deuxième du catalogue**, entre la moto et l'éco : il porte plus de monde
qu'une moto et coûte bien moins qu'une voiture. Ouvert dans tous les pays.

⚠️ **`seats` VAUT 3, ET C'EST TOUTE LA DIFFÉRENCE AVEC LA MOTO.** Trois
passagers, des bagages, un toit. C'est exactement le cas que le §précédent
annonçait : **lisez `seats`**, ne le déduisez pas de la `key`. Une application
qui aurait écrit « si c'est deux roues alors une place » se trompe ici.

⚠️ **PAS DE COURSE PARTAGÉE DESSUS NON PLUS** — et, comme pour la moto, vous
le savez **sans règle spéciale** : `modes` ne porte pas la clé `pool`. Mais
attention au raisonnement : ce n'est pas parce qu'il n'y aurait pas la place
(il y en a). C'est une **décision de produit** que personne n'a encore prise,
et elle peut s'inverser demain **sans que vous changiez une ligne** — le jour
où `pool` apparaît dans ses `modes`, il se partage. N'écrivez donc jamais
« si key == tricycle alors pas de partage ».

⚠️ **IL NE SE SUBSTITUE À RIEN, DANS AUCUN SENS.** Comme la moto, il est en
dehors de la hiérarchie : il ne prend pas une course éco — le passager a
demandé une voiture fermée et climatisée — et aucune voiture ne répond à un
appel de tricycle, qui serait servi au tarif d'un tricycle. Ne promettez pas
« une voiture si aucun tuk-tuk n'est libre ».

⚠️ **`icon_url` ET `map_icon_url` SONT `null` AU DÉPART**, et c'est voulu : il
n'existe pas d'illustration embarquée pour lui. Dessinez la silhouette
`map_icon` (`tricycle`) — **jamais** une voiture de repli. Le marqueur partagé
de la plateforme est une berline vue de dessus : la poser ici dessinerait une
voiture là où roule un tricycle, ce qui est **pire** que la silhouette sobre.

> ### 🧭 ⚠️ LES DEUX PINS D'UN VÉHICULE, ET LEUR ORIENTATION (v4.20.0)
>
> **Un véhicule a DEUX images de marqueur, pas une.** Elles viennent de le mode du chauffeur — `GET /classes` :
>
> | | Quand | Comment elle est dessinée |
> |---|---|---|
> | `map_icon_url` | la carte à plat — suivi, liste, vue d'ensemble | **vue de dessus**, caméra à la verticale (**90°**) |
> | `nav_icon_url` | la carte de **navigation**, inclinée vers l'horizon | **vue 3D**, caméra penchée à **~45°** |
>
> ⚠️ **UNE SEULE IMAGE POUR LES DEUX SE VOIT.** Une carte à plat regarde le
> sol à la verticale ; une carte de navigation penche la caméra. Un dessin vu
> de dessus y paraît **écrasé, couché sur la chaussée** — et une vue 3D sur
> une carte à plat paraît, elle, tombée sur le côté.
>
> **`nav_icon_url` absent : retombez sur `map_icon_url`.** Une vue de dessus
> sur une carte penchée reste lisible ; l'absence de tout marqueur, non.
>
> ⚠️ **DANS LES DEUX, LE NEZ POINTE VERS LE HAUT DE L'IMAGE** — le capot (ou
> la roue avant) vers le bord supérieur, quel que soit le véhicule et quelle
> que soit la vue. C'est la convention des images envoyées depuis la console,
> et c'est sur elle que reposent les rotations.
>
> Ne confondez pas les deux angles : **90° et 45° décrivent la CAMÉRA**
> (à la verticale, ou penchée), jamais l'orientation du véhicule dans
> l'image. Celle-ci ne change pas d'une vue à l'autre — c'est précisément ce
> qui permet à votre code de tourner l'une ou l'autre sans rien savoir de
> laquelle il s'agit.
>
> **Appliquez donc `heading` TEL QUEL** comme rotation du marqueur : un cap
> de 0° (plein nord) laisse l'image droite, 90° la tourne d'un quart de tour
> vers la droite. Pas de correction, pas d'offset de −90° — si vous avez
> besoin d'en ajouter un, c'est l'image qui est mal orientée, pas le code :
> signalez-le à l'exploitation plutôt que de le compenser chez vous.
>
> Pourquoi cette règle est écrite : une correction appliquée dans UNE
> application et pas dans les autres fait rouler les véhicules de côté sur
> un seul écran, et personne ne sait lequel a raison.
>
> ⚠️ **Le cap ne tourne pas à l'arrêt.** Un `heading` déduit (`heading_source`
> ≠ `gps`) sur un véhicule immobile pivote dans le vide : gardez la dernière
> orientation plutôt que de l'animer.
>
> ⚠️ **La règle vaut pour les VÉHICULES, pas pour les marqueurs de lieu.** Un
> pin de client, de marchand ou d'étape ne tourne jamais : il désigne un
> endroit, pas une direction.

⚠️ **Aucun prix ici.** Une grille tarifaire affichée hors d'un trajet donne un
chiffre que la course ne confirmera pas. Le prix vient du devis, pour CE
trajet.

Si la liste est vide, le service **refuse tout devis** (`409
no_classes_configured`) : ce n'est pas une panne réseau, c'est une plateforme
non configurée. Dites-le autrement qu'avec « réessayez ».

---

## 3. Le devis — le prix avant de commander

```
POST /rides/quote
{ "stops": [ { "kind": "pickup", "label": "Almadies", "geo": [1.2255, 6.1319] },
             { "kind": "dest",   "label": "Aéroport", "geo": [1.2545, 6.1656] } ] }
```

`kind` vaut `pickup`, `stop` ou `dest`. De **2 à 5** arrêts.

Réponse : **une entrée par classe**, toutes tarifées sur le même trajet.

```json
{ "items": [ {
  "id": "6aa2…", "class_key": "eco",
  "stops": [...], "distance_m": 6141, "duration_s": 467,
  "surge_name": "Aéroport", "surge_multiplier": 1300,
  "fare_xof": 2250,
  "promo_title": "Offre de lancement", "promo_discount_xof": 250,
  "expires_at": "2026-09-10T12:43:20Z"
} ] }
```

> ⚠️ **`expires_at` n'est pas décoratif.** Passé cet instant, la confirmation
> est refusée (`409 quote_expired`). Une application qui l'ignore laisse son
> utilisateur devant un bouton qui échouera. Redemandez un devis plutôt que de
> laisser le bouton actif.

### 🆕 ⚠️⚠️ UN DEVIS SE DÉPENSE UNE FOIS — ET UN RENVOI REND LA MÊME COURSE (v4.99.3)

```
POST /vtc/rides
Idempotency-Key: <l'identifiant de CET appel>
{ "quote_id": "…", "payment_method": "cash" }
```

⚠️⚠️ **Le même `quote_id` pouvait créer plusieurs courses.** Rien ne consommait
le devis : trois courses au même prix pour un devis demandé une fois, bornées
seulement par le nombre de courses simultanées du pays. C'est corrigé — un devis
est une **offre à un prix**, et une offre se dépense une fois.

⚠️ **`409 quote_spent` n'est pas `409 quote_expired`.** Les deux appellent le
même geste — redemander un devis — mais ils ne disent pas la même chose :

| refus | ce qui s'est passé | ce que vous montrez |
|---|---|---|
| `quote_expired` | le temps a passé | « le prix a changé » → nouveau devis |
| 🆕 `quote_spent` | **la course existe déjà** | **la course**, pas le bouton |

Reproposer la commande sur `quote_spent` fait appuyer la personne une seconde
fois en croyant que rien ne s'est passé.

⚠️⚠️ **ENVOYEZ `Idempotency-Key`, ET VOUS NE VERREZ PAS CE REFUS SUR UN
RENVOI.** Avec la **même** clé, la même requête rend la **même commande** : les
mêmes identifiants de course, le même ordre de voitures, rien de créé en plus.
C'est la seule façon de distinguer « ma requête s'est perdue en route » de « je
commande encore ».

- **Une clé par intention**, la même sur tous les renvois de cet appel — ce que
  vous voulez (l'identifiant du tour, un UUID), du moment qu'elle est stable sur
  un renvoi et **différente** sur une nouvelle commande.
- ⚠️ **Ne réutilisez jamais une clé pour autre chose** : une clé différente sur
  un devis dépensé est refusée, et une clé réutilisée rendrait une commande que
  vous n'avez pas faite.
- ⚠️ **Sans clé, pas de rejeu.** Le silence ne vaut pas accord : un devis
  dépensé sans clé refuse tout, y compris un renvoi.

⚠️ Une commande de **plusieurs voitures** dépense le devis **une seule fois** —
c'est l'appel qui se dépense, pas chaque voiture. Et une commande **défaite**
(une des voitures échoue, tout est annulé) **libère** le devis : elle n'a rien
produit.

> ⚠️ **La majoration se DIT.** `surge_multiplier` est en **millièmes** :
> `1300` = ×1,3. Renseigné, il doit apparaître à l'écran — une majoration
> découverte au paiement se lit comme une arnaque ; annoncée, elle informe.

**`409 route_unavailable`** : le réseau routier est injoignable. Aucun prix
n'est deviné — une distance à vol d'oiseau ferait payer un trajet qui n'existe
pas. C'est un refus temporaire, pas une erreur de saisie.

---

### 🎁 Les PROMOTIONS (v4.16.0)

> ⚠️ **`fare_xof` EST DÉJÀ REMISÉ.** C'est ce que le passager paie, un point
> c'est tout. Ne soustrayez **rien** : `promo_discount_xof` est là pour
> **afficher** la remise, pas pour la calculer. Servir un prix plein à
> soustraire ensuite aurait obligé chaque écran à refaire l'opération, et l'un
> d'eux l'aurait oublié — sur le total, ou sur le reçu.

`promo_title` et `promo_discount_xof` ne sont **servis que si une promotion
s'applique**, exactement comme la majoration. Affichez alors le prix barré et
le titre de l'offre :

```
  2 500 F  2 250 F   ·  Offre de lancement
  ───────
```

Le prix plein se reconstitue par `fare_xof + promo_discount_xof` — si vous
tenez à le barrer. Ne l'affichez **pas** quand `promo_discount_xof` est
absent : un « −0 F » se lit comme une remise nulle, pas comme une absence de
remise.

**La promotion est FIGÉE avec le devis.** Une offre retirée entre le devis et
la confirmation ne change pas le prix promis, et `promo_title` reste sur la
course. C'est la même règle que pour la majoration : ce qui a été affiché est
ce qui est payé.

**Les offres en cours**, pour une bannière ou un écran « promotions » :

```
GET /promotions
→ { "items": [ { "id": "6ab…", "scope": "class", "class_keys": ["eco"],
                 "title": "−20 % sur Éco", "description": "…",
                 "kind": "percent", "value": 20,
                 "starts_at": "…", "ends_at": "…", "live": true } ] }
```

Une seule promotion s'applique à une course — **la plus avantageuse**. Il n'y
a pas de cumul : deux offres lancées par deux personnes différentes
offriraient la course sans que ni l'une ni l'autre ne l'ait voulu.

> ⚠️ **`GET /promotions` ANNONCE, LE DEVIS DÉCIDE (v4.17.0).** Une offre
> listée là peut très bien ne pas s'appliquer à **votre** course, et ce n'est
> pas un bogue :
>
> - chaque opération a une **enveloppe**, et quand elle est consommée l'offre
>   s'arrête — parfois dans la journée ;
> - une offre peut être limitée à **N fois par personne**, et vous l'avez
>   peut-être déjà utilisée ;
> - une remise qui ne tient plus dans ce qui reste du budget est **refusée**,
>   jamais rabotée — les petites courses en profitent donc encore quand les
>   longues n'y ont plus droit.
>
> **N'affichez donc jamais un prix calculé à partir de `GET /promotions`.**
> Le seul prix vrai est `fare_xof` du devis. Servez-vous de cette liste pour
> une bannière — « des offres en ce moment » — et laissez le devis dire
> combien.

---

### 🎟️ LE CODE PROMO — celui qu'on TAPE (v4.51.0)

Une promotion s'applique d'elle-même ; un **code** se saisit. Il vient d'une
affiche, d'un influenceur ou du parrainage d'un proche, et il est **unique pour
toute la plateforme** : le même mot ne peut pas valoir une chose sur une course
et une autre sur une commande de repas.

**Un seul champ, au devis** :

```
POST /rides/quote
{ "stops": [ … ], "code": "DIRA2000" }
```

La réponse ne change pas de forme : chaque classe porte son prix, déjà remisé,
plus ce que le code a donné **sur cette classe**.

```json
{ "items": [ {
  "class_key": "eco", "fare_xof": 2000,
  "promo_code": "DIRA2000", "promo_code_xof": 500,
  "expires_at": "2026-10-08T12:43:20Z"
} ] }
```

> ⚠️ **`fare_xof` EST DÉJÀ REMISÉ**, comme avec une promotion automatique. Ne
> soustrayez **rien** : `promo_code_xof` sert à **afficher** la remise, pas à
> la calculer.

> ⚠️ **LA REMISE N'EST PAS LA MÊME SUR TOUTES LES CLASSES**, et c'est normal :
> 20 % d'une moto et 20 % d'un van ne font pas le même nombre de francs, et un
> code peut être plafonné. Affichez `promo_code_xof` **ligne par ligne**, jamais
> une remise unique en tête de liste — elle serait fausse sur trois classes sur
> quatre.

> ⚠️ **UN SEUL APPEL, POUR LES QUATRE CLASSES.** Le serveur valide le code
> **une fois** et calcule une remise par prix. C'est ce qui garantit qu'un code
> ne peut pas être « accepté pour la moto et refusé pour le van » dans le même
> écran : le verdict est unique, seule la remise varie.

#### ⚠️ JAMAIS DEUX REMISES SUR UNE COURSE

`promo_title` et `promo_code` ne sont **jamais servis en même temps**. Une
application qui additionnerait les deux afficherait un montant que personne ne
paie.

C'est **la meilleure des deux** qui gagne, pour le passager. Et quand c'est
l'offre automatique qui gagne, le code n'est pas effacé pour autant — il revient
avec un drapeau :

```json
{ "fare_xof": 2100, "promo_title": "Soirée", "promo_discount_xof": 400,
  "promo_code": "DIRA2000", "promo_code_ignored": true }
```

> ⚠️ **`promo_code_ignored` DOIT ÊTRE DIT À L'ÉCRAN.** Un code accepté qui ne
> change pas le prix se lit comme un code cassé : le passager le ressaisit,
> vérifie les majuscules, puis appelle le support — qui n'en saura pas plus. La
> seule phrase vraie est **« gardez-le : une meilleure offre s'applique déjà »**,
> et ce champ est la seule chose qui permet de l'écrire. Ne le traitez pas comme
> un refus : il n'y a rien à corriger, et le code n'a **rien consommé** — il
> servira une autre fois.

#### Les refus, et ce que l'écran doit en faire

Un code saisi mérite une réponse qui dit **quoi faire**. Le devis échoue avec un
code nommé plutôt que de servir le prix plein en silence — sans quoi le passager
croirait que son code marche et qu'il ne donne rien.

| Code | Ce qui se passe | Ce que l'écran dit |
|---|---|---|
| `promo_code_invalid` | la saisie n'a pas la forme d'un code | « vérifiez le code » — garder le champ ouvert |
| `promo_code_unknown` | ce code n'existe pas | « ce code n'existe pas » |
| `promo_code_expired` | hors de sa fenêtre, ou éteint | « cette offre est terminée » |
| `promo_code_wrong_country` | valable ailleurs — `meta.valid_in` porte le pays | « ce code vaut au Togo, pas ici » |
| `promo_code_wrong_service` | valable pour l'autre métier — `meta.valid_for` | « ce code est pour les commandes » |
| `promo_code_amount_too_low` | sous le minimum — `meta.min_amount_xof` | « à partir de 3 000 F » |
| `promo_code_exhausted` | l'enveloppe de l'offre est consommée | « cette offre est épuisée » |
| `promo_code_already_used` | la limite par personne est atteinte — `meta.max_uses_per_user` | « vous avez déjà utilisé ce code » |
| `promo_code_own_referral` | c'est **son propre** code de parrainage | « partagez-le : il est pour vos proches » |

> ⚠️ **N'AFFICHEZ PAS « CODE INVALIDE » POUR TOUT.** C'est le message qui envoie
> tout le monde au support, et le support n'en saura pas plus que l'écran. Les
> neuf refus ci-dessus ont chacun une suite différente : l'un demande de corriger
> une faute de frappe, l'autre de renoncer, l'autre d'attendre d'avoir une course
> plus longue. `message` est déjà traduit par l'API — affichez-le.

> ⚠️ **`promo_code_own_referral` N'EST PAS UNE ERREUR DE L'UTILISATEUR.** C'est
> le premier geste de tout le monde : on reçoit son code, on l'essaie. L'écran
> doit l'accueillir comme une explication, pas comme un échec — « ce code est
> celui que vous donnez à vos proches ».

#### Le code est FIGÉ avec le devis, et engagé à la commande

Comme la majoration et la promotion : **ce qui a été affiché est ce qui est
payé**. Le code, son libellé et son montant restent sur la course, et le reçu
pourra l'expliquer des semaines plus tard.

L'enveloppe de l'offre, elle, n'est **engagée qu'à la commande** — un passager
demande dix prix et en commande un. Conséquence : un code encore valable au
devis peut voir son enveloppe se vider entre deux écrans. **Le prix promis ne
change pas pour autant** : la course part au prix affiché. Vous ne verrez jamais
un prix bouger sous les yeux du passager à cause de notre comptabilité.

Annuler la course **rend** l'enveloppe : le code redevient utilisable.

### ⚠️ Une course se fait DANS une ville (v3.6.0)

Le départ et l'arrivée doivent être dans la **même ville desservie** —
dedans, ou à moins de **50 km** de sa limite (la banlieue, la plage,
l'aéroport voisin). Le refus arrive **au devis**, avant tout prix :

```
422 { "error": { "code": "out_of_service_area",
                 "message": "Ce lieu est à 380 km de Lomé : nous ne desservons pas au-delà de 70 km de la ville" } }
422 { "error": { "code": "different_cities",
                 "message": "Le départ est à Lomé et l'arrivée à Kara : une course se fait dans une seule ville" } }
```

Afficher le message tel quel (il est localisé et porte la distance). Les
villes sont publiques — `GET /cities` → `{ items: [ { key, name,
center: [lng, lat], radius_km, tolerance_km, active, country } ] }` — pour
dire « nous ne desservons pas encore ici » dès qu'un passager pose un point,
avant même le devis : un point est acceptable s'il est à moins de `radius_km
+ tolerance_km` du `center` d'une ville active, et les deux points doivent
tomber dans la **même** ville.

---

### 🧳 LA COURSE DU VOYAGEUR — elle se fait là où elle se fait (v4.27.0)

**Un passager commande dans le pays où il EST, pas dans celui où il s'est
inscrit.** Un Togolais de passage à Dakar pose son départ au Plateau et
obtient un prix, comme chez lui.

⚠️ **Ce qui se passait avant.** Le pays du COMPTE décidait de tout. Un
compte togolais à Dakar recevait `out_of_service_area` — « ce lieu est à
2 249 km de Lomé » — et l'application, qui ne recevait que les villes du
Togo, refusait même le point **avant d'appeler le serveur** : écran bloqué,
aucune raison lisible, alors que Dakar est desservie depuis des mois.

**Ce qui suit le passager** — tout ce qui fait la course : villes
desservies, tarifs et modes, majorations, promotions de ville, réglages
d'appel, chauffeurs appelés, facturation. Le pays vient du **point de
départ** et il est **figé sur le devis**, comme le prix ; la confirmation ne
le relit pas.

**Ce qui reste chez lui** : son compte, son historique, son portefeuille.

```
POST /rides/quote → items[].country = "SN"     ⚠️ v4.27.0
GET  /rides/{id}  → country = "SN"
```

⚠️ **`country` DIT LA MONNAIE DU PRIX.** Un montant de la plateforme est un
entier dans la monnaie de son pays : `fare_xof` vaut des francs CFA à Dakar,
et des francs guinéens à Conakry. Formatez avec la monnaie de **`country`**
(lue dans `GET /countries`), **jamais** avec celle du compte — sinon vous
écrivez le bon nombre derrière le mauvais symbole.

**`GET /cities` rend désormais TOUTES les villes, tous pays confondus**,
chacune avec son `country`. Votre contrôle local marche donc tel quel : la
ville qui sert le point est trouvée, où qu'elle soit.

#### ⚠️ Le catalogue des modes doit suivre le lieu, lui aussi

```
GET /classes?near=-17.4370,14.6690      ⚠️ v4.27.0
```

**Envoyez `near` (le `lng,lat` du départ) dès que le passager a posé un
point.** Sans lui, le catalogue reste celui du pays du COMPTE — et vous
afficheriez les modes du Togo (leurs noms, leurs pictogrammes, leurs frais
d'attente) avec des prix sénégalais collés dessus. Un mode servi à Dakar
mais absent du Togo n'aurait même aucune ligne où s'afficher.

Un `near` illisible est **ignoré, pas refusé** : le catalogue reste public
et lisible.

⚠️ **Re-demandez le catalogue quand le départ change de pays.** Les frais
d'attente et de temps réel affichés sous chaque mode viennent de ce
catalogue : laissés sur ceux de chez soi, ils annonceraient un tarif que la
course n'appliquera pas.

#### 💳 Le portefeuille, lui, ne traverse pas une monnaie

```
POST /rides   { quote_id, payment_method: "wallet" }
422 { "error": { "code": "wallet_other_currency", "reason": "wallet_currency",
                 "message": "Votre portefeuille est dans une autre monnaie que ce pays." } }
```

Le solde est un entier dans la monnaie de SON pays. Débiter 45 000 d'un
solde en francs CFA pour une course facturée 45 000 francs guinéens
prendrait **treize fois** le prix. Le refus ne vise donc **que ce
moyen-là** : proposez les espèces ou le paiement en ligne, et n'annulez
pas la course.

Entre deux pays de **même monnaie** — Togo et Sénégal, Gabon et Tchad — le
portefeuille marche comme chez soi : c'est le même franc.

⚠️ **Le refus arrive à la CONFIRMATION, pas au devis** : le devis ne sait
pas encore comment on paiera. Si votre écran présélectionne le
portefeuille, prévoyez le repli.

#### Là où nous ne sommes pas du tout

Le pays du compte reste, et le refus nomme une ville de chez lui. C'est
voulu : nous n'y opérons pas, et nommer une ville lointaine serait moins
honnête que nommer la sienne.

---

## 3 bis. 🗣️ Commander EN PARLANT — l'assistant du passager (v4.15.0)

```
POST /ai/chat   { "message": "…", "origin": [lng, lat] }   → { reply, plan? }
```

« Je veux aller de Tokoin à l'aéroport en van » rend une **phrase** et,
quand il y a de quoi composer, un **plan vérifié** :

```jsonc
{ "reply": "Je vous propose un van, il y en a un tout près.",
  "plan": {
    "stops": [ { "kind": "pickup", "label": "Tokoin, Lomé", "geo": [1.21, 6.15] },
               { "kind": "dest",   "label": "Aéroport de Lomé", "geo": [1.2545, 6.1656] } ],
    "quotes": [ { "id": "6aa2…", "class_key": "van", "fare_xof": 4200, "expires_at": "…" },
                { "id": "6aa3…", "class_key": "eco", "fare_xof": 2500, "expires_at": "…" } ],
    "unresolved": [], "when": "" } }
```

> ⚠️ **`reply` est une phrase à lire ; seuls `plan.quotes` commandent.**
> Chaque lieu a été **géocodé et borné à la ville desservie** par le
> serveur, chaque mode validé, et **le prix vient du devis** — jamais du
> modèle, qui n'a pas le droit d'annoncer un montant. Pour commander, c'est
> `POST /rides { "quote_id": … }` (§4), exactement comme un devis composé à
> la main. **L'assistant ne commande jamais.**

Ce qu'il faut envoyer et afficher :

- **`origin`, la position du téléphone**, à chaque appel si vous l'avez :
  elle sert de départ quand le passager n'en nomme pas (« je vais à
  l'aéroport »). Sans elle et sans départ nommé, la réponse vient **sans
  plan** — demandez la position, ne devinez pas.
- **`plan.stops` sur la carte** avant les prix : le passager doit voir
  *où* on l'emmène avant de voir *combien*. Un lieu mal compris se corrige
  d'un appui, sur la carte, pas dans une conversation.
- **`plan.quotes` en liste de modes**, dans l'ordre servi — celui que le
  passager a nommé est déjà en tête. Les devis **expirent** (`expires_at`,
  2 min) : passé ce délai, redemandez (§3), n'envoyez pas un `quote_id`
  périmé.
- **`plan.unresolved`** nomme ce que la carte ne connaît pas (« chez Tantie
  Adjo ») : dites-le et proposez la recherche d'adresse. Un **arrêt**
  introuvable ne fait pas échouer la course. Une **destination**
  introuvable — ou un départ nommé introuvable sans `origin` — la fait
  échouer : 🆕 (v4.110.0) **le plan revient quand même, SANS DEVIS**
  (`quotes: []`), `stops` avec ce qui a été trouvé et `unresolved` avec ce
  qui ne l'a pas été. Rien à commander : pas de bouton, dites ce qui manque.
  Avant, ce plan disparaissait, et avec lui le nom du lieu à dire.
- 🆕 **`reply` connaît la carte** (v4.110.0) : la phrase est écrite après la
  recherche des lieux, et ne promet plus une course qu'aucun lieu ne permet.
- **`plan.when`** (« dans 20 minutes ») est rendu **tel quel** :
  l'assistant ne programme pas. Si c'est une course à l'avance, envoyez le
  passager sur l'écran de programmation (§4 bis).
- `plan` **absent** = ce n'était pas une demande de course (un bonjour, une
  question de prix), ou ni départ ni position. Affichez la phrase, rien
  d'autre.

`502`/`503 assistant_unavailable` : l'assistant est indisponible — dites-le
et **laissez la composition manuelle**, qui ne dépend d'aucun modèle.

### 🎙️ Le VOCAL — 60 s, transcrit, MONTRÉ, puis envoyé

```
POST /ai/voice                multipart : file=<audio>, language=<code facultatif>
                                                                → { "text": "…", "language": "fr" }
```

**La langue parlée (v4.39.0).** Ajoutez `language=<code>` au multipart quand
la personne a choisi sa langue ; **n'envoyez rien** sinon — le serveur prend
alors la **première langue du pays**, réglée par l'exploitation (au Sénégal
ce peut être le wolof, au Togo le français). ⚠️ **Ce n'est PAS
`Accept-Language`** : celui-ci dit la langue de l'interface, et un téléphone
réglé en français n'empêche pas son propriétaire de parler wolof. Douze codes
sont admis, tout autre est refusé (`422 unknown_language`) :

| Code | Langue | Pays |
|---|---|---|
| `fr` · `en` | français · anglais | tous |
| `ee` · `kbp` | éwé · kabiyè | Togo |
| `wo` · `ff` · `srr` | wolof · peul (pulaar) · sérère | Sénégal |
| `man` · `sus` · `ff` | malinké · soussou · peul (pular) | Guinée |
| `shu` · `sba` · `ff` | arabe tchadien · ngambay (sara) · peul (fulfulde) | Tchad |
| `fan` | fang | Gabon |

La réponse porte la langue retenue : `{ "text": "…", "language": "wo" }`.
Deux refus de plus : `503 language_not_served` (`reason` = la langue) quand
aucun fournisseur configuré ne l'entend — gardez le clavier, comme pour
`assistant_unavailable` — et `422 unknown_language`. Un vocal dans une
langue locale est transcrit **dans cette langue**, mots français compris :
ne le traduisez pas, montrez-le tel quel.

⚠️ **La transcription n'est PAS exécutée.** Elle revient à vous ; vous
l'**affichez dans le champ de saisie**, corrigible, et c'est le client qui
l'envoie à `/ai/chat`.

### 🆕 🎯 REMPLIR LE DÉPART OU LA DESTINATION À LA VOIX (v4.108.0)

```
POST /ai/voice   multipart : file, language?, seconds?, context=pickup|dropoff, near="lng,lat"
→ { "text": "au marché de Bè", "language": "fr", "context": "dropoff",
    "candidates": [
      { "label": "Marché de Bè, Lomé", "point": [1.2390, 6.1319],
        "place_id": "dira:overture:42", "kind": "market",
        "distance_m": 1500, "confidence": 1 } ] }
```

**Pour Dira Lite, et pour tout champ qu'on remplit en parlant.** La
reconnaissance du téléphone ne connaît ni les quartiers ni les repères d'ici ;
la carte de Dira, si. Ajoutez `context` (le champ : `pickup` ou `dropoff`) et
`near` (la position du téléphone, « longitude,latitude ») : ce qui a été dit
est cherché dans la carte — sa base locale d'abord —, et les lieux trouvés
reviennent dans `candidates`.

- **Montrez « J'ai entendu « … » »** dans le champ, corrigible, et **proposez
  les candidats** directement — le premier lu à voix haute. Un candidat touché
  remplit le champ avec `label` et `point`.
- **Cinq au plus**, classés par `confidence` — la part de ce qui a été dit
  retrouvée dans le nom du lieu, de 0 à 1 — puis par l'ordre de la carte.
  `distance_m` est mesurée à vol d'oiseau depuis `near` (absente sans lui) ;
  `kind` est la catégorie la plus précise que la carte connaisse (`market`,
  `hospital`…), absente quand elle ne sait pas.
- ⚠️ **Les tournures d'ouverture ne sont pas cherchées** : « emmène-moi au
  marché de Bè » cherche « marché de Bè ». Vous n'avez rien à nettoyer.
- ⚠️ **`candidates` absent ou vide n'est PAS une erreur** : la carte n'a rien
  trouvé. Lancez votre recherche habituelle sur `text`.
- ⚠️ **Rien n'est choisi à la place de la personne** : ce n'est jamais une
  course, ni même un lieu retenu — une liste à montrer.
- **Envoyez `seconds`**, la durée mesurée par l'enregistreur : au-delà de 60,
  `413 audio_too_long`, **avant** toute transcription (même écran que
  `audio_too_large` : « message trop long, recommencez plus court »).
- Sans `context`, ou avec une valeur inconnue : une transcription nue, comme
  avant. Une `near` illisible est ignorée, jamais refusée.

> ### 🆕 ⚠️⚠️ CETTE ROUTE NE GARDE RIEN, ET C'EST VOULU (v4.84.0)
>
> `/vtc/ai/voice` **transcrit, puis oublie** : le fichier audio n'est
> conservé nulle part, et l'échange n'est écrit dans aucun fil. Vous recevez du
> texte, et c'est tout.
>
> Ce n'est pas un oubli de notre part : c'est la route d'une application **d'un
> seul métier**, où nous ne pouvons rien composer de plus. Depuis la v4.84.0,
> l'application cliente **unifiée** dispose d'une route — `POST
> /analytics/ai/voice` — qui, elle, garde l'audio, le rend réécoutable, écrit
> l'échange dans un fil durable, et alimente la file du support quand l'IA n'a
> pas compris.
>
> ⚠️ **TROIS CONSÉQUENCES QUI VOUS CONCERNENT DIRECTEMENT :**
>
> 1. **Le support ne verra JAMAIS un vocal envoyé par cette route.** Un client
>    qui se plaint (« j'ai dit riz sauce, on m'a livré riz gras ») ne laissera
>    aucune trace réécoutable : il n'y aura que la parole de l'un contre celle
>    de l'autre.
> 2. **Aucune commande ne partira jamais toute seule d'ici.** La transcription
>    revient à l'application, et c'est le client qui l'envoie. Le seuil de
>    confiance réglable dans la console ne s'applique qu'à la route unifiée.
> 3. **Si votre application est celle qui fait les deux métiers, utilisez la
>    route unifiée** et non celle-ci. Si elle n'en fait qu'un, celle-ci reste la
>    bonne — en sachant ce qu'elle ne fait pas.
 Les accents d'ici ne pardonnent pas : partir à
« l'aéroport » quand quelqu'un a dit « la gare » est pire que pas
d'assistant du tout.

**Enregistrez en opus ou AAC, MONO, 16-24 kbit/s** — 60 s pèsent alors
~150 Ko. Le WAV est refusé : une minute fait 5 Mo, soit **des minutes**
d'envoi sur un réseau lent, et c'est l'envoi, pas l'IA, qui fait attendre.
Limite **2 Mio**, ~60 s.

| Étape | à montrer | 3G lent | 4G |
|---|---|---|---|
| envoi de l'audio | la barre d'envoi | 10-16 s | 1-2 s |
| transcription | « transcription… » | 1-3 s | 1-3 s |
| réponse | « l'assistant écrit… » | 2-10 s | 2-10 s |

Une seule requête en vol. Pas de réessai automatique en boucle : un bouton,
une fois. `413 audio_too_large` = l'application n'a pas compressé ;
`503 assistant_unavailable` = la voix n'est pas servie — **gardez le clavier
disponible**, tout marche sans elle.

---

## 4. Commander

```
POST /rides
{ "quote_id": "6aa2…", "payment_method": "cash" | "wallet" | "online",
  "cars": 1 }                                              🆕 v4.83.0
```

⚠️ **Aucun montant.** Le prix vient du devis mémorisé.

⚠️ 🆕 **`voice_id` (v4.99.2)** : si cette course vient d'un plan que l'assistant
a proposé et que la personne vient de **confirmer**, passez le `voice_id` de la
bulle. Facultatif, et il ne change **rien** au prix ni aux règles — il relie la
course au tour, pour que le fil cesse de reproposer la confirmation d'un plan
déjà commandé. Voir `DIRA-CLIENT.md` §8 bis.

### 🆕 🚗🚗 PLUSIEURS VOITURES EN UNE COMMANDE (v4.83.0)

« Il nous faut trois voitures pour l'aéroport. » Un geste, **trois courses** au
même départ et à la même arrivée, chacune avec son chauffeur.

```
GET /settings/rider[?near=lng,lat]
→ { "max_concurrent_rides": 3, "max_cars_per_request": 1 }
```

⚠️ **`?near=` ICI AUSSI (v4.95.0)**, comme sur `/settings/modes`,
`/settings/share` et `/classes`. Les bornes qui s'appliquent sont celles du pays
où l'on **commande**, pas celles du pays du compte : sans ce paramètre, un
passager togolais à Dakar lit les bornes du Togo — et se voit refuser une
deuxième voiture que le Sénégal autorisait, ou proposer un sélecteur que le
Sénégal n'ouvre pas. Passez le **point de départ** que vous avez déjà sous la
main pour le devis.

⚠️⚠️ **LISEZ CE RÉGLAGE AVANT DE DESSINER QUOI QUE CE SOIT.**
`max_cars_per_request` vaut **1 par défaut** : la fonctionnalité est **éteinte**
tant qu'un pays ne l'ouvre pas. À 1, **ne dessinez aucun sélecteur** — proposer
trois voitures dans un pays qui n'en sert qu'une fait arriver le refus après le
choix.

⚠️⚠️ **LA FORME DE LA RÉPONSE SUIT `cars`** — et c'est le **seul** endroit de
cette API où cela arrive.

| `cars` | Réponse |
|---|---|
| absent ou `1` | **la course**, exactement comme avant |
| `> 1` | `{ "booking_id": "…", "cars": 3, "rides": [ … ] }` |

Deux routes auraient dupliqué toutes les règles de la commande — devis, pays,
paiement, bornes — et deux copies d'une règle d'argent finissent par ne plus
dire la même chose. Vous savez ce que vous avez demandé : la forme n'est jamais
une surprise.

⚠️ **TROIS COURSES, ET NON UNE COURSE À TROIS VOITURES.** Chacune a son
identifiant, son statut, son chauffeur, son reçu et sa note. Elles portent le
même `booking_id`, et un `booking_index` de 1 à `booking_size`.

⚠️ **AFFICHEZ `booking_index`** : « Voiture 2 sur 3 ». Trois cartes au même
départ, à la même arrivée et au même prix sont **indistinguables** — sans ce
rang, vous les numérotez à votre façon, et l'ordre change à chaque
rechargement de la liste.

⚠️ **ELLES NE SE TERMINENT PAS ENSEMBLE**, et ce n'est pas une panne : trois
chauffeurs, trois trajets. L'une peut être `in_transit` quand une autre cherche
encore.

**Ce qui est refusé, et pourquoi :**

| Refus | Quand | Ce que vous affichez |
|---|---|---|
| `422 too_many_cars` | au-delà de `max_cars_per_request` ; `meta.max_cars` le dit | corrigez le sélecteur, ne montrez pas « erreur » |
| `409 too_many_active_rides` | la borne de courses simultanées est atteinte ; `meta.active`, `meta.max_rides`, `meta.ride_ids` | « vous avez déjà N courses en cours » **+ un lien vers elles** |
| `422 cars_on_payment_method` | `online` (trois paiements mobile money à enchaîner) ou `subscription` (il couvre **une** course) | proposez espèces ou solde Dira |
| `422 cars_on_pool` | mode partagé : il remplit **une** voiture | proposez un autre mode |
| `402 insufficient_funds` | en `wallet`, le solde ne couvre pas le **total** ; `meta.fare_xof` est le total, `meta.cars` le nombre | routez vers la recharge |

⚠️⚠️ **LA REMISE NE S'APPLIQUE QU'À LA PREMIÈRE VOITURE.** Une commande part
d'**un** devis : les courses suivantes paient le **tarif plein**, et leur
`fare_xof` est donc **plus élevé** que celui du devis. Dites-le sur l'écran de
confirmation — un total inattendu au moment de payer est le pire endroit pour
une surprise. (Trois fois la remise pour un code saisi une fois ferait de
« commander trois voitures pour en garder une » le moyen le moins cher de
voyager.)

⚠️ **TOUT OU RIEN** : si la commande échoue en chemin, **aucune** voiture ne
part. Vous ne recevrez jamais une commande à moitié servie.

### 🆕 📋 MES COURSES EN COURS (v4.83.0)

```
GET /rides/active
→ { "items": [ { … une course, comme GET /rides/{id} … } ] }
```

⚠️⚠️ **N'UTILISEZ PLUS LA PREMIÈRE PAGE DE `GET /rides` POUR CELA.**
`GET /rides` est l'**historique**, paginé, les plus récentes d'abord : il
contient vos courses en cours **par accident**, parce qu'elles sont récentes.
Le jour où quelqu'un termine quatre courses dans l'heure, sa course en cours
sort de la première page — et l'écran qu'il ouvre en attendant sa voiture est
vide.

La liste comprend les courses **en recherche**. Elle n'est **pas paginée** :
elle est bornée par `max_concurrent_rides` (trois par défaut, vingt au plus).

Regroupez par `booking_id` si vous voulez une carte par commande.

⚠️ **Le `mission_id` du suivi est l'`id` de la course** — voir §6, et la
nouvelle socket qui en porte plusieurs.

| Moyen | Ce qui se passe à la commande | À l'annulation |
|---|---|---|
| `cash` | rien : le chauffeur encaisse à l'arrivée | rien à rendre |
| `wallet` | le solde est **vérifié** (`402` s'il ne couvre pas) — il n'est **débité qu'à l'acceptation** d'un chauffeur (v4.4.0) | **remboursé** sur le portefeuille si un chauffeur avait accepté ; rien à rendre avant |
| `online` | `payment_url` est rendue — **rien n'est encaissé** | remboursé **si** le paiement a été confirmé |

> `payment_method: "subscription"` (v4.23.0) se **lit** sur une course née
> d'un abonnement — il ne se **commande** pas ici. Cette course est déjà
> payée : pas d'écran de paiement à la fin — section **4 ter**.

> ⚠️ **`402 insufficient_funds`** sur `wallet` : le solde ne couvre pas la
> course. Ce n'est pas une panne — routez vers la recharge du portefeuille,
> pas vers un message d'erreur.
>
> ⚠️⚠️ **LA RECHARGE D'UN PASSAGER EST `POST /payments/initiate`, PAS
> `POST /wallet/purchase`** (v4.91.0). Ce document a indiqué la seconde pendant
> des mois, à trois endroits. Elle achète des **jetons** — le droit d'entrée
> d'un livreur, l'outil de promotion d'un marchand — et le socle la **refuse à
> un client** (`403`). Une application qui l'a suivie envoyait donc le passager
> vers un refus, au moment précis où il venait de voir « solde insuffisant ».
>
> ```
> POST /payments/initiate                  # mobile money, au socle
>   Idempotency-Key: <votre clé>           # voir §recharge
>   { "purpose": "wallet_topup", "ref_id": "<votre propre id>",
>     "amount": 5000, "provider": "tmoney" }
> → 201 { "payment": { "id", "status": "pending", "expires_at",
>                      "operator_reached" }, "payment_url": "…" }
> ```
>
> **Le solde part à l'acceptation, pas à la commande (v4.4.0).** Une
> recherche sans preneur ne fait pas partir puis revenir l'argent, et un
> passager qui annule pendant la recherche n'a rien à se faire rendre. Le
> solde est **vérifié** à la commande pour que personne ne roule pour une
> course impayable. Si le solde a fondu entre la commande et l'acceptation
> (dépensé ailleurs), la course est **annulée** — `status: cancelled`,
> `cancelled_reason: "payment_failed"`, push `ride_cancelled` — et le
> passager doit recharger avant de recommander. Affichez le solde sur
> l'écran de recherche : c'est lui qui paiera quand un chauffeur dira oui.
>
> **Le pays peut débiter à une autre étape (v4.12.0).** La course rend
> **`charge_at`** : `accept` (le défaut ci-dessus), `request` (débité à la
> commande — remboursé si annulée), `start` (à la montée à bord) ou
> `complete` (déposé). Affichez « débité à … ». Après `accept`, la course
> ne s'annule plus pour un solde insuffisant : à la montée ou à l'arrivée,
> l'impayé devient la **dette** du passager (`GET /wallet` → `debt_xof`),
> remboursée d'office sur sa prochaine recharge.

> ⚠️ **`payment_url` ne prouve RIEN.** Elle ouvre la page de l'opérateur. La
> course reste impayée tant que le serveur n'a pas reçu la confirmation :
> conclure au retour de cette page offrirait la course. Rafraîchissez
> `GET /rides/{id}` et lisez… rien de plus que le statut — le passager n'a pas
> besoin de savoir si la plateforme a encaissé.

La réponse est la course, en `searching`.

---

## 4 bis. Programmer une course — ponctuelle ou récurrente (v3.5.0)

Une programmation est une **intention datée**, pas une course. À l'heure
dite, la plateforme fait ce que le passager aurait fait — un devis **au prix
du moment**, une commande, l'appel d'un chauffeur — et la course qui en sort
est une course ordinaire, avec `schedule_id`.

```
POST /scheduled-rides
{ "stops": [ { "kind": "pickup", "label": "Maison", "geo": [lng, lat] },
             { "kind": "dest",   "label": "Bureau", "geo": [lng, lat] } ],
  "class_key": "eco", "payment_method": "cash",
  "kind": "once",      "at": "2026-09-15T07:30:00Z",              # ponctuelle
  — ou —
  "kind": "recurring", "recurrence": { "days": [1,2,3,4,5], "time": "07:30",
                                       "tz": "Africa/Lome", "until": "2026-12-31T00:00:00Z" },
  "alert_lead_min": 5, "note": "…" }
→ 201 { "id", "kind", "status": "active", "at": <prochaine occurrence>, "alert_at",
        "recurrence", "describe": "lun, mar, mer, jeu, ven à 07:30", "alert_lead_min", "runs": [] }

GET  /scheduled-rides            # les actives et en pause ; ?all=1 pour tout
GET  /scheduled-rides/{id}       # avec `runs` : chaque lancement
POST /scheduled-rides/{id}/pause · /resume · /cancel
```

- `at` est l'heure du **lancement de l'appel** — pas une heure d'arrivée du
  chauffeur. Une ponctuelle doit être au moins `alert_lead_min` minutes dans
  le futur (sinon `422` : commander tout de suite). Les jours de la
  récurrence sont ISO (1 = lundi … 7 = dimanche), l'heure est **locale** au
  fuseau donné (`Africa/Lome` par défaut).
- ⚠️ **Le passager est PRÉVENU avant que l'appel ne parte** :
  `ride_scheduled_soon` (« nous appelons un chauffeur dans 5 min ») à
  `alert_at` ; puis `ride_scheduled_started` quand la course existe (data :
  `ride_id` — ouvrir l'écran de la course, elle est `searching`) ; ou
  `ride_scheduled_failed` avec la raison (solde insuffisant, itinéraire,
  plateforme indisponible à l'heure dite). Data commun :
  `{ "type": "scheduled_ride", "schedule_id", "at" }`.
- `runs[]` : `launched` (avec `ride_id`), `failed` (avec `reason`),
  `skipped` (occurrence passée en pause). Une occurrence manquée de plus de
  15 min n'est **pas** lancée en retard.
- `pause` suspend ; `resume` reprend à la prochaine occurrence — une
  ponctuelle dont l'heure est passée ne reprend pas (`409 schedule_passed`).
  `cancel` met fin ; les courses déjà lancées continuent.
- Statuts : `active` · `paused` · `done` (ponctuelle lancée, ou récurrence
  arrivée à `until`) · `cancelled`.

### ⚠️ « Pas demain » et « plutôt à 8 h » — UNE occurrence (v4.23.0)

```
POST /scheduled-rides/{id}/skip-next          → la prochaine, et seulement elle, est sautée
POST /scheduled-rides/{id}/postpone { "minutes": 45 }
```

Mettre en pause arrête la série ; annuler y met fin. **Ni l'un ni l'autre ne
répond à « pas demain matin »** — un rendez-vous, un jour férié, un enfant
malade —, et c'est le geste que fait le plus souvent quelqu'un qui roule tous
les jours. Ces deux routes sont **le bouton du rappel** : quand la
notification `ride_scheduled_soon` arrive, le passager doit pouvoir sauter ou
reporter d'un geste, sans ouvrir la liste des programmations.

- `skip-next` : la série continue. Une programmation **ponctuelle** n'a rien
  à sauter — c'est une annulation, et la réponse le dit (`409 not_recurring`).
- `postpone` : 1 à 720 minutes (12 h). ⚠️ **Seulement la prochaine** : le
  motif ne bouge pas. « Aujourd'hui à 8 h au lieu de 7 h » ne veut pas dire
  « désormais à 8 h ». Les rappels repartent à zéro — le passager sera
  prévenu à la **nouvelle** heure. Un report qui rattraperait l'occurrence
  suivante est refusé (`409 postpone_overlaps`) : deux départs le même matin,
  c'est deux chauffeurs.
- Les deux rendent la programmation à jour : relisez `at` et `alert_at`.

---

## 4 ter. 🎟️ L'ABONNEMENT — le trajet de tous les jours (v4.23.0)

Un abonnement, c'est **les mêmes trajets, tous les jours, payés d'avance et
moins cher**. Aller au bureau le matin, rentrer le soir, déposer les enfants
à l'école en chemin. Le passager décrit son rythme une fois ; la plateforme
appelle le chauffeur toute seule, chaque jour, aux heures dites.

Trois moments, et l'application les suit dans cet ordre : **estimer**,
**payer**, **être prévenu**.

### 1. Estimer — ce que ça coûte, avant de s'engager

```
POST /subscriptions/estimate
{ "tz": "Africa/Dakar",  "starts_on": "2026-10-05T00:00:00Z",   # demain par défaut
  "legs": [
    { "label": "Aller bureau",
      "stops": [ { "kind": "pickup", "label": "Maison",  "geo": [lng, lat] },
                 { "kind": "stop",   "label": "École",   "geo": [lng, lat] },
                 { "kind": "dest",   "label": "Bureau",  "geo": [lng, lat] } ],
      "class_key": "eco", "days": [1,2,3,4,5], "time": "07:00" },
    { "label": "Retour",
      "stops": [ … ], "class_key": "eco", "days": [1,2,3,4,5], "time": "18:00" } ] }

→ 200 { "legs": [ { "label": "Aller bureau", "distance_m", "duration_s",
                    "unit_xof": 2500, "surge_name", "surge_multiplier",
                    "occurrences": 5, "normal_xof": 12500 }, … ],
        "weekly":  { "period": "weekly",  "from", "to", "occurrences": 10,
                     "normal_xof": 25000, "discount_pct": 10,
                     "price_xof": 22500, "saving_xof": 2500 },
        "monthly": { "period": "monthly", "occurrences": 46, "normal_xof": 115000,
                     "discount_pct": 20, "price_xof": 92000, "saving_xof": 23000 } }
```

- Un **trajet** (`leg`) est un aller OU un retour, avec ses propres jours et
  son heure. Un `leg` peut avoir **jusqu'à 5 arrêts** : l'école puis le
  bureau, c'est **un seul trajet et une seule course**. Six trajets au plus.
- `days` : jours ISO, 1 = lundi … 7 = dimanche. `time` : `"HH:MM"`, l'heure
  de **récupération**, **locale au fuseau du passager** — « 7 h » veut dire
  7 h chez lui.
- ⚠️ **Les deux périodes arrivent ENSEMBLE.** Ne demandez pas « la semaine »
  puis « le mois » : le passager choisit en **comparant**, et il faut les
  montrer côte à côte, avec `saving_xof` en évidence. C'est l'économie qui
  décide, pas le prix.
- ⚠️ **Le mois n'est pas « quatre semaines ».** `occurrences` compte les
  jours **réels** entre deux dates : du 5 octobre au 5 novembre, il y a 23
  jours ouvrés — pas 20, pas 21,67. Affichez ce nombre : c'est ce que
  quelqu'un qui travaille cinq jours par semaine vérifie avant de s'engager.
- ⚠️ **La majoration est dans le prix.** `surge_name` / `surge_multiplier`
  (en millièmes, 1400 = ×1,4) sont rendus par trajet, **présents seulement si
  une majoration s'applique**. Un abonnement au départ de l'aéroport coûte ce
  que coûtent des courses depuis l'aéroport — dites-le, sinon le passager
  croira à une erreur.
- `422` : un motif qui ne produit **aucune** course dans la période, ou
  **trop peu** (« *at least 4 rides over the period* ») — en dessous du
  plancher du pays, l'abonnement n'a pas de sens : deux courses par mois se
  commandent à la course.

Les **conditions du pays** — pour annoncer « jusqu'à −20 % le mois » avant
même l'estimation :

```
GET /settings/subscription
→ { "weekly_discount_pct": 10, "monthly_discount_pct": 20,
    "min_occurrences": 4, "alert_leads_min": [30, 1], "pending_hours": 24 }
```

### 2. Souscrire et payer — l'abonnement est payé d'AVANCE

```
POST /subscriptions
{ …le même corps que l'estimation…, "period": "monthly",
  "payment_method": "wallet",     # ou "online" — PAS d'espèces
  "note": "…" }
→ 201 { "id", "status": "pending_payment", "period", "price_xof", "normal_xof",
        "saving_xof", "occurrences", "starts_on", "ends_on",
        "alert_leads_min": [30, 1], "legs": [ … ] }

POST /subscriptions/{id}/pay
→ 200 { "status": "active", "paid_at" }                       # portefeuille
→ 200 { "status": "pending_payment", "payment_url": "https://…" }   # en ligne
```

- ⚠️ **Le prix est RECALCULÉ à la souscription**, il n'est pas repris de
  l'estimation. Une estimation vieille d'une heure n'engage pas la
  plateforme. Montrez le prix rendu par le `201`, pas celui que vous aviez.
  S'il a changé, dites-le.
- ⚠️ **`pending_payment` n'est PAS un abonnement.** Aucune course ne part
  tant que le paiement n'est pas acquis. Passé `pending_hours` (24 h par
  défaut), il **expire tout seul**.
- `payment_url` **n'est pas un encaissement** : ouvrez-la, puis attendez. Le
  statut passe à `active` quand l'opérateur rappelle le socle — relisez
  `GET /subscriptions/{id}`, ou attendez la notification
  `ride_subscription_active`.
- `402 insufficient_funds` : solde insuffisant. Proposez de recharger, puis
  de rappeler `/pay` — l'abonnement est toujours là.
- À l'activation, **chaque trajet devient une programmation récurrente** :
  `legs[].schedule_id` apparaît. C'est elle qui préviendra et appellera.
- Les courses lancées par un abonnement portent
  `payment_method: "subscription"` : ⚠️ **elles ne sont pas re-facturées**.
  N'affichez ni prix à payer ni écran de paiement à la fin — c'est déjà payé.
  Le montant reste visible à titre indicatif.

### 3. Vivre avec — être prévenu, et pouvoir dire non

⚠️ **Le passager est prévenu DEUX FOIS avant chaque départ** :
`alert_leads_min` vaut `[30, 1]` par défaut — **30 minutes pour se préparer,
1 minute pour renoncer**. Un seul rappel loin du départ s'oublie ; un seul
rappel juste avant ne laisse pas le temps de s'habiller.

Les deux rappels sont la notification `ride_scheduled_soon` (data :
`{ "type": "scheduled_ride", "schedule_id", "at" }`), suivie de
`ride_scheduled_started` quand l'appel part — exactement comme une
programmation ordinaire, parce que **c'en est une**.

⚠️ **Un rappel sans bouton pour dire non est une alarme, pas un service.**
Chaque rappel doit offrir, sur place :

| Le passager veut | Vous appelez |
|---|---|
| « pas aujourd'hui » | `POST /scheduled-rides/{schedule_id}/skip-next` |
| « dans 30 minutes » | `POST /scheduled-rides/{schedule_id}/postpone { "minutes": 30 }` |
| « laissez partir » | rien — l'appel part à l'heure |

Le `schedule_id` est celui du **trajet** (`legs[].schedule_id`), pas celui de
l'abonnement : on saute un aller, pas un mois.

Et sur l'abonnement lui-même :

```
GET  /subscriptions              # les miens en cours ; ?all=true pour l'historique
GET  /subscriptions/{id}
POST /subscriptions/{id}/pause · /resume · /cancel
```

- `pause` arrête les départs ; ⚠️ **la période ne s'allonge pas d'autant** —
  un mois payé est un mois de calendrier. Pour une absence d'**un jour**,
  `skip-next` sur le trajet concerné, pas une pause.
- `cancel` : un abonnement encore `pending_payment` est simplement abandonné.
  Un abonnement **actif n'est pas remboursé au prorata** par cette route —
  le remboursement est une décision de l'exploitation, à demander au support.
- Statuts : `pending_payment` · `active` · `paused` · `expired` (la période
  est passée — notification `ride_subscription_ended`) · `cancelled`.
- ⚠️ **Un abonnement ne se renouvelle PAS tout seul.** À `ends_on`, il
  expire. Proposez de reprendre le même — c'est le moment où le passager y
  pense, et le seul.

---

## 4 quater. 🚕 LES AUTRES MODES DE COURSE (v4.31.0)

**Trois modes en plus du mode ordinaire. Le PAYS les ouvre, et chaque VOITURE
dit lesquels elle vend — les deux conditions, pas l'une ou l'autre.**

```
GET /settings/modes?near=lng,lat
  → { free:   { enabled, scannable, max_hours },
      rental: { enabled, max_radius_km, alert_km_before },
      pool:   { enabled, pickup_radius_m, dropoff_radius_m,
                min_distance_m, search_ttl_s, max_riders,
                fare_pct } }                                      # v4.44.0

GET /classes?near=lng,lat
  → items[].modes = {
       "free":   { base_xof, per_km_xof, per_min_xof, min_fare_xof },
       "rental": { tiers: [ { hours, price_xof, included_km } ], extra_per_km_xof },
       "pool":   { } }    # v4.44.0 — présence = cette voiture partage
```

⚠️ **PASSEZ `near` SUR LES DEUX, AVEC LE POINT DE DÉPART** (v4.42.0). Les deux
routes rendent alors les réglages et le catalogue **du pays où la course se
fera**. Sans `near`, elles rendent ceux du pays du **compte** — et un passager
togolais à Dakar lisait jusqu'ici les modes du Togo avec les prix du Sénégal :
deux pays dans un seul écran. Tant que vous ne connaissez pas encore le départ,
appelez sans `near` ; **rappelez dès que le passager l'a posé**.

⚠️ **DEUX DOCUMENTS, ET ILS NE DISENT PAS LA MÊME CHOSE.** Les réglages disent
ce que le pays **ouvre** et les bornes qui valent pour toutes les voitures ; le
catalogue dit ce que **cette voiture-là** vend, et **à quel prix**.

⚠️ **LISEZ-LES AVANT DE MONTRER QUOI QUE CE SOIT.** Un mode éteint doit
**disparaître de l'écran**, pas y rester et échouer.

⚠️ **UNE CLÉ PRÉSENTE DANS `modes` = UN MODE VENDU PAR CETTE VOITURE, DANS CE
PAYS.** Le serveur a déjà croisé les deux conditions : `modes` ne contient
jamais un mode que le pays a fermé. Vous n'avez donc **pas** à recouper les
deux documents écran par écran — et c'est le point : le premier écran qui
l'oublierait proposerait une location qu'on refuse à la commande, au pire
moment. Une voiture **sans** `modes.rental` ne se loue pas ; ne l'affichez pas
dans la liste des locations.

⚠️ **LA POLITIQUE EST DANS LES RÉGLAGES, LES PRIX DANS LE CATALOGUE — et il
n'y a qu'un endroit pour chacune.** « Le partage est-il ouvert ? à partir de
combien de kilomètres ? pendant combien de temps cherche-t-on ? » :
`GET /settings/modes`. « Combien coûte ce trajet dans cette voiture ? » : le
devis, et la grille du catalogue pour l'expliquer. Si vous trouvez la même
valeur aux deux endroits, c'est une erreur de notre part — dites-le.

⚠️ **LES PRIX D'UN MODE NE SONT PLUS DANS LES RÉGLAGES DEPUIS LA v4.31.0.**
Les plages de location, leur forfait, les kilomètres compris, le tarif du
compteur et son plancher **vivent sur la voiture**, parce qu'ils en dépendent :
un van immobilisé cinq heures n'est pas une eco. Une application qui lirait
encore `settings.rental.tiers` n'y trouverait **plus rien** — et n'aurait aucune
durée à proposer.

---

### 📷 La course LIBRE — scanner un taxi qu'on a hélé

Un chauffeur peut lancer un compteur sans que personne ne l'ait commandé : un
taxi dans la rue. Vous montez, puis vous **scannez son code** pour suivre la
course et recevoir la facture à la fin.

```
GET  /rides/free/{code}        → la course, avec la carte du chauffeur
POST /rides/free/{code}/join   → elle devient la vôtre
```

**Deux temps, et l'ordre compte.** Montrez d'abord ce que `GET` rend — le nom
du chauffeur, la voiture, la plaque — et ne rattachez qu'après confirmation.
⚠️ Un scan qui rattache d'un coup, sans rien montrer, fait des réclamations :
la personne n'a pas vérifié dans quelle voiture elle monte.

Le code fait **6 caractères** de `ABCDEFGHJKLMNPQRTUVWXYZ2346789` — **sans
O/0, I/1 ni S/5**, parce qu'on le recopie à la main quand la caméra refuse.
Acceptez la saisie manuelle, en majuscules, et validez sur cet alphabet avant
d'appeler.

| Refus | Ce que ça veut dire | Ce que vous affichez |
|---|---|---|
| `404 free_ride_not_found` | code inconnu, course finie, ou **course d'un autre pays** | « ce code n'est plus valable » — proposez de réessayer |
| `409 free_ride_taken` | quelqu'un a scanné avant vous | « cette course est déjà suivie par un autre passager » |
| `409 free_ride_not_scannable` | le pays veut le compteur sans le rattachement | ne montrez pas le scanner du tout |
| `409 free_ride_own` | c'est votre propre course (compte chauffeur) | — |

⚠️ **RATTACHER NE CHANGE PAS LE MOYEN DE PAIEMENT.** La course reste **en
espèces** : un passager monté dans la rue n'a pas de portefeuille Dira.
Vous obtenez la **facture**, pas une autre façon de payer. Ne proposez ni
portefeuille ni mobile money sur une course libre.

⚠️ **LE PRIX N'EXISTE PAS AVANT LA FIN.** `fare_xof` vaut **0** pendant toute
la course : elle est facturée sur ce qui a été réellement roulé, au tarif du
mode du véhicule. **N'affichez pas « 0 F »** — dites « compteur en cours », et
montrez le montant à `completed`.

⚠️ **`auto_closed: true`** : la plateforme a fermé le compteur elle-même,
parce qu'il tournait depuis plus longtemps que la borne du pays. La course est
**terminée et facturée**. Dites-le, sans accuser le chauffeur.

#### ⚠️ UNE COURSE PEUT SE TERMINER SANS QUE PERSONNE N'APPUIE — `closed_by` (v4.66.0)

**Sur n'importe quelle course**, pas seulement un compteur. Deux valeurs, et
elles ne veulent pas dire la même chose :

| `closed_by` | ce qui s'est passé | `closed_reason` |
|---|---|---|
| `platform` | la course est restée **à bord plus d'un jour** : le téléphone du chauffeur s'est éteint pour de bon et personne n'a jamais signalé la descente. Le serveur l'a refermée. | un **code** (`stranded_in_transit`) — traduisez-le |
| `operation` | **quelqu'un chez Dira** l'a refermée, à une heure qu'il a choisie — souvent après vous avoir appelé | le **texte** de cette personne — affichez-le **tel quel** |

⚠️ **LE PRIX NE S'ENVOLE PAS, ET C'EST LE POINT.** Une course refermée par la
plateforme est facturée **au prix du devis** : le serveur ferme à la fin du
temps que le devis tolérait, jamais à l'heure où il s'en aperçoit. Autrement
dit, un oubli de seize jours sur une course de 2 km ne facture **pas** seize
jours de compteur. Vous n'avez donc **rien à expliquer ni à contester** à
l'écran — montrez le motif, et le reçu ordinaire.

⚠️ **NE DITES JAMAIS « ANNULÉE ».** La course a eu lieu, elle est `completed`,
et elle a son reçu. « Annulée » enverrait le passager chercher un
remboursement qui n'existe pas.

> 🧪 **Si vous voyez une course qui ne finit jamais** — compteur qui tourne
> des heures à l'écran — ce n'est plus un état normal : signalez-la. Elle sera
> refermée, et son prix restera celui annoncé.

---

### ⏳ La LOCATION — retenir un chauffeur pour une durée

Vous n'achetez pas un trajet, vous achetez **du temps**. Le prix est le
**forfait de la plage**, et il ne bouge pas avec le chemin parcouru.

```
POST /rides/rental/quote  { "class_key": "van", "hours": 5,
                            "pickup": { "label": "…", "geo": [lng, lat] } }
  → { mode: "rental", class_key: "van", fare_xof: 35000, rental_hours: 5,
      rental_included_km: 80, rental_extra_per_km_xof: 150,
      rental_max_radius_km: 60, rental_alert_km_before: 10, expires_at }

POST /rides  { quote_id, payment_method }        # comme un devis ordinaire
```

⚠️ **`class_key` EST OBLIGATOIRE, ET IL SERT DEUX FOIS** : il choisit le
**prix** et il décide **qui sera appelé**. Une journée de van ne se vend pas
au prix d'une journée d'eco — ce n'est ni le même véhicule, ni le même
carburant, ni le même chauffeur qu'on immobilise.

⚠️ **PAS DE DESTINATION À DEMANDER.** Un seul point : le départ. Un écran qui
réclamerait une arrivée empêcherait de commander ce que le passager veut
justement — un chauffeur à disposition.

⚠️ **LA DURÉE DOIT ÊTRE UNE PLAGE VENDUE POUR CE MODE.** « 3 h » entre 1 h et
5 h est refusé (`422`, `fields: ["hours"]`, avec `class_key` dans `meta`) :
interpoler inventerait un prix que personne n'a décidé, et le passager verrait
un montant introuvable dans la grille.

#### ⚠️ Ne proposez que les durées DE LA VOITURE choisie

Les durées à afficher sont **celles de sa classe, et rien d'autre** :

```
GET /classes → items[].modes.rental.tiers
```

```jsonc
// "van" se loue 1 h, 5 h et 24 h ; "eco" se loue 1 h et 5 h — et pas au même prix.
{ "key": "eco", "name": "Eco", "modes": {
    "rental": { "extra_per_km_xof": 150, "tiers": [
      { "hours": 1, "price_xof": 5000,  "included_km": 20 },
      { "hours": 5, "price_xof": 20000, "included_km": 80 } ] } } }

{ "key": "van", "name": "Van", "modes": {
    "rental": { "extra_per_km_xof": 250, "tiers": [
      { "hours": 1,  "price_xof": 12000,  "included_km": 20 },
      { "hours": 5,  "price_xof": 35000,  "included_km": 80 },
      { "hours": 24, "price_xof": 200000, "included_km": 300 } ] } } }
```

⚠️ **IL N'Y A PLUS DE GRILLE COMMUNE À RECOUPER** (v4.31.0). Jusqu'à la
v4.30.0 les plages vivaient dans les réglages du pays, une ligne pouvait ne
nommer aucune classe, et il fallait faire l'emboîtement soi-même — le mode
exact l'emportant sur la ligne commune. Une seule application qui inversait
l'ordre affichait un van au prix d'une eco. **Chaque voiture porte maintenant
ses propres plages, entières.** Prenez-les telles quelles.

⚠️ **MONTRER LA GRILLE D'UNE AUTRE VOITURE EST UNE ERREUR.** Un passager qui
choisit « 24 h » sur une eco qui ne se loue pas à la journée se verrait refuser
après coup — c'est pire qu'une durée qu'on ne lui a jamais proposée.

⚠️ **NI `modes.rental` NI DE `tiers` = CETTE VOITURE NE SE LOUE PAS.** Retirez-la
de l'écran de location ; elle reste commandable normalement.

#### Dire les garde-fous AVANT de réserver

**Les trois chiffres du devis sont à montrer sur l'écran de confirmation**,
pas enfouis dans des conditions générales :

| Champ | Ce que vous dites |
|---|---|
| `rental_included_km` | « 80 km compris » |
| `rental_extra_per_km_xof` | « puis 150 F le km » — `0` : le dépassement est offert, ne dites rien |
| `rental_max_radius_km` | « à 60 km de votre départ au plus » |

⚠️ **« CINQ HEURES » NE VEUT PAS DIRE « CINQ HEURES DE ROUTE ».** Personne ne
roule cinq heures d'affilée en ville, et le forfait n'est pas calculé pour ça.
Un passager qui découvre ces bornes à la facture n'a pas acheté ce qu'on lui
avait promis — et c'est la réclamation la plus coûteuse qui existe.

#### Pendant la location

```
PUT /rides/{id}/rental-stops  { "stops": [ { "label": "…", "geo": [lng, lat] }, … ] }
```

**Donnez les destinations au fur et à mesure.** La liste ENTIÈRE des arrêts
après le départ, dans l'ordre ; le départ ne se change pas.

⚠️ **CE N'EST PAS `PATCH /rides/{id}/stops`, et surtout ne l'utilisez pas
ici.** L'autre route **recalcule le prix** et fait bouger l'argent. Sur une
location, le prix ne change **jamais** avec le trajet : c'est la durée qui est
vendue.

⚠️ **LE CHAUFFEUR PEUT AUSSI APPELER CETTE ROUTE** — pour noter ce qu'on lui a
dit de vive voix. Vos arrêts peuvent donc changer sans que vous les ayez
envoyés : **relisez la course** sur une trame `status` comme d'habitude, et
n'écrasez pas la liste sans l'avoir relue.

**`422 rental_out_of_range`** : l'arrêt demandé sort du rayon. ⚠️ **Sa phrase
porte les deux chiffres** — « Cet arrêt est à 190 km du départ : votre location
va jusqu'à 60 km » — et c'est elle qu'il faut afficher : un « trop loin » sans
chiffres laisse deviner de combien.

⚠️ **Corrigé en v4.42.0** : ce paragraphe annonçait un `meta` portant
`distance_km` et `max_km`. **Il n'arrive pas** — l'enveloppe ne rend que `code`,
`message`, `fields` et `reason` (§8), et c'est la règle depuis le début. Les
nombres n'ont jamais voyagé qu'à l'intérieur de la phrase traduite.

#### Le temps qui reste

⚠️ **`rental_ends_at` COURT À LA MONTÉE À BORD**, pas à la commande : le temps
que le chauffeur met à venir n'est pas du temps loué. Le champ est **absent**
tant que vous n'êtes pas monté — n'affichez pas de compte à rebours avant.

C'est un **instant** : décomptez-le localement, sans réinterroger.

#### À la fin

`rental_overage_km` dit les kilomètres facturés au-delà des compris, et le
supplément apparaît dans `fare_adjustments` avec le motif `rental_overage`.
⚠️ **Le kilomètre commencé est dû** — c'était écrit dans les conditions.
Aucun supplément pour un **retard** : la durée est ce qu'on a vendu.

---

### 🤝 La course PARTAGÉE — deux passagers, une voiture (v4.42.0)

Deux personnes dont les **départs** et les **arrivées** sont proches montent
dans la même voiture et paient **chacune son trajet**, moins cher. Le chauffeur
sert deux courses pour un seul déplacement.

⚠️ **ET LE MODE CHERCHE LE CO-PASSAGER *AVANT* LE CHAUFFEUR.** C'est ce qui le
distingue de tous les autres modes, et ce qui change l'écran d'attente : lisez
la section suivante avant de câbler quoi que ce soit.

> ⚠️ **Ce n'est PAS le « partage de trajet » de l'écran sécurité** (envoyer sa
> position à un proche), qui n'existe toujours pas. Ici, on partage la
> **voiture** avec un inconnu.

```
POST /rides/pool/quote  { "stops": [ { kind: "pickup", … }, { kind: "dest", … } ] }
  → { items: [ { id, class_key, mode: "pool", fare_xof, distance_m, duration_s,
                 country, expires_at }, … ] }

POST /rides  { quote_id, payment_method }        # comme un devis ordinaire
```

#### ⚠️ LE PRINCIPE : ON CHERCHE LE CO-PASSAGER *AVANT* LE CHAUFFEUR

**C'est la seule chose à retenir, et tout le reste en découle.** Une course
partagée ne cherche pas une voiture tout de suite : elle cherche d'abord
**quelqu'un qui fait le même trajet**. Le groupe formé, et seulement alors, on
appelle **un** chauffeur pour les deux.

```
1. RECHERCHE DU CO-PASSAGER          2. RECHERCHE DU CHAUFFEUR
   dispatch_state: "pooling"            dispatch_state: "calling"
   jusqu'à 5 min (pool_until)           UN seul appel, pour le groupe
   ⚠️ AUCUN chauffeur n'est appelé      sur un cercle couvrant les 2 départs
            │                                      │
            └──── le groupe est FIGÉ ──────────────┘
                  (pool_id, pool_size: 2)
```

⚠️ **PENDANT L'ÉTAPE 1, PERSONNE N'EST APPELÉ. Ne dites pas « nous cherchons un
chauffeur »** — ce serait faux, et le passager s'étonnerait que ça dure cinq
minutes. Dites **« nous cherchons quelqu'un qui fait le même trajet »**, et
décomptez `pool_until`.

⚠️ **POURQUOI DANS CET ORDRE, ET PAS L'INVERSE.** Appeler un chauffeur dès le
premier passager aurait semblé plus rapide, et ne tenait pas : un chauffeur
accepte en vingt secondes. Il aurait fallu, ensuite, soit **lui imposer un arrêt
qu'il n'a pas accepté**, soit **faire partir le premier passager seul au prix du
partage**. Le groupe est donc figé **avant** que le téléphone d'un chauffeur ne
sonne : ce qu'il accepte est ce qu'il roulera.

⚠️ **CONSÉQUENCE DIRECTE : ON NE REJOINT PLUS UN GROUPE APRÈS L'ACCEPTATION.**
Un troisième passager qui arriverait après coup ne sera **pas** ajouté — ni à
votre course, ni à la voiture.

**Les mêmes deux temps, champ par champ :**

```
POST /rides
   │
   ├─▶ dispatch_state: "pooling"      ← 1. on cherche un CO-PASSAGER
   │     pool_until: <instant>           AUCUN chauffeur n'est appelé
   │
   ├─▶ dispatch_state: "calling"      ← 2. le groupe est formé, on appelle
   │     pool_id, pool_size: 2           UNE voiture pour les deux
   │
   └─▶ status: "accepted"             ← un chauffeur a pris le groupe
```

⚠️ **`pool_until` EST UN INSTANT, pas une durée** — comme `eta_at`. Décomptez-le
localement, sans réinterroger : une durée servie il y a trois minutes en vaut
une de moins.

**Quand le groupe se forme**, le passager qui attendait reçoit le push
**`ride_pool_matched`** (`data.type: "ride_status"`, `dispatch_state: "calling"`,
`pool_id`) — relisez la course. Celui qui vient de commander, lui, voit déjà
`calling` dans la réponse de `POST /rides` : il n'y a pas de push pour lui.

#### Le marché, et les règles qui en découlent

**Trois personnes y gagnent quelque chose, et chacune y renonce à quelque
chose.** C'est le marché, et tout le reste en découle :

| | Ce qu'elle gagne | Ce qu'elle accepte |
|---|---|---|
| **Le passager** | un prix plus bas (70 % du tarif, par défaut) | attendre qu'on trouve quelqu'un, un détour, un inconnu à bord |
| **Le chauffeur** | deux courses pour un seul déplacement | un arrêt de plus, et un trajet qui n'est pas le plus direct |
| **La plateforme** | des sièges vides remplis | apparier, et une commission par course un peu plus haute |

⚠️ **SI L'UN DES TROIS N'Y GAGNE RIEN, LE MODE NE PREND PAS.** C'est pour cela
qu'il y a un **trajet minimum** (sur deux kilomètres, le détour coûte plus que
la remise), une **remise réelle** (sans elle, personne n'accepterait l'inconnu),
et une **limite de détour** (les deux rayons). Ces trois réglages ne sont pas
des garde-fous techniques : ce sont les termes du marché.

##### 1. Ce qui décide que deux trajets peuvent se partager

Le serveur compare votre course aux courses **qui attendent déjà** un
co-passager. Cinq conditions, **toutes** nécessaires :

| Condition | Pourquoi |
|---|---|
| **même classe de véhicule** | le prix, les places et ce que chacun a choisi en dépendent : une eco et un van ne se partagent pas |
| **même pays** | réglages, monnaie et villes desservies diffèrent |
| **départs proches** — ≤ `pickup_radius_m` | c'est le détour que le second impose au premier **avant** de rouler |
| **arrivées proches** — ≤ `dropoff_radius_m` | ⚠️ **les deux, pas l'une des deux** : deux personnes peuvent partir du même immeuble et aller à l'opposé de la ville |
| **le groupe n'est pas complet** — < `max_riders` | deux par défaut |

Entre deux candidates valables, le serveur garde **la plus ancienne** : elle
attend depuis plus longtemps, et sa fenêtre se referme en premier.

```
Lomé — rayons à 2,5 km, trajet minimum 3 km

  Ana  : Tokoin ─────────────▶ Aéroport        ⏳ attend depuis 2 min
  Boris: Nukafu ─────────────▶ Aéroport        ✅ départs à 1,0 km, même arrivée
  Carl : Agoé   ─────────────▶ Aéroport        ❌ départ à 5,4 km de Tokoin
  Dina : Tokoin ─────────────▶ Bè Plage        ❌ même départ, arrivée à 4 km
```

⚠️ **CE N'EST PAS VOTRE TRAVAIL DE CALCULER ÇA**, et il ne faut pas essayer : le
serveur ne vous donne jamais les courses qui attendent (ce serait donner
l'adresse d'inconnus). Vous proposez le mode, vous commandez, vous affichez ce
qui revient. Les rayons sont servis dans les réglages **pour que vous puissiez
l'expliquer**, pas pour que vous l'appliquiez.

##### 2. La chronologie, les deux passagers côte à côte

Ana commande à 9 h 00, Boris à 9 h 02.

| | Ana (première) | Boris (second) |
|---|---|---|
| 9 h 00 | `POST /rides` → `pooling`, `pool_until` = 9 h 05. Écran : « on cherche quelqu'un qui fait le même trajet », décompte 5 min | — |
| 9 h 02 | push **`ride_pool_matched`** → relire la course : `calling`, `pool_id`, `pool_size: 2` | `POST /rides` répond **déjà** `calling` + `pool_id`. ⚠️ **Pas de push pour lui** : il a la réponse sous les yeux |
| 9 h 02 | « nous cherchons une voiture pour vous deux » | idem |
| 9 h 03 | `accepted` — push `ride_accepted`, la carte du chauffeur | `accepted` aussi, **même chauffeur, même plaque** |
| 9 h 03+ | la voiture vient chercher Ana | ⚠️ la voiture vient chercher **Ana d'abord** |
| 9 h 09 | Ana à bord (`in_transit`) | la voiture arrive ensuite chez Boris |
| 9 h 14 | — | Boris à bord |
| 9 h 30 | Ana déposée → `completed`, note, reçu | course encore `in_transit` |
| 9 h 36 | — | Boris déposé → `completed` |

⚠️ **LES DEUX COURSES AVANCENT SÉPARÉMENT.** Chacune a son `status`, son prix,
sa note, son reçu, sa conversation. Il n'existe **aucun** objet « course de
groupe » à suivre : vous suivez **votre** course, comme toujours.

##### 3. Pourquoi la voiture semble venir d'ailleurs

L'appel du chauffeur n'est **pas** centré sur votre départ : il est centré au
**milieu** des deux départs, sur un cercle élargi de leur demi-distance.

```
     Tokoin ●───────── 1 000 m ─────────● Nukafu
                       ▲
                   le centre de l'appel (ni l'un ni l'autre)
     rayon = rayon d'appel du pays + 500 m
```

⚠️ **APPELER SUR LE DÉPART DE L'UN DES DEUX SERAIT FAUX** : le chauffeur le plus
proche d'Ana peut être hors de portée de Boris, et l'on appellerait des voitures
qui n'ont aucune raison d'être proches du trajet réel. Conséquence pour vous :
**le chauffeur peut arriver d'une direction inattendue**, et son `eta_at` peut
être plus long que sur une course ordinaire. N'expliquez pas, **prévenez** : « il
récupère d'abord l'autre passager » suffit.

##### 4. L'ordre de route : les deux montées, PUIS les deux descentes

⚠️ **LE CHAUFFEUR NE DÉPOSE PERSONNE AVANT D'AVOIR PRIS LES DEUX.** Déposer le
premier avant d'aller chercher le second ferait **deux courses à la suite**, pas
une course partagée — et le second attendrait tout le trajet du premier.

Pour vous, cela veut dire deux choses :

- **si vous montez en premier**, la voiture fait un arrêt **avant** de partir
  vers votre destination. Dites-le **au moment de commander**, pas quand ça
  arrive ;
- **si vous montez en second**, la voiture arrive chez vous avec quelqu'un
  dedans.

⚠️ **ET `eta_at` EST OPTIMISTE POUR LE SECOND PASSAGER — sachez-le.** Il est
calculé en ligne directe depuis la position du chauffeur vers **votre** arrêt :
il **ne passe pas** par la prise en charge de l'autre. Tant que le chauffeur n'a
pas récupéré le premier passager, le chiffre annoncé est donc **plus court** que
la réalité.

Ce qu'on vous demande d'en faire, et c'est la seule chose honnête : **ne
l'affichez pas comme une promesse sur une course partagée tant que `pool_size`
> 1 et que vous n'êtes pas encore à bord.** Dites « il récupère d'abord l'autre
passager » plutôt qu'un décompte qui va glisser. Dès que vous êtes `in_transit`,
`eta_at` redevient exact : il n'y a plus d'arrêt intercalé.

⚠️ **VOUS NE RECEVEZ NI LE NOM NI L'ADRESSE DE L'AUTRE PASSAGER**, et c'est
délibéré : on ne donne pas l'adresse de quelqu'un à un inconnu avant qu'il ne
monte en voiture. Vous savez seulement que vous partagez (`pool_size`). Le
chauffeur, lui, a l'itinéraire complet : c'est lui qui conduit.

##### 5. Les quatre façons dont ça se termine

```
pooling ──┬──▶ personne n'est venu          → exhausted · pool_no_match
          └──▶ groupe formé → calling ──┬──▶ un chauffeur prend  → accepted ✅
                                        ├──▶ aucun preneur       → exhausted
                                        ├──▶ le co-passager annule→ exhausted · pool_partner_left
                                        └──▶ l'appel n'a pas pu s'ouvrir
                                                                 → exhausted · pool_call_failed
```

⚠️ **DANS LES QUATRE CAS D'ÉCHEC, LA COURSE RESTE LA VÔTRE** : `searching`, non
annulée, mode et prix inchangés. C'est à vous de proposer la suite — et
`dispatch_reason` dit laquelle (voir plus bas).

##### 6. Ce que le passager va demander

Préparez ces réponses : ce sont celles que le support reçoit.

| Il demande | Répondez |
|---|---|
| « pourquoi ça prend si longtemps ? » | on cherche quelqu'un qui fait le même trajet — jusqu'à `search_ttl_s` ; après, vous décidez |
| « où est mon chauffeur, il va dans l'autre sens » | il récupère l'autre passager d'abord |
| « ça disait 4 min et ça fait 10 » | ⚠️ sur une course partagée, avant d'être à bord, le décompte ne compte pas l'autre prise en charge — **ne le montrez pas** |
| « qui est cette personne ? » | un autre client Dira, qui va au même endroit. Ni son nom ni son adresse ne vous sont donnés |
| « pourquoi je paie moins ? » | `100 − share_pct` % de moins, parce que vous partagez la voiture |
| « l'autre a annulé, je paie plus ? » | **non** — le prix est figé, et l'annulation n'est pas la vôtre |
| « je peux partager avec un ami ? » | **non** : c'est le serveur qui apparie, sur la proximité des trajets |

#### Le prix : chacun sa distance, remisée d'une PART (v4.44.0)

Le devis et la course portent **`share_pct`** : la **part du tarif ordinaire**
que ce passager paie.

```jsonc
// `fare_xof: 2000` au lieu de 2 850 — et on vous dit pourquoi
{ "mode": "pool", "fare_xof": 2000, "share_pct": 70, … }
```

⚠️ **`share_pct: 70` VEUT DIRE « IL PAIE 70 % », donc 30 % de moins.** C'est
une **part**, pas une remise : le nombre servi est celui qu'on paie. Pour
afficher l'économie, montrez `100 − share_pct`.

⚠️ **AFFICHEZ-LA.** « 2 000 F » à côté de « 2 850 F » ne se comprend pas tout
seul : c'est « −30 % » qui donne une raison d'accepter un détour et un inconnu
à bord. Et sur le **reçu**, des mois plus tard, c'est la seule chose qui
explique un montant inférieur au tarif — sans elle, le client lit une erreur de
facturation.

⚠️ **IL N'Y A PLUS DE GRILLE DE PARTAGE** (v4.44.0). Jusqu'à la v4.43.0 chaque
voiture avait la sienne, servie dans `modes.pool`. Elle ne facture plus et
n'est plus servie : le prix est **une part de la grille ordinaire**, réglée par
**pays** (`settings.modes.pool.fare_pct`). Une application qui lirait encore
`modes.pool.base_xof` n'y trouverait **rien**.

⚠️ **IL N'EXISTE AUCUN PRIX COMMUN.** Chaque passager paie **son** trajet,
remisé. Les deux ne font pas le même chemin — l'un roule peut-être deux fois
plus longtemps que l'autre. **N'affichez pas un montant « à deux », ne divisez
rien** : `fare_xof` est ce que *cette* personne paie.

⚠️ **LE PRIX EST FIGÉ AU DEVIS, ET IL NE BOUGE PLUS.** Trouver un co-passager
ne le baisse pas ; ne pas en trouver ne le remonte pas ; le co-passager qui
annule en route ne le change pas non plus. ⚠️ Et un **pays** qui change sa part
à midi ne change pas un devis déjà affiché. C'est ce qui a été vendu.

⚠️ **PAS DE MAJORATION DE ZONE, PAS DE PROMOTION** sur un devis partagé : la
remise **est** la promotion de ce mode. `surge_name`, `promo_title` sont
absents — ne réservez pas de place pour eux sur cet écran.

#### Quand ça ne marche pas — `dispatch_reason` décide de ce que vous proposez

La recherche s'arrête, la course **reste** `searching`, et **rien n'est changé
à sa place** : son mode et son prix sont ceux qu'elle a achetés.

| `dispatch_reason` | Ce qui s'est passé | Push | Ce que vous proposez |
|---|---|---|---|
| `pool_no_match` | personne ne partageait ce trajet | `ride_pool_no_match` | **Relancer** · **Prendre une course ordinaire** · Annuler |
| `pool_partner_left` | le co-passager a annulé | `ride_pool_partner_left` | **Relancer** · Annuler — dites que le prix ne change pas |
| `pool_call_failed` | le groupe était formé, l'appel n'a pas pu s'ouvrir | `ride_search_exhausted` | **Relancer** · Annuler |
| *(absent)* | aucun chauffeur n'a pris le groupe | `ride_search_exhausted` | **Relancer** · Annuler |

⚠️ **« PRENDRE UNE COURSE ORDINAIRE » EST UN AUTRE DEVIS, ET UN AUTRE PRIX.**
Annulez la course partagée (`POST /rides/{id}/cancel` — remboursée, personne ne
l'a prise), puis repassez par `POST /rides/quote` + `POST /rides`. **Il n'existe
pas de route qui change le mode d'une course** : elle ferait payer un tarif que
le passager n'a pas vu.

⚠️ **NE BASCULEZ JAMAIS D'OFFICE.** Au bout de cinq minutes, l'écran demande —
il ne décide pas. C'est la règle de tout ce document : le passager a payé, c'est
à lui de choisir.

```
POST /rides/{id}/relaunch
```

**La relance reprend là où ça s'est arrêté**, et vous n'avez rien à distinguer :
sans groupe, elle recherche un co-passager (`pooling`) ; avec un groupe intact,
elle rappelle une voiture (`calling`). La réponse dit lequel.

| Refus | Quand |
|---|---|
| `409 search_running` | `dispatch_state` vaut `pooling` **ou** `calling` — masquez « Relancer » dans les deux cas. ⚠️ Sinon un passager impatient remet sa fenêtre à zéro toutes les dix secondes et n'atteint jamais les cinq minutes, donc jamais un groupe |
| `409 pool_off` | le pays a fermé le partage entre-temps — proposez d'annuler et de commander une course ordinaire |

#### Ce que le devis refuse, et pourquoi le dire AVANT

| Refus | Ce que vous affichez |
|---|---|
| `409 pool_off` | ne montrez pas le mode du tout (lisez `settings.pool.enabled`) |
| `409 pool_too_short` | **la phrase du serveur, telle quelle** : « Ce trajet fait 2 100 m : la course partagée commence à 3 000 m » |
| `409 pool_direct_only` | « une course partagée va d'un point à un autre » — retirez les arrêts intermédiaires |
| `409 pool_no_class` | aucune voiture ne le propose ici pour le moment |

⚠️ **LES CHIFFRES SONT DANS LA PHRASE, PAS DANS UN `meta`.** L'enveloppe
d'erreur ne rend que `code`, `message`, `fields` et `reason` — c'est la règle de
toute la plateforme (§8), et `pool_too_short` ne fait pas exception : son
`message` est déjà traduit **et déjà chiffré**. Si vous voulez votre propre
mise en forme, **prenez les deux nombres de `settings.pool.min_distance_m` et du
`distance_m` du devis ordinaire** — ne tentez pas de lire un `meta` qui
n'arrive pas.

⚠️ **PROPOSEZ LE MODE SEULEMENT QUAND IL EST POSSIBLE.** `settings.pool` vous
donne tout pour le savoir avant d'appeler : `enabled`, et `min_distance_m` à
comparer au `distance_m` du devis ordinaire. Un bouton qu'on refuse est pire
qu'un bouton absent.

⚠️ **N'ÉCRIVEZ JAMAIS LE MINIMUM EN DUR.** Il valait 5 km jusqu'à la v4.44.0,
3 km depuis la v4.45.0, et **un pays peut régler autre chose** : « à partir de
3 km » codé dans l'application mentirait chez le voisin. Lisez
`min_distance_m`, et composez la phrase avec lui.

⚠️ **PAS D'ARRÊT INTERMÉDIAIRE.** Le partage n'accepte que **deux** étapes, un
départ et une arrivée : l'itinéraire du groupe en contient déjà quatre, et une
escale la ferait disparaître sans rien dire. `PATCH /rides/{id}/stops` reste
possible **après** l'acceptation, comme sur une course ordinaire.

#### Ce que vous montrez d'une course partagée

| Champ | Quand | Ce que vous en faites |
|---|---|---|
| `mode: "pool"` | toujours | un badge « partagée » — un passager doit savoir qu'un inconnu montera |
| `pool_until` | pendant `pooling` | le décompte de la recherche de co-passager |
| `pool_id` | dès le groupe formé | rien à afficher ; utile au support |
| `pool_size` | dès le groupe formé | « vous partagez avec 1 personne » |
| `share_pct` | toujours | « −30 % » (`100 − share_pct`) — au devis **et** sur le reçu |

⚠️ **VOUS NE RECEVEZ NI LE NOM NI L'ADRESSE DE L'AUTRE PASSAGER, et c'est
délibéré.** On ne donne pas l'adresse de quelqu'un à un inconnu avant qu'il ne
monte en voiture. Le chauffeur, lui, a l'itinéraire complet : c'est lui qui
conduit. Sur la carte, vous verrez donc la voiture faire un détour que vous ne
pouvez pas expliquer point par point — **dites-le d'avance** : « le chauffeur
prend d'abord l'autre passager ».

⚠️ **LE CHAUFFEUR PREND LES DEUX PASSAGERS AVANT D'EN DÉPOSER UN.** Si vous
montez en second, la voiture arrive chez vous après un premier arrêt ; si vous
montez en premier, vous attendez une prise en charge de plus. `eta_at` en tient
compte — **servez-le tel quel**, ne recalculez pas.

⚠️ **VOTRE COURSE RESTE LA VÔTRE.** Statut, prix, reçu, note, pourboire,
conversation : tout fonctionne exactement comme sur une course ordinaire. Le
partage est un **lien**, pas une fusion — ne fusionnez rien à l'écran.

---

## 5. La course, statut par statut

```
searching → accepted → picking_up → in_transit → completed
         ↘ cancelled (par le passager, le chauffeur, ou l'échec de la recherche)
```

> **v4.0.0 — le vocabulaire commun.** Ce sont les **mêmes mots** que pour une
> course de livraison et que la fin d'une commande de repas : une application
> qui suit les deux n'a qu'un seul écran d'état à écrire.

```
searching → accepted → picking_up → [arrived] → in_transit → completed
                                                       ↘ cancelled   (tout état avant completed)
```

| Statut | Ce que ça veut dire | Course de livraison | Course VTC |
|---|---|---|---|
| `searching` | on cherche quelqu'un | la course attend un livreur — proposée dès que le repas est **prêt** | on appelle des chauffeurs |
| `accepted` | quelqu'un a pris l'opération | un livreur l'a acceptée, il part vers le restaurant | un chauffeur l'a prise |
| `picking_up` | il est au point de départ | la **première collecte** est faite, il en reste | il **roule vers le passager** |
| `arrived` | il est arrivé et attend (v4.14.0) | — (une course de livraison passe directement à `in_transit`) | il est **au point de départ**, le passager n'est pas encore monté ; l'attente offerte court, puis se facture à la minute |
| `in_transit` | le colis / le passager est à bord | toutes les collectes faites, en route vers le client | le passager est monté |
| `completed` | livré / déposé | remise au client | passager déposé |
| `cancelled` | fini sans être fait | commande annulée (client, marchand, exploitation) | par le passager, le chauffeur ou la plateforme |

La **commande de repas** garde ce qui n'existe que pour un repas —
`pending_payment → paid → preparing → ready` — puis reprend **mot pour mot** le
cycle de sa course : `accepted → picking_up → in_transit → completed`.
`ready` côté commande = `searching` côté course.

Ce qui a été **renommé** en v4.0.0 (les anciens mots n'existent plus dans
aucune réponse, et sont refusés en entrée) :

| Avant | Après | Où |
|---|---|---|
| `available` | `searching` | course de livraison |
| `assigned` | `accepted` | course de livraison, commande |
| `delivering` | `in_transit` | course de livraison, commande |
| `delivered` | `completed` | course de livraison, commande |
| `approach` | `picking_up` | course VTC |
| `onboard` | `in_transit` | course VTC |

| Statut | Ce que voit le passager |
|---|---|
| `searching` | « nous cherchons un chauffeur » — `driver_id` est vide ; `dispatch_state` dit si l'on appelle encore (v4.1.0, ci-dessous) |
| `accepted` | un chauffeur a pris la course — `driver` dit qui (v3.8.0) |
| `picking_up` | il roule vers le point de départ — `eta_at` dit quand il y sera (v4.28.0) |
| `arrived` | il est là et attend (v4.14.0) — `arrived_at`, et le compteur d'attente : `waiting_free_min` minutes offertes, puis `waiting_per_min_xof` la minute |
| `in_transit` | le passager est à bord — `waiting_minutes` / `waiting_fee_xof` disent ce que l'attente a coûté, déjà compris dans `fare_xof` ; `eta_at` décompte le **prochain arrêt** (v4.28.0) |
| `completed` | terminée |
| `cancelled` | `cancelled_by` dit qui, `cancelled_reason` pourquoi |

```
GET /rides/{id}
GET /rides?cursor=…     # l'historique de VOS courses, page par page
GET /rides?before=2026-10-05T00:00:00Z   # bornée à ce qui est né AVANT (v4.43.0)
```

⏳ **`before` BORNE L'HISTORIQUE À UNE FENÊTRE DE DATE (v4.43.0)** — un instant
RFC 3339, **strictement** avant. Il se combine avec `cursor` sans se gêner :
l'un borne la fenêtre, l'autre dit où l'on en était dedans.

⚠️ **UNE DATE ILLISIBLE EST REFUSÉE** (`422`, `fields: ["before"]`), pas
ignorée : l'ignorer servirait silencieusement l'historique entier là où vous
demandiez une tranche, et une application qui pagine ne s'en apercevrait jamais.

Il existe pour **l'application cliente unifiée** (`DIRA-CLIENT.md`), qui mêle
les courses et les commandes semaine par semaine : sans borne de date, elle
repartait du haut à chaque page. Vous n'en avez pas besoin pour un historique
ordinaire — `cursor` suffit.

L'historique rend **les plus récentes d'abord** — par date de création, puis identifiant (v4.12.1) ; `?cursor=` est l'identifiant de la dernière course reçue et rend la page suivante, plus ancienne.

### ⏱️ DANS COMBIEN DE TEMPS — le décompte du PROCHAIN arrêt (v4.28.0)

```jsonc
GET /rides/{id} → {
  "status": "picking_up",
  "eta_stop_index": 0,                    // de quel arrêt on parle
  "eta_at": "2026-09-26T08:41:30Z"        // quand il y sera
}
```

⚠️ **`eta_at` EST UN INSTANT, à décompter localement.** Pas une durée à
réinterroger : une durée vieillit dans le tuyau et dans l'écran — « 4 min »
servi il y a trois minutes en vaut une. Faites tourner votre compteur sur
`eta_at`, et ne rappelez l'API que sur un signal (trame `status`, push, ou
votre rafraîchissement ordinaire).

⚠️ **CE QUI MANQUAIT.** La seule durée servie était `duration_s`, celle du
devis : le trajet entier, du départ à la destination. Vous ne pouviez donc
décompter que l'arrivée FINALE. Pendant l'approche, le passager qui attend
sur le trottoir n'avait **aucun chiffre** ; et sur un trajet à plusieurs
arrêts, le décompte sautait les étapes intermédiaires comme si elles
n'existaient pas.

**`eta_stop_index` dit DE QUEL arrêt il s'agit** — et il change en cours de
route :

| État de la course | `eta_stop_index` | Ce que vous affichez |
|---|---|---|
| `accepted`, `picking_up` | **`0`** — le départ | « il arrive dans 4 min » |
| `arrived` | *absent* | rien : il est là. C'est le compteur d'**attente** qui prend le relais |
| `in_transit` | l'arrêt **suivant** celui atteint (`stop_index + 1`) | « prochain arrêt dans 7 min » |
| dernier arrêt atteint, `completed`, `cancelled`, `searching` | *absent* | rien |

⚠️ **ABSENTS, LES DEUX CHAMPS VEULENT DIRE « ON NE SAIT PAS »** — chauffeur
silencieux, position trop vieille (plus de 2 min), moteur d'itinéraire muet,
course à l'arrêt. **N'affichez alors aucun chiffre, et surtout pas le dernier
connu** : un décompte qui continue de tourner sur une estimation morte
afficherait « 2 min » pendant que le chauffeur est immobile depuis un quart
d'heure. Effacez la ligne, ou dites « position en attente ».

⚠️ **Ne recalculez pas d'estimation vous-même** à partir de la position du
socket et d'une distance à vol d'oiseau. Elle serait systématiquement
optimiste — et le passager la comparerait à la nôtre.

### ⚠️ Le chauffeur est là — `arrived` et l'attente facturée (v4.14.0)

Quand le chauffeur signale son arrivée au point de départ, la course passe
à **`arrived`** : vous recevez le push **`ride_driver_arrived`** (« [driver]
vous attend · [vehicle]. [free] min d'attente offertes, puis [rate]/min ») et
la trame `status: arrived` sur le socket. **Affichez-le fort, avec un
compteur** depuis `arrived_at` : les minutes offertes (`waiting_free_min`,
5 par défaut) en vert, puis le montant qui monte — `waiting_per_min_xof`
par minute **entamée**. Les deux nombres sont sur la course dès le devis,
figés : ce que le passager a vu en commandant est ce qu'il paie.

À la montée à bord (`in_transit`), la course porte `waiting_minutes` et
`waiting_fee_xof`, **déjà ajoutés à `fare_xof`**, et une ligne
`fare_adjustments` (`reason: "waiting"`, `delta_xof`, `movement`). Le
push `ride_fare_adjusted` dit ce qui a bougé : débité du solde Dira
(`charged`), porté à la dette si le solde ne suivait pas (`owed` —
`debt_xof` sur `GET /wallet`, remboursée d'office à la prochaine recharge),
à régler au chauffeur en espèces (`cash`), ou compris dans le débit à
venir quand le pays débite à la montée ou à l'arrivée (`deferred`). Aucune
attente facturée = aucun de ces champs.

Une course `arrived` s'annule encore (`POST /rides/{id}/cancel`) ; le
chauffeur est là, dites-le avant de confirmer.

#### 🔔 Il klaxonne — `ride_driver_honked` (v4.24.0)

Le chauffeur est sur place et ne vous voit pas : il appuie sur un bouton, et
vous recevez **`ride_driver_honked`** — « [driver] est devant, dans
[vehicle]. Il vous cherche. »

```
data : { "type": "ride_status", "ride_id", "status": "arrived",
         "event": "honk", "honk_count": "1" }
```

⚠️ **Ce n'est pas `ride_driver_arrived` à nouveau.** L'arrivée est une
**information** ; le klaxon est un **appel** : il arrive plus tard, il veut
dire « maintenant », et il doit sonner comme tel. Donnez-lui le son et la
vibration que vous réservez aux choses urgentes — sinon il se noie dans un
fil que le passager ne regarde pas, puisqu'il croyait avoir encore cinq
minutes.

Le passager n'a rien à répondre : le bon écran est celui de la course, avec
le nom et la plaque **en grand**. C'est ce qu'il cherche des yeux en sortant.

Le chauffeur ne peut klaxonner **qu'une fois arrivé**, et son application
grise le bouton entre deux coups. ⚠️ **Les deux délais se règlent par pays
depuis la v4.72.0** — le temps qu'on lui demande d'attendre après son arrivée
avant le premier coup, et l'intervalle entre les suivants. Vous n'avez rien à
régler : sachez seulement que la cadence que vous observez n'est pas la même
d'un pays à l'autre, et qu'elle n'est pas celle de son doigt. `honk_count` reste sur la
course : un passager qui se plaint d'avoir été harcelé doit pouvoir être cru.

#### 📍 Ce que le chauffeur a roulé pour venir (v4.24.0)

Quand il signale son arrivée, la plateforme fige son **approche** sur la
course : `approach_duration_s` (depuis l'**acceptation** — depuis le moment
où vous avez commencé à attendre), `approach_distance_m`,
`approach_polyline` (son parcours réel) et `approach_source`.

De quoi dire « il a mis 14 min » sur l'écran de fin, et de quoi trancher une
réclamation sur autre chose qu'une impression.

⚠️ **`actual_distance_m` ne compte plus ces kilomètres-là** : la distance de
la course commence là où vous montez. Une course de 8 km après 6 km
d'approche affichait 14 km avant la v4.24.0.

### ⚠️ Le temps réel — 5 km en une heure ne coûtent pas 5 km en dix minutes (v4.14.0)

Le devis tarife la **durée prévue** (`duration_s`, celle du réseau routier).
Quand le mode le règle (`bill_actual_time` sur la course), la durée
**réellement roulée** — de la montée à bord (`in_transit_at`) à l'arrivée —
compte aussi : chaque minute **entamée** au-delà de la durée prévue plus
`time_tolerance_min` coûte `per_min_xof`. À `completed`, la course porte
`actual_duration_s`, `extra_minutes` et `time_fee_xof`, **déjà compris dans
`fare_xof`**, avec une ligne `fare_adjustments` (`reason: "duration"`) et le
même push `ride_fare_adjusted` (`event: duration`) ; les mouvements
d'argent sont ceux de l'attente (`charged` · `owed` · `cash` · `deferred`).

**Pendant la course, affichez la durée prévue et le temps écoulé** depuis
`in_transit_at`, et dès que la tolérance est dépassée, le supplément qui
monte — le passager ne doit pas découvrir le prix à l'arrivée. Sur le reçu,
une ligne « Temps supplémentaire : 12 min · 600 F » sous le prix du devis.
`bill_actual_time` faux = rien de tout cela, la course coûte son devis
(plus l'attente au départ, s'il y en a eu).

### Quand personne ne répond — `dispatch_state` et la relance (v4.1.0)

La plateforme appelle les chauffeurs autour du départ selon un réglage
d'exploitation (tous en même temps dans un rayon, ou un par un du plus
proche) et **s'arrête** au bout d'un nombre de chauffeurs ou de minutes.
Une course `searching` porte alors **`dispatch_state`** :

| `dispatch_state` | Ce que ça veut dire | Ce que montre l'écran |
|---|---|---|
| `calling` | on appelle des chauffeurs | « nous cherchons un chauffeur », animation |
| `pooling` | course **partagée** uniquement (v4.42.0) : on cherche un **co-passager**, aucun chauffeur n'est encore appelé | « nous cherchons quelqu'un qui fait le même trajet » + le décompte de `pool_until` — ⚠️ **pas** « nous cherchons un chauffeur », ce serait faux |
| `exhausted` | la recherche s'est arrêtée **sans preneur** — la course n'est **pas** annulée | « aucun chauffeur disponible » + deux boutons : **Relancer** et **Annuler** |

`dispatch_at` date le dernier passage.

### 🆕 🛰️ `dispatch_call` — CE QUE LE PASSAGER VOIT DE SON APPEL (v4.75.0)

> Demandé par l'équipe mobile (socle #205) **et** par le propriétaire. Jusqu'ici
> l'application dessinait des ondes de **taille fixe** autour de l'épingle du
> passager : le rayon n'existait que côté serveur, et elle n'avait aucun moyen
> de l'inventer.

Tant que `dispatch_state` vaut `calling`, la course porte :

```jsonc
"dispatch_call": {
  "center": [1.2255, 6.1319],
  "radius_m": 3500,
  "attempt": 2,
  "max_attempts": 3,
  "attempt_ends_at": "2026-10-24T12:01:30Z",
  "ringing": [
    { "position": [1.2301, 6.1402], "type": "car",  "distance_m": 420 },
    { "position": [1.2410, 6.1511], "type": "moto", "distance_m": 980 }
  ],
  "target_missed": false
}
```

**Absent hors de `calling`** — il n'y a alors rien à dessiner.

⚠️⚠️ **`radius_m` EST LE RAYON DE LA TENTATIVE EN COURS, DÉJÀ ÉLARGI.** L'appel
commence petit et s'écarte à chaque tentative (`call_radius_step_m`) : dessiner
le rayon de départ au quatrième essai tracerait un cercle **à l'intérieur
duquel plus personne n'est appelé**. Le serveur lit le rayon sur la vague en
cours ; ne le recalculez pas.

⚠️ **`center` EST SERVI, NE LE DÉDUISEZ PAS DU DÉPART.** En course **partagée**,
l'appel part d'un point que la plateforme a retenu pour le groupe, et le
passager **ne connaît pas** le départ de l'autre : il ne peut donc pas le
calculer. Centrez l'onde sur `center`.

⚠️ **`attempt_ends_at` EST UN INSTANT**, à décompter sur place — jamais une
durée. « Il reste 12 s » servi il y a huit secondes en vaut quatre.

#### `ringing` — où sonnent les voitures qu'on appelle pour vous

⚠️⚠️ **CES POINTS NE NOMMENT PERSONNE, ET C'EST DÉFINITIF.** Position, genre de
véhicule, distance — pas d'identifiant, pas de plaque, pas de nom, **pas de
`ref`** (contrairement aux points du radar) : une voiture qu'on sonne n'est plus
libre, donc un jeton d'épingle ne la désignerait pas. Pour en choisir une autre,
c'est le radar ; pour rappeler, `POST /rides/{id}/relaunch`.

⚠️⚠️ **ET IL N'Y A AUCUN COMPTE DE REFUS, NI DE CHAUFFEURS APPELÉS.** Ni
`ringing.length` ni rien d'autre ne doit être présenté comme « 4 chauffeurs
contactés, 2 ont refusé ». Ce chiffre n'apprend au passager qu'une chose —
qu'on ne veut pas de lui — **et c'est faux** : un chauffeur qui ne répond pas
est au volant, pas en train de le juger. Les refusés et les écartés **ne sont
pas** dans la liste.

⚠️ **`ringing` PEUT ÊTRE ABSENT ALORS QUE LE RESTE EST LÀ** : le pays n'a pas
ouvert le radar, ou a fermé le suivi de l'appel (`progress_enabled`). L'onde et
le rayon suffisent alors à dire qu'il se passe quelque chose — **n'inventez pas
de points**.

⚠️ **`target_missed: true`** : la voiture que le passager avait choisie n'était
**plus libre**, et la recherche a continué sans elle. **Dites-le tout de
suite** — « la voiture que vous avez choisie vient d'être prise, nous appelons
les plus proches » —, sinon son écran attend la réponse d'une voiture qui n'a
jamais sonné, puis annonce un autre chauffeur sans explication.

#### ⚠️ LA MISE À JOUR ARRIVE PAR UNE TRAME QUI NE PORTE RIEN

À chaque nouvelle tentative, le serveur pousse une trame de statut sur le
socket de la course — **le statut ne change pas** (`searching`). Elle dit
« quelque chose a bougé, **relis** » : `GET /rides/{id}` rend le nouveau
`dispatch_call`.

⚠️ **UN ÉCRAN QUI IGNORE UNE TRAME DONT LE STATUT N'A PAS CHANGÉ NE VERRA
JAMAIS LE RAYON S'ÉLARGIR.** C'est la règle de toute la plateforme — aucune
trame ne porte la vérité, elles disent de relire —, et c'est ici qu'elle se
voit le plus.

### 🆕 🤝 ON VOUS CHANGE DE CHAUFFEUR — `transfer` (v4.77.0)

> Demandé par le propriétaire : quand le chauffeur ne peut pas continuer
> (panne, malaise, route coupée, fin de service), un autre vient **vous
> reprendre** — et vous devez l'apprendre, **avec la raison**.

La course porte alors :

```jsonc
"transfer": {
  "state": "searching",            // searching · done · failed · cancelled
  "reason": "vehicle_breakdown",
  "by": "driver",                  // driver · support
  "grave": false,                  // accident, malaise, sécurité
  "requested_at": "2026-10-24T14:05:00Z"
}
```

#### ⚠️⚠️ VOTRE CHAUFFEUR NE DISPARAÎT PAS — ET IL PEUT CONTINUER À ROULER

Pendant `state: "searching"`, **rien n'a changé** : la course garde son statut,
son chauffeur, sa carte, et la voiture **avance peut-être encore**. On cherche
quelqu'un **en parallèle**.

**Ce que l'écran doit dire** : « votre chauffeur ne peut pas continuer
(*raison*), nous cherchons quelqu'un pour vous reprendre — restez où vous
êtes ». ⚠️ **Et surtout pas « course annulée »** : elle ne l'est pas, et le
passager qui le croit descend de la voiture.

| `state` | Ce qui s'est passé | Ce que l'écran dit |
|---|---|---|
| `searching` | on cherche un remplaçant | « votre chauffeur ne peut pas continuer : *raison* » — la **carte du chauffeur actuel reste affichée** |
| `done` | quelqu'un a repris | le **nouveau** chauffeur et son véhicule (`driver` est remplacé dans la course) — « il vient à votre rencontre » |
| `failed` | personne n'a repris | ⚠️ **« votre chauffeur continue »** — la bonne nouvelle. Sans cette phrase, l'écran reste sur « on cherche » pendant que la voiture roule vers la destination |
| `cancelled` | le chauffeur ou le support a retiré la demande | même phrase que `failed` |

⚠️ **LES TROIS PUSH** : `ride_transfer_started` (avec le motif),
`ride_transferred` (nom + véhicule du nouveau), `ride_transfer_failed`. Chacun
porte `data.type: "ride_status"` et `data.transfer_state` — **relisez la
course**, c'est elle qui porte la vérité.

⚠️ **`grave: true`** (accident, malaise, sécurité) : **changez de ton**. Un
« changement de chauffeur » annoncé comme une fin de service après un accident
est une faute — et un incident a déjà été ouvert côté support.

⚠️ **LE MOTIF EST UN CODE, TRADUISEZ-LE VOUS-MÊME** si vous voulez votre propre
phrase ; le **push**, lui, arrive déjà traduit. Ne montrez **jamais** le code
brut (`shift_end` ne veut rien dire pour un passager).

⚠️ **VOUS NE PAYEZ PAS PLUS.** Le prix de votre devis ne change pas : le
partage entre les deux chauffeurs est un problème de la plateforme. Si un écran
laisse craindre un supplément, c'est un appel au support garanti.

⚠️ **ET VOUS NE POUVEZ PAS DEMANDER UN TRANSFERT VOUS-MÊME.** Il n'y a pas de
route pour cela : si vous voulez un autre chauffeur, c'est **le support** qui
le décide (son motif est `rider_asked`) — ou vous annulez, avec les règles
d'annulation habituelles.

⚠️ **`dispatch_reason` (v4.42.0) DIT POURQUOI, quand il y a plusieurs façons de
s'arrêter** — et il change ce que vous proposez. Il n'apparaît que sur une
course partagée : `pool_no_match`, `pool_partner_left`, `pool_call_failed`.
Voir §4 quater. Sur une course ordinaire il est **absent** : il n'y a qu'une
façon d'y échouer. Le passager l'apprend par **push**
`ride_search_exhausted` (`data.type: "ride_status"`, `ride_id`,
`status: "searching"`, `dispatch_state: "exhausted"`) — ouvrir la course,
`GET /rides/{id}`, afficher les deux gestes.

```
POST /rides/{id}/relaunch      → 200 Ride, dispatch_state: calling
POST /rides/{id}/cancel        → remboursé sur le solde si la course était payée
```

| Refus | Quand |
|---|---|
| `409 search_running` | un appel court déjà — masquer « Relancer » tant que `dispatch_state` vaut `calling` |
| `409 not_searching` | la course a été prise, terminée ou annulée entre-temps — relire |
| `503 dispatch_unavailable` | la recherche est momentanément indisponible — proposer de réessayer |

> ⚠️ **N'annulez jamais d'office** une course épuisée : le passager a payé et
> attend peut-être encore devant sa porte ; l'exploitation peut aussi lui
> attribuer un chauffeur à la main. Laissez-lui les deux gestes.

#### ⏳ Au bout d'un moment, la plateforme abandonne (v4.25.0)

Quand le pays l'a réglé (`search_expiry_min`, 0 = jamais), une recherche
épuisée depuis trop longtemps est **abandonnée par la plateforme** :

```
status: cancelled · cancelled_by: "system" · cancelled_reason: "search_expired"
```

Le passager est **remboursé** (si la course était payée) et reçoit
`ride_cancelled`. Côté application, c'est une annulation comme une autre —
rien de spécial à coder ; affichez simplement le motif, et proposez de
commander à nouveau.

⚠️ **Le délai se compte depuis l'ÉPUISEMENT, pas depuis la commande.** Chaque
**Relancer** remet le compteur à zéro : une recherche relancée n'est jamais
coupée au milieu de la tentative suivante.

⚠️ **Ce n'est pas la fin de la recherche, c'est la fin de l'attente.** Tant
que le délai n'est pas écoulé, les deux gestes restent — et sur un pays où
le délai vaut 0, la course attend indéfiniment, comme avant.

### 🆕 🛣️ LA ROUTE PRÉVUE — dessinez la ROUTE, pas une ligne droite (v4.85.0)

```jsonc
// GET /vtc/rides/{id}  ·  GET /vtc/rides  ·  dès la CRÉATION
{ "planned_polyline": "sgpd@kxlF{T{T_]_q@…", … }
```

L'itinéraire que le réseau routier a rendu **au devis** — celui sur lequel
`fare_xof` est calculé, et celui que la navigation du chauffeur suit. Polyline
Google (`[lat, lng]`, précision 1e-5), le même encodage que les deux tracés
ci-dessous.

⚠️⚠️ **PRÉSENT DÈS LA CRÉATION, et c'est tout l'intérêt.** Les deux autres
tracés n'existent qu'à la **fin** de la course. Pendant le trajet, une carte
qui n'a pas celui-ci ne peut que relier le départ à l'arrivée par une **ligne
droite** — c'est ce que faisait notre console, et un opérateur ne pouvait donc
pas voir qu'un chauffeur s'écartait de sa route. Si votre carte fait la même
chose, ce champ est la correction.

| Tracé | Ce qu'il dit | Quand |
|---|---|---|
| `planned_polyline` | par où la course **devrait** passer | dès la création |
| `traveled_polyline` | par où le chauffeur **est** passé | à `completed` |
| `approach_polyline` | ce qu'il a roulé **pour venir** | à `arrived` |

⚠️ **NE LE DESSINEZ PAS COMME UN PARCOURS RÉEL.** « Il a fait un détour » se
tranche sur `traveled_polyline`, jamais sur celui-ci. Quand les deux existent,
montrez le réel **par-dessus** et le prévu atténué : l'écart entre les deux est
exactement ce qu'on vient lire.

⚠️ **UN TRAIT PLEIN POUR UNE ROUTE, POINTILLÉ POUR DES ARRÊTS.** Quand
`planned_polyline` est **absent** — le réseau routier n'a pas rendu de
géométrie — retombez sur `stops` en **pointillé**. Un trait plein entre deux
points dirait « la voiture passe par là », ce que personne ne sait.

⚠️ **ET UN TRACÉ DE MOINS DE DEUX POINTS N'EST PAS UNE ROUTE.** Le serveur ne
vous en servira pas, mais vérifiez-le quand même avant de sauter votre repli :
un point ne dessine aucune ligne, et vous n'afficheriez **plus rien** là où
vous affichiez au moins une ligne droite.

⚠️ **LE PRIX NE DÉPEND PAS DE CE TRACÉ**, comme pour les deux autres :
`fare_xof` vient du devis. Le tracé dessine.

**Le parcours d'une course terminée (v3.2.0).** À `completed`, la course
porte **`traveled_polyline`** (polyline Google, le chemin réellement roulé,
recalé sur la route), **`actual_distance_m`** et **`distance_source`** :
`tracked` quand le tracé existe, `planned` quand le chauffeur n'a envoyé
aucune position — la distance est alors celle du devis et il n'y a rien à
dessiner. Les trois champs sont absents avant la fin de la course et sur une
course annulée. **Le prix ne dépend pas de ce tracé** : `fare_xof` vient du
devis, le parcours est ce qu'on montre dans le détail et le reçu.

**La carte du chauffeur (v3.8.0).** Dès `accepted`, `GET /rides/{id}` porte
`driver` : `{ id, name, rating_avg, rating_count, rides_count,
vehicle: { class_key, brand, model, license_plate, color, description, photo_url } }`
— qui vient, dans quelle voiture, avec quelle note. **`description`
(v4.10.0)** est GÉNÉRÉE — *marque modèle couleur*, « Toyota Avensis rouge » :
c'est ce que le passager guette, à afficher avec la **plaque** ;
`photo_url` est la photo de couverture du véhicule, quand le chauffeur en a
déposé une. `id` est le **profil** du chauffeur,
celui dont `GET /agents/{id}/ratings` liste les avis. Absente sur la liste
`GET /rides` : c'est le détail qui la porte. Pas de téléphone : la mise en
relation passe par la conversation (§7).

`stop_index` est l'étape **atteinte** — zéro tant que le départ n'est pas fait.
Chaque `stops[i].reached_at` porte l'instant.

> ⚠️ **`commission_xof` et `driver_xof` ne vous sont pas servis.** Ce que la
> plateforme prend est une affaire entre elle et le chauffeur ; l'afficher au
> passager le ferait s'interroger sur ce qui revient à qui au moment où il
> paie. Ne les attendez pas.

> ⚠️ **Une recherche épuisée n'annule PAS la course.** Elle reste `searching`,
> et l'exploitation peut attribuer à la main. N'affichez pas « annulée » sur un
> délai : affichez que la recherche continue.

**Annuler** :

```
POST /rides/{id}/cancel   { "reason_code": "wait_too_long", "reason": "…" }   // les deux facultatifs
```

`409 invalid_transition` sur une course `in_transit` : le bouton doit
disparaître à ce statut, pas échouer.

### 🏷️ LE MOTIF, NOMMÉ — `reason_code` et `GET /rides/cancel-reasons` (v4.55.0)

Le motif d'annulation était un **texte libre**, et il ne se comptait pas :
« changé d'avis », « Changé d'avis », « chg avis », « il est pas venu » — quatre
façons d'écrire deux faits, et aucun moyen de répondre à « combien de
passagers posés par un chauffeur cette semaine ? ». Un champ qu'on ne peut pas
grouper n'existe que pour celui qui l'a tapé.

**Demandez la liste, n'inventez pas les codes :**

```
GET /rides/cancel-reasons
→ { "by": "rider", "items": [ { "code": "changed_mind", "grave": false }, … ] }
```

```jsonc
{ "by": "rider", "items": [
  { "code": "changed_mind",      "grave": false },   // je n'ai plus besoin de la course
  { "code": "wait_too_long",     "grave": false },   // l'attente est trop longue
  { "code": "driver_not_moving", "grave": false },   // il a accepté et ne vient pas
  { "code": "driver_asked",      "grave": false },   // le chauffeur m'a demandé d'annuler
  { "code": "wrong_address",     "grave": false },   // je me suis trompé de départ
  { "code": "price",             "grave": false },   // le prix ne me convient pas
  { "code": "other",             "grave": false } ] }
```

⚠️ **LA LISTE DÉPEND DU RÔLE DU JETON**, et le serveur **refuse** un code de
l'autre rôle. Avec votre jeton de client vous recevez les motifs du PASSAGER ; « le passager n'est pas venu » n'y est pas, et l'envoyer serait refusé. Les deux listes ne décrivent pas les mêmes faits. Appelez la route, mettez le résultat en cache pour la
session, et composez la liste depuis elle — un code écrit en dur dans
l'application sera refusé le jour où la taxonomie change.

⚠️ **TRADUISEZ LES CODES CHEZ VOUS.** La route sert les codes et leur gravité,
**pas des phrases** : à vous de les afficher dans la langue de l'utilisateur.
Un serveur qui renverrait du français vous obligerait à l'ignorer de toute
façon.

⚠️ **LE CODE RESTE FACULTATIF, ET LA PHRASE LIBRE RESTE À CÔTÉ.** Les deux
ensemble, parce qu'ils ne disent pas la même chose : le code **se compte**, la
phrase **explique le cas**. Une taxonomie ne couvre jamais tout, et forcer un
choix fait cocher le premier élément de la liste — ce qui est pire qu'un champ
vide, parce qu'on le croit. `other` existe pour ça.

⚠️ **UN CODE INVENTÉ EST REFUSÉ** — `422 cancel_reason_unknown`, et **la phrase
nomme les motifs acceptés pour ce rôle**. N'envoyez rien plutôt qu'un code
approximatif : une annulation refusée pour un détail de formulaire laisse
l'utilisateur coincé sur une course dont il veut sortir.

> ## ⚠️⚠️ CE CONTRAT PROMETTAIT « LA LISTE `allowed` », ET ELLE N'EST JAMAIS ARRIVÉE (v4.76.0)
>
> Depuis la v4.55.0, ces lignes annonçaient un `422` « avec la liste
> `allowed` ». **Cette liste n'a jamais été servie** : elle voyageait dans le
> `meta` du refus, et l'enveloppe ne porte que `code`, `message`, `fields` et
> `reason`. L'application recevait donc `validation_failed` / « Requête
> invalide » + le nom du champ, sans jamais savoir quels motifs existent.
>
> Le refus porte désormais **son propre code** et la liste est **dans la
> phrase traduite**. ⚠️ Si votre écran lit `validation_failed` pour ce cas,
> il lit l'ancien code.

⚠️ **ET LE MOTIF SE RELIT** : la course rend `cancelled_reason_code` à côté de
`cancelled_reason`. Les courses **annulées avant la 4.55.0** n'ont que la
phrase — ne traduisez pas son absence par `other`, et affichez-la telle quelle :
c'est leur seule explication.

⚠️ **`driver_asked` EST LE MOTIF QU'IL FAUT VRAIMENT PROPOSER.** « Le chauffeur
m'a demandé d'annuler » est un **signal de fraude** : un chauffeur qui fait
annuler son passager évite les frais d'annulation **et** garde sa place dans le
vivier. Sans ce choix, le geste est indistinguable d'un passager qui renonce, et
il ne se compte nulle part. Formulez-le en clair dans la liste.

⚠️ **AUCUN MOTIF N'EST `grave` CÔTÉ PASSAGER**, et `grave` vous est pourtant
servi : lisez-le plutôt que de supposer qu'il vaut toujours `false`. Un passager
qui signale une agression passe par le **support** (§ support), pas par une
annulation — c'est un échange, pas une case à cocher.

### 💸 CE QU'ANNULER COÛTE — à lire AVANT le bouton (v4.50.0)

Un barème d'annulation se règle par pays et par véhicule. Il voyage **avec la
course**, dans `cancellation` :

```jsonc
{ "cancellation": {
    "fee_xof": 800,        // ce qui serait retenu. 0 = gratuit
    "pct": 40,             // la part du tarif appliquée, pour l'expliquer
    "step": "waiting",     // free | on_the_way | waiting
    "why": "",             // la RAISON quand c'est gratuit
    "grace_left_s": 0 } }  // secondes restantes avant que ça devienne payant
```

⚠️ **RETENIR DE L'ARGENT À QUELQU'UN QUI N'A PAS PU LE LIRE AVANT EST
INDÉFENDABLE**, et c'est la réclamation qu'on ne peut pas gagner. L'écran de
confirmation doit dire le montant — ou dire que c'est gratuit, **et jusqu'à
quand**. Ne montrez jamais un bouton « Annuler » nu quand `fee_xof > 0`.

⚠️ **`grace_left_s` EST LE CHAMP QUI ÉVITE LA RÉCLAMATION.** Annoncer
« gratuit » sans dire jusqu'à quand fait découvrir les frais **après** le
geste. Rendez le compte à rebours **depuis ce nombre**, pas depuis l'horloge du
téléphone — et relisez-le à chaque rafraîchissement de la course plutôt que de
le décompter indéfiniment : le chauffeur peut arriver avant la fin de la grâce,
et la grâce tombe alors d'un coup.

⚠️ **AFFICHEZ `why` QUAND C'EST GRATUIT.** « C'est gratuit » sans dire pourquoi
laisse croire à une faveur, et la prochaine fois que ce sera payant, personne
ne comprendra ce qui a changé.

| `why` | Ce que l'écran dit |
|---|---|
| `no_driver_yet` | « Personne n'a encore pris votre course : l'annulation est gratuite. » |
| `within_grace` | « Gratuit pendant encore N secondes. » |
| `no_schedule` | rien de particulier — ce pays n'a pas de barème |
| `not_the_rider` | ne vous concerne pas (c'est le chauffeur qui annule) |

| `step` | Quand |
|---|---|
| `free` | rien à payer |
| `on_the_way` | un chauffeur roulait vers vous |
| `waiting` | il était **arrivé** et vous attendait |

⚠️ **COMMENT C'EST PERÇU, et il faut le dire à l'écran** : sur une course déjà
payée, le remboursement est **amputé** des frais (vous recevez le tarif moins
les frais, en un seul mouvement). Sur une course **en espèces**, personne n'a
rien versé : les frais deviennent une **dette** réglée sur votre prochaine
recharge. Un écran qui promettrait « remboursement intégral » dans le second
cas annoncerait un mouvement qui n'existe pas.

⚠️ **ET CE QUI EST RETENU VA AU CHAUFFEUR**, moins la commission. Le dire
change la façon dont les frais sont reçus : ce n'est pas une pénalité de la
plateforme, c'est un dédommagement pour quelqu'un qui s'est déplacé.



Sur la carte : le départ et chaque étape sont `stop`, l'arrivée `client` ; le chauffeur est dessiné par son mode (`map_icon_url` de `GET /classes`).

### Les marqueurs de carte — `GET /map-markers` (v4.13.0)

```
GET /api/v1/map-markers          (public, SOCLE — sans `{PREFIX}`)
```

```json
{ "items": [
  { "kind": "courier", "icon_url": "…/courier.png", "map_icon_url": "…/courier-map.png",
    "nav_icon_url": "…/courier-nav.png" },
  { "kind": "client", "map_icon_url": "…/client-map.png", "max_numbered": 4,
    "numbered": [ { "index": 1, "map_icon_url": "…/stop-1.png" },
                  { "index": 2, "map_icon_url": "…/stop-2.png" },
                  { "index": 3 }, { "index": 4 } ] },
  { "kind": "merchant", "map_icon_url": "…/store-map.png", "max_numbered": 4,
    "numbered": [ { "index": 1, "map_icon_url": "…/store-1.png" }, { "index": 2 }, { "index": 3 }, { "index": 4 } ] } ] }
```

Toujours les **trois genres**, dans cet ordre : `courier` (le livreur — le
chauffeur VTC est dessiné par son mode de véhicule, `GET /classes`),
`client`, `merchant` (le point de vente, la collecte). Chacun porte deux
images **facultatives**, réglées depuis la console : `icon_url` (à côté d'un
nom, dans une liste) et `map_icon_url` (le pin **principal**, dans la
pastille du marqueur). **Absentes, gardez votre pictogramme** — le réglage
ajoute, il ne retire rien. Lisez la liste à l'ouverture, mettez les images en
cache par URL (elles changent d'URL quand elles changent). Pour toute la
plateforme, pas par pays.

#### 🔢 Les pins NUMÉROTÉS (v4.18.0)

Une carte porte souvent **plusieurs points du même genre** : une commande
collectée chez trois marchands, une course qui s'arrête deux fois avant
d'arriver. Un seul pictogramme pour tous ces points ne dit pas **dans quel
ordre** on y passe — et c'est justement ce qu'on cherche sur une carte.

`client` et `merchant` portent donc, en plus de leur pin principal, jusqu'à
**quatre pins numérotés** : `numbered`, rangs **1 à 4**, toujours servis au
complet même vides. `courier` n'en a pas — il n'y a qu'un livreur par course,
et le champ est **absent** chez lui (absent = « sans objet » ; une liste vide
se lirait « rien de réglé »).

#### 🏁 Et `client` porte, LUI, un pin de DESTINATION

`client.dest_icon_url` : le point d'**arrivée** — la fin d'une course.

⚠️ **Le client et sa destination ne sont pas le même point.** Le pin
**principal** marque **quelqu'un** : le passager qui attend au départ, la
personne à qui on remet une commande. Le pin de **destination** marque un
**lieu** où personne n'attend encore. Les dessiner pareil oblige à lire les
libellés pour savoir lequel est lequel — sur une carte, c'est exactement ce
qu'on n'a pas le temps de faire.

Le marchand n'en a pas : une boutique est une **étape**, la destination de
personne.

Absent : **retombez sur `map_icon_url`**.

Le client porte donc, à lui seul, **tous les points du trajet d'un
passager** : lui (principal), ses étapes (numérotés 1 à 4), son arrivée
(destination). C'est la contrepartie d'y avoir fusionné `stop`.

#### 🚗 Et `courier` porte, LUI, une image de navigation

`courier.nav_icon_url` : le même livreur en **vue 3D à ~45°**, pour une carte
de navigation inclinée. `client` et `merchant` n'en ont pas, et c'est voulu.

⚠️ **La distinction n'est pas « véhicule / pas véhicule », c'est « couché sur
la route / debout sur la carte ».** Un livreur est dessiné **à plat sur la
chaussée** : dès que la caméra penche, une vue de dessus paraît écrasée. Un
client, un marchand, une étape sont des pastilles qui **se dressent** — comme
une épingle plantée, elles gardent la même image quelle que soit
l'inclinaison.

Absent : **retombez sur `map_icon_url`**.

> ⚠️ **LE RANG EST CELUI DU PASSAGE**, pas un identifiant. La première
> collecte porte le pin **1**, la deuxième le **2**. Numérotez dans l'ordre où
> l'on s'y rend — celui que le serveur vous donne —, jamais dans l'ordre
> d'affichage de votre liste.

> ⚠️ **AU-DELÀ DE `max_numbered`, RETOMBEZ SUR LE PIN PRINCIPAL.** Cinq
> collectes, ou un rang qu'on n'a pas réglé : ce n'est pas une panne, c'est
> prévu. Un chiffre dans une pastille de vingt-quatre pixels ne se lit plus
> très loin, et un pin manquant vaut mieux qu'un pin vide.

Un pin numéroté ne porte **que** `map_icon_url` : dans une liste, le nom de la
boutique distingue déjà les points ; c'est sur la carte, où aucun nom ne
tient, que le chiffre sert.

> ⚠️ **`stop` A DISPARU (v4.18.0).** Il a fusionné dans `client` : un arrêt de
> course et l'adresse d'un client sont le même objet vu à deux moments. Les
> étapes d'une course se dessinent désormais avec les **pins numérotés du
> client**, et son arrivée avec le pin principal.

**Sur VOS cartes**, les trois points d'une course viennent tous du genre
`client` — c'est votre trajet, du début à la fin :

| Le point | Le pin |
|---|---|
| le **départ** (`kind: "pickup"`) — vous, qui attendez | `map_icon_url`, le pin **principal** |
| les **étapes** (`kind: "stop"`) | `numbered`, dans l'ordre de `stops[]` : le 1ᵉʳ arrêt après le départ porte le pin **1** |
| l'**arrivée** (`kind: "dest"`) | `dest_icon_url`, le pin de **destination** |

Le pin principal marque **quelqu'un** — vous —, celui de destination un
**lieu**. C'est ce qui permet de lire une carte sans lire les libellés.

### Changer le trajet EN COURS DE ROUTE — un arrêt de plus, un de moins (v4.4.0)

Le passager peut ajouter un arrêt, en retirer un, ou changer la
destination **pendant qu'un chauffeur est assigné** (`accepted`,
`picking_up`, `arrived`, `in_transit`). Il envoie le **nouveau trajet en entier** :

```
PATCH /api/v1/vtc/rides/{id}/stops
{ "stops": [
  { "kind": "pickup", "label": "Almadies",  "geo": [1.2255, 6.1319] },   ← figé
  { "kind": "stop",   "label": "Pharmacie", "geo": [1.2300, 6.1400] },   ← nouveau
  { "kind": "dest",   "label": "Aéroport",  "geo": [1.2545, 6.1656] }
]}
→ 200 Ride  — nouveau `stops`, `distance_m`, `duration_s`, `fare_xof`,
              et `fare_adjustments: [{ from_xof, to_xof, delta_xof, movement, by, at }]`
```

**Ce qui est figé.** Les arrêts **déjà atteints** ne changent plus : le
départ dès qu'un chauffeur roule vers lui, puis chaque arrêt marqué
atteint (`reached_at`). Renvoyez-les **tels quels, en tête** — `422` sinon.
Grisez-les dans l'éditeur : on ne réécrit pas ce qui a eu lieu. Même règle
de forme qu'au devis : 2 à 5 arrêts, `pickup` en premier, `dest` en
dernier, tous dans la ville desservie.

**Le prix suit le trajet.** Il est recalculé sur la grille du jour avec la
**même majoration qu'au devis** — une majoration montée ou retombée
entre-temps ne change pas une course qui roule. Montrez le nouveau prix
**avant** de confirmer : demandez un devis avec le nouveau trajet
(`POST /rides/quote`) pour l'afficher, puis envoyez le `PATCH`. (Le devis
peut différer de quelques francs si une majoration s'est ajoutée depuis :
c'est le `PATCH` qui fait foi.)

**L'argent suit le prix, tout de suite** — `fare_adjustments[].movement` :

| Paiement | `delta_xof > 0` (plus cher) | `delta_xof < 0` (moins cher) |
|---|---|---|
| `wallet` | la différence est **débitée du solde Dira** → `charged` | rendue sur le solde → `refunded` |
| `online` | **débitée du solde Dira** aussi (l'opérateur ne se rappelle pas à chaud) → `charged` | rendue sur le solde → `refunded` |
| `cash` | rien ne bouge : le chauffeur encaisse le nouveau prix → `cash` | idem |

> ⚠️ **`402 insufficient_funds` = le changement est REFUSÉ**, la course reste
> exactement telle qu'elle était. Ce n'est pas une panne : routez vers la
> recharge (**`POST /payments/initiate` avec `purpose: "wallet_topup"`**, au
> socle — ⚠️ **et non `POST /wallet/purchase`**, qui achète des jetons et
> refuse un client en `403`), et proposez de réessayer.
> `409 payment_pending` : une course mobile money pas encore confirmée ne
> change pas de trajet tant que le paiement n'est pas arrivé.

**Les deux sont prévenus.** Le chauffeur reçoit `ride_stops_changed` et
relit la course ; le passager reçoit `ride_fare_adjusted`
(`data.type: "ride_status"`, `event: "stops_changed"`, `fare_xof`,
`delta_xof`) avec ce qui a bougé — relisez `GET /rides/{id}` pour
redessiner le trajet, comme pour tout signal.

`409 ride_not_reroutable` : pas de chauffeur (encore `searching`) ou
course finie — pendant la recherche, annulez et recommandez.

---

## 🧾 LE REÇU ET LE RELEVÉ, EN PDF (v4.52.0)

Deux documents téléchargeables, pensés pour être envoyés à un employeur, à un
comptable ou à une assurance.

```
GET /rides/{id}/receipt.pdf
GET /rides/statement.pdf?from=2026-09-01&to=2026-09-30
```

Les deux rendent `application/pdf` avec `Content-Disposition: attachment` et un
nom de fichier déjà daté — `dira-course-2026-10-08-718293.pdf`. **Ne les ouvrez
pas dans une WebView** : laissez le système les enregistrer ou les partager, c'est
ce que la personne veut en faire.

> ⚠️ **`Cache-Control: no-store`, ET CE N'EST PAS DÉCORATIF.** Un reçu porte un
> nom, un trajet et un montant. Ne le gardez pas dans un cache disque applicatif :
> sur un téléphone partagé, il se relit après la déconnexion. Téléchargez à la
> demande.

### Ce que le reçu dit de la distance — et pourquoi c'est le cœur du document

Le reçu imprime la **distance réellement parcourue**, celle mesurée sur le trajet
du chauffeur, et **nomme sa source** :

| `distance_source` | Ce que le document écrit |
|---|---|
| `tracked` | « mesurée sur le trajet réel » |
| `planned` (ou absent) | « estimée — le suivi n'a rien enregistré » |

> ⚠️ **LE SECOND CAS EST COURANT, PAS RARE** : il suffit que le téléphone du
> chauffeur ait perdu le réseau pendant la course. C'est pourquoi la source est
> écrite sur le document plutôt que sous-entendue — « 11,4 km » présenté comme
> mesuré alors qu'il vient d'une estimation est la phrase d'un reçu qu'on ne peut
> plus défendre devant une réclamation.

> ⚠️ **L'ESTIMATION DU DEVIS FIGURE À CÔTÉ**, jamais à la place : l'écart entre
> les deux est exactement ce qu'un litige examine (« il a fait un détour »).

> ⚠️ **ET L'APPROCHE EST SÉPARÉE.** Ce que le chauffeur a roulé pour venir est
> imprimé à part. Les additionner ferait lire « 10,3 km » pour une course de
> 8,2 km — avec un prix qui ne colle plus avec la distance affichée juste
> au-dessus.

### Ce que le reçu garantit

- **Le détail s'additionne jusqu'au prix payé.** Si ce n'est pas le cas, le
  serveur **ne sert rien** (500) plutôt qu'un document faux : un reçu dont les
  lignes ne tombent pas juste prouve une erreur, sous notre nom, devant le
  comptable du client.
- La **majoration** est une ligne d'information, sans montant : elle est déjà dans
  le tarif, et l'ajouter la compterait deux fois.
- **Une seule remise** par course, et le libellé dit laquelle — code ou promotion.
- Le **pourboire** est une note, pas une ligne : il n'est pas dans le prix de la
  course.
- Une **course annulée** imprime ce qui a été **retenu**, pas le tarif d'une
  course qui n'a pas eu lieu. Rien à payer ⇒ le document le dit en une phrase.
- La **monnaie** est celle du pays de la course, et l'**heure** celle de son
  fuseau.

### Le relevé d'une période

Une ligne par course — date, trajet, distance réellement parcourue, montant —
puis le nombre de courses, la distance totale et la somme.

> ⚠️ **`from` ET `to` SONT OBLIGATOIRES, SANS DÉFAUT.** Un défaut implicite
> produirait un document dont la période n'est pas celle qu'on croyait demander —
> et c'est un document qu'on additionne. Format `2026-09-01` ou RFC 3339. Une date
> illisible est **refusée** (`422`), pas ignorée.

> ⚠️ **IL REFUSE AU LIEU DE TRONQUER.** Au delà de **500 courses** ou d'une
> fenêtre de **366 jours** : `409 statement_too_large`, avec `meta.max_rows`.
> Proposez de **découper la période** — c'est la seule suite utile. Un relevé qui
> s'arrêterait en silence à la cinq-centième ligne produirait une somme fausse,
> présentée comme vraie.

> ⚠️ **SI LA PÉRIODE COUVRE PLUSIEURS PAYS**, le total mêle des monnaies et le
> document le dit. Il reste servi : le retirer priverait du relevé quelqu'un qui
> n'a roulé qu'une fois à l'étranger.

### Où mettre le bouton

- Sur une course terminée ou annulée : « Reçu » dans l'écran de détail. Pas sur
  une course en cours — il n'y a pas encore de prix définitif.
- Dans l'historique : « Exporter la période », avec deux dates à choisir.

---

## 6. Suivre le chauffeur

Le suivi temps réel passe par **`dira-tracking`**, sur son propre hôte — pas
par cette API.

```
WS wss://tracking-staging.dira.llc/track/subscribe/{ride_id}

{ "type": "hello",    "mission_id": "…", "vehicle_id": "…", "vehicle_type": "voiture",
  "plate": "…", "lng": 1.2255, "lat": 6.1319, "heading": 122.5,
  "heading_source": "gps", "speed": 8.3, "ts": 1757… }          ⚠️ v4.28.0
{ "type": "position", "mission_id": "…", "vehicle_id": "…", "vehicle_type": "voiture",
  "plate": "…", "lng": 1.2255, "lat": 6.1319, "heading": 122.5, "speed": 8.3, "ts": 1757… }
{ "type": "status",   "mission_id": "…", "status": "picking_up", "ts": 1757… }
```

L'identifiant de course sert d'identifiant de mission (`mission_id` =
`ride_id`). Le jeton d'accès passe dans l'URL (`?token=`) — un WebSocket de
navigateur ne porte pas d'en-tête.

### 🆕 ⚠️ LES TROIS TRAMES PORTENT LEUR `mission_id` (v4.95.0)

`hello`, `position` **et** `status` : toutes les trois, toujours, même sur la
route à une seule mission. Les exemples de ce document ne le montraient pas sur
`position` et `status`, et il fallait le croire sur parole — il est servi depuis
toujours, et le serveur ne peut plus l'oublier (le canal nomme la trame).

⚠️ **Aiguillez dessus plutôt que sur l'ordre d'arrivée**, surtout sur la socket
à plusieurs courses ci-dessous : une trame sans nom se jette, une trame mal
nommée se dessine sur la mauvaise voiture.

### 🆕 ⚠️⚠️ UNE POIGNÉE DE MAIN REFUSÉE DIT LAQUELLE DES DEUX (v4.95.0)

Jusqu'ici le suivi refusait **`401`** pour deux choses qui n'ont rien à voir :
un jeton expiré — qu'il faut rafraîchir, après quoi ça marche — et une course
qui n'est pas la vôtre, qu'aucun rafraîchissement ne rendra suivable. La boucle
« `401` → rafraîchir → redemander → `401` » tournait indéfiniment sur une carte
vide.

| statut | corps | ce qu'il faut faire |
|---|---|---|
| `401` | `{"error":"unauthorized"}` | **rafraîchir** (`POST /auth/refresh`) puis rouvrir |
| `403` | `{"error":"mission_forbidden","mission_id":"…"}` | **cesser d'essayer** sur cette course ; elle ne vous appartient pas |
| `400` | `{"error":"missions_required"}` | la liste `?missions=` manque |
| `400` | `{"error":"too_many_missions"}` | plus de 20 missions demandées |

⚠️ **C'est le STATUT qui porte la décision, pas le corps.** Une poignée de main
échouée ne rend pas toujours son corps au client : OkHttp et Starscream donnent
le statut à coup sûr, le corps au mieux. Branchez votre logique sur `401` contre
`403` ; le code du corps sert à écrire une trace lisible.

⚠️ **À ne pas confondre avec le code de fermeture `4401 token_expired`** : celui-là
arrive quand le jeton expire **pendant** une connexion déjà ouverte. Ici la
socket n'a jamais existé — il n'y a rien à fermer.

### 🆕 🛰️ PLUSIEURS COURSES SUR UNE SEULE SOCKET (v4.83.0)

```
WS wss://tracking-staging.dira.llc/track/subscribe?missions=<id1>,<id2>,<id3>&token=…
```

⚠️⚠️ **N'OUVREZ PAS TROIS SOCKETS.** Sur un réseau mobile, chaque socket coûte
une poignée de main TLS, un maintien en vie qui **réveille la radio du
téléphone**, et une reconnexion à chaque passage de tunnel. Trois courses à
suivre, c'est trois fois tout cela — pour des trames qui portent **déjà** leur
`mission_id`.

- **Un `hello` par mission** arrive à l'ouverture, chacun avec la dernière
  position connue : dessinez vos trois voitures tout de suite. Une voiture
  arrêtée au feu rouge n'envoie aucune trame — sans son accueil, elle n'aurait
  aucun point sur la carte jusqu'à ce qu'elle reparte, et le passager la
  croirait perdue.
- **Chaque trame porte son `mission_id`** : aiguillez dessus.
- ⚠️ **Vingt missions au plus.** Au-delà, la socket est refusée (`400`).
- ⚠️ **Une mission que vous n'avez pas le droit de suivre refuse TOUTE la
  socket** — `403 {"error":"mission_forbidden","mission_id":"…"}` **depuis la
  v4.95.0**, et non plus `401`. Elle n'est pas écartée en silence : un
  abonnement partiel qui a l'air complet dessinerait deux voitures sur trois
  sans que vous puissiez le savoir. Le corps **nomme la mission en cause** :
  retirez-la de la liste et rouvrez avec les autres, plutôt que de les
  abandonner toutes.
- ⚠️ **Ne demandez pas deux fois la même mission** : la liste est
  dédoublonnée côté serveur, mais comptez-y — deux abonnements au même canal
  doubleraient chaque position.

La route à une mission (`/track/subscribe/{mission_id}`) **ne change pas** : une
seule course, une seule socket, c'est toujours le plus simple.

### 🆕 🤝 SUIVRE LE TRAJET QU'UN AUTRE PASSAGER A PARTAGÉ (v4.83.0)

Quelqu'un vous envoie un lien de partage (§ *Partager mon trajet*). Connecté,
vous pouvez l'**épingler** : le trajet apparaît à côté de vos propres courses,
sur la même carte, aussi longtemps que le lien vit.

```
POST   /trip-shares/pinned      { "token": "<le jeton du lien reçu>" }
→ 201 { "id": "<l'épingle>", "trip": { … la même page que le lien public … } }

GET    /trip-shares/pinned      → { "items": [ { "id", "trip" } ] }
DELETE /trip-shares/pinned/{id} → 204
```

⚠️⚠️ **ÉPINGLER N'ACCORDE RIEN DE PLUS QUE LE LIEN.** Le trajet se relit par
les **mêmes** règles : révocation immédiate, durée choisie, fin du trajet plus
la grâce. Celui qui a partagé peut couper à tout instant, et votre épingle se
vide dans la seconde.

⚠️ **LES TRAJETS MORTS SONT OMIS DE LA LISTE, pas rendus grisés.** Un lien
révoqué ou un trajet terminé ne montre plus rien — et surtout pas sa dernière
position connue, qui dirait « la voiture est là » alors que personne ne sait
plus où elle est. Votre liste rétrécit toute seule ; c'est normal.

⚠️ **`id` EST CELUI DE L'ÉPINGLE, pas du lien** : c'est lui qu'on envoie pour
la retirer. Le jeton, lui, n'est écrit nulle part — ni chez vous, ni chez nous.

| Refus | Sens |
|---|---|
| `409 follow_own_trip` | c'est **votre** trajet : il est déjà dans `GET /rides/active`. L'épingler en plus donnerait deux marqueurs au même endroit, dont l'un disparaîtrait à la révocation |
| `409 too_many_followed_trips` | dix au plus (`meta.max`) |
| `404 share_not_found` · `403 share_revoked` | jeton inconnu, lien expiré ou coupé |
| `503 follow_unavailable` | pas servi sur cette plateforme : masquez le geste |

**L'écran de suivi, en entier :** `GET /rides/active` (les vôtres) +
`GET /trip-shares/pinned` (les autres), **une** socket avec tous les
`mission_id` des vôtres, et le `trip` des épinglés se **relit** (la page
publique fait de même) — un trajet partagé n'ouvre **pas** de socket.

### 🤝 SUR UNE COURSE PARTAGÉE, VOUS VOYEZ LA VOITURE COMME TOUT LE MONDE (v4.65.0)

**Rien à faire de particulier : abonnez-vous à VOTRE `ride_id`.** Vous recevez
les positions de la voiture pendant **tout** le trajet — y compris le temps
qu'elle met à venir chercher l'autre passager — et votre reçu porte la
distance **réellement** parcourue (`distance_source: "tracked"`).

⚠️ **CE QUE ÇA RÉPARE, ET QUI A ÉTÉ VU EN RECETTE.** Le chauffeur ne pousse
ses positions que sous **une** course — c'est tout ce que le protocole du
suivi lui permet. Le second passager était donc abonné à un canal où personne
ne publiait jamais : accueil **nu**, **carte vide du départ à l'arrivée**,
course qui semblait figée, puis distance « prévue » sur le reçu. Le serveur
recopie maintenant chaque position aux **deux** courses, chacune gardant son
propre parcours.

⚠️ **ET LES DEUX COURSES NE SE TERMINENT PAS ENSEMBLE** — c'est normal, ce
n'est pas une panne. Vous êtes déposé, votre course passe à `completed` et
votre socket se ferme ; le co-passager roule encore et voit la voiture
continuer. L'inverse est vrai aussi : vous pouvez rester `in_transit`
longtemps après que l'autre est descendu. **N'affichez jamais l'état de
l'autre course** — vous ne le recevez pas, et vous n'avez pas à le deviner.

### ⚠️ VOUS NE RECEVEZ PLUS « TERMINÉE » À L'ARRIVÉE DU CHAUFFEUR (v4.65.0)

**Correctif.** Le socket envoyait une trame `{"type":"status","status":"completed"}`
**au moment où le chauffeur arrivait à votre point de départ**, et une autre
sur une course **annulée**. C'était un effet de bord du gel du parcours côté
suivi, pas un état de votre course.

Sur une course ordinaire, le vrai `arrived` suivait une milliseconde plus tard
et le masquait. Une application qui traite `completed` comme un **terminus**
(socket fermée, écran du reçu) ne voyait jamais passer la correction — et
affichait une course terminée alors que le chauffeur se garait devant chez
vous. Sur une course **partagée**, c'est exactement la scène qui a été
signalée : « terminée » pour l'un, « en cours » pour l'autre.

⚠️ **LA RÈGLE QUI PROTÉGEAIT DE TOUT ÇA N'A PAS CHANGÉ : une trame `status`
n'est pas la vérité, c'est une invitation à relire.** `GET /rides/{id}` fait
foi. Une application qui relisait à chaque trame n'a jamais rien vu de ce
défaut.

### 🛰️ L'ACCUEIL PORTE LA DERNIÈRE POSITION CONNUE (v4.28.0)

**`hello` arrive désormais avec la position du véhicule**, aux mêmes champs
qu'une trame `position`. **Dessinez la voiture dès l'accueil**, sans attendre
autre chose.

⚠️ **Ce que ça répare.** L'accueil ne disait que « bonjour ». Il fallait
attendre la trame suivante — jusqu'à plusieurs secondes — pour savoir où
était le chauffeur. À chaque reconnexion (réseau retrouvé, application
revenue au premier plan, jeton rafraîchi), le passager voyait donc **sa carte
se vider, puis se remplir**. Sur un trajet, ce clignotement revenait
plusieurs fois.

- **`ts` est l'horodatage du DERNIER point reçu**, pas l'instant de la
  connexion. Il peut dater de quelques dizaines de secondes ; c'est à vous
  de dire « il y a 12 s » si vous le jugez utile. La plateforme ne l'invente
  pas et ne l'extrapole pas.
- **Un `hello` NU — sans `lng`/`lat` — reste possible** : chauffeur pas
  encore attribué, téléphone silencieux depuis trop longtemps, suivi
  indisponible. Comportez-vous alors comme avant : attendez la première
  `position`. **Ne dessinez jamais un `hello` sans coordonnées** comme s'il
  en avait.
- Un `hello` n'annule pas la règle du §5 : **l'état de la course se relit sur
  `GET /rides/{id}`**, le socket ne porte que la position et le mot du
  statut.

### 🔁 LA RECONNEXION EST OBLIGATOIRE, PAS OPTIONNELLE (v4.28.0)

**Tant qu'une course est vivante** (`searching` → `in_transit`), l'application
**doit** rouvrir le socket toute seule. Ce n'est pas un confort : un socket
tombé sans reconnexion laisse une voiture figée sur la carte, et rien à
l'écran ne dit que l'information a cessé d'arriver.

| Événement | Ce que l'application fait |
|---|---|
| Socket fermé, quelle qu'en soit la cause | rouvrir, back-off **1 s, 2 s, 4 s … 30 s** |
| Retour au **premier plan** | rouvrir **immédiatement**, sans attendre le back-off |
| Réseau retrouvé | rouvrir immédiatement |
| Code **4401 `token_expired`** | `POST /auth/refresh` **puis** rouvrir — jamais avec le même jeton |
| **403** à la poignée de main | s'arrêter : ce rôle ne suit pas cette course |
| Course terminée ou annulée | fermer, et ne plus rouvrir |

À chaque réouverture : relisez aussi **`GET /rides/{id}`**. Le socket rend la
position ; la course, elle, a pu changer d'état pendant la coupure.

### 📴 QUAND C'EST LE CHAUFFEUR QUI N'A PLUS DE RÉSEAU (v4.32.0)

**Votre socket va très bien, et la voiture ne bouge plus.** Ce n'est pas la
même panne, et cela ne se traite pas de la même façon.

⚠️ **NE DITES JAMAIS QUE LA COURSE EST TERMINÉE, ANNULÉE OU PERDUE.** Elle a
lieu : le chauffeur conduit, son téléphone garde tout, et il rendra le récit en
retrouvant le réseau. Une application qui annonce « course introuvable » parce
qu'une position manque fait descendre un passager d'une voiture qui l'emmenait
au bon endroit.

| Ce que vous observez | Ce que vous affichez |
|---|---|
| Aucune position depuis **> 1 min**, course vivante | « position en attente » sur le marqueur, **sans le déplacer** |
| Aucune position depuis **> 3 min** | « nous ne voyons pas la voiture en ce moment — la course continue » |
| L'état change (`status`) mais les positions manquent | l'état fait foi : **relisez `GET /rides/{id}`** |

- ⚠️ **NE FAITES PAS GLISSER LE MARQUEUR VERS UNE POSITION VIEILLE.** Le
  serveur ne rediffuse **pas** les positions rattrapées : il ne vous enverra
  jamais le trajet manqué, justement pour que la voiture ne recule pas sur
  votre carte. Après une coupure, la voiture **saute** à sa vraie position —
  et c'est la vérité de la route, pas un défaut.
- Le **temps d'arrivée estimé** vieillit pendant une coupure : montrez-le
  comme une estimation, pas comme un compte à rebours exact.
- ⚠️ **Le bouton d'appel téléphonique reste la sortie de secours.** C'est le
  seul canal qui ne dépend pas de nos serveurs.

**À la fin**, une course faite en partie hors ligne peut porter un prix qui
n'est pas celui que le chauffeur vous a annoncé — il calculait avec la grille
gardée dans son téléphone. **C'est le montant de la course qui fait foi**, et
le support tranche les écarts.

- **Reconnexion avec back-off** (1 s, 2 s, 4 s … 30 s) : le socket tombe
  quand le téléphone change de réseau ; ne laissez pas une voiture figée sur
  la carte.
- **Interpolation** entre deux positions : une position toutes les quelques
  secondes, un marqueur qui glisse — pas qui saute.
- **Repli REST** : sans socket, `GET /rides/{id}` toutes les 10 s donne
  l'état ; la position, elle, ne se lit que sur le socket.
- **Échéance du jeton** : le suivi ferme le socket avec le code **4401
  `token_expired`** — rafraîchir (`POST /auth/refresh`) **puis** reconnecter,
  jamais reconnecter avec le même jeton.
- Poignée de main : **401 `unauthorized`** = rafraîchir et revenir ;
  **403 `mission_forbidden`** = cette course n'est pas la vôtre, ne pas
  réessayer. ⚠️ **Ce document promettait déjà ces deux codes ; le suivi
  renvoyait `401` dans les deux cas jusqu'à la v4.95.0.** Si votre
  application distinguait déjà les deux, elle fonctionne enfin comme prévu.

⚠️ **La base d'URL du suivi est distincte de celle de l'API** : deux variables
d'environnement.

### Suivre l'état — socket, push, et `GET` (v4.0.0)

**Le socket du suivi porte aussi les changements d'état de la course** :
une trame `{ "type": "status", "status": "…" }` à chaque passage —
`accepted`, `picking_up`, `arrived`, `in_transit`, `completed`, `cancelled`. Elle ne
porte que le mot : à réception, **relisez `GET /rides/{id}`** et redessinez
(qui vient, où en est-il, le prix, le parcours). Vous pouvez changer le badge
avant la réponse.

**Application en arrière-plan** : les mêmes passages arrivent en **push**
(socle, `POST /me/devices`), avec en données `type: "ride_status"`,
`ride_id`, `status` :

| Gabarit | Quand | Données |
|---|---|---|
| `ride_accepted` | un chauffeur a pris la course — nom et voiture dans le texte | `status: accepted` |
| `ride_driver_on_the_way` | il roule vers vous | `status: picking_up` |
| `ride_driver_arrived` | il est là et attend — les minutes offertes et le tarif ensuite dans le texte (v4.14.0) | `status: arrived`, `arrived_at`, `waiting_free_min`, `waiting_per_min_xof` |
| `ride_driver_honked` | **il klaxonne** : sur place, il ne vous voit pas (v4.24.0) — ⚠️ son et vibration d'urgence, ce n'est pas l'arrivée redite | `status: arrived`, `event: honk`, `honk_count` |
| `ride_cancelled` | annulée par le chauffeur ou la plateforme (`reason`) | `status: cancelled` |
| `ride_search_exhausted` | personne n'a pris la course — relancer ou annuler (§5, v4.1.0) | `status: searching`, `dispatch_state: exhausted` |
| `ride_pool_matched` | course **partagée** : on a trouvé un co-passager, on cherche maintenant une voiture (§4 quater, v4.42.0) — ⚠️ seulement pour celui qui **attendait** | `status: searching`, `dispatch_state: calling`, `pool_id` |
| `ride_pool_no_match` | personne ne partageait ce trajet — relancer **ou** prendre une course ordinaire (v4.42.0) | `status: searching`, `dispatch_state: exhausted`, `dispatch_reason: pool_no_match` |
| `ride_pool_partner_left` | le co-passager a annulé — relancer, le prix ne change pas (v4.42.0) | `status: searching`, `dispatch_state: exhausted`, `dispatch_reason: pool_partner_left` |
| `ride_cancelled` (`search_expired`) | la plateforme a **abandonné** une recherche épuisée depuis trop longtemps, et **remboursé** (§5, v4.25.0) | `status: cancelled`, `cancelled_by: system` |
| `ride_fare_adjusted` | le trajet a changé en route, le prix aussi — ce qui a été débité ou rendu (§5, v4.4.0) | `event: stops_changed`, `fare_xof`, `delta_xof` |
| `ride_rate_prompt` | terminée — noter, remercier (§7 bis) | `type: ride_rate_prompt` |
| `ride_scheduled_soon` · `_started` · `_failed` | course programmée (§4 bis) | |

À réception : ouvrir la course, `GET /rides/{id}`. Une trame reçue deux fois
(socket **et** push) est normale — le second `GET` répond la même chose.

**Le flux, dans l'ordre — un signal, un `GET` :**

1. **À l'ouverture d'un écran** : `GET /rides/{id}`. C'est l'état de référence —
   jamais ce que dit le socket.
2. **Socket ouvert** : sur une trame d'état, comparez à ce que vous affichez ;
   si ça diffère, `GET /rides/{id}` et redessinez. La trame porte le statut : vous
   pouvez changer le badge **avant** la réponse. Une trame qui « recule »
   (un `from` qui n'est pas votre état) signale une trame manquée — relisez.
3. **Push reçu** (application en arrière-plan) : `data.type` dit quoi ouvrir,
   l'identifiant sur quoi, `data.status` ce qui a changé. Même geste : ouvrir
   l'écran, `GET /rides/{id}`.
4. **Reconnexion** du socket (back-off 1 s → 2 s → 4 s … 30 s) :
   `GET /rides/{id}` **immédiatement**, avant d'appliquer la moindre trame — tout
   ce qui s'est passé pendant la coupure n'est que dans la base.
5. **Sans socket** (refusé, réseau captif, batterie) : **sondez** `GET /rides/{id}`
   toutes les **10 s** tant que l'opération n'est ni `completed` ni
   `cancelled`, en comparant `updated_at` ; passez à 30 s au bout de cinq
   minutes sans changement. Ne sondez **jamais** une opération terminée.

**Ce qu'aucun canal ne garantit** : l'ordre, l'unicité, la livraison. Deux
trames pour le même passage (socket **et** push) sont normales — le second
`GET` répond la même chose. Une application qui ferait du socket sa source
de vérité verrait, un jour, une course « en route » qu'un `GET` dit terminée.

---

## 6 bis. 🔗 PARTAGER SON TRAJET — « suis-moi jusqu'à ce que j'arrive » (v4.66.0)

Un passager monte dans une voiture et envoie un lien à quelqu'un : sa mère, un
collègue, un conjoint. Ce quelqu'un ouvre une page **sans compte Dira** et voit
où la voiture est, qui conduit, et où elle va.

C'est une fonction de **sécurité**, et elle ne sert qu'à une chose : qu'une
personne de confiance puisse dire « la voiture ne va pas dans la bonne
direction » **pendant** le trajet, pas après.

```
GET  /settings/share[?near=lng,lat]
POST /rides/{id}/share                  { "minutes": 0, "label": "Maman" }
POST /scheduled-rides/{id}/share        { "minutes": 0, "label": "Maman" }
GET  /rides/{id}/shares
GET  /scheduled-rides/{id}/shares
POST /trip-shares/{id}/revoke
```

### CE QUE LE PAYS PERMET — relisez-le avant de dessiner le bouton

```json
{ "enabled": true, "choices_min": [60, 240, 720], "max_minutes": 1440,
  "grace_minutes": 15, "stale_seconds": 120, "max_per_trip": 5 }
```

⚠️ **`enabled: false` → LE BOUTON N'EXISTE PAS.** Pas grisé, pas présent. Un
pays peut fermer le partage, et un bouton qui échoue fait appeler le support.

⚠️ **`choices_min` SONT LES BOUTONS À DESSINER, dans cet ordre.** N'écrivez
aucune durée en dur : la liste change par décision d'exploitation, et elle est
déjà rangée, dédoublonnée et bornée au plafond côté serveur.

⚠️ **`?near=` ICI AUSSI.** La politique qui s'applique est celle du pays où
l'on **roule**, pas celle du pays du compte. Sans ce paramètre, un passager
togolais à Dakar lit les règles du Togo.

### CRÉER UN LIEN — et le jeton qu'on ne reverra jamais

```json
{ "id": "…", "kind": "ride", "label": "Maman",
  "token": "<JETON-EN-CLAIR-UNE-SEULE-FOIS>",
  "url": "https://dira.llc/t/<JETON-EN-CLAIR-UNE-SEULE-FOIS>",
  "deep_link": "dira://trip/<JETON-EN-CLAIR-UNE-SEULE-FOIS>",
  "expires_at": "2026-10-12T23:05:02Z", "live": true,
  "created_at": "…", "views": 0 }
```

⚠️⚠️ **`token` N'APPARAÎT QUE DANS CETTE RÉPONSE, UNE SEULE FOIS.** Le serveur
ne le garde que haché : il ne peut plus jamais le relire, pas même pour aider
quelqu'un qui a perdu son lien. Cette impossibilité **est** la fonction — un
jeton relisible, c'est la position en direct d'un passager à la portée de tout
ce qui approche la base.

**Ce que cela impose à l'application :**

- **Partagez immédiatement** (feuille de partage du système, `navigator.share`,
  intention Android). Ne stockez pas le jeton pour « plus tard ».
- Si l'utilisateur ferme la feuille sans envoyer, **le lien existe mais il ne
  l'a plus**. Proposez de le retirer et d'en créer un autre — c'est le geste
  correct, et il est volontairement moins confortable que de relire.
- `GET /rides/{id}/shares` **ne porte pas le jeton**. N'essayez pas de
  reconstruire le lien à partir de l'identifiant : ce n'est pas le même objet.

⚠️ **`url` EST COMPOSÉE PAR LE SERVEUR. NE LA FABRIQUEZ PAS.** Trois
applications qui composeraient le lien chacune de leur côté sont trois endroits
à corriger le jour où le domaine change, et deux seront oubliés.

### 🆕 ⚠️⚠️ `url` EST TOUJOURS SERVIE — ET CE CONTRAT DISAIT LE CONTRAIRE (v4.73.0)

Ce document demandait, quand `url` était absente, de « partager le jeton brut et
le signaler ». **Voici ce que cela donnait**, reçu par un passager de la
préproduction le 24 octobre 2026 :

```
Code de suivi de mon trajet Dira (l'adresse du lien n'est pas disponible) :
fDj3MQoA-XC0ycce69moVV4RID-FaN3SYaISSpe05xY
```

Quarante-trois caractères de hasard, envoyés à quelqu'un qui n'a **rien** pour
les ouvrir. L'application faisait exactement ce qu'on lui demandait ; la demande
était fausse. Le domaine public de la plateforme n'est pas une donnée qu'un
téléphone connaît, et aucun déploiement ne l'avait posée.

**La racine a désormais un défaut côté serveur : `url` et `deep_link` sont
toujours là.**

- **RETIREZ CE REPLI.** Plus de « code de suivi », plus de phrase entre
  parenthèses, plus de jeton collé dans une conversation.
- Si les deux champs manquaient quand même (déploiement d'essai), **ne partagez
  rien** et dites « le partage n'est pas disponible ici » : un jeton nu n'a
  jamais aidé personne.

### 🆕 DEUX FORMES DU MÊME LIEN, ET CHACUNE A SON USAGE (v4.73.0)

| Champ | Où ça va |
|---|---|
| `url` (`https://…`) | **la conversation** : WhatsApp, SMS, n'importe quoi. S'ouvre chez tout le monde, avec ou sans l'application — la **seule** forme qu'un proche sans compte Dira peut suivre |
| `deep_link` (`dira://trip/…`) | **le QR code**, et lui seul |

⚠️ **NE METTEZ PAS `url` DANS LE QR CODE.** Scanné par l'application Dira, un
`https` ouvrirait un navigateur **par-dessus** l'application qui vient de le
lire. Et **ne collez pas `deep_link` dans une conversation** : chez qui n'a pas
l'application, un `dira://` n'ouvre **rien** — pas même un message d'erreur
compréhensible.

⚠️ **LE QR CODE SE DESSINE DANS L'APPLICATION, À PARTIR DE `deep_link`.** Le
serveur ne sert aucune image : un QR tracé sur place reste net à toutes les
tailles, fonctionne sans réseau, et peut être **enregistré ou envoyé comme
image** dans une conversation — ce qu'une URL d'image ne permet pas sans
re-télécharger.

⚠️ **LE QR CODE SERT AU PROCHE QUI EST EN FACE DE VOUS** — on se montre un
écran, on ne s'envoie pas un lien. Pour tous les autres cas, c'est `url` qu'il
faut, et c'est elle qui doit être proposée en premier.

### LA DURÉE — `minutes`, et pourquoi `0` est le bon défaut

⚠️⚠️ **`0` — OU LE CHAMP ABSENT — VEUT DIRE « JUSQU'À CE QUE J'ARRIVE », ET
C'EST LE DÉFAUT À PROPOSER EN PREMIER.** La bonne réponse à « combien de
temps ? » n'est presque jamais un nombre d'heures. Toute durée fixe est soit
trop courte — le lien meurt alors que la voiture roule encore, exactement au
moment où il servait — soit trop longue, et elle devient un traceur qui survit
au trajet.

⚠️ **`expires_at` EST TOUJOURS SERVI, même sur un lien « jusqu'à mon
arrivée ».** C'est un **plafond**, pas une promesse : le lien meurt à la fin du
trajet (plus `grace_minutes`) ou à cette date, **le premier des deux**.
Affichez-le — « ce lien s'arrête à votre arrivée, et au plus tard demain à
23 h 05 » — plutôt que de laisser croire à un lien éternel.

⚠️ **UNE DURÉE AU-DELÀ DU PLAFOND EST BORNÉE, PAS REFUSÉE.** Le serveur répond
`201` avec un `expires_at` plus proche que demandé. Lisez-le et affichez-le :
c'est ainsi que l'écran apprend ce qui a été retenu. (Ce n'est pas une erreur
du passager — c'est une application qui a proposé un bouton que le pays ne sert
plus.)

⚠️ **`label` EXISTE POUR RETIRER LE BON LIEN.** Cinq liens dans une liste ne se
distinguent que par leur heure, et personne ne se souvient duquel il a envoyé à
qui. Facultatif (40 caractères), mais proposez-le : « Maman », « Awa ».

### PARTAGER UN TRAJET **PROGRAMMÉ** — le cas où ça sert le plus

`POST /scheduled-rides/{id}/share` partage une course qui n'existe pas encore.
On prévient quelqu'un **la veille au soir**, pas depuis la voiture.

Le lien montre d'abord `status: "scheduled"` et `starts_at`, puis **devient
vivant tout seul** quand la course naît : rien à renvoyer, rien à rafraîchir du
côté du passager.

⚠️ **LE PLAFOND COURT ALORS DEPUIS LE DÉPART PRÉVU**, pas depuis le partage —
sinon un lien créé la veille serait mort avant que la voiture n'arrive.

⚠️⚠️ **SUR UNE PROGRAMMATION RÉCURRENTE, LE LIEN SUIT LA PREMIÈRE COURSE ET
MEURT AVEC ELLE.** Il ne suit pas le trajet de tous les matins. Dites-le au
passager — « ce lien suit le trajet de demain » — parce que l'intuition est
l'inverse, et que partager son trajet quotidien pour toujours serait partager
son emploi du temps.

### RETIRER UN LIEN

`POST /trip-shares/{id}/revoke` l'éteint **immédiatement**. La réponse rend
`live: false`.

- **Mettez ce bouton sur l'écran de la course**, pas dans un réglage. On retire
  un lien parce qu'on vient de changer d'avis.
- Appuyer deux fois n'est pas une erreur : le serveur répond `200` et l'heure de
  la première révocation ne bouge pas.
- Retirer **libère une place** sur `max_per_trip`.

### SAVOIR QU'ON A ÉTÉ LU — `views`, `last_view_at`

`GET /rides/{id}/shares` rend, pour chaque lien, combien de fois il a été
ouvert et quand.

⚠️ **C'EST POUR LE PASSAGER, et c'est ce qui lui dit que son message est
arrivé.** « Maman a ouvert votre lien » vaut mieux que de ne pas savoir s'il a
prévenu quelqu'un ou parlé dans le vide. Affichez-le à côté du `label`.

⚠️ **ON NE SAIT PAS QUI A OUVERT, NI D'OÙ.** Ce serait suivre celui qui
regarde, et personne ne le lui a demandé. N'inventez pas « vu par Maman » — on
sait seulement qu'un lien nommé « Maman » a été ouvert.

### LES REFUS

| `code` | HTTP | Ce que l'écran dit |
|---|---|---|
| `share_disabled` | 422 | le partage n'est pas ouvert dans ce pays — et le bouton n'aurait pas dû être là |
| `share_not_yours` | 403 | ce n'est pas votre trajet |
| `share_trip_over` | 422 | ce trajet est terminé, il n'y a plus rien à suivre |
| `share_too_many` | 409 | déjà `max_per_trip` liens actifs — **`reason` porte le nombre** ; proposez d'en retirer un |

---

### 👁️ LA PAGE DU SUIVEUR — `GET /shared/{token}`

**Aucune authentification.** Celui qui ouvre le lien n'a pas de compte Dira et
n'en aura pas : lui en demander un viderait la fonction de son sens, parce que
la personne qu'on veut prévenir est précisément celle qui n'a pas
l'application.

> **Cette section concerne la page web de suivi, pas l'application du
> passager.** Elle est ici parce que c'est le même contrat, et qu'une
> application qui propose un partage doit savoir ce que son destinataire verra.

```json
{ "kind": "ride", "status": "in_transit", "shared_by": "Nadège",
  "driver": "Jean-Pierre", "vehicle": "Toyota Yaris", "plate": "GA-2451-A",
  "stops": [ { "kind": "pickup", "label": "Hôtel du 2 Février", "geo": [1.22, 6.13] },
             { "kind": "dest", "label": "Aéroport de Lomé", "geo": [1.25, 6.16] } ],
  "stop_index": 1, "eta_stop_index": 1, "eta_at": "…",
  "position": [1.235, 6.145], "position_at": "…", "position_silent": false,
  "expires_at": "…", "over": false }
```

⚠️⚠️ **`position_silent: true` → NE DESSINEZ AUCUN POINT.** C'est la règle la
plus importante de cette page.

Un point qui n'a pas bougé depuis dix minutes ne dit pas « la voiture est
arrêtée » : il dit **« nous ne savons pas où elle est »**. À l'écran les deux se
ressemblent, et l'un rassure à tort. Écrivez « la voiture ne s'est pas signalée
depuis quelques minutes » — surtout pas la dernière position connue.

Le serveur retire la position au-delà de `stale_seconds` ; vous n'avez rien à
calculer, mais vous devez **écrire la phrase**.

⚠️ **ET « PAS ENCORE DE CHAUFFEUR » N'EST PAS « LE CHAUFFEUR SE TAIT ».**
`status: "searching"` avec `driver` vide et `position_silent: false` veut dire
qu'il n'y a pas encore de voiture. Les confondre inquiète dans un cas et
rassure dans l'autre.

⚠️ **`eta_at` EST UN INSTANT**, à décompter localement — jamais une durée à
réinterroger. Absent = la plateforme ne sait pas : n'affichez **aucun** chiffre,
surtout pas le dernier connu.

⚠️ **`over: true` + la grâce** : le trajet est fini et le lien répond encore
quelques minutes. C'est fait exprès — celui qui suivait veut lire « arrivé à
destination », l'information qu'il attendait depuis le début. Il n'y a alors
**plus de position**, et c'est voulu : la voiture continue de rouler avec son
prochain passager.

**Les trois refus, et ils ne se confondent pas :**

| `code` | HTTP | Ce que la page dit |
|---|---|---|
| `share_unknown` | 404 | ce lien n'existe pas |
| `share_expired` | 410 | ce lien a expiré, le trajet est terminé |
| `share_revoked` | 410 | ce lien a été retiré par la personne qui l'a partagé |

⚠️ **NE LES FONDEZ PAS EN « LIEN INVALIDE ».** « Expiré » dit au suiveur que le
trajet s'est bien passé et qu'il n'a rien à faire ; « retiré » que la personne a
repris la main. Un message générique lui ferait croire qu'il s'est trompé
d'adresse — et il téléphonerait à celui qu'il voulait laisser tranquille.

⚠️ **CE QUE LA PAGE NE SERT PAS, ET N'AURA JAMAIS :** le prix, l'identifiant du
passager, un numéro de téléphone, le moyen de paiement, l'historique. Le prénom
du chauffeur est servi, **pas son nom de famille** ; la plaque l'est **en
entier**, parce que c'est elle qu'on donne à la police.

⚠️ **LA RÉPONSE EST `no-store` ET `noindex`.** Ne la mettez pas en cache côté
client, ne la préchargez pas : une page de suivi en cache montre une voiture là
où elle n'est plus.

---

## 🛰️ LE RADAR — les voitures libres autour de vous, AVANT de commander (v4.74.0)

> Demandé par le propriétaire. Avant de commander, un passager veut savoir s'il
> y a quelque chose à commander. Sans cela, l'écran ne répond ni à « y a-t-il
> des voitures ? » ni à « combien de temps vais-je attendre ? », et la seule
> façon de trancher est d'appuyer sur « commander » pour voir.

```
GET /nearby-drivers?lng=&lat=            (rôle client)
```

```jsonc
{ "enabled": true,
  "mode": "auto",            // auto · on_demand
  "refresh_s": 15,           // le MINIMUM entre deux balayages
  "radius_m": 5000,          // le rayon réellement balayé
  "drivers": [
    { "position": [9.4531, 0.3924], "type": "car",  "distance_m": 420 },
    { "position": [9.4602, 0.3881], "type": "moto", "distance_m": 1180 }
  ],
  "count": 2,
  "as_of": "2026-10-24T12:00:00Z" }
```

### ⚠️⚠️ CE SONT LES VOITURES QUE LE DISPATCH APPELLERAIT, PAS « CELLES DU QUARTIER »

Le rayon est **celui de l'appel** — et le rayon **maximum** quand il est fluide
—, et la liste est celle des véhicules **libres** du même vivier, lus chez le
même service que le dispatch.

C'est ce qui rend le radar utilisable : **ne dessinez aucune autre source**, et
ne filtrez pas la liste par classe. Un point montré est un point appelable.

⚠️ **ET L'INVERSE EST FAUX : UNE LISTE VIDE N'EST PAS « PERSONNE NE VIENDRA ».**
Le dispatch cherche **plus longtemps** que le radar ne montre (plusieurs vagues,
un rayon qui s'élargit, des chauffeurs qui se libèrent). Écrivez « aucune
voiture libre pour l'instant » et **laissez commander** : un écran qui
découragerait la commande ferait perdre des courses que la plateforme aurait
servies.

### ⚠️⚠️ AUCUN CHAUFFEUR N'EST IDENTIFIÉ — ET PAS MÊME PAR UN IDENTIFIANT D'AFFICHAGE

Un point porte **trois champs**, et il n'y en aura pas d'autres : `position`,
`type`, `distance_m`. Pas d'identifiant, pas de plaque, pas de nom.

**Deux balayages ne se recollent donc pas.** C'est délibéré — un radar qui
numérote ses points est un traceur de chauffeurs, et les chauffeurs n'ont rien
choisi (c'est l'exact inverse du partage de trajet, où c'est le passager qui
expose sa propre position).

**Ce que cela impose à l'écran :** dessinez un **état**, pas un mouvement.
N'essayez pas d'animer un point d'un balayage au suivant, de compter les
voitures qui arrivent ou partent, ni de suivre « la voiture la plus proche » :
rien ne garantit que le point le plus proche de deux balayages soit le même
véhicule.

### Les quatre champs qui se lisent de travers

| Champ | Ce qu'il est | Ce qu'il n'est PAS |
|---|---|---|
| `type` | le **type de véhicule** du suivi (`car`, `moto`, `tricycle`…) — de quoi choisir un **pictogramme** | ni la classe tarifaire, ni le mode de course. Une voiture libre n'a pas de mode : elle en a plusieurs possibles. ⚠️ **N'annoncez aucun prix à partir de ce champ** |
| `distance_m` | la distance **à vol d'oiseau**, telle que l'index la rend | ⚠️ **pas une durée.** La route peut valoir le triple (un pont, un sens unique) : un « 2 min » déduit d'ici serait démenti par la première commande |
| `count` | le nombre de points **servis**, borné par le pays | ⚠️ **pas le nombre de voitures disponibles.** « 12 voitures disponibles » sur un plafond de 12 est un chiffre faux dès la treizième. Écrivez « des voitures sont disponibles », ou rien |
| `radius_m` | le rayon **balayé** | pas une promesse de temps d'attente |

### `mode` — automatique ou à la demande

- **`auto`** : balayez tant que la carte est à l'écran, **en respectant
  `refresh_s`**, et **suspendez** dès qu'elle ne l'est plus (onglet caché,
  écran éteint, autre page). Un radar qui tourne en arrière-plan vide une
  batterie et un forfait pour une carte que personne ne regarde.
- **`on_demand`** : **aucun balayage spontané.** Un geste du passager, un
  balayage. ⚠️ Ce n'est pas « le radar en moins bien » : c'est le choix d'un
  pays dont le réseau coûte cher, ou dont l'offre est mince aux heures creuses.

⚠️ **`refresh_s` EST UN MINIMUM, PAS UNE CADENCE À TENIR.** Balayer plus vite ne
montrera pas autre chose — un véhicule libre se déplace peu — et se fera border
par la limitation de débit du service.

### ⚠️ `enabled: false` → L'ÉCRAN N'EXISTE PAS

Pas de carte vide, pas de bouton grisé, pas de message : **rien**. C'est le
**défaut de la plateforme** — montrer la position de chauffeurs qui n'ont rien
demandé est une décision que chaque pays prend pour lui.

⚠️ **ET CE N'EST PAS UNE ERREUR À TRAITER** : la route répond `200` avec
`enabled: false` et une liste vide, précisément pour que l'application puisse
cacher l'écran **sans** afficher de panne.

### Le point à envoyer

Envoyez le **point de départ envisagé** — le centre de la carte, ou le départ
que le passager vient de saisir —, pas sa position GPS si elle en diffère :
c'est ce point-là que le dispatch utilisera, et donc celui dont le vivier
compte.

⚠️ **LA POLITIQUE EST CELLE DU PAYS DE CE POINT**, pas celle du pays du compte :
un passager togolais à Libreville lit le radar du Gabon — ou ne lit rien, si le
Gabon l'a éteint. Il n'y a pas de `?near=` ici : **les coordonnées sont la
question**.

### Ce qui ne répond pas, et ce que ça veut dire

| | |
|---|---|
| `422` avec `fields: ["lng","lat"]` | point manquant, illisible, ou hors du globe |
| `403` | **rôle chauffeur** : le radar est réservé au passager. Un chauffeur qui le lirait verrait ses concurrents en direct |
| liste vide avec `enabled: true` | aucune voiture libre **à portée**, ou le suivi n'a pas répondu. ⚠️ Les deux se disent pareil et **aucun des deux n'est une panne à signaler** |

⚠️ **LA RÉPONSE EST `no-store`.** Ne la mettez pas en cache : un radar en cache
montre des voitures là où elles ne sont plus.

### 🆕 ⚠️⚠️ APPUYER SUR UNE VOITURE ET L'APPELER (v4.75.0)

> Demandé par le propriétaire : « cliquez sur la map l'icône d'un chauffeur et
> l'appeler (chauffeur anonyme tant qu'il n'a pas décroché) ».

```
POST /rides/{id}/pick   { "ref": "<le `ref` de l'épingle>" }   → 200 Ride
```

⚠️ **LE CORPS NE PORTE PAS D'IDENTIFIANT DE VÉHICULE, ET IL NE PEUT PAS EN
PORTER** : le radar n'en sert aucun. Il porte le **`ref` de l'épingle** sur
laquelle le passager a appuyé — et ce jeton n'est servi que si le pays a ouvert
le geste. **Pas de `ref` sur un point ⇒ pas de bouton.**

#### Ce que le jeton est, et ce qu'il n'est pas

| | |
|---|---|
| Ce qu'il est | une **empreinte signée** du véhicule et d'une **fenêtre de deux minutes** |
| Ce qu'il permet | appeler **cette** voiture en premier |
| Ce qu'il ne permet pas | ⚠️ **suivre une voiture** : il change à chaque fenêtre, donc deux balayages ne se recollent pas. ⚠️ **lire quoi que ce soit** : aucune route ne rend d'information contre un `ref` |
| Où il se résout | ⚠️ **parmi les voitures encore LIBRES autour de votre départ**, et nulle part ailleurs : un jeton d'une autre ville ou d'un autre passager ne désigne rien |

#### ⚠️⚠️ « EN PREMIER », PAS « SEULEMENT ELLE »

La voiture choisie est sonnée **la première**. Si elle ne répond pas, **la
recherche continue d'elle-même** auprès des plus proches : rien à renvoyer.

C'est délibéré. Un appel qui n'appellerait **que** la voiture choisie
laisserait le passager attendre le silence d'un chauffeur en course pendant que
trois autres passaient devant chez lui. **Dites-le à l'écran** : « nous
appelons d'abord cette voiture ».

#### Les deux refus, et ce qu'ils demandent d'afficher

| Refus | Quand | Ce que l'écran dit |
|---|---|---|
| `409 pick_driver_gone` | la voiture **n'est plus libre** — prise, hors ligne, déjà sonnée pour quelqu'un d'autre, ou jeton trop vieux | « cette voiture vient d'être prise — choisissez-en une autre, ou laissez-nous appeler les plus proches ». ⚠️ **Ce n'est pas une panne** : rebalayez et proposez la suite |
| `422 pick_disabled` | le pays n'ouvre pas le geste | **ne devrait jamais arriver** : sans `ref`, aucun bouton. Si vous le recevez, c'est que l'écran a fabriqué un jeton |
| `409 not_searching` | la course a été prise, terminée ou annulée entre-temps | relire la course |

#### Ce que le passager peut faire ensuite — les trois gestes, et rien d'autre

| Geste | Route | ⚠️ |
|---|---|---|
| **Rappeler** (relancer la recherche) | `POST /rides/{id}/relaunch` | masqué tant que `dispatch_state` vaut `calling` (`409 search_running`) |
| **Appeler une autre voiture** | `POST /rides/{id}/pick` avec une **nouvelle** épingle | le radar a rebalayé : les `ref` précédents peuvent être périmés |
| **Ne rien faire** | — | ⚠️ **c'est souvent le bon conseil** : la recherche continue seule, s'élargit, et finit par trouver. Un écran qui pousse à agir fait annuler des courses que la plateforme aurait servies |

⚠️⚠️ **LE CHAUFFEUR RESTE ANONYME TANT QU'IL N'A PAS ACCEPTÉ.** Avant son
acceptation, vous n'avez qu'un point et un genre de véhicule — ni nom, ni
plaque, ni photo, ni note. **N'inventez pas de carte de chauffeur** « en cours
d'appel » : elle serait vide, et une carte vide se lit comme une panne. La
carte (`driver`) arrive avec l'acceptation, d'un coup, comme avant.

---

## 🔒 CE QUE VOUS AVEZ LE DROIT D'AFFICHER DE L'AUTRE PARTIE (v4.49.0)

**C'est l'exploitation qui décide, pays par pays et métier par métier** —
Paramètres › Confidentialité de la console. Vous n'avez rien à arbitrer, et
surtout rien à deviner : **le serveur n'envoie pas ce qu'on n'a pas le droit de
montrer.**

`GET /rides/{id}` → `driver` porte ce qui est ouvert de votre chauffeur :

```jsonc
{ "driver": {
    "id": "…", "rides_count": 1240,
    "name": "Kofi A.",             // déjà masqué au niveau choisi, ou absent
    "rating_avg": 4.8, "rating_count": 530,  // absents si la note est fermée
    "vehicle": { "description": "Toyota Avensis rouge", "license_plate": "TG-4417", … },
    "contact": {                   // ⚠️ ABSENT quand rien n'est ouvert — le défaut en course
      "first_name": "…", "last_name": "…", "avatar_url": "…", "gender": "…",
      "phone": "+228…",            // ⚠️ présent dès qu'il est AFFICHABLE **ou** COMPOSABLE
      "show_phone": false, "direct_call": true, "in_app_alert": false } } }
```

⚠️ **`contact` ABSENT EST LE CAS NORMAL EN COURSE**, et ce n'est pas une panne :
par défaut, aucun numéro ne s'échange — la conversation est le canal. Ne
construisez pas un écran qui suppose sa présence.

⚠️ **LE VÉHICULE N'EST JAMAIS FERMÉ**, et c'est voulu : une plaque, une marque et
une couleur ne nomment personne, et c'est ce que vous guettez au trottoir. C'est
ce qui reste quand tout le reste est fermé — y compris si le serveur n'a pas pu
lire la politique, où vous ne recevrez QUE la voiture.

⚠️ **NE PROMETTEZ PAS CE QUE VOUS N'AVEZ PAS.** Si `direct_call` est faux, pas de
bouton « Appeler le chauffeur » grisé ni d'écran qui le mentionne : un bouton
qui n'existe pas se cache, il ne se désactive pas.

⚠️ **UN CHAMP ABSENT N'EST PAS UNE PANNE.** C'est un champ que l'exploitation
n'a pas ouvert dans ce pays. Affichez votre propre libellé — « votre chauffeur » —
comme pour toute donnée manquante, et **ne réessayez pas** : rien ne viendra.

⚠️ **N'ÉCRIVEZ AUCUNE RÈGLE EN DUR.** Pas de « si le pays est le Togo », pas de
« si c'est une course alors pas de téléphone ». Ces décisions changent sans
redéploiement de votre application, et une règle recopiée chez vous
contredirait celle du serveur sans que personne ne sache laquelle croire.

### ⚠️ TROIS PERMISSIONS DE CONTACT, ET CE NE SONT PAS LES MÊMES

| Champ | Ce qu'il autorise |
|---|---|
| `show_phone` | **afficher** le numéro à l'écran |
| `direct_call` | proposer un **bouton d'appel** |
| `in_app_alert` | faire **sonner** l'application de l'autre |

⚠️ **`phone` PRÉSENT NE VEUT PAS DIRE « AFFICHABLE ».** Le numéro arrive dès
qu'il peut être affiché **ou composé** — un bouton d'appel a besoin du numéro
pour le composer. **Lisez `show_phone` avant de l'écrire à l'écran.** Un
`phone` affiché parce qu'il était là annulerait le réglage dans l'écran même
qui le lit, et c'est l'erreur que ce paragraphe existe pour empêcher.

⚠️ **`direct_call: true` AVEC `show_phone: false`** est le cas le plus
courant : un bouton « Appeler », et le numéro **nulle part** — ni à l'écran, ni
dans un champ copiable, ni dans une capture d'écran. Composez-le sans le
rendre. Nous savons qu'il reste dans le journal d'appels du téléphone : c'est
de la friction, pas du secret, et c'est assumé côté serveur. Ne compensez pas
en l'affichant « puisqu'il est de toute façon visible ».

⚠️ **TOUT FERMÉ N'EST PAS UNE IMPASSE** : la **conversation** reste le canal, et
elle existe précisément pour que ces deux-là se parlent sans rien s'échanger.
Un écran sans nom ni numéro doit mener à elle, pas à un cul-de-sac.

### Les quatre niveaux de `name`

Le nom arrive **déjà masqué** — vous n'avez rien à couper. `full` (« Awa
Diallo »), `first` (« Awa »), `initials` (« A. D. »), ou **absent**.

⚠️ **NE RECONSTRUISEZ JAMAIS UN NOM** à partir de `first_name` et `last_name` :
ces deux champs ont leurs propres interrupteurs, et les concaténer rendrait un
nom complet là où l'exploitation n'a ouvert que des initiales.

---

## 7. Parler au chauffeur — sans échanger de numéros

```
GET  /rides/{id}/messages          → { items, unread }
POST /rides/{id}/messages          { "body": "je suis au portail bleu" }
POST /rides/{id}/messages/read
```

#### 🆕 🎙️ LES MESSAGES VOCAUX — pour qui ne lit pas (v4.94.0)

```
POST /rides/{id}/messages          (multipart/form-data)
  audio=<fichier>   seconds=7   language=wo (facultatif)
→ 201 { "id", "from", "kind": "voice", "body": "…", "audio_url": "…", "seconds": 7 }

GET  /rides/{id}/messages
→ { "items": [ { …, "kind": "text" | "voice", "audio_url"?, "seconds"? } ],
    "voice": { "enabled": true, "max_seconds": 60 } }
```

⚠️⚠️ **NE MONTREZ LE MICRO QUE SI `voice.enabled`.** Un bouton d'enregistrement
sur un déploiement sans stockage ni transcription produit un échec **après** que
la personne a parlé — le pire moment pour l'apprendre, et surtout pour quelqu'un
qui ne lit pas le message d'erreur. `voice` est **absent** là où les vocaux ne
marchent pas, et il est servi **à chaque lecture** : il peut disparaître en cours
de course si le stockage tombe.

| Champ | Ce que c'est |
|---|---|
| `kind` | `text` ou `voice`. ⚠️ **Toujours servi**, même sur les messages d'avant : ne devinez pas le genre à la présence d'`audio_url` |
| `audio_url` | où réécouter. ⚠️ **Lisible sans en-tête** : un lecteur audio ne porte pas votre `Authorization`. L'adresse est signée et datée |
| `seconds` | la durée **déclarée par le téléphone de l'envoyeur**, pour dessiner la barre avant de télécharger le fichier |
| `body` | ⚠️ **la TRANSCRIPTION du vocal** — voir ci-dessous |

⚠️⚠️ **`body` PORTE LA TRANSCRIPTION, ET C'EST VOULU.** Une app qui ne connaît
pas encore `kind` affiche alors du texte au lieu de rien. Côté vous : affichez la
barre d'écoute **et** la transcription sous elle — c'est ce qui permet au
destinataire de décider s'il écoute maintenant.

⚠️ **`body` peut être VIDE sur un vocal.** La transcription a échoué (service
indisponible, langue non servie) et **le message part quand même** : refuser
priverait de parole précisément les gens pour qui cette fonction existe. Affichez
alors « Message vocal » et la barre, pas un blanc.

#### Envoyer

- `multipart/form-data` sur **la même route** que le texte — c'est le
  `Content-Type` qui choisit. Pas de seconde route à gérer.
- `audio` : **m4a, aac, mp3, ogg, opus, webm, 3gp**. ⚠️ **Pas de WAV** — un vocal
  d'une minute y pèse dix mégaoctets, et c'est le réseau de quelqu'un qui paie.
- `seconds` : **obligatoire**, la durée que vous avez mesurée, 1 à `max_seconds`.
- `language` : la langue **parlée** si vous la connaissez (`wo`, `ee`, `shu`…).
  ⚠️ Pas l'`Accept-Language` : celui-ci dit la langue de l'**interface**, et un
  téléphone réglé en français n'empêche pas son propriétaire de parler wolof.

| Refus | Quand | Que faire |
|---|---|---|
| `413 audio_too_large` | au-delà de 2 Mio | encoder en opus ou AAC mono |
| 🆕 `422 audio_unsupported_type` | format non accepté — `reason` porte le type envoyé | **réenregistrer** dans un format accepté ; inutile de réessayer tel quel |
| 🆕 `422 audio_required` | la partie `audio` n'est pas arrivée | **réessayer** : le fichier s'est perdu en chemin |
| 🆕 `422 voice_too_long` | plus long que `voice.max_seconds` | **réenregistrer** plus court |
| `422` `fields: ["seconds"]` | `seconds` absent ou illisible | un défaut de votre code : la durée est obligatoire |
| `503 voice_messages_unavailable` | les vocaux ne marchent pas ici. ⚠️ C'est le **filet** : `voice.enabled` est la règle |
| `422 message_blocked` | la **transcription** contient une menace. ⚠️ Oui, la modération lit les vocaux |

⚠️ **CES QUATRE REFUS AVAIENT LE MÊME CODE (v4.96.0).** Jusqu'ici, un format
refusé, un fichier manquant et une durée trop longue rendaient tous les trois
`validation_failed` avec un `fields` pour seule différence : impossible de
choisir entre « recommencez, c'était trop long », « votre téléphone enregistre
dans un format qu'on ne prend pas » et un défaut de votre propre code. Ces
phrases n'ont rien à voir — et la première s'adresse à quelqu'un qui ne sait pas
lire, pour qui les vocaux existent.

⚠️ **La borne n'est PAS dans le refus**, et c'est volontaire : elle est servie à
chaque lecture de la conversation (`voice.max_seconds`). N'écrivez pas « une
minute » en dur — un pays peut la changer.

⚠️ **`audio_unsupported_type` porte le type fautif dans `reason`.** Mettez-le
dans vos traces : les téléphones d'entrée de gamme enregistrent dans des formats
qu'on découvre un par un, et c'est cette ligne qu'on lit pour décider d'en
ajouter un à la liste.

⚠️ **La modération lit la transcription**, exactement comme du texte : une menace
dite à voix haute est une menace. Un vocal **non transcrit**, en revanche, lui
échappe — le signalement (`POST …/messages/{id}/report`) reste la seule prise sur
ceux-là.

⚠️ **Un message retiré par la modération perd son audio AVEC son corps** :
`hidden: true`, `body` et `audio_url` vides. Affichez « message retiré », pas un
lecteur qui ne joue rien.

#### La notification

`chat_message` quand il y a une transcription (corps = la transcription), et
**`chat_voice_message`** quand il n'y en a pas (corps = « Message vocal »). Les
deux portent `data.kind` et, pour un vocal, `data.seconds` — de quoi dessiner le
bon aperçu **avant** d'ouvrir la conversation.

Une conversation par course, entre ses deux parties, sans téléphone échangé.
`items` sont les messages du plus ancien au plus récent (`from`: `client` |
`driver`, `body`, `created_at`) ; `unread` compte ceux que vous n'avez pas
encore marqués lus. `body` : 1 à 1000 caractères.

| Refus | Quand |
|---|---|
| `409 no_driver_yet` | personne n'a encore pris la course — **lisible, pas écrivable** |
| `409 conversation_closed` | plus de **deux heures** après l'arrivée |
| `403 forbidden` | cette conversation n'est pas la vôtre |

> La saisie se désactive sur les deux premiers ; l'historique **reste lisible**.
> Deux heures après l'arrivée : « j'ai oublié mon téléphone sur la banquette »
> se dit dans la minute qui suit, pas un mois plus tard.

> 🔔 **Le message est POUSSÉ à l'autre côté (v4.8.0).** Il n'y a pas de
> socket de conversation sur les courses : quand l'autre application n'est
> pas sur l'écran de la course, c'est la notification `chat_message` qui
> l'atteint — titre « Nouveau message », corps = le texte, **jamais le nom
> de l'expéditeur** — avec `data: { type: "chat_message", ride_id }`. Ouvrez
> la conversation de cette course dessus et relisez
> `GET /rides/{id}/messages`. Sur l'écran de la course, sondez la
> conversation toutes les 5 s tant qu'elle est ouverte ; une notification
> reçue pendant ce temps ne s'affiche pas deux fois — c'est le même message.

### 🛡️ LA MODÉRATION — l'automate d'abord, l'humain ensuite (v4.66.0)

Chaque message passe par un filtre avant d'être relayé. Deux issues, et elles
ne se ressemblent pas.

| Gravité | Ce qui se passe | Ce que vous voyez |
|---|---|---|
| **insulte** | **le message part**, et il est signalé à l'exploitation | `201`, rien de particulier |
| **menace** | **le message ne part pas** | `422 message_blocked` |

⚠️⚠️ **UNE INSULTE PASSE, ET C'EST UNE DÉCISION, PAS UN TROU.** Deux personnes
qui s'énervent sur un prix s'insultent ; les bloquer ne calme personne et fait
croire à une panne de réseau — après quoi on réécrit la même chose autrement.
Ce qui ne passe pas, c'est la **menace sur la personne** et l'**injure
discriminatoire** : ce qui est en jeu n'est plus le confort de la conversation.

⚠️ **N'ÉCRIVEZ AUCUNE LISTE DE MOTS DANS L'APPLICATION.** Les mots qui blessent
changent d'un pays à l'autre et d'une année à l'autre ; l'exploitation ajoute et
retire depuis la console, sans redéploiement. Une liste embarquée vieillirait
dans votre binaire, et deux applications filtreraient différemment la même
conversation.

**Quand le message est refusé (`422`, `code: "message_blocked"`) :**

⚠️⚠️ **GARDEZ LE TEXTE DANS LA ZONE DE SAISIE.** C'est le seul point de cette
section qui change tout. Vider le champ sur un refus fait perdre ce qu'on
venait d'écrire, et la personne recommence — en pire. Affichez la phrase du
serveur **au-dessus** de sa saisie intacte, et laissez-la corriger.

⚠️ **N'AFFICHEZ PAS LE MESSAGE DANS LE FIL.** Il n'a pas été envoyé : une bulle
grise « non envoyé » laisse croire à un problème de réseau, et la personne
retente dix fois. Le refus est définitif tant que le texte ne change pas.

⚠️ **N'ESSAYEZ PAS DE DEVINER CE QUI A ÉTÉ REFUSÉ.** Le serveur ne dit pas quel
mot a mordu, et c'est délibéré : le dire serait expliquer comment contourner le
filtre.

**`hidden: true` — un message retiré par un modérateur :**

Le corps est **vide** et la ligne **reste** dans le fil, avec son heure et son
auteur.

⚠️ **AFFICHEZ « message retiré », PAS UN BLANC, ET NE SUPPRIMEZ PAS LA LIGNE.**
L'autre partie a vu qu'il y avait quelque chose là ; faire disparaître le trou
fabrique une conversation qui n'a jamais eu lieu, et c'est exactement ce qu'on
ne veut pas dans un échange qui peut finir devant le support.

---

## 7 bis. Noter et remercier — après la course (v3.8.0)

À `completed`, le passager reçoit **`ride_rate_prompt`** (push, données
`{ type: "ride_rate_prompt", ride_id }`) : ouvrir l'écran de fin de course
dessus, avec les deux gestes.

```
POST /rides/{id}/rating   { "score": 5, "comment": "…" }      // comment facultatif, 1000 car. max
POST /rides/{id}/tip      { "amount_xof": 500 }               // 100 ≤ montant ≤ 50 000
```

Les deux rendent la **course** (`201`) : `rating` porte la note que **vous**
venez de laisser, `tip_xof` / `tipped_at` le pourboire passé. Sur
`GET /rides/{id}` et l'historique, `rating` absent = **pas encore notée** —
c'est ce qui affiche « notez votre course » ; `tip_xof` absent = pas de
pourboire.

**La note** va au **profil du chauffeur** (`driver.rating_avg`,
`driver.rating_count`, ses avis sur `GET /agents/{driver.id}/ratings`). Une
fois par course, dans les **7 jours** qui suivent l'arrivée. Le chauffeur note
aussi le passager — vous ne lisez jamais sa note, il ne lit jamais la vôtre :
`rating` est toujours **la vôtre**.

**Le pourboire** part du **solde Dira** (`GET /wallet`) et arrive au chauffeur
**sans commission**, en une fois par course. Il est prévenu à l'instant.

| Refus | Quand |
|---|---|
| `409 ride_not_completed` | la course n'est pas terminée — masquer les deux gestes avant |
| `409 already_rated` · `409 already_tipped` | déjà fait : afficher ce qui a été laissé, pas le formulaire |
| `409 rating_window_closed` · `409 tip_window_closed` | plus de 7 jours |
| `409 no_driver` | course sans chauffeur (annulée en recherche) |
| `402 insufficient_funds` | solde Dira insuffisant — **la course reste sans pourboire** : proposer la recharge (**`POST /payments/initiate`**, `purpose: "wallet_topup"` — ⚠️ **pas** `/wallet/purchase`, qui achète des jetons et refuse un client), puis réessayer |
| `422` | note hors de 1..5, montant hors de 100..50 000 |

> ⚠️ **Pas de pourboire en espèces ni par mobile money ici.** Un pourboire en
> espèces se donne dans la voiture et ne regarde pas la plateforme ; un
> pourboire par opérateur attendrait le rappel pour un geste qui doit être
> immédiat. L'écran propose le solde Dira, et la recharge s'il manque.

---

## 🆘 LE BOUTON D'ALERTE — SOS (v4.56.0)

Un bouton, dans **toutes** les applications. Il ne se coupe pas, il ne se règle
pas, il n'a pas de condition : ce qui se règle, c'est ce qui le propose **sans
qu'on le touche**.

```
GET  /sos/settings          → ce que ce pays décide (détections, délai, numéros)
POST /sos                   → DÉCLENCHER
GET  /sos/me                → mon alerte en cours (200 avec `alert: null` s'il n'y en a pas)
POST /sos/{id}/position     → où je suis MAINTENANT
POST /sos/{id}/cancel       → « fausse alerte »
```

> ⚠️ **AU SOCLE** (`…/api/v1/sos`, **sans** `/vtc` ni `/food`). Le même bouton
> pour un passager de course, un client de livraison, un chauffeur et un
> livreur : l'exploitation doit les voir dans la même file.

### ⚠️ LA RÈGLE QUI GOUVERNE TOUT CE QUI SUIT

**Une alerte ne se perd jamais.** `POST /sos` n'a **aucun champ obligatoire** —
pas même la position.

```jsonc
POST /sos
{ "source": "button",        // button | shake | crash | voice. Absent = button
  "confirmed": true,         // une DÉTECTION validée par la personne
  "lat": 6.1319, "lng": 1.2255, "accuracy_m": 18,
  "battery": 7,              // le pourcentage de batterie
  "vertical": "vtc",         // vtc | food, quand il y a une opération
  "ride_id": "…",            // ou delivery_id
  "note": "un homme me suit" }
→ 201  { "id": "…", "status": "open", "trigger": "bouton pressé", … }
```

⚠️ **ENVOYEZ CE QUE VOUS AVEZ, TOUT DE SUITE — N'ATTENDEZ RIEN.** C'est la
consigne la plus importante de cette section : n'attendez **pas** un point GPS
pour envoyer. Le serveur accepte une alerte **sans position**, et « on ne sait
pas où il est » est une alarme qu'un opérateur traite en premier — pas une
requête à compléter. Attendre un fix de dix secondes au fond d'un parking
souterrain, c'est perdre les dix secondes qui comptent. Envoyez d'abord,
**poussez la position ensuite** (`POST /sos/{id}/position`).

⚠️ **ET RÉESSAYEZ, EN BOUCLE, JUSQU'À UN `2xx`.** Le seul échec possible de
cette route est un serveur ou un réseau en panne. Mettez l'appel dans la file
que vous avez déjà pour le hors-ligne, mais **en tête** et sans attendre la
fenêtre de synchronisation : une alerte remise trois minutes plus tard ne sert
plus à personne.

⚠️ **N'AJOUTEZ AUCUNE VALIDATION DE VOTRE CÔTÉ.** Pas de « position
obligatoire », pas de « choisissez un motif », pas d'écran de confirmation à
deux champs. Tout ce qui est bancal est **corrigé par le serveur**, jamais
rejeté : source inconnue → `button`, coordonnées impossibles → position
ignorée (l'alerte reste), note trop longue → coupée proprement.

### 📍 LE DOUBLE APPUI N'EST PAS UNE ERREUR

Quelqu'un qui panique appuie cinq fois. Le serveur rend **la même alerte** et y
ajoute la position — une seule alerte ouverte par personne.

⚠️ **NE DÉSACTIVEZ DONC PAS LE BOUTON** après le premier appui, et n'affichez
pas « déjà envoyé » comme une erreur. Laissez-le actif : appuyer encore
**améliore** l'alerte (une position de plus) et rassure. Ce que vous devez
montrer, c'est que c'est **parti** — pas que c'est interdit.

### 🧭 POUSSEZ LA POSITION PENDANT TOUTE L'ALERTE

```
POST /sos/{id}/position   { "lat": …, "lng": …, "accuracy_m": … }   → 204
```

⚠️ **L'OPÉRATEUR A BESOIN DE SAVOIR OÙ LA PERSONNE EST, PAS OÙ ELLE A APPUYÉ.**
Un véhicule continue de rouler. Poussez toutes les **5 à 10 secondes** tant que
l'alerte est vivante, plus souvent que votre cadence habituelle : c'est le seul
moment du produit où la fraîcheur d'une position vaut la batterie qu'elle coûte.

Le serveur ignore silencieusement un point envoyé après la fermeture (`204`) :
**ne traitez pas cela comme une erreur** et n'arrêtez pas votre boucle sur un
refus — relisez `GET /sos/me` pour savoir si c'est fini.

### 🔁 RETROUVEZ L'ÉCRAN APRÈS UN REDÉMARRAGE — `GET /sos/me`

```
GET /sos/me → { "alert": { … } }   ou   { "alert": null }
```

⚠️ **APPELEZ-LA AU DÉMARRAGE, TOUJOURS.** Un téléphone qui redémarre après un
choc, une application tuée par le système, un réseau qui revient : sans cela, la
personne ne sait plus si son alerte est partie, et elle appuie encore — ou, bien
pire, elle croit avoir appelé alors que non.

⚠️ `200` **avec `alert: null`**, et non `404` : « je n'ai pas d'alerte en
cours » est le cas NORMAL.

### 🛑 ANNULER — et ce que ça ne fait pas

```
POST /sos/{id}/cancel → l'alerte, refermée
```

⚠️ **L'ALERTE N'EST PAS SUPPRIMÉE, ET L'EXPLOITATION LA VOIT ENCORE PENDANT
QUINZE MINUTES.** Dites-le à la personne : « l'exploitation a été prévenue et
vous rappellera peut-être pour vérifier ». Une annulation peut être
**contrainte** — c'est le scénario même que ce bouton existe pour couvrir —, et
promettre que « tout est effacé » serait un mensonge.

⚠️ **PAS DE DEUXIÈME CONFIRMATION POUR ANNULER.** Un appui, c'est annulé. Un
« êtes-vous sûr ? » à ce moment-là fait rester une fausse alerte dans la file de
l'exploitation, qui appelle pour rien, et la prochaine vraie sera prise moins au
sérieux.

### ⚙️ CE QUE LE PAYS DÉCIDE — `GET /sos/settings`

```jsonc
{ "button": true,            // TOUJOURS true
  "shake": true,             // la secousse propose l'alerte
  "crash": true,             // la détection de choc la propose
  "voice": false,            // le mot-clé vocal — ÉTEINT par défaut
  "countdown_seconds": 10,   // le délai d'annulation d'une DÉTECTION
  "calls_back": true }       // ⚠️ CE QUE VOUS DEVEZ PROMETTRE : on vous rappelle
```

⚠️ **RELISEZ-LA À CHAQUE OUVERTURE**, et n'écrivez aucune de ces valeurs en
dur : elles changent depuis la console, sans redéploiement, et un pays peut
couper une détection du jour au lendemain.

### ⚠️⚠️ IL N'Y A AUCUN NUMÉRO À COMPOSER, ET CE N'EST PAS UN OUBLI (v4.58.0)

**Le téléphone de la personne en danger ne compose rien.** L'alerte part au
**SERVICE CLIENT** : un opérateur l'appelle, et c'est **lui** qui appelle les
secours s'il le faut.

Cette route **ne sert donc aucun numéro**, et il n'y a pas de champ `numbers` à
lire. **N'en inventez aucun** — pas de 112, pas de numéro codé dans
l'application, pas de repli « au cas où », pas de liste tirée d'un site. Un
écran qui proposerait « appeler la police » enverrait quelqu'un composer un
numéro que nous ne lui avons pas donné.

⚠️ **CE QUE VOUS AFFICHEZ À LA PLACE** : « **Le service client a été prévenu et
va vous appeler.** » C'est `calls_back`, et c'est la seule chose que la personne
cherche à savoir après avoir appuyé.

⚠️ **POURQUOI C'EST MIEUX, ET PAS SEULEMENT DIFFÉRENT** — dites-le à votre
équipe produit, parce que la question viendra :
>
> - **Quelqu'un répond toujours.** Les secours d'un pays peuvent sonner dans le
>   vide : un relevé mené en Guinée en 2024 a composé les numéros officiels un
>   par un et en a trouvé plusieurs **hors service**. Un opérateur qui tombe sur
>   un numéro mort l'entend, raccroche et prend le suivant ; une personne en
>   panique, non.
> - **L'opérateur sait ce qu'il dit.** « Un chauffeur au carrefour X, voiture
>   grise immatriculée AB-1234-CD, course en cours » se transmet. Quelqu'un de
>   terrorisé ne décrit pas sa position.
> - **Le premier appel est souvent le bon** : téléphone tombé, dos-d'âne,
>   dispute déjà calmée. Appeler la police pour ça la ferait cesser de nous
>   écouter.

### 🫨 LES DÉTECTIONS — ⚠️ ELLES PROPOSENT, ELLES N'ENVOIENT PAS

C'est la règle la plus importante de cette partie, et elle vaut pour les trois
(`shake`, `crash`, `voice`).

**Une détection ouvre un compte à rebours de `countdown_seconds`, visible et
sonore, avec UN bouton « Annuler ». À l'expiration, l'alerte part** avec
`confirmed: false`.

```
secousse / choc / mot-clé  →  écran plein, compte à rebours, vibration + son
                           →  « Annuler »   : rien ne part
                           →  « Envoyer »   : POST /sos  { confirmed: true }
                           →  rien du tout  : POST /sos  { confirmed: false }
```

⚠️ **POURQUOI UN COMPTE À REBOURS, ET NON UN ENVOI IMMÉDIAT.** Un dos-d'âne,
un téléphone qui tombe, un sac qu'on pose : l'envoi direct remplirait la file de
faux, l'opérateur apprendrait à les ignorer, et la vraie alerte se noierait
dedans. **Un faux positif traité comme une vraie alerte coûte plus cher qu'un
faux positif annulé.**

⚠️ **POURQUOI IL PART QUAND PERSONNE N'ANNULE.** C'est tout l'intérêt : après
un choc violent, **personne n'annule parce que personne ne peut**. Un compte à
rebours qui s'arrêterait sans rien envoyer serait un bouton de plus, pas une
détection.

⚠️ **`confirmed: false` EST PLUS GRAVE, PAS MOINS — et dites-le-vous bien, parce
que l'intuition dit l'inverse.** Le serveur le traite comme tel (`grave: true`,
et `trigger` vaut `"choc détecté, PERSONNE N'A ANNULÉ"`). Ne le présentez donc
jamais à la personne comme « envoyé par erreur », et ne le rangez pas plus bas
dans vos écrans.

⚠️ **PENDANT LE COMPTE À REBOURS, PRÉPAREZ TOUT.** Démarrez l'acquisition GPS,
lisez la batterie, composez le corps de la requête : à l'expiration, l'envoi
doit partir en une milliseconde. Un compte à rebours qui finit sur « recherche
du GPS… » a gaspillé dix secondes.

⚠️ **UN SEUL BOUTON PENDANT LE COMPTE À REBOURS : ANNULER.** Pas de champ de
texte obligatoire, pas de choix de motif, pas de liste de contacts. La note est
facultative, et elle s'ajoute **après** l'envoi si la personne en a le temps.

#### Les trois, et ce que chacune coûte

| Détection | Ce qu'elle écoute | Ce qu'elle coûte |
|---|---|---|
| `shake` | l'accéléromètre déjà allumé | rien — aucune permission, aucune batterie en plus |
| `crash` | un pic d'accélération | rien de plus que `shake` |
| `voice` | **le micro, en permanence** | une permission, de la batterie, et une surveillance |

⚠️ **LA SECOUSSE EST LA PLUS UTILE DU LOT, et pas la plus gadget.** Elle existe
pour le cas où **on ne peut pas regarder l'écran** : téléphone en poche, main
sur le volant, quelqu'un à côté qui ne doit pas voir. C'est précisément quand
viser un bouton est impossible qu'on en a besoin. Réglez le seuil pour qu'une
marche rapide ou un nid-de-poule ne suffise pas : trois secousses franches,
pas un mouvement.

⚠️ **LE VOCAL NE S'ALLUME PAS EN SILENCE.** `voice: false` par défaut, et quand
un pays l'active : demandez la permission du micro **en expliquant**, montrez un
indicateur visible quand l'écoute tourne, et offrez de la couper dans vos
réglages. Une application qui écoute sans le dire est un problème plus grave que
celui qu'elle résout. La reconnaissance se fait **sur l'appareil** : n'envoyez
aucun flux audio à nos serveurs — aucune route ne l'accepte.

### 🖥️ CE QUE L'ÉCRAN D'ALERTE DOIT MONTRER

Pendant qu'une alerte est vivante :

> - **« Le service client a été prévenu et va vous appeler »** — la seule chose
>   que la personne cherche à savoir. Dites-le dès le `201`, pas après un
>   aller-retour.
> - **Annuler**, en un appui.
> - **De quoi ajouter une note**, facultatif et secondaire — et elle sert
>   vraiment : l'opérateur la lit avant d'appeler, et « un homme me suit » change
>   ce qu'il dit au téléphone.
> - ⚠️ **AUCUN BOUTON D'APPEL**, ni vers les secours, ni vers nous. C'est
>   l'exploitation qui appelle ; un bouton « appeler Dira » ferait patienter
>   quelqu'un en danger dans une file d'attente téléphonique pendant qu'un
>   opérateur essaie de le joindre sur la même ligne.
>
> ⚠️ **ET RIEN D'AUTRE.** Pas de menu, pas de navigation, pas de retour
> accidentel vers la course. Cet écran se tient devant quelqu'un dont les mains
> tremblent.

⚠️ **PAS DE VERROU D'APPLICATION SUR CET ÉCRAN** (`app_lock`, v4.46.0) : un
code secret entre quelqu'un et son bouton d'alerte serait indéfendable. La règle
est déjà écrite dans la section du verrou — elle vaut ici littéralement.

⚠️ **LE BOUTON SOS RESTE ATTEIGNABLE EN DEUX APPUIS MAXIMUM**, depuis n'importe
quel écran d'une opération en cours. Enterré dans un menu « Aide › Sécurité », il
n'existe pas.

---

> ⚠️ **LE PASSAGER EST LA PARTIE LA PLUS EXPOSÉE D'UNE COURSE**, et c'est la
> raison pour laquelle ce bouton n'est pas réservé aux chauffeurs : il est seul,
> dans le véhicule de quelqu'un d'autre, sur un trajet qu'il ne choisit pas.
> Le bouton doit être atteignable **pendant toute la course**, depuis l'écran de
> suivi, en deux appuis au plus.
>
> ⚠️ **NE LE CONFONDEZ PAS AVEC LE SUPPORT** (ci-dessous). Le support est une
> conversation qu'on relit le lendemain ; le SOS est une alarme qu'un opérateur
> prend dans la minute. Un passager qui se trompe de porte parce que les deux se
> ressemblent perd exactement le temps qu'il n'a pas.
>
> ⚠️ **ET LE SOS N'ANNULE PAS LA COURSE.** Ce sont deux gestes : on peut
> déclencher l'alerte et rester dans le véhicule — c'est même le cas le plus
> fréquent. N'enchaînez pas automatiquement sur une annulation.

---

## 7 ter. Le SUPPORT — réclamations et objets perdus (v4.9.0)

> Jusqu'ici l'application n'avait **aucune porte** vers le support : les
> courses ne servaient pas de tickets, et le seul recours était d'appeler.
> Désormais une réclamation se dépose **depuis l'application**, se suit dans
> un fil, et le cas de l'**objet oublié dans la voiture** a son propre
> parcours — parce que c'est dans les minutes qui suivent qu'un sac se
> retrouve, pas quand le support ouvre sa file le lendemain.

```
POST /tickets                       { category, message, ride_id?, priority?, lost_item? }   → 201 ticket
GET  /tickets                       → { items, next_cursor }   mes tickets, du plus récent au plus ancien
GET  /tickets/{id}                  → le ticket et son fil
POST /tickets/{id}/messages         { "body": "…" }   → 201 ticket
```

**Les catégories**, dans l'ordre où l'écran les propose :

| `category` | Quand |
|---|---|
| `ride` | quelque chose s'est mal passé **pendant** une course — trajet, retard, prix : `ride_id` obligatoire |
| `lost_item` | **objet oublié** dans la voiture — parcours à part, ci-dessous |
| `payment` | débit, remboursement, mobile money |
| `tokens` | (réservé aux chauffeurs et marchands — ne pas proposer au passager) |
| `account` | connexion, profil, téléphone |
| `behaviour` | conduite ou comportement du chauffeur : `ride_id` recommandé |
| `other` | le reste |

- `ride_id` rattache la course. **Seul un passager qui l'a vécue** peut s'y
  référer (`403 forbidden` sinon) ; le serveur fige un libellé lisible
  (`ref_label` : « Lomé Centre → Aéroport · 19/09 14:02 ») pour que le fil
  reste compréhensible même des mois plus tard. Depuis l'écran d'une course
  terminée, proposez « Signaler un problème » et « J'ai oublié quelque chose »
  avec `ride_id` déjà rempli.
- `priority` est facultative (`normal` par défaut) : ne la demandez pas au
  passager, c'est le support qui la règle.
- ⚠️ **`order_id` n'existe pas ici** — c'est le mot de la livraison. L'envoyer
  répond `422 fields: ["order_id"], reason: "wrong_vertical"`.

Le ticket rendu :

```json
{ "id": "…", "reference": "TCK-000123", "user_id": "…", "role": "client",
  "category": "lost_item", "priority": "high", "status": "open",
  "ride_id": "…", "ref_label": "Lomé Centre → Aéroport · 19/09 14:02",
  "counterpart_id": "…",                       ← le chauffeur de la course (objet perdu seulement)
  "lost_item": { "item": "Sac à dos noir", "details": "avec un ordinateur",
                 "found": null, "answered_at": null, "note": "" },
  "messages": [ { "author_id": "…", "author_role": "client", "body": "…", "at": "…" } ],
  "created_at": "…", "updated_at": "…" }
```

**Afficher la référence** (`TCK-000123`) : c'est ce que le passager dira au
téléphone. `status` ∈ `open` (déposé) · `in_progress` (pris en charge) ·
`waiting` (on attend le passager) · `resolved` · `closed`. `author_role` dit
qui parle dans le fil — `client`, `admin` (le support), `driver` (sur un objet
perdu) : dessinez trois bulles différentes, jamais un nom.

### 🎒 L'OBJET PERDU — `lost_item`

```
POST /tickets  { "category": "lost_item", "ride_id": "…",
                 "message": "Je l'ai laissé sur la banquette arrière",
                 "lost_item": { "item": "Sac à dos noir", "details": "avec un ordinateur portable" } }
```

Ce qui se passe **côté serveur**, et que l'écran doit dire :

1. `ride_id` **et** `lost_item.item` sont obligatoires (`422` qui les nomme).
   Seul le passager de la course peut le déclarer, et la course doit avoir eu
   un chauffeur — `409 no_driver_yet` sinon (une course annulée en recherche).
2. La priorité monte à **`high`** d'office.
3. **Le chauffeur de cette course est prévenu à l'instant** (notification
   `lost_item_reported`) et devient partie au ticket : il lit le fil, y
   répond, et dit s'il a trouvé l'objet.
4. Le passager reçoit sa réponse : **`lost_item_found`** (« Bonne nouvelle :
   votre Sac à dos noir a été retrouvé. Le support vous contacte pour la
   restitution ») ou **`lost_item_not_found`**. Données
   `{ type: "lost_item", ticket_id, ride_id }` : ouvrir le ticket dessus.
5. Dans le ticket, `lost_item.found` a **trois états** : `null` — le
   chauffeur n'a pas encore regardé ; `true` — retrouvé, `lost_item.note` dit
   où, le statut passe `in_progress` et le support organise la restitution ;
   `false` — pas trouvé, le ticket **reste ouvert**, le support poursuit.
   Affichez les trois différemment : « pas encore regardé » n'est pas « pas
   trouvé ».

⚠️ **La restitution passe par le support**, jamais par un échange de numéros
dans le fil : c'est ce qui protège les deux côtés.

### Le fil, et ce qui réveille l'application

- `POST /tickets/{id}/messages` : le passager écrit ; le support et, sur un
  objet perdu, le chauffeur reçoivent **`ticket_reply`** (« Nouveau message
  sur votre demande TCK-000123 »). Quand c'est le support ou le chauffeur qui
  écrit, c'est le passager qui la reçoit. Données `{ type: "ticket",
  ticket_id, ride_id? }`.
- Quand le support clôt (`resolved` · `closed`) : **`ticket_resolved`**, une
  fois.
- Ces notifications sont dans la catégorie `support`, **non coupable** dans
  les préférences : on a posé une question, on reçoit la réponse.
- Pas de socket : sur l'écran d'un ticket ouvert, relisez `GET /tickets/{id}`
  à l'ouverture et sur chaque notification reçue.

---

## 💳 RECHARGER SON SOLDE — les bornes du pays, et retrouver une recharge perdue (v4.91.0)

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

## 8. Ce que le SOCLE sert (sans `/vtc`)

| | |
|---|---|
| `POST /auth/register` · `/auth/login` · `/auth/refresh` · `/auth/logout` | la session — ⚠️ la connexion porte `app: "client"` depuis la v4.26.0 |
| `GET · PATCH /me` · `PATCH /me/preferences` | le profil |
| `POST /uploads?kind=avatar` | la photo de profil — **v3.0.0**, une seule porte pour toute la plateforme. ⚠️ **jpeg, png, webp — 5 Mio** (v4.73.0) : le **SVG n'est plus accepté**, et le **PDF** ne l'est que sur une pièce comptable ou de conformité |
| `POST /bug-reports` (**LIVRAISON**, `…/api/v1/food/bug-reports`) | signaler un bug de l'application — pas une réclamation : celles-ci sont §7 ter, sur `/vtc/tickets` |
| `GET · POST /me/addresses` · `PUT · DELETE /me/addresses/{id}` | le carnet d'adresses |
| `GET /wallet` · `/wallet/transactions` | le solde Dira, et ses **bornes de recharge** (`topup`, v4.91.0) |
| `GET /payments/providers` · `POST /payments/initiate` · `GET /payments/{id}` · `GET /payments` | mobile money, et **retrouver un paiement perdu** (v4.91.0) |
| `GET /me/notifications` · `POST /me/devices` | les notifications |
| `GET /agents/{id}/ratings` | les avis d'un chauffeur — `id` = `driver.id` de la course (le profil) |


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
- **Inscription** : `{ phone (E.164, avec le +), name, password, role: "client", email?, first_name?, last_name? }`. Sans `+`, `422` avec `fields: ["phone"]` ; `phone_taken` (409) → proposer la connexion ; `account_suspended` (403) → le dire tel quel.
- **Un `422` nomme ses champs** (`fields`, `reason`) — §0.
- `GET /wallet` répond toujours `200` à un client : le Dira Cash s'ouvre à la première lecture.

### 🆕 S'INSCRIRE ET SE CONNECTER PAR CODE — le téléphone, et rien d'autre (v4.46.0)

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
(`client` · `driver` · `courier` · `merchant` · `console`). Sans ce mot, un
chauffeur se connectait dans l'application cliente, recevait un jeton
parfaitement valide, puis voyait chaque écran répondre `403` — et croyait
l'application cassée.

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

### 🚪 DIRE QUELLE APPLICATION SE CONNECTE — `app` (v4.26.0)

`POST /auth/login` accepte un champ `app`. **Envoyez `app: "client"` à chaque
connexion**, à côté du téléphone et du mot de passe :

```
POST /auth/login   { phone | email, password, app: "client" }
```

⚠️ **Ce qui se passait sans lui** : un chauffeur ou un livreur se connectait ici, et rien ne
l'arrêtait. Le mot de passe est bon, le jeton est émis, l'accueil s'ouvre —
puis chaque écran répond `403`, et la personne conclut que l'application est
cassée au lieu de comprendre qu'elle s'est trompée d'application. C'est du
support pour rien, et une mauvaise première impression.

Le socle refuse désormais, **`403 wrong_app`**, et le refus DIT OÙ ALLER :

| champ | ce qu'il porte |
|---|---|
| `error.code` | `wrong_app` |
| `error.reason` | **l'application à ouvrir** : `client` · `driver` (courses) · `courier` (livraison, **v4.37.0**) · `merchant` · `partner` (**v4.60.0** — console web d'un propriétaire de flotte) · `console` |
| `error.message` | la phrase déjà traduite, à afficher telle quelle si vous n'avez pas la vôtre |

> ⚠️⚠️ **UNE VALEUR DE `reason` QUE VOUS NE CONNAISSEZ PAS N'EST PAS UNE
> ERREUR — ET SURTOUT PAS UN BLANC.** La liste s'allonge : `courier` est arrivé
> en v4.37.0, `partner` en v4.60.0, et le prochain rôle arrivera sans vous
> prévenir. Une application qui traduit `reason` par un nom d'application
> affiche alors « Ouvrez l'application  » — phrase coupée, sans nom.
>
> **La règle : si vous ne connaissez pas la valeur, affichez `error.message`
> tel quel.** Il est déjà traduit par le serveur et il nomme l'application. Un
> repli générique (« ce compte n'est pas un compte de cette application »)
> convient aussi ; un trou dans la phrase, non.
>
> ⚠️ **ET `partner` NE S'OUVRE PAS DEPUIS UN TÉLÉPHONE** : c'est une console
> **web**. Ne proposez ni « ouvrir l'application », ni lien vers un magasin
> d'applications — il n'y en a pas. Dites « ce compte est un compte de
> partenaire : il s'ouvre depuis la console web de Dira. »

⚠️ **`reason`, et rien d'autre.** L'enveloppe d'erreur du socle ne rend que
`code`, `message`, `fields`, `reason` et `feature` — il n'y a pas de `meta` sur le fil.
C'est écrit ici parce qu'une première rédaction de cette section promettait
un `meta.open_instead` qui n'existe pas, et l'erreur se serait découverte à
l'intégration.

Affichez « Ce compte est un compte chauffeur. Ouvrez l'application Dira
Chauffeur. » — **jamais « identifiants invalides »** : le mot de passe était
juste, et envoyer la personne changer un mot de passe correct ne mène nulle
part.

**Ce que la règle sépare, et ce qu'elle ne sépare pas — v4.37.0.**
`client` vaut pour la course **et** la livraison : deux applications de
client, un seul compte, et elles ne se distinguent pas l'une de l'autre à la
connexion. En revanche, les deux applications d'**agent** sont désormais
DEUX mots : `driver` pour le chauffeur des courses, **`courier` pour le
livreur** — elles envoyaient toutes les deux `driver` jusqu'à cette version.
Un compte d'agent appartient à l'une ou à l'autre, et le socle lui refuse
l'entrée dans celle qui n'est pas la sienne.

⚠️ **Pourquoi cela vous concerne, même ici.** Les valeurs de `error.reason`
ne sont plus quatre mais cinq, et `courier` en fait partie : un `switch` qui
ne connaît que `client`, `driver`, `merchant` et `console` retombera sur son
cas par défaut — c'est-à-dire, presque toujours, sur « erreur de connexion »,
exactement le message que cette règle existe pour éviter. **Prévoyez un
libellé pour `courier`** (« ouvrez l'application Dira Livreur »), et une
phrase générique de repli pour une valeur que vous ne connaîtriez pas.

La famille `client`, elle, reste tenue à l'écart des autres, comme avant.

**Un compte de DIRECTION entre partout.** C'est voulu, pas un trou :
l'exploitation ouvre votre application pour reproduire ce qu'un utilisateur
décrit au support.

**`app` omis ne vérifie rien.** Une version d'application pas encore mise à
jour continue donc de fonctionner exactement comme avant — mais le mauvais
compte y entre aussi comme avant. C'est la raison d'envoyer le champ dès
cette version.

#### 📱 VOUS N'ÊTES PAS BORNÉ À UN SEUL APPAREIL (v4.35.0)

La v4.35.0 introduit un champ `device_id` à la connexion, et une règle **« un
seul appareil à la fois »** — **elle ne concerne QUE les chauffeurs et les
livreurs**, jamais un passager.

⚠️ **N'ENVOYEZ PAS `device_id`, ET NE CODEZ RIEN POUR CETTE RÈGLE.** Un passager a le droit d'être connecté sur sa tablette **et** son téléphone, et de suivre sa course depuis les deux.
Le champ est ignoré pour votre public, le jeton émis ne nomme aucun appareil, et
**aucune** de vos sessions ne peut être fermée par une connexion ailleurs.

Concrètement :

- Vous **ne recevrez jamais** `401 session_superseded`, ni sur
  `POST /auth/refresh`, ni sur un appel authentifié : inutile de le traiter.
- Se connecter sur un second appareil **n'invalide pas** le premier : les deux jetons de rafraîchissement continuent de vivre côte à côte, chacun avec sa propre rotation.
- La réponse de `POST /auth/login` ne porte **pas** d'objet `session` pour vous.

**Pourquoi c'est écrit ici alors que ça ne vous concerne pas.** La règle existe parce que deux
téléphones connectés sur un compte de chauffeur poussent **deux flux de
positions pour un seul véhicule**, et que le vivier d'appel le lit comme deux
véhicules. Rien de tel chez un passager, qui ne pousse aucune position — et l'appliquer « par prudence » à un public qui
n'a pas ce problème coûterait des déconnexions quotidiennes pour rien.


---

## 🎁 INVITER UN AMI — le parrainage (v4.51.0)

```
GET /me/referral
→ { "enabled": true, "code": "AWA7K2M",
    "invitee_xof": 1000, "sponsor_xof": 500, "max_sponsored": 10,
    "ends_at": "2027-10-08T00:00:00Z", "uses": 3 }
```

Un seul appel, et il dit tout ce que l'écran doit afficher : le code à partager,
ce que gagne celui qui arrive, ce que gagne celui qui invite, et combien de
personnes ont déjà utilisé ce code.

> ⚠️ **`enabled` EST EXPLICITE — NE LE DÉDUISEZ PAS DE L'ABSENCE DE CODE.** Le
> parrainage se règle **pays par pays**, et il est **éteint par défaut** : tant
> qu'une direction ne l'a pas décidé et budgété, il ne distribue rien. Quand
> `enabled` est faux, il n'y a **pas** de code — et il faut **cacher l'entrée**
> « inviter un ami », pas afficher un écran vide ni un bouton qui ne copie rien.

```json
{ "enabled": false, "invitee_xof": 0, "sponsor_xof": 0, "max_sponsored": 0 }
```

> ⚠️ **LE CODE EST TIRÉ À LA DEMANDE**, au premier appel de cette route. Il ne
> change plus ensuite : c'est le même code toute l'année, celui qu'on peut
> imprimer, dicter au téléphone ou coller dans un statut. Appelez cette route
> quand l'écran s'ouvre, et gardez le résultat.

> ⚠️ **AFFICHEZ LES DEUX MONTANTS, ET DITES QUI REÇOIT QUOI.** `invitee_xof` est
> une **remise** sur la première opération du filleul ; `sponsor_xof` est un
> **crédit** sur le solde du parrain. Les confondre — « gagnez 1 500 F » — fait
> attendre 1 500 F à tout le monde, et produit deux déceptions au lieu d'un
> parrainage.

> ⚠️ **LE PARRAIN N'EST PAYÉ QUE QUAND LA REMISE EST VRAIMENT CONSOMMÉE** — la
> course terminée, la commande livrée. Jamais à la saisie du code. Écrivez-le sur
> l'écran : sans cela, quelqu'un qui voit son filleul commander et son solde ne
> pas bouger pensera que le parrainage ne marche pas. La bonne phrase est
> **« crédité quand votre filleul aura terminé sa première course »**.

> ⚠️ **`max_sponsored` EST UN PLAFOND, ET `uses` DIT OÙ ON EN EST.** À
> `max_sponsored` atteint, le code cesse de valoir : montrez
> « 3 / 10 » plutôt qu'un compteur nu, sinon le dixième filleul découvrira un
> refus que rien n'annonçait. Quand `max_sponsored` vaut `0`, il n'y a pas de
> plafond — n'affichez alors aucun rapport.

**Le code de parrainage est un code promo comme un autre** : le filleul le saisit
dans le même champ que n'importe quel autre code, et les mêmes refus s'appliquent
— avec un de plus, pour celui qui essaie le sien.

---

## 🗑️ SUPPRIMER SON COMPTE — et ce qui reste des courses passées (v4.47.0)

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

## 🎯 LES OBJECTIFS À ATTEINDRE — un bonus à gagner (v4.59.0)

```
GET /me/challenges   → { "items": [ … ] }
```

> ⚠️ **AU SOCLE** (`…/api/v1/me/challenges`, sans `/vtc` ni `/food`), et **une
> seule route pour tous les rôles** : le public vient du JETON. Il n'y a pas de
> paramètre à passer — et c'est voulu, puisqu'il déciderait de ce qu'on peut
> gagner.

```jsonc
{ "items": [
  { "id": "…",
    "title": "5 jours de travail cette semaine",
    "description": "…",
    "metric_label": "Jours travaillés",
    "target": 5,
    "money": false,          // true ⇒ la cible est un MONTANT, pas un compte
    "reward_xof": 5000,
    "value": 3,              // où vous en êtes
    "percent": 60,           // BORNÉ À 100
    "ends_at": "2026-10-19T00:00:00Z",
    "reached": false,
    "paid_xof": 0,
    "pending": false } ] }
```

### Ce que l'écran doit faire, et ce qu'il ne doit pas

⚠️ **AFFICHEZ `percent` TEL QUEL — IL EST DÉJÀ BORNÉ À 100.** Quelqu'un qui a
fait 7 courses sur un objectif de 5 est à 100 %, pas à 140 % : une barre qui
dépasse a l'air d'un bug, et chaque application l'aurait bornée à sa façon.

⚠️ **`value` ET `target` ENSEMBLE, JAMAIS LE POURCENTAGE SEUL.** « 60 % » ne dit
pas quoi faire ; « 3 sur 5 » dit qu'il en reste deux. C'est la même règle que le
taux d'acceptation (v4.55.0), et pour la même raison : un chiffre dont on juge
son avancement doit porter ce qui le compose.

⚠️ **`money: true` CHANGE L'AFFICHAGE** : « 15 000 F sur 20 000 F », et non
« 15 000 fois sur 20 000 ». Le serveur le dit pour que vous n'ayez pas à deviner
d'après le nom de la mesure.

⚠️ **`ends_at` EST CE QUI FAIT AGIR.** « 5 jours de travail » sans échéance n'est
pas un objectif. Affichez le temps restant, et laissez-le visible quand il
devient court — c'est l'information qui décide de sortir travailler ce soir.

⚠️ **`reached: true` S'AFFICHE AVANT `paid_xof`.** Ce sont deux moments : le
franchissement ouvre le droit, le versement suit. Attendre l'argent pour
annoncer la victoire ferait douter quelqu'un qui a compté ses courses lui-même.
Quand `pending: true`, dites-le en clair — **« bonus en cours de versement »** —
parce que le silence se lit comme un refus.

### 🔔 La notification — `challenge_reached`

```jsonc
{ "key": "challenge_reached",
  "title": "Objectif atteint — 5 000 F",
  "body":  "5 jours de travail cette semaine. Le bonus est sur votre solde.",
  "data":  { "type": "challenge", "challenge_id": "…" } }
```

⚠️ **ROUTEZ-LA VERS L'ÉCRAN DES OBJECTIFS**, pas vers l'accueil. Et **catégorie
`support`, donc NON COUPABLE** : quelqu'un qui aurait coupé les offres
commerciales doit tout de même apprendre qu'il vient d'être payé. Un bonus versé
que personne ne sait n'a motivé personne — et c'est tout ce qu'un objectif
cherche à faire.

### Ce que vous ne recevez PAS, et pourquoi

⚠️ **NI LE BUDGET, NI LE NOMBRE DE GAGNANTS, NI LES PLACES RESTANTES.** Ce que
l'entreprise a provisionné n'est pas l'affaire de quelqu'un qui joue : afficher
« 240 000 F d'enveloppe » invite à calculer combien d'autres ont déjà gagné, et
transforme un objectif en **course aux places** — où la moitié des gens
abandonnent en se croyant trop tard. N'essayez pas de les récupérer : aucune
route ne les sert à une application.

### Les règles qui vous concernent sans être visibles

⚠️ **UN OBJECTIF SE GAGNE UNE SEULE FOIS**, même si vous dépassez largement la
cible. Ce n'est pas un tarif : « 3 courses = 5 000 F » répété serait une prime au
volume. N'affichez donc pas « 2 fois gagné ».

⚠️ **UNE COURSE COMPTE À SON RÈGLEMENT, PAS À SON ACCEPTATION.** Une course
acceptée puis annulée ne fait pas avancer l'objectif — sinon il se gagnerait en
acceptant puis annulant vingt fois. Si votre écran montre l'avancement en temps
réel, attendez la fin de la course pour l'incrémenter, ou relisez la route.

⚠️ **UN OBJECTIF PÉRIODIQUE REMET SON COMPTEUR À ZÉRO.** « 20 courses cette
semaine » est un objectif NEUF chaque lundi, avec son propre identifiant : ne
gardez pas l'avancement de la semaine passée en cache, et ne supposez pas que
l'`id` reste le même d'une période à l'autre.

⚠️ **ET LA SEMAINE COMMENCE LE LUNDI**, pas le jour du lancement : si votre écran
affiche « cette semaine », comptez du lundi au dimanche comme le serveur le fait,
sinon vos deux nombres divergeront sans que personne ne comprenne pourquoi.

---

> ⚠️ **VOS OBJECTIFS PORTENT SUR CE QUE VOUS PRENEZ, PAS SUR CE QUE VOUS
> CONDUISEZ** : courses prises, commandes passées, montant dépensé. Et le montant
> compté est le **prix final** — une course dont le trajet a changé en route
> coûte autre chose que son devis.

> ⚠️ **UN OBJECTIF DE CLIENT MÊLE LES DEUX MÉTIERS.** « 5 commandes ce mois-ci »
> compte vos commandes de livraison ; « dépensez 20 000 F » compte les deux.
> N'essayez pas de deviner le métier d'après la mesure : `metric_label` le dit.

---

## 9. ⚠️ Ce que la maquette demande et que l'API ne sert PAS

Écrit ici pour être découvert **maintenant**, pas à l'intégration.

### ✅ Noter une course, laisser un pourboire — SERVIS (v3.8.0)

`POST /rides/{id}/rating` et `POST /rides/{id}/tip` — §7 bis. L'écran de fin
de course a tout ce qu'il lui faut, y compris la carte du chauffeur (§5).

### ✅ Les adresses enregistrées « Maison » / « Travail » — SERVIES

Le carnet d'adresses du socle porte tout ce que la maquette demande :

```
GET    /me/addresses
POST   /me/addresses      { "label": "Maison", "address": "…", "geo": [lng, lat],
                            "details": "portail bleu", "is_default": true }
PUT    /me/addresses/{id}
DELETE /me/addresses/{id}
```

`label` est le nom que l'utilisateur donne — « Maison », « Bureau ». Les
raccourcis de l'écran d'accueil se câblent dessus, et `geo` alimente
directement le devis.

> ⚠️ **`is_default` est UNIQUE par compte** : poser le drapeau le retire de
> l'ancienne, dans la même opération. N'entretenez pas votre propre notion de
> « par défaut » en local, elle divergerait.
>
> Le carnet est **partagé avec la livraison** : c'est le même compte et le même
> socle. Une adresse ajoutée depuis l'application de courses apparaît côté
> livraison, et c'est voulu.

### ❌ Le partage de trajet et le contact d'urgence

Envoyer sa position à un proche, et prévenir un contact choisi : non commencé.

> ⚠️ **À NE PAS CONFONDRE AVEC LE BOUTON D'ALERTE** (v4.56.0, section 🆘), qui
> existe et qui prévient **l'exploitation** — pas un proche. Les deux sont
> utiles et ne se remplacent pas : l'exploitation peut envoyer quelqu'un, un
> proche peut venir.

> ⚠️ **À ne pas confondre avec la course PARTAGÉE** (§4 quater, v4.42.0), qui
> est un mode de course — partager la **voiture** avec un autre passager.
> Envoyer sa position à un proche reste à faire.

### 🟡 Le prix « à partir de » sur l'écran d'accueil

`GET /classes` ne rend aucun prix, et c'est délibéré. Si la maquette en affiche
un, il faut demander un devis — donc connaître le trajet.
