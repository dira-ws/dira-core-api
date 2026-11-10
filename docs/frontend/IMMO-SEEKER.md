# IMMOBILIER — CHERCHEUR (louer, acheter) — contrat d'API

> **Version 4.122.0** · 10 novembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Immobilier : `https://api-staging.dira.llc/api/v1/immo`
> Site public en service : `https://immo-staging.dira.llc`

---

## 0. ⚠️⚠️ LIRE CECI AVANT TOUT LE RESTE

**On cherche sans compte ; on garde, on est prévenu et on visite avec un
compte.** La recherche, les annonces, leurs photos et leurs créneaux de visite
se lisent **sans connexion** — une annonce qu'on ne peut pas voir sans
s'inscrire n'est trouvée par personne. Les favoris, les alertes et les visites
demandent le compte **`client`** de Dira : le même que pour commander un repas
ou une course. Il n'y a **pas** d'inscription propre à l'immobilier.

Trois règles de confiance tiennent toute la surface. Elles ne sont pas des
manques à contourner :

1. **Le téléphone de l'annonceur n'apparaît nulle part avant une visite
   confirmée.** Pas sur l'annonce, pas dans la fiche de l'annonceur, pas dans
   une réponse d'erreur. Il arrive dans **la visite**, une fois que l'annonceur
   l'a acceptée — et dans l'autre sens, l'annonceur n'a votre nom et votre
   numéro qu'à ce moment-là. Un numéro publié sur une annonce est un numéro
   moissonné : le démarchage commencerait avant la première visite.
2. **L'adresse d'un PARTICULIER est un cercle de 300 m.** L'annonce porte le
   centre du cercle et son rayon (`approx_m`), et `address` est vide. L'adresse
   exacte n'est donnée qu'au chercheur **dont la visite est confirmée**, dans
   sa visite. Une agence, elle, a pignon sur rue : sa position est exacte.
   ⚠️ **Ne placez jamais une épingle au centre d'un cercle** : elle désignerait
   une maison qui n'est pas la bonne. Dessinez le cercle.
3. **Une annonce non vérifiée le dit.** `property.verified` et
   `publisher.verified` sont des booléens servis **toujours** : l'absence de
   badge doit se lire comme « non vérifié », écrit, pas comme un oubli.

---

## 1. Deux bases d'URL, aucun socket

| | Rôle | Base |
|---|---|---|
| **Socle** (`dira-core-api`) | connexion, compte, **boîte de notifications** | `…/api/v1/` |
| **Immobilier** (`dira-immo-api`) | **recherche**, **annonces**, **photos**, **favoris**, **alertes**, **visites** | `…/api/v1/immo/` |

> **Le jeton est le même des deux côtés.** Une seule connexion, au socle.

Prévoyez deux variables : `API_BASE` et `IMMO_BASE`. Aucun temps réel par
socket : une annonce ne bouge pas à la seconde, et les changements qui
comptent pour vous (une visite confirmée, une alerte) arrivent en
**notification**.

---

## 1 bis. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — seulement pour `/immo/me/...` |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | curseur **opaque** : `?cursor=…` → `{ "items": [...], "next_cursor": "…" \| null }` |
| Montants | **entiers dans la plus petite unité** de la monnaie, **avec** la monnaie (`{ "amount": 250000, "currency": "XAF" }`). Jamais de flottant. |
| Dates | ISO 8601 UTC (`2026-11-10T10:41:23Z`). Une **date seule** s'écrit `YYYY-MM-DD`. |
| Coordonnées | `lat`, `lng` **nommés** dans les objets ; dans les paramètres de recherche, `lat,lng` dans cet ordre |
| Langue | `Accept-Language: fr` ou `en` — les messages d'erreur suivent |
| Fil | `X-Request-ID` — sur chaque réponse ; à l'aller, facultatif mais recommandé |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat. **Affichez le `message`** quand un refus doit être lu
par la personne : il dit quoi faire.

⚠️ **UN REFUS DE SAISIE NOMME SES CHAMPS.** `422 validation_failed` porte
`fields` (les paramètres en cause) et `reason`. Une recherche dont le prix
minimum dépasse le maximum rend `fields: ["price_max", "price_min"]` : **les
deux** — elles se jugent ensemble. Surlignez-les ; « requête invalide » ne dit
à personne quoi corriger.

**`401`** = jeton expiré : `POST /auth/refresh`, puis rejouer ; si le
rafraîchissement échoue, revenir à la connexion. ⚠️ **Un seul
rafraîchissement à la fois** : le jeton de rafraîchissement tourne (l'ancien
est consommé), et trois `401` simultanés qui rafraîchissent chacun ferment la
session.

### 🧵 Le fil d'une requête — `X-Request-ID`

Chaque réponse porte un `X-Request-ID` : le nom de la requête dans nos
journaux, **y compris sur un refus et sur un `500`**. Gardez-le avec l'erreur
que vous enregistrez. Vous pouvez envoyer le vôtre (64 caractères au plus,
`a-z A-Z 0-9 - _ .`) — **jamais de donnée personnelle dedans**.

### ⚠️ Le pays

Toute annonce appartient à **un** pays, et chaque lecture est bornée par pays.

- **Sans compte**, envoyez `X-Dira-Country: GA` (le pays que la personne a
  choisi). Sans l'en-tête, le serveur retient le pays par défaut du
  déploiement. Un pays **non servi** est **ignoré, pas refusé** : la réponse
  porte, dans le même en-tête, le pays **retenu** — lisez-le pour l'afficher.
- **Connecté**, le pays est **celui du compte**, inscrit dans le jeton : un
  compte du Tchad lit le Tchad, même si l'en-tête dit autre chose. Vos favoris,
  vos alertes et vos visites sont **rangés par pays** : ceux du Gabon ne se
  voient pas depuis le Tchad.

La monnaie suit le pays (`XAF` au Gabon et au Tchad, `XOF` au Togo et au
Sénégal, `GNF` en Guinée, ~14 fois plus petite). **Aucune monnaie en dur** :
affichez celle de chaque montant.

---

## 2. Se connecter — seulement pour garder, être prévenu, visiter

```json
POST /auth/login
{ "phone": "+24177000000", "password": "…", "app": "client" }
```

Ou sans mot de passe :

```json
POST /auth/otp          { "phone": "+24177000000", "app": "client", "channel": "whatsapp", "locale": "fr" }
POST /auth/otp/verify   { "phone": "+24177000000", "code": "123456", "app": "client",
                          "name": "Amina", "device_id": "<identifiant d'installation>" }
```

Le code **inscrit** la personne si le compte n'existe pas encore (`name`,
`first_name`, `last_name` ne sont lus que dans ce cas), et la **connecte**
sinon : un seul parcours pour les deux. `channel` : `whatsapp` ou `sms`.
`device_id` est l'identifiant **d'installation** — il survit à une mise à jour
et meurt avec une réinstallation ; jamais l'IMEI ni l'identifiant publicitaire.

⚠️ **`"app": "client"` est obligatoire.** Un compte annonceur ou chauffeur qui
tente d'entrer ici est refusé **avant** d'obtenir un jeton (`403 wrong_app`), et
`reason` **nomme l'application à ouvrir** : affichez-le tel quel. Un annonceur
qui cherche aussi un logement ouvre cette application **avec son compte
client**, comme partout sur Dira.

⚠️ **Ne demandez pas la connexion pour chercher.** Proposez-la au moment du
geste qui en a besoin (garder, créer une alerte, demander une visite) et
**revenez sur l'écran d'où l'on vient** après.

`POST /auth/logout` ferme la session.

---

## 3. Chercher — `GET /immo/listings`

Public. Une page de résultats, la plus pertinente d'abord.

### 3.1 Les critères

| Paramètre | Forme | Notes |
|---|---|---|
| `q` | texte, 200 caractères au plus | tolère les fautes **à partir de 5 lettres** ; « vila » ne trouve pas « villa » |
| `kind` (ou `facet`) | `rent` · `sale` · `stay` · `land` | location au mois, vente, séjour à la nuit, terrain |
| `type` | `apartment` · `house` · `villa` · `land` · `commercial` · `room` | |
| `city` | texte, 80 caractères au plus | la ville **telle qu'écrite sur les annonces** — proposez une liste, pas un champ libre |
| `price_min`, `price_max` | entiers ≥ 0, plus petite unité | jugés **ensemble** |
| `bedrooms_min`, `bathrooms_min` | entiers 0 à 100 | |
| `surface_min`, `surface_max` | m², décimaux ≥ 0 | jugés ensemble |
| `amenities` | liste séparée par des virgules | **toutes** doivent y être ; valeurs en 3.3 |
| `furnished` | `true` · `false` (`1` · `0`) | absent = indifférent |
| `verified_only` (ou `verified`) | `true` | annonceur vérifié seulement |
| `has_3d` | `true` | visite 3D seulement |
| `available_by` | `YYYY-MM-DD` | libre **au plus tard** ce jour-là |
| `bbox` | `sud,ouest,nord,est` | le rectangle visible de la carte |
| `near` + `radius_m` | `lat,lng` + mètres (1 à 50 000) | un rayon **sans** `near` est refusé |
| `polygon` | `lat,lng;lat,lng;…` (3 à 50 points) | une zone dessinée |
| `sort` | `relevance` (défaut) · `price_asc` · `price_desc` · `newest` · `nearest` | `nearest` exige `near` |
| `limit` | 1 à 50, défaut 20 | au-delà de 50 : 50 |
| `cursor` | opaque | **rendu par la page précédente, jamais calculé** |

⚠️ **UNE SEULE FORME SUR LA CARTE.** `bbox`, `radius_m` et `polygon` s'excluent :
les trois sont nommés dans le refus si vous en envoyez deux. La carte n'en
montre qu'une, la recherche aussi.

⚠️ **UN CURSEUR NE VAUT QUE POUR SA RECHERCHE.** Rejoué avec d'autres critères,
il est refusé (`422`, `fields: ["cursor"]`) : il rendrait la page 3 d'une autre
liste sans que rien ne le dise. Changer un filtre, c'est repartir sans curseur.

### 3.2 La réponse

```json
{
  "items": [ /* annonces, forme de la section 4 */ ],
  "total": 37,
  "next_cursor": "eyJvIjoyMCwicSI6ImE0YjEifQ",
  "source": "index"
}
```

- `total` est **estimé**, et plafonné à 1 000 : au-delà, il n'y a plus de page
  suivante. Affichez « plus de 1 000 » plutôt qu'un nombre exact.
- `next_cursor` vaut `null` à la dernière page.
- `source` : `index` (le moteur de recherche), `mongo` (secours, voir 3.4) ou
  `ids` (section 6).

### 3.3 Les équipements (`amenities`)

`running_water` · `borehole` · `water_tank` · `hot_water` · `septic_tank` ·
`electricity` · `generator` · `solar_power` · `air_conditioning` ·
`ceiling_fan` · `fitted_kitchen` · `laundry_room` · `storage_room` · `parking` ·
`garage` · `elevator` · `wheelchair_access` · `gated_compound` ·
`perimeter_wall` · `security_guard` · `balcony` · `terrace` · `garden` ·
`swimming_pool` · `fiber_internet`

Un équipement inconnu est refusé (`fields: ["amenities"]`), pas ignoré : un
filtre ignoré rendrait des biens qui ne l'ont pas.

### 3.4 Quand le moteur de recherche ne répond pas

La recherche par **catégorie** (`kind` seul, tri par pertinence ou `newest`)
continue d'être servie depuis la base (`source: "mongo"`, curseur préfixé
`m.`). Toute autre recherche — texte, prix, carte — rend **`503
search_unavailable`** : on ne rend pas des biens hors budget en laissant croire
qu'ils correspondent. Affichez le message (« parcourez les catégories en
attendant ») et proposez les catégories.

---

## 4. Une annonce — `GET /immo/listings/{ref}`

Public. `{ref}` est le **slug** (`villa-contemporaine-a-la-sabliere-21038f233fbc3debd94bffbd`)
ou l'identifiant seul : le slug **se termine par l'identifiant**, qui seul fait
foi. Un titre corrigé change le slug — un ancien lien continue de marcher ;
si vous affichez une adresse, prenez le `slug` de la réponse.

`404 listing_not_found` pour une annonce **retirée, en brouillon ou d'un autre
pays** — sans distinction, exprès : confirmer qu'un brouillon existe à une
adresse renseignerait un concurrent.

```json
{
  "id": "21038f233fbc3debd94bffbd",
  "slug": "villa-contemporaine-a-la-sabliere-21038f233fbc3debd94bffbd",
  "reference": "IM-3DEBD94B",
  "country": "GA",
  "kind": "sale",
  "status": "published",
  "title": "Villa contemporaine à La Sablière",
  "pitch": "…",
  "price": { "amount": 280000000, "currency": "XAF", "period": "total" },
  "charges": null,
  "deposit_months": 0,
  "negotiable": true,
  "available_from": "2026-12-01",
  "published_at": "2026-10-10T02:33:33Z",
  "updated_at": "2026-11-10T09:24:15Z",
  "sponsored": false,
  "property": {
    "type": "villa",
    "city": "Libreville",
    "neighborhood": "La Sablière",
    "address": "",
    "geo": { "lat": 0.4162, "lng": 9.4127, "approx_m": 300 },
    "bedrooms": 4, "bathrooms": 3, "floor": null,
    "areas": [ { "kind": "living", "square_meters": 240, "source": "declared" } ],
    "surface_m2": 240, "surface_source": "declared",
    "amenities": ["generator", "swimming_pool", "security_guard"],
    "furnished": false,
    "verified": true,
    "has_3d": false
  },
  "publisher": { "id": "…", "name": "Estuaire Habitat", "kind": "agency",
                 "verified": true, "verified_at": "2026-10-10T02:33:33Z" },
  "media": [
    { "id": "6af2e2c115ec33d946d843e1", "kind": "photo",
      "url": "https://api-staging.dira.llc/api/v1/immo/media/GA/6af2e2c115ec33d946d843e1/display.jpg",
      "thumb_url": "https://api-staging.dira.llc/api/v1/immo/media/GA/6af2e2c115ec33d946d843e1/thumb.jpg",
      "alt": "Pièce de vie", "width": 1500, "height": 1000 }
  ],
  "completeness": 90,
  "share_url": "https://immo-staging.dira.llc/fr/properties/villa-contemporaine-a-la-sabliere-21038f233fbc3debd94bffbd"
}
```

| Champ | À savoir |
|---|---|
| `reference` | ce qu'on **dicte au téléphone** ; affichage seulement, ne sert à aucun appel |
| `price.period` | `month` (location), `night` (séjour), `total` (vente, terrain) |
| `charges` | `null` sauf location longue durée ; même forme que le prix, sans période |
| `deposit_months` | caution, en **mois de loyer** ; `0` hors location |
| `negotiable` | `null` = l'annonceur n'a rien dit — ce n'est pas « non » |
| `available_from` | **un jour local**, pas un instant : ne le convertissez pas de fuseau |
| `sponsored` | ⚠️ **affichez « Annonce sponsorisée »** sur une annonce à `true` : la mise en avant payée remonte dans le classement, et sans la mention le chercheur prendrait le mieux payé pour le plus pertinent |
| `property.address` | **vide pour un particulier** (règle 2 de la section 0) |
| `property.geo` | `null` si le bien n'a pas de position ; `approx_m: 0` = position exacte, sinon **un cercle** de ce rayon |
| `areas[].source` | `declared` (dit par l'annonceur) ou une **mesure** (`measured_lidar`, `measured_ar`, `measured_photogrammetry`, `surveyed`, `gps_polygon`) — dites laquelle : « 120 m² (déclarés) » n'est pas « 120 m² (mesurés) » |
| `publisher.kind` | `agency` ou `individual` |
| `publisher.verified_at` | **toujours présent**, `null` sans badge |
| `media` | **jamais `null`** ; la première photo est la **couverture** (section 5) |
| `completeness` | 0 à 100 ; sert au classement, ne l'affichez pas comme une note |
| `share_url` 🆕 | l'adresse de l'annonce **sur le site public**, dans la langue de la requête — ce qu'on partage. **Servie par cette route seulement**, et **absente** quand le déploiement ne connaît pas son site : pas de bouton « partager », alors. ⚠️ **Ne la fabriquez pas** (section 10 bis.7) |

---

## 5. Les photos

`media[].url` (1 600 px au plus) et `media[].thumb_url` (480 px) sont des
adresses **stables** : elles répondent par une **redirection `302`** vers une
adresse signée du dépôt de fichiers, valable **douze heures**. Un composant
d'image ordinaire la suit sans rien faire.

- ⚠️ **Gardez l'adresse stable, jamais l'adresse signée** (le `Location` de la
  redirection) : la signée meurt, la stable non. Un cache d'images doit être
  indexé par l'adresse stable.
- ⚠️ **Les vignettes d'une liste prennent `thumb_url`.** La grande taille pèse
  quatre fois plus : sur un réseau mobile, une page de vingt annonces en grande
  taille ne se charge pas.
- **Une photo n'est servie que tant que l'annonce est en ligne.** Retirée,
  vendue ou louée, ses photos répondent `404`. Prévoyez l'emplacement vide.
- `alt` n'est **jamais vide** (la description de l'annonceur, ou à défaut
  « titre — photo N ») : mettez-la dans l'attribut d'accessibilité.
- Les photos sont **réencodées** à l'envoi : aucune métadonnée de l'original
  (dont la position GPS du téléphone de l'annonceur) n'y est. Il n'y a rien à
  retirer de votre côté.

---

## 6. Comparer — `GET /immo/listings?ids=a,b,c`

Public. Des annonces **nommées** (slugs ou identifiants, **10 au plus**, les
doublons comptés une fois), rendues **dans l'ordre demandé**, lues dans la
base — le comparatif marche même quand le moteur de recherche est tombé.

```json
{ "items": [ /* … */ ], "total": 2, "next_cursor": null, "source": "ids",
  "missing": ["0123456789abcdef01234567"] }
```

- `missing` : les annonces demandées qui ne sont **plus en ligne dans ce
  pays**. Dites-le (« un des biens n'est plus disponible ») au lieu d'un trou
  muet dans le tableau. Absent quand il ne manque rien.
- ⚠️ **`ids` est une forme à part** : avec n'importe quel autre critère (prix,
  texte, tri, curseur), refus `422`, `fields: ["ids"]`. Un filtre rendrait en
  silence moins de biens que demandés. La taille de page (`limit`) est ignorée.
- Plus de 10 ? Faites plusieurs appels.

---

## 7. Mes favoris — compte `client`

| Geste | Route | Réponse |
|---|---|---|
| lister | `GET /immo/me/favorites` | `200` — `{ "items": [annonces], "unavailable": ["id", …] }` |
| garder | `PUT /immo/me/favorites/{ref}` | `204`, **idempotent** |
| retirer | `DELETE /immo/me/favorites/{ref}` | `204`, idempotent — même hors ligne |

- `items` : les annonces **encore en ligne**, les plus récemment gardées
  d'abord, sous la forme complète de la section 4.
- `unavailable` : les annonces gardées qui ne sont **plus en ligne** (louées,
  vendues, retirées). ⚠️ Nommez-les et proposez de les retirer : les faire
  disparaître sans un mot fait croire à une perte. Jamais `null`.
- `404 listing_not_found` en gardant une annonce qui n'est pas en ligne dans
  ce pays.
- `409 favorites_full` : **200 favoris au plus**.
- ⚠️ **Hors connexion**, gardez les favoris sur l'appareil, puis portez-les
  au compte à la connexion (`PUT` un par un — idempotent, sans risque de
  doublon). C'est **une union** : n'effacez jamais ce que la personne avait
  gardé avant de se connecter.

---

## 8. Mes recherches gardées, et leurs alertes — compte `client`

Garder une recherche, c'est demander à être **prévenu** quand une annonce y
correspond.

```json
POST /immo/me/saved-searches
{ "name": "Trois pièces à Libreville",
  "query": "city=Libreville&bedrooms_min=3&kind=rent",
  "alerts": true }
```

→ `201` :

```json
{ "id": "6af28dea53bc48e9299ac475", "country": "GA",
  "name": "Trois pièces à Libreville",
  "query": "bedrooms_min=3&city=Libreville&kind=rent",
  "alerts": true, "last_alert_at": null,
  "created_at": "2026-11-10T03:20:00Z" }
```

- `query` : **les paramètres de `GET /immo/listings`**, tels quels (section 3.1).
  Le serveur les valide **comme une recherche** : un critère invalide est
  refusé ici, avec son nom dans `fields` — une recherche qu'on ne pourrait pas
  rejouer n'enverrait jamais d'alerte, sans que personne ne le voie.
- Le serveur **retire** ce qui ne dit pas « ce que l'on cherche » : `cursor`,
  `limit`, `sort`, `ids`. Lisez la `query` rendue, pas celle envoyée.
- ⚠️ **Une recherche sans aucun critère est refusée** (`422`, `fields:
  ["query"]`) : elle annoncerait chaque annonce du pays.
- `name` : 1 à 60 caractères (`fields: ["name"]`). Proposez le titre de la
  page de résultats.
- `alerts` absent = `true`.
- **20 recherches gardées par pays** (`409 saved_searches_full`).
- Une recherche gardée **n'annonce pas** ce qui était déjà en ligne au moment
  où on l'a gardée.

| Geste | Route |
|---|---|
| lister | `GET /immo/me/saved-searches` → `{ "items": [ … ] }`, les plus récentes d'abord |
| couper ou rallumer l'alerte | `PATCH /immo/me/saved-searches/{id}` `{ "alerts": false }` → `200`, la recherche à jour |
| oublier | `DELETE /immo/me/saved-searches/{id}` → `204` |

⚠️ `alerts` est **obligatoire** dans le `PATCH` : un corps vide n'éteint rien
(`422`). ⚠️ **Rallumer ne rattrape rien** : l'alerte repart de maintenant —
sinon une alerte éteinte un mois annoncerait tout d'un coup.

`404 saved_search_not_found` : la recherche n'existe pas, n'est pas la vôtre,
ou appartient à un autre pays.

### L'alerte

Le serveur passe **toutes les trente minutes**. Pour chaque recherche dont des
annonces ont été publiées depuis la dernière alerte, **une seule**
notification — « 3 nouvelles annonces », pas trois messages :
`immo_saved_search_match` (section 10). Elle désigne la plus récente ; ouvrez
la recherche pour voir les autres.

---

## 9. Visiter — compte `client`

### 9.1 Les créneaux — `GET /immo/listings/{ref}/visit-slots`

Public : on choisit son heure **avant** de se connecter.

```json
{ "items": [
  { "id": "6af29e6de6b6de65ee6b7e1a", "starts_at": "2026-11-11T14:00:00Z",
    "duration_min": 30, "mode": "in_person", "seats_left": 1 } ] }
```

- Seulement les créneaux **libres**, à **plus d'une heure**, non retirés, dans
  l'ordre. Jamais qui a déjà demandé.
- `mode` : `in_person` (sur place) ou `video` (en visio).
- ⚠️ `starts_at` est un **instant**. Affichez-le dans le fuseau **du pays du
  bien** (Libreville, N'Djamena : UTC+1), pas dans celui du téléphone : un
  chercheur en déplacement demanderait sinon une heure qui n'existe pas chez
  l'annonceur.
- `items` vide : aucun créneau libre. Proposez de garder l'annonce.

### 9.2 Demander — `POST /immo/me/visits`

```json
{ "slot_id": "6af29e6de6b6de65ee6b7e1a", "note": "Je viens avec ma sœur" }
```

→ `201`, la visite (forme en 9.4), `status: "requested"`, `contact` et
`address` à `null`.

- `note` : facultative, 500 caractères au plus — c'est **le seul** texte qui
  part vers l'annonceur avant la confirmation.
- ⚠️ **Ni nom, ni téléphone dans la demande** : c'est le compte qui les porte,
  et l'annonceur ne les reçoit qu'à la confirmation.
- `409 visit_slot_full` : la dernière place vient d'être prise — **relisez les
  créneaux** et proposez-en un autre.
- `409 visit_too_late` : le créneau commence dans moins d'une heure.
- `409 visit_already_requested` : une visite est **déjà en cours** (demandée
  ou confirmée) pour cette annonce. Une seule à la fois — envoyez vers elle.
- `404 visit_slot_not_found` : le créneau a été retiré, ou n'est pas de ce pays.
- `404 listing_not_found` : l'annonce n'est plus en ligne.

### 9.3 Suivre, annuler

| Geste | Route |
|---|---|
| mes visites | `GET /immo/me/visits` → `{ "items": [ … ] }`, les plus lointaines d'abord |
| annuler | `POST /immo/me/visits/{id}/cancel` → `200`, la visite à jour |

On annule **avant l'heure** (`409 visit_too_late` ensuite) ; une visite déjà
close rend `409 visit_status_transition`. L'annonceur est prévenu.

### 9.4 La visite

```json
{
  "id": "6af29e6ee6b6de65ee6b7e1b",
  "slot_id": "6af29e6de6b6de65ee6b7e1a",
  "listing": { "id": "dbd36b8d6454b31a3849068a",
               "title": "Appartement 2 chambres à Klémat",
               "slug": "appartement-2-chambres-a-klemat-dbd36b8d6454b31a3849068a" },
  "starts_at": "2026-11-11T14:00:00Z",
  "duration_min": 30,
  "mode": "in_person",
  "status": "confirmed",
  "note": "Je viens avec ma sœur",
  "requested_at": "2026-11-10T04:32:30Z",
  "decided_at": "2026-11-10T04:32:48Z",
  "contact": { "name": "Chari Immobilier", "phone": "+23566990101" },
  "address": { "label": "Avenue Charles de Gaulle", "city": "N'Djamena",
               "neighborhood": "Klémat", "lat": 12.119, "lng": 15.05 }
}
```

| `status` | Ce qui s'est passé | `contact` / `address` |
|---|---|---|
| `requested` | demandée, l'annonceur n'a pas tranché | `null` |
| `confirmed` | acceptée | **remplis** |
| `declined` | refusée par l'annonceur | `null` |
| `cancelled` | annulée — `closed_by` : `seeker`, `host` ou `slot` (créneau retiré) | `null` |
| `expired` | jamais tranchée avant l'heure | `null` |
| `attended` | l'annonceur a constaté votre venue | remplis |
| `no_show` | l'annonceur a constaté votre absence | `null` |

⚠️⚠️ **`contact` et `address` sont la seule source du téléphone et de
l'adresse exacte**, et seulement pour `confirmed` / `attended`. Annulée, la
visite les **referme** : ne les gardez pas sur l'appareil au-delà de la visite.
`note`, `decided_at` et `closed_by` sont absents quand ils n'ont pas de valeur.

---

## 9 bis. Signaler une annonce — `POST /immo/listings/{ref}/reports`

Compte `client`. Une annonce **en ligne** qui semble fausse se signale :

```json
POST /immo/listings/{ref}/reports
{ "reason": "scam", "comment": "On me demande la caution par transfert avant de visiter" }
```

→ `201` `{ "id": "…", "listing_id": "…", "reason": "scam", "comment": "…", "created_at": "…" }`

| `reason` | Ce que la personne a vu |
|---|---|
| `scam` | on lui demande de l'argent avant la visite |
| `unavailable` | le bien est déjà loué ou vendu |
| `wrong_info` | prix, photos ou adresse faux |
| `duplicate` | le même bien publié plusieurs fois |
| `offensive` | contenu choquant |
| `other` | autre — le `comment` dit quoi |

- `comment` : facultatif, 500 caractères au plus.
- ⚠️ **Un signalement ouvert par personne et par annonce** : le second rend
  `409 report_already_open` (« l'équipe regarde ») — affichez-le comme une
  réussite, pas comme une erreur. Une fois l'annonce examinée, on peut
  signaler à nouveau.
- `404 listing_not_found` : l'annonce n'est plus en ligne.
- **Rien ne disparaît sur un signalement** : c'est un humain de l'équipe qui
  examine, et l'annonce peut rester en ligne. Ne promettez pas « annonce
  retirée » ; dites « merci, l'équipe va regarder ».

⚠️ **Le meilleur signalement est celui qu'on n'a pas besoin de faire** : dites
dans l'écran d'une annonce qu'**aucun paiement ne se fait avant une visite**
sur Dira. Une demande de caution par transfert avant de voir le bien est le
signe d'une arnaque.

---

## 10. Les notifications

Elles arrivent par le socle (poussée + boîte, `GET /me/notifications`). Les
`data` permettent d'ouvrir **le bon écran** :

| Clé | Quand | `data` |
|---|---|---|
| `immo_saved_search_match` | des annonces correspondent à une recherche gardée (une par recherche et par passage) | `type: "immo_saved_search"`, `saved_search_id`, `listing_id`, `slug` |
| `immo_visit_confirmed` | l'annonceur a accepté | `type: "immo_visit"`, `visit_id`, `listing_id` |
| `immo_visit_declined` | l'annonceur a refusé | idem |
| `immo_visit_cancelled` | l'annonceur a annulé, ou retiré le créneau | idem |
| `immo_visit_reminder` | **deux heures avant** une visite confirmée | idem |

Le texte porte l'**heure locale du pays** de la visite. ⚠️ **Aucune notification
ne porte d'adresse ni de téléphone** : un écran verrouillé se lit par-dessus
l'épaule. Ouvrez la visite pour les lire.

---

## 10 bis. 🆕 📱 L'ONGLET « LOGEMENT » DE L'APPLICATION CLIENTE (v4.122.0)

Sur téléphone, le chercheur **n'a pas d'application à lui** : l'immobilier est
un **onglet** de l'application cliente Dira — celle des repas et des courses —,
avec le même compte, la même session et la même boîte de notifications. Cette
section dit ce qu'un téléphone ajoute aux précédentes ; tout le reste vaut tel
quel.

### 10 bis.1 ⚠️⚠️ L'onglet n'existe que là où `/app/config` l'ouvre

```
GET /app/config                    ← socle, SANS jeton, lu à l'ouverture
→ { "country": "GA", "edition": "full",
    "features": { …, "immo": true }, … }
```

- `features.immo` vaut `true` **seulement** dans les pays que l'exploitation a
  ouverts, un par un. Ailleurs : **pas d'onglet**, pas d'entrée de menu, pas
  d'écran « bientôt chez vous ».
- ⚠️⚠️ **CE DRAPEAU S'ALLUME, ET SON DÉFAUT EST `false`.** Presque toutes les
  fonctions de `/app/config` partent de « disponible » et une règle les retire ;
  celle-ci non. L'immobilier n'a d'annonces que là où des agences publient, et
  un onglet vide se lit « Dira ne marche pas ici » — ce qui est faux pour les
  repas et les courses du même écran. **Une clé `immo` absente** (serveur plus
  ancien que l'application) se lit donc **`false`**, à l'inverse des autres clés.
- **Le pays est celui où la personne cherche** : connecté, celui du compte (le
  jeton l'impose) ; sans compte, celui que vous envoyez dans `X-Dira-Country`.
  **Relisez `/app/config` à chaque changement de pays** : l'onglet peut
  apparaître ou disparaître.
- ⚠️ **C'est un réglage d'AFFICHAGE, pas un verrou.** Les routes `/immo/…` ne
  le lisent pas : une recherche dans un pays fermé rend simplement une liste
  vide. C'est l'onglet qui ne doit pas exister.

### 10 bis.2 La même session — rien à rouvrir

- Le jeton de l'application cliente (`app: "client"`) sert **tel quel** sur
  `…/api/v1/immo/`. Pas de seconde connexion, pas d'« espace immobilier » : une
  base d'URL de plus (`IMMO_BASE`), c'est tout.
- **On cherche sans compte.** L'onglet s'ouvre et cherche déconnecté ; la
  connexion se propose **au geste** qui en a besoin (garder, être prévenu,
  visiter), puis on **revient sur l'annonce** d'où l'on venait.
- `401` sur une route `/immo/me/…` : **le même rafraîchissement** que pour les
  repas et les courses, **un seul à la fois** — le jeton de rafraîchissement
  tourne, et deux onglets qui rafraîchissent chacun de leur côté ferment la
  session.
- `X-Dira-Country` et `Accept-Language` sur **chaque** requête `/immo/…`.
  `X-Dira-Edition` n'est pas lu par l'immobilier ; l'envoyer ne coûte rien.

### 10 bis.3 Les notifications — le téléphone est déjà déclaré

L'application cliente déclare déjà son téléphone (`POST /me/devices`, avec
`edition`) : les notifications de l'immobilier arrivent **par le même canal**,
dans la **même boîte** (`GET /me/notifications`). Rien de plus à déclarer.

⚠️ **ROUTEZ SUR `data.type`, JAMAIS SUR LA CLÉ** — la clé est un gabarit que
l'exploitation peut renommer :

| `data.type` | Clés | Ouvrir |
|---|---|---|
| `immo_visit` | `immo_visit_confirmed` · `immo_visit_declined` · `immo_visit_cancelled` · `immo_visit_reminder` | **la visite** `visit_id` |
| `immo_saved_search` | `immo_saved_search_match` | **l'annonce** `slug` — avec, à côté, « voir toute la recherche » (`saved_search_id`) |

- ⚠️ **IL N'Y A PAS DE ROUTE POUR UNE SEULE VISITE NI POUR UNE SEULE RECHERCHE
  GARDÉE.** `GET /immo/me/visits` puis celle dont l'`id` est `visit_id` ;
  `GET /immo/me/saved-searches` puis celle dont l'`id` est `saved_search_id`,
  et rejouez sa `query` sur `GET /immo/listings`.
- ⚠️ **La notification dit qu'il s'est passé quelque chose ; la visite dit
  quoi.** Relisez-la à l'ouverture : `contact` et `address` n'arrivent que par
  elle, et seulement confirmée.
- ⚠️ **Aucune notification ne porte d'adresse ni de téléphone**, et n'en
  composez pas une locale qui les ajouterait : un écran verrouillé se lit
  par-dessus l'épaule.
- Une clé inconnue s'affiche **dans la boîte**, sans rien ouvrir.

### 10 bis.4 Les photos, sur un réseau mobile

- Listes, cartes et carrousels : **`thumb_url`**. Galerie plein écran : `url`.
- ⚠️ **Indexez le cache d'images par l'adresse STABLE** (celle de la réponse),
  jamais par l'adresse signée vers laquelle elle redirige : la signée meurt au
  bout de douze heures, la stable non.
- **Ne préchargez pas les galeries d'une liste** : vingt annonces de dix photos
  en grande taille, c'est des dizaines de mégaoctets sur un forfait payé au
  mégaoctet. Préchargez la **couverture** (`media[0].thumb_url`), le reste à
  l'ouverture de l'annonce.
- **Une photo `404` = une annonce close** : emplacement vide, pas d'erreur. Et
  **purgez du cache** les images d'une annonce que la relecture dit close —
  l'intérieur d'un logement loué n'a plus rien à faire sur le téléphone d'un
  inconnu.

### 10 bis.5 La carte

- **Le même fond que le reste de l'application**, et la même règle : le
  **choix de la personne**, sinon le `basemap` du pays où elle cherche
  (`maps.basemap` à la connexion et au rafraîchissement, `GET /countries →
  items[].basemap` sans compte), sinon `dira`. ⚠️ Le serveur ne sert **aucune
  clé** de carte : celle de Google appartient à l'application.
- ⚠️⚠️ **UN PARTICULIER EST UN CERCLE, UNE AGENCE EST UNE ÉPINGLE.**
  `property.geo.approx_m > 0` : dessinez un cercle de ce rayon autour de
  `geo` — **jamais** d'épingle au centre, même au zoom maximal, et **jamais**
  de bouton « itinéraire » vers ce point : il conduirait chez un voisin.
  `approx_m: 0` : la position est exacte.
- **L'itinéraire n'existe que depuis une visite `confirmed`**, vers
  `address.lat` / `address.lng` — l'adresse exacte, que la visite seule porte.
- **« Autour de moi »** : la position du téléphone en `near=lat,lng` (avec
  `radius_m`, ou `sort=nearest`). Demandez la permission **au geste**, pas au
  démarrage — et sans elle, la recherche par ville marche.
- **Ce que l'écran montre** : `bbox=sud,ouest,nord,est` du rectangle visible,
  relancé quand la carte **s'arrête** de bouger (pas à chaque image du geste).

### 10 bis.6 Garder sans compte, puis se connecter

- Les favoris se gardent **sur l'appareil** tant que la personne n'est pas
  connectée, puis se portent au compte à la connexion : `PUT
  /immo/me/favorites/{ref}` **un par un** (idempotent). C'est **une union** :
  n'effacez rien de ce qu'elle avait gardé.
- ⚠️ **Les favoris sont rangés par PAYS.** Gardés au Gabon sans compte, ils ne
  se portent pas sur un compte du Tchad : chacun rend `404 listing_not_found`.
  **Gardez-les sur l'appareil** au lieu de les jeter — la personne les
  retrouvera en revenant au Gabon.
- **Une recherche gardée et une visite exigent le compte** : il n'y a pas de
  version locale à porter ensuite.

### 10 bis.7 Partager une annonce — `share_url`

```json
GET /immo/listings/{ref}
→ { …, "share_url": "https://immo-staging.dira.llc/fr/properties/appartement-2-chambres-a-klemat-dbd36b8d6454b31a3849068a" }
```

- **L'adresse de l'annonce sur le site public**, composée par le serveur, dans
  la **langue de la requête** (`Accept-Language` ; une langue que le site ne
  sert pas donne le français). Passez-la à la **feuille de partage du
  système** — WhatsApp en tête.
- ⚠️ **Servie par la fiche seulement** (pas par la recherche), et **absente**
  quand le déploiement ne connaît pas son site : **pas de bouton**, alors.
- ⚠️ **NE FABRIQUEZ PAS L'ADRESSE.** L'hôte change d'un environnement à
  l'autre ; une adresse écrite en dur dans l'application partagerait la
  préproduction le jour où elle part en production.
- ⚠️ **Le lien ouvre le SITE, pas l'application** : aucun lien universel
  (iOS) ni App Link (Android) n'est servi aujourd'hui (§11).

### 10 bis.8 Appeler, y aller

- **Le bouton « appeler » n'existe que sur une visite `confirmed` ou
  `attended`** dont `contact.phone` est servi : un lien `tel:`. ⚠️ **Jamais dans
  le carnet de contacts du téléphone**, et rien de gardé sur l'appareil au-delà
  de la visite — annulée, elle referme le contact.
- **`mode: "video"`** : aucun lien de visioconférence n'est servi. La visite
  confirmée donne le contact ; c'est par lui qu'on se joint.
- **« Ajouter à l'agenda »** est permis, à l'heure **du pays du bien**, avec le
  titre de l'annonce — et l'adresse **seulement** si la visite est confirmée.

### 10 bis.9 Hors ligne, réseau faible

- Gardez **pour l'affichage** la dernière recherche, les favoris et les visites,
  **avec l'heure de leur lecture** (« vu à 14 h 05 »).
- ⚠️ **NE METTEZ JAMAIS EN FILE une demande de visite, une annulation ou un
  signalement.** Quand la file repartira, le créneau aura été pris, ou la
  visite aura commencé — et l'annonceur aura été prévenu à tort. Ces gestes
  partent **en ligne**, ou pas du tout, et l'écran le dit.
- Retirer un favori peut attendre le réseau : la route est idempotente.

### 10 bis.10 ⚠️ Ce que l'application cliente ne fait PAS pour l'immobilier

| Absent | Pourquoi |
|---|---|
| **L'assistant unifié sur l'onglet Logement** | il ne sert que les repas et les courses : `vertical: "immo"` est **refusé** (`422 validation_failed`, `fields: ["vertical"]`), et « je cherche un trois-pièces » envoyé sans `vertical` partirait vers un taxi ou un repas. **Pas de boîte de l'assistant sur cet onglet.** |
| **Les visites dans le fil d'activité** | le fil compte les courses et les commandes ; les visites se lisent par `GET /immo/me/visits` |
| **Le portefeuille, les codes promo, le SOS** | rien ne se paie dans l'immobilier aujourd'hui, et une visite n'est pas un trajet |
| **Une socket** | une annonce ne bouge pas à la seconde ; ce qui compte arrive en notification |

---

## 11. ⚠️ CE QUE CETTE SURFACE NE SERT PAS — et pourquoi

| Absent | Pourquoi |
|---|---|
| **Le téléphone de l'annonceur avant une visite confirmée** | règle 1 de la section 0 — ce n'est pas un manque |
| **L'adresse exacte d'un particulier sur l'annonce** | règle 2 — elle vient dans la visite confirmée |
| **Écrire à l'annonceur avant la visite** | la messagerie n'est **pas encore servie** ; seul le mot joint à la demande de visite lui parvient |
| **Faire une offre d'achat, réserver un séjour, payer** | pas servis : le séjour attend le cadre du séquestre, l'offre d'achat son module |
| **Le loyer, le bail, les quittances** | la gestion locative n'est pas ouverte |
| **La visite 3D** | `property.has_3d` le dit, le visionneur n'est pas encore servi |
| **Choisir son pays en étant connecté** | le pays est celui du compte (1 bis) |
| **Ouvrir l'application depuis un lien d'annonce** | le site ne publie ni `assetlinks.json` (Android) ni `apple-app-site-association` (iOS) : `share_url` ouvre le **navigateur** |
| **L'onglet Logement dans un pays fermé** | `features.immo` le ferme (10 bis.1) — ouvert pays par pays, quand des annonces y sont en ligne |

---

## 12. Les refus de cette surface

| Code | HTTP | Phrase (fr) |
|---|---|---|
| `validation_failed` | 422 | selon le cas — lisez `fields` et `reason` |
| `search_unavailable` | 503 | La recherche ne répond pas pour l'instant — parcourez les catégories en attendant |
| `search_no_country` | 400 | Une recherche se fait dans un pays : précisez lequel |
| `listing_not_found` | 404 | Annonce introuvable |
| `media_not_found` | 404 | Photo introuvable |
| `favorites_full` | 409 | Vous gardez déjà 200 annonces : retirez-en avant d'en ajouter |
| `account_unknown` | 401 | Cette session ne désigne aucun compte : reconnectez-vous |
| `saved_searches_full` | 409 | Vous gardez déjà 20 recherches dans ce pays : supprimez-en une avant d'en ajouter |
| `saved_search_not_found` | 404 | Recherche sauvegardée introuvable |
| `visit_slot_not_found` | 404 | Ce créneau de visite n'existe pas ou a été retiré |
| `visit_slot_full` | 409 | Ce créneau est complet : choisissez-en un autre |
| `visit_already_requested` | 409 | Vous avez déjà une visite en cours pour cette annonce |
| `visit_too_late` | 409 | Trop tard : la visite a commencé ou va commencer |
| `visit_status_transition` | 409 | Cette visite ne peut pas aller là depuis son état actuel |
| `visit_not_found` | 404 | Visite introuvable |
| `report_already_open` | 409 | Vous avez déjà signalé cette annonce : l'équipe regarde |
| `wrong_app` | 403 | (connexion) — affichez `reason`, qui nomme l'application à ouvrir |

---

## 13. Journal

### 4.122.0 — 10 novembre 2026

🟢 **SUR TÉLÉPHONE — l'onglet « Logement » de l'application cliente** (§10 bis) : il
n'existe que si `GET /app/config` sert `features.immo: true` — un drapeau qui
**s'allume pays par pays** et dont l'absence vaut `false` ; la même session, le
téléphone déjà déclaré, les notifications routées sur `data.type`, les photos sur
réseau mobile, le cercle sur la carte, les favoris gardés sans compte, l'appel et
l'itinéraire réservés à la visite confirmée, ce qui ne part jamais en file, et ce
que l'assistant unifié ne fait pas ici.

🟢 **`share_url`** sur `GET /immo/listings/{ref}` (§4) : l'adresse publique de
l'annonce, composée par le serveur dans la langue de la requête.

### 4.121.0 — 10 novembre 2026

🟢 **Première version.** La recherche et l'annonce (§3, §4), les photos (§5), le
comparatif (§6), les favoris (§7), les recherches gardées et leurs alertes (§8), les
visites (§9), le signalement (§9 bis), les notifications (§10). Ce qui n'est pas
servi, et pourquoi : §11.
