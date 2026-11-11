# IMMOBILIER — ANNONCEUR (agence ou particulier) — contrat d'API

> **Version 4.131.0** · 11 novembre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Immobilier : `https://api-staging.dira.llc/api/v1/immo`

---

## 0. ⚠️⚠️ LIRE CECI AVANT TOUT LE RESTE

**L'annonceur met en ligne le logement de quelqu'un — souvent le sien.** Une
agence publie celui de ses mandants ; un particulier publie la maison où il
dort. Chaque écran de cette application manipule donc une adresse, des photos
d'intérieur et un numéro de téléphone. Les règles ci-dessous ne sont pas des
manques à contourner : elles sont le contrat.

### 0.1 Une fiche, deux formes

Tout passe par **la fiche d'annonceur** (`publisher`). Elle a une forme, fixée
à la création et qui ne change plus :

| `kind` | Qui | Collaborateurs | Implantations (`sites`) |
|---|---|---|---|
| `individual` | un **particulier** qui publie son bien | aucun | aucune |
| `agency` | une **agence** | oui, rang `manager` ou `agent` | oui |

- **Un compte PORTE la fiche** : le particulier, ou le propriétaire de
  l'agence. Il n'y a **pas** de rang `owner` : le porteur *est* l'agence, et il
  ne peut pas se retirer lui-même par mégarde.
- **Un collaborateur est un autre compte**, rattaché par le porteur. Un compte
  est **soit** porteur de sa propre fiche, **soit** collaborateur d'une agence
  — jamais les deux : sans cette règle, une annonce aurait deux annonceurs et
  personne ne saurait qui en répond.
- **Tout appartient à la FICHE**, jamais au collaborateur qui l'a saisi :
  biens, photos, annonces, créneaux. Une agence qui perd un collaborateur garde
  son catalogue.

⚠️ **UN `agent` NE VOIT QUE LES ANNONCES QU'IL A SAISIES** — et les créneaux et
visites de ces annonces. Les annonces d'un collègue lui répondent `404`, comme
si elles n'existaient pas.

⚠️⚠️ **MAIS LES BIENS ET LEURS PHOTOS SONT OUVERTS À TOUTE L'ÉQUIPE.** Le
serveur n'applique la règle de l'agent **qu'aux annonces** : un agent lit,
modifie et **archive** n'importe quel bien de l'agence, et gère ses photos.
C'est ce que fait le code au 10 novembre 2026. Ne dessinez pas un écran qui
laisse croire qu'un agent ne voit « que ses biens » : il les voit tous.

### 0.2 Ce que le chercheur voit de vous

| Il voit | Il ne voit pas |
|---|---|
| le **nom** de la fiche, sa **forme** (`agency` / `individual`) | ⚠️ le **téléphone** et le **courriel** de la fiche, **avant une visite confirmée** |
| le badge **`verified`** et sa date — posés par l'équipe Dira, **jamais par vous** | le nom du collaborateur qui a saisi l'annonce |
| pour une **agence** : la rue et la position exacte du bien | ⚠️ pour un **particulier** : la rue et la position exacte |
| les photos, **réencodées** (section 7) | les métadonnées des photos, dont la position GPS |

- ⚠️ **L'ADRESSE D'UN PARTICULIER EST UN CERCLE DE 300 M.** L'annonce publique
  montre un point déplacé (de façon stable : recharger la page ne le fait pas
  bouger) et le rayon du cercle ; la rue est vide. L'adresse exacte n'est
  donnée **qu'au chercheur dont la visite est confirmée**, dans sa visite. Une
  agence a pignon sur rue : sa position est exacte.
- ⚠️ **LE TÉLÉPHONE QUE REÇOIT LE CHERCHEUR EST CELUI DE LA FICHE**, une fois
  la visite confirmée — pas celui du compte connecté, ni celui du collaborateur
  qui reçoit. Une fiche sans téléphone donne au chercheur un contact **vide**
  (section 4.1).
- ⚠️ **LE SERVEUR NE RELIT NI LE TITRE, NI LE TEXTE, NI LES PHOTOS.** Un
  particulier qui écrit « villa au 14 rue des Cocotiers » dans son titre, ou
  qui photographie sa façade avec le numéro, défait lui-même le cercle de
  300 m. **Dites-le à côté de ces champs**, au moment de la saisie.

### 0.3 Rien n'est en ligne sans un humain

Une annonce naît **brouillon** (`draft`). L'annonceur la **soumet**
(`in_review`) ; **seule l'équipe Dira la publie**. Un annonceur qui se
publierait lui-même ferait de la relecture une formalité réservée à ceux qui
veulent bien y passer. Ensuite, l'annonceur met en pause, reprend et clôt
lui-même (section 8.6).

### 0.4 Rien n'est facturé aujourd'hui

`GET /immo/settings/publication` annonce un quota d'annonces gratuites et des
prix en jetons. ⚠️ **Vérifié dans le code au 10 novembre 2026 : aucun quota
n'est compté, aucun jeton n'est débité**, à aucune étape. Section 11.

> ⚠️ **SI UN CHAMP INTERDIT APPARAÎT UN JOUR DANS UNE RÉPONSE** — le téléphone
> d'un chercheur sur une visite non confirmée, par exemple —, **NE L'AFFICHEZ
> PAS : signalez-le.** C'est un défaut côté serveur, et une interface qui
> l'affiche le rend définitif.

---

## 1. Deux bases d'URL, aucun socket

| | Rôle | Base |
|---|---|---|
| **Socle** (`dira-core-api`) | inscription, connexion, compte, **appareils et boîte de notifications** | `…/api/v1/` |
| **Immobilier** (`dira-immo-api`) | **fiche**, **collaborateurs**, **biens**, **photos**, **annonces**, **créneaux**, **visites**, **réglages** | `…/api/v1/immo/` |

> **Le jeton est le même des deux côtés.** Une seule connexion, au socle.

Prévoyez deux variables : `API_BASE` et `IMMO_BASE`. Dans ce document, une
route de l'immobilier s'écrit avec son préfixe de passerelle
(`POST /immo/properties` = `POST {IMMO_BASE}/properties`) ; une route du socle
s'écrit sans (`POST /auth/login`).

⚠️ **AUCUN TEMPS RÉEL PAR SOCKET.** Une demande de visite arrive par
**notification** (section 10) ; la liste des visites se **relit**. Un bien et
une annonce ne bougent pas à la seconde.

---

## 1 bis. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et l'immobilier ; toutes les routes de ce document le demandent, sauf l'aperçu public d'une annonce (8.8) |
| Rôle | **`publisher`** : les routes `/immo/...` de ce document refusent un autre rôle (`403 forbidden`) — sauf `GET /immo/settings/publication` (tout compte connecté) et l'aperçu public (sans compte) |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | ⚠️ **trois formes**, dites route par route : `?limit=&cursor=<id>` sur les biens et sur les annonces (elles ne se terminent pas de la même façon), et des listes **bornées sans pagination** ailleurs |
| Montants | **entiers dans la plus petite unité** de la monnaie du pays, toujours **avec** sa monnaie (`price` + `currency`). Jamais de flottant, jamais de conversion |
| Surfaces | mètres carrés, nombre décimal (`84`, `84.5`), **toujours avec leur provenance** (`source`) |
| Dates | ISO 8601 UTC (`2026-11-10T10:41:23Z`). Une **date seule** s'écrit `YYYY-MM-DD` (`available_from`) |
| Coordonnées | `lat`, `lng` **nommés**, dans des objets — jamais un tableau |
| Langue | `Accept-Language: fr` ou `en` — les messages d'erreur suivent |
| Pays | **rien à choisir** : voir *Le pays* ci-dessous |
| Fil | `X-Request-ID` — sur chaque réponse ; à l'aller, facultatif mais recommandé |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat. **Affichez le `message`** quand un refus doit être lu
par la personne : les refus de cette surface disent **quelle case corriger**
(« chaque surface doit dire d'où vient son chiffre »), et les récrire perd la
phrase qui évite l'appel au support.

⚠️⚠️ **SAUF `422 validation_failed` : SON MESSAGE N'EST PAS TRADUIT.** Ce code
générique n'a pas de phrase dans les langues du service : son `message` sort
en anglais technique (`unknown field: country`, `invalid fields: kind`),
même avec `Accept-Language: fr`. **Ne l'affichez pas.** Servez-vous de
`fields` — les **clés JSON** en cause — pour souligner **ces** cases, et écrivez
votre propre phrase. `reason` précise quand ce n'est pas la valeur d'un champ :

| `reason` | Sens | Ce que l'application fait |
|---|---|---|
| `unknown_field` | une clé que la route ne connaît pas — **refusée, pas ignorée** ; son nom est dans `fields` | c'est un **bug de l'application** : à corriger, pas à montrer |
| `invalid_json` | corps illisible (ou date `starts_at` mal écrite) | idem |
| autre | le champ en cause (`file`, `ids`, `alt`, `decision`…) | souligner ce champ |

**`401`** = jeton expiré ou absent : `POST /auth/refresh`, puis rejouer la
requête ; si le rafraîchissement échoue, revenir à la connexion.
**`403`** = ce rôle, ou cette personne, n'a pas accès — **ne pas réessayer**.
**`429 rate_limited`** = trop de requêtes en une minute : attendre, ne pas
boucler (la limite est un réglage du déploiement, n'en codez aucune valeur).

⚠️ **UN SEUL RAFRAÎCHISSEMENT À LA FOIS.** Le jeton de rafraîchissement
**tourne** (l'ancien est consommé). Trois listes chargées ensemble rendent trois
`401` : sans verrou, deux des trois rejouent avec un jeton déjà dépensé et la
session tombe au premier chargement. Un seul appel en vol, les autres
l'attendent.

⚠️ **DEUX LISTES PEUVENT RENDRE `"items": null`** quand elles sont vides : celle
des biens (`GET /immo/properties`) et celle des annonces
(`GET /immo/publishers/me/listings`). Toutes les autres rendent `[]`. Traitez
`null` comme une liste vide — un `items.map(…)` sur `null` plante chez
l'utilisateur, pas en recette.

⚠️ **AUCUNE CRÉATION N'EST IDEMPOTENTE.** Ni `POST /immo/properties`, ni
`POST /immo/listings`, ni l'envoi d'une photo, ni la proposition d'un créneau
ne connaissent de clé d'idempotence : une requête rejouée après un délai
dépassé **crée un second objet**. Avant de rejouer, **relisez la liste** pour
voir si la première est passée.

### Le fil d'une requête — `X-Request-ID`

Chaque réponse porte un `X-Request-ID` : le nom sous lequel cette requête est
écrite dans nos journaux, **y compris sur un refus et sur un `500`**. Gardez-le
avec l'erreur que vous enregistrez — un ticket qui cite le fil se lit en trente
secondes. Vous pouvez envoyer le vôtre : **64 caractères au plus**, dans
`a-z A-Z 0-9 - _ .` ; hors de ces règles il est **refusé, pas tronqué**, et le
serveur tire le sien (la réponse dit lequel a été retenu). ⚠️ **Jamais de
donnée personnelle dedans** : ce mot est recopié dans les journaux de plusieurs
services, et une base de journaux ne se purge pas comme un compte se supprime.

### ⚠️ Le pays — il vient du COMPTE

Dira s'installe pays par pays et **toute donnée est bornée par pays**. Une fois
connecté, le pays est **celui du compte**, inscrit dans le jeton : chaque
lecture et chaque écriture y sont bornées par le serveur.

- **Un bien naît dans le pays du compte.** Aucun corps de requête ne porte de
  champ `country` : l'envoyer rend `422 validation_failed`
  (`reason: unknown_field`). Un annonceur qui choisirait son pays publierait
  dans un marché dont il ne voit ni les prix, ni les règles, ni la monnaie — et
  où personne ne le relit.
- **Les coordonnées du bien doivent tomber dans ce pays** — ou nulle part de
  connu : un point franchement au Ghana pour un compte du Togo est refusé
  (`422 address_outside_country`) ; un point au bord de la mer, que la
  frontière simplifiée ne couvre pas, passe.
- **La monnaie suit le pays** : `XOF` au Togo et au Sénégal, `XAF` au Gabon et
  au Tchad, `GNF` en Guinée (environ quatorze fois plus petit). ⚠️ **Aucune
  monnaie en dur** : affichez celle de chaque montant (`currency`).
- ⚠️ **N'envoyez pas `X-Dira-Country` une fois connecté pour « voir un autre
  pays »** : le pays du compte l'emporte, et la réponse porte, dans le même
  en-tête, le pays **retenu**. Lisez-le pour l'afficher, pas pour le choisir.
- **Une agence présente dans deux pays, ce sont deux fiches et deux comptes.**
  La monnaie, les prix annoncés et la relecture diffèrent d'un pays à l'autre.

⚠️ **LE PAYS D'UN COMPTE SE FIXE À L'INSCRIPTION** (section 2.1) : par
`X-Dira-Country` s'il est envoyé et que ce pays est servi, sinon par
l'indicatif du téléphone. C'est le seul moment où l'en-tête décide.

---

## 2. Entrer — s'inscrire, se connecter

### 2.1 S'inscrire — `POST /auth/register`

Un particulier qui met son appartement en ligne **s'inscrit lui-même** ; un
collaborateur d'agence aussi, avant d'être rattaché (section 5.2).

```json
POST /auth/register
{
  "phone":       "+22890112233",
  "name":        "Afi Mensah",
  "password":    "…",                          // 8 à 128 caractères
  "role":        "publisher",                  // ⚠️ OBLIGATOIRE ici : absent = compte client
  "app":         "immo_pro",
  "email":       "afi@example.tg",             // facultatif
  "first_name":  "Afi", "last_name": "Mensah", // facultatifs
  "device_id":   "<identifiant d'installation>",
  "device_name": "Tecno Spark 10 · Android 13",
  "platform":    "android"                     // accepté et ignoré
}
→ 201 { "access_token": "…", "refresh_token": "…", "user": { … },
        "maps": { … }, "app_lock": { … } }
```

⚠️⚠️ **`"role": "publisher"` EST CE QUI FAIT UN ANNONCEUR.** Sans lui, le
socle crée un compte **client** : la personne est connectée, puis chaque route
de cette application lui répond `403 forbidden`, et le compte ne pourra jamais
devenir annonceur (un compte a un rôle, un seul).

| Refus | Code | Ce que l'écran fait |
|---|---|---|
| Numéro déjà inscrit | `409 phone_taken` | proposer la connexion |
| Numéro d'un compte supprimé, encore en délai de grâce | `403 account_closed` | afficher le `message` |
| Champ manquant ou mal formé | `422 validation_failed` | souligner `fields` |

### 2.2 Se connecter — `POST /auth/login`

```json
POST /auth/login
{
  "phone":       "+22890112233",       // ou "email": "…" — un seul des deux
  "password":    "…",
  "app":         "immo_pro",           // ⚠️ OBLIGATOIRE
  "device_id":   "<identifiant d'installation>",
  "device_name": "Tecno Spark 10 · Android 13",
  "platform":    "android"
}
→ 200 { "access_token": "…", "refresh_token": "…", "user": { … },
        "maps": { "basemap": "dira" }, "app_lock": { … } }
```

⚠️⚠️ **`"app": "immo_pro"` EST OBLIGATOIRE.** Sans lui, un compte client ou
chauffeur se connecte ici, reçoit un jeton parfaitement valide, puis se fait
refuser écran par écran — et croit l'application cassée. Avec lui, le socle
refuse **avant** d'émettre le jeton :

```
→ 403 { "error": { "code": "wrong_app", "reason": "client",
        "message": "Ce compte n'est pas un compte de cette application. Ouvrez celle qui lui correspond — votre mot de passe est correct." } }
```

⚠️ **`error.reason` NOMME L'APPLICATION À OUVRIR** (`client`, `driver`,
`merchant`, `partner`…) : affichez-la. Une valeur inconnue n'est pas une erreur
— repli sur `error.message`, déjà traduit. ⚠️ **Et c'est `reason`, pas
`meta`.** Un annonceur qui cherche aussi un logement le fait dans **Dira Immo**,
l'application du chercheur, **avec un compte client** : un compte n'a qu'un
rôle, et un numéro de téléphone ne porte qu'un compte.

| Refus | Code |
|---|---|
| Mauvais numéro ou mot de passe | `401 invalid_credentials` |
| Mauvaise application | `403 wrong_app` (afficher `reason`) |
| Compte suspendu | `403 account_suspended` |
| Compte supprimé | `403 account_closed` |

⚠️ **PAS DE CODE PAR SMS OU WHATSAPP POUR UN ANNONCEUR.** `POST /auth/otp` et
`POST /auth/otp/verify` ne servent **que** l'application cliente : avec
`"app": "immo_pro"`, ou pour un compte `publisher`, ils répondent
`403 otp_not_available` (« Ce compte se connecte avec un mot de passe. »).
Ne dessinez pas ce parcours.

Puis `POST /auth/refresh` (`{ "refresh_token": "…" }`, rotation) et
`POST /auth/logout` (`{ "refresh_token": "…" }`) pour fermer la session.

### 2.3 Ce qu'une application ajoute

- ⚠️ **`device_id` EST L'IDENTIFIANT D'INSTALLATION**, pas celui du matériel :
  il survit à une mise à jour et meurt avec une réinstallation. Ni l'IMEI, ni
  l'identifiant publicitaire.
- ⚠️ **PLUSIEURS APPAREILS SONT PERMIS, ET LE PLUS ANCIEN EST CHASSÉ** au-delà
  d'un plafond qui est un **réglage de pays** (n'en codez ni n'en affichez
  aucun nombre). La session chassée l'apprend au prochain rafraîchissement de
  son jeton, par un `401` : revenez à la connexion **sans drame**, comme d'une
  session fermée ailleurs — pas comme d'une panne. ⚠️ La règle de l'**appareil
  unique** ne vise que les chauffeurs : un agent immobilier a souvent un
  téléphone et une tablette, et rien ne les fait se chasser.
- **`maps.basemap`** dit quel fond de carte ce pays a choisi pour placer
  l'épingle d'un bien : `dira` (nos tuiles, aucune clé) ou `google` (avec la
  clé **embarquée dans l'application**). ⚠️ **Le serveur ne sert aucune clé.**
  Relisez-le à chaque connexion et rafraîchissement : c'est un réglage de pays.
- **`app_lock`** (`mode` `off` / `optional` / `required`, `biometrics`,
  `pin_length`, `grace_seconds`, `max_attempts`) règle le verrou **local** de
  l'application. Il protège contre quelqu'un qui ramasse le téléphone d'un
  agent — et ce téléphone porte les numéros des visiteurs confirmés.
- **Hors ligne : aucune route de synchronisation** sur cette surface. Gardez
  pour l'affichage la dernière liste de biens, d'annonces et de visites, **avec
  leur heure** ; ⚠️ **ne mettez jamais en file d'attente** une décision de
  visite, une présence, un changement d'état d'annonce ou un rattachement : la
  situation aura changé quand la file repartira, et l'autre partie aura été
  prévenue à tort.

---

## 3. La porte — `GET /immo/publishers/me`

**Appelez-la en premier, après chaque connexion.** Elle dit de quelle fiche
parle ce compte, et **à quel titre**.

```json
GET /immo/publishers/me

→ 200 {
  "publisher": {
    "id":         "6b0f3c1a9e2d4f5a6b7c8d90",
    "user_id":    "6b0f3c1a9e2d4f5a6b7c8d01",
    "country":    "TG",
    "kind":       "agency",
    "name":       "Lomé Habitat",
    "phone":      "+22890112233",
    "email":      "contact@lome-habitat.tg",
    "sites":      [ { "id": "6b0f3c1a9e2d4f5a6b7c8d55", "name": "Agence de Kara",
                      "address": "Avenue Eyadéma", "phone": "+22890445566" } ],
    "verified":   true,
    "verified_at": "2026-11-02T09:12:00Z",
    "verified_by": "6a77c5be57f350bd9013280d",
    "created_at": "2026-10-28T08:00:00Z",
    "updated_at": "2026-11-02T09:12:00Z"
  },
  "membership": {                     ← ABSENT quand le compte PORTE la fiche
    "id":           "6b0f3c1a9e2d4f5a6b7c8e10",
    "publisher_id": "6b0f3c1a9e2d4f5a6b7c8d90",
    "user_id":      "6b0f3c1a9e2d4f5a6b7c8d77",
    "country":      "TG",
    "role":         "agent",
    "site_id":      "6b0f3c1a9e2d4f5a6b7c8d55",
    "name":         "Kossi Agbeko",
    "phone":        "+22891223344",
    "created_at":   "2026-11-03T10:00:00Z",
    "updated_at":   "2026-11-03T10:00:00Z"
  }
}
```

| Réponse | Ce que c'est | Ce que l'application ouvre |
|---|---|---|
| `200`, **sans** `membership` | ce compte **porte** la fiche (particulier, ou propriétaire de l'agence) | tout |
| `200`, **avec** `membership` | ce compte est **collaborateur** ; `membership.role` dit son rang | selon le rang (tableau de la section 5.1) |
| `404 publisher_not_found` | ce compte n'a **aucune** fiche et n'est rattaché à aucune agence | ⚠️ l'accueil à **deux portes**, ci-dessous |

⚠️⚠️ **`404 publisher_not_found` N'EST PAS UNE PANNE, C'EST UN ACCUEIL À DEUX
PORTES** :

1. **« Je publie pour moi » / « J'ouvre mon agence »** → créer la fiche
   (section 4.1).
2. **« Je travaille pour une agence »** → **ne rien créer**. Afficher le numéro
   de téléphone du compte, à donner au propriétaire de l'agence, qui le
   rattache (section 5.2) ; puis relire la porte.

⚠️ **UN COLLABORATEUR QUI CRÉE SA PROPRE FICHE NE PEUT PLUS ÊTRE RATTACHÉ** :
le rattachement lui est refusé (`409 publisher_exists`), et il n'existe aucune
route pour supprimer une fiche. C'est l'erreur la plus probable du premier
jour : écrivez la seconde porte aussi lisiblement que la première.

🆕 **UN RATTACHEMENT ET UN DÉTACHEMENT PRÉVIENNENT LE COLLABORATEUR**
(v4.129.0) : `immo_member_attached` / `immo_member_detached` (section 10).
Jusqu'au 11 novembre, rien ne partait — il découvrait qu'il publiait pour une
agence en rouvrant l'application. ⚠️ **Relisez quand même la porte à chaque
ouverture** : une notification peut ne pas arriver (téléphone éteint, jeton
périmé), et la porte, elle, dit toujours vrai.

⚠️ **AILLEURS, LE MÊME CAS PORTE UN AUTRE CODE.** Un compte sans fiche qui
appelle les biens, les annonces ou les visites reçoit `403 not_a_publisher`
— pas `404 publisher_not_found`. Les deux veulent dire « pas de fiche » :
renvoyez vers la porte dans les deux cas.

---

## 4. La fiche annonceur

### 4.1 Créer — `POST /immo/publishers`

```json
POST /immo/publishers
{
  "kind":  "agency",                    // individual | agency — OBLIGATOIRE, définitif
  "name":  "Lomé Habitat",              // 2 à 160 caractères — OBLIGATOIRE
  "phone": "+22890112233",              // facultatif, format international (+228…)
  "email": "contact@lome-habitat.tg"    // facultatif
}

→ 201 { "id": "…", "user_id": "…", "country": "TG", "kind": "agency",
        "name": "Lomé Habitat", "phone": "+22890112233",
        "email": "contact@lome-habitat.tg", "sites": [], "verified": false,
        "created_at": "…", "updated_at": "…" }
```

⚠️ **LA RÉPONSE EST LA FICHE NUE**, pas l'enveloppe `{ publisher, membership }`
de la porte. Rangez-la sous `publisher`.

- **`country` et `verified` ne s'envoient pas** : le pays vient du compte, le
  badge d'un humain de l'équipe Dira. Les envoyer rend `422 validation_failed`
  (`reason: unknown_field`).
- ⚠️⚠️ **`phone` EST FACULTATIF POUR LE SERVEUR, PAS POUR LA PERSONNE.** C'est
  **le numéro que reçoit un chercheur** dont la visite est confirmée. Sans lui,
  la visite confirmée lui donne un contact au téléphone vide — et il vient sans
  pouvoir appeler. **Demandez-le à la création**, et proposez par défaut le
  numéro du compte.
- **`kind` est définitif.** Aucune route ne fait d'un particulier une agence.
  Dites-le avant de valider.
- `phone` et `email` sont **absents** de la réponse quand ils sont vides.
  `verified` est **toujours** servi (`false` n'est pas « inconnu »).
  `verified_at` et `verified_by` sont **absents** tant que le badge n'est pas
  posé, et disparaissent s'il est retiré.

| Refus | Code | Ce que l'écran fait |
|---|---|---|
| Ce compte a déjà sa fiche | `409 publisher_exists` | relire la porte |
| Ce compte est collaborateur d'une agence | `409 publisher_is_member` | relire la porte : il travaille déjà pour une agence |
| Forme, nom, téléphone ou courriel invalide | `422 validation_failed` | souligner `fields` (`kind`, `name`, `phone`, `email`) |

### 4.2 Lire

`GET /immo/publishers/me` — la porte, section 3.

⚠️ **`sites` PEUT VALOIR `null`** à la lecture d'une fiche qui n'a aucune
implantation (il vaut `[]` dans la réponse de création seulement). Traitez
`null` comme une liste vide.

### 4.3 Modifier — `PATCH /immo/publishers/me`

**Le porteur seul.** Un collaborateur, même `manager`, reçoit
`403 not_the_agency_owner` : le nom, le téléphone et les implantations engagent
l'agence entière.

```json
PATCH /immo/publishers/me
{
  "name":  "Lomé Habitat & Fils",      // facultatif
  "phone": "+22890112299",             // facultatif ; "" efface
  "email": "",                         // facultatif ; "" efface
  "sites": [                           // facultatif ; ⚠️ REMPLACE la liste entière
    { "id": "6b0f3c1a9e2d4f5a6b7c8d55", "name": "Agence de Kara",
      "address": "Avenue Eyadéma", "phone": "+22890445566" },
    { "name": "Agence de Sokodé" }     // sans id : le serveur en donne un
  ]
}
→ 200 la fiche nue (comme à la création)
```

- **Champ absent = inchangé.** `kind` ne s'envoie pas (`422`, `unknown_field`).
- 🆕 **`name` : 2 à 160 caractères, espaces retirés** (v4.124.0) — `"name": ""`
  est **refusé** (`422 validation_failed`, `fields: ["name"]`) — un nom fait
  d'espaces aussi, avec `reason` `"0 — 2…160"`. Il effaçait le nom de l'agence
  jusqu'au 10 novembre. (🔧 4.125.0 : la 4.124.0 promettait `reason` sur le nom
  vide ; il n'y est que pour un nom fait d'espaces.)
- ⚠️ **`sites` REMPLACE, IL NE FUSIONNE PAS.** Reposez **toutes** les
  implantations, **avec leur `id`** : un `id` conservé garde les collaborateurs
  qui y sont rattachés ; un site renvoyé sans `id` en reçoit un **nouveau**.
  50 implantations au plus. Un site a `id`, `name`, et `address` et `phone`
  facultatifs (absents quand vides).
- 🆕 ⚠️ **UN `id` DE SITE EST CELUI D'UN SITE DE LA FICHE, OU RIEN** (v4.124.0).
  Un identifiant inventé (`"kara"`) ou répété est **refusé** :
  `422 unknown_site`, `fields: ["sites"]`, `reason` = l'identifiant en cause.
  Il était rangé tel quel, et le site ne pouvait plus jamais recevoir personne.
- 🆕 ⚠️ **RETIRER UN SITE OÙ TRAVAILLENT ENCORE DES COLLABORATEURS EST
  REFUSÉ** : `409 site_has_members`, `reason` = les identifiants des sites en
  cause. Ils restaient rattachés à un site qui n'existait plus. Réaffectez-les
  d'abord (section 5.3), puis retirez le site.
- 🆕 **`PATCH {}` EST REFUSÉ** : `422 nothing_to_update`, comme pour les biens et
  les annonces (il répondait `200` sans rien changer).

| Refus | Code |
|---|---|
| Un collaborateur tente de modifier | `403 not_the_agency_owner` |
| Un particulier envoie `sites` | `422 members_need_an_agency` |
| Un `id` de site inventé ou répété | `422 unknown_site` (`reason` = l'identifiant) |
| Un site retiré porte encore des collaborateurs | `409 site_has_members` (`reason` = les sites) |
| Corps vide | `422 nothing_to_update` |
| Pas de fiche | `404 publisher_not_found` |
| Valeur invalide (dont un nom de moins de 2 caractères) | `422 validation_failed` |

---

## 5. Les collaborateurs d'une agence

### 5.1 Trois rangs — et ce que le serveur laisse faire à chacun

| Geste | Porteur de l'agence | `manager` | `agent` | Particulier |
|---|---|---|---|---|
| Lire la fiche | oui | oui | oui | oui |
| Modifier la fiche | oui | ⚠️ non (`403`) | non (`403`) | oui (sans `sites`) |
| Lire l'équipe | oui | oui | oui | oui (liste vide) |
| Rattacher, modifier, détacher | oui | ⚠️ **non** (`403`) | non (`403`) | non (`422 members_need_an_agency`) |
| Biens et photos — **lire** | **tous** ceux de la fiche | tous | **tous** (le stock de l'agence) | tous |
| 🆕 Biens et photos — **modifier, archiver, ajouter/ordonner/retirer/décrire une photo** | tous | tous | ⚠️ **ceux qu'il a saisis** (`403 property_edit_reserved`) | tous |
| Annonces (lire, modifier, changer d'état, historique) | toutes | toutes | **les siennes** | toutes |
| Créneaux d'une annonce | toutes | toutes | ceux de ses annonces | toutes |
| Visites (lire, décider, constater) | toutes | toutes | celles de ses annonces | toutes |
| **Notifications de visite** | ⚠️ seulement pour les annonces **qu'il a saisies** — 🆕 **plus** `immo_visit_unanswered` pour toute demande de la fiche restée sans réponse | idem | ⚠️ seulement les siennes | toutes |

- ⚠️ **UN `manager` NE GÈRE PAS L'ÉQUIPE**, et ce n'est pas un oubli : ajouter
  quelqu'un, c'est lui donner le droit de publier au nom de l'agence. C'est un
  geste de propriétaire. Un `manager` **voit** tout et **relit** le travail des
  autres.
- ⚠️⚠️ **LA NOTIFICATION VA À QUI A SAISI L'ANNONCE**, pas à l'agence. Si un
  agent saisit une annonce, lui seul est prévenu des demandes de visite ; le
  porteur et les `manager` les **voient** dans la liste. 🆕 (v4.131.0) Et si
  l'agent ne tranche pas à temps — **4 h après la demande, ou à mi-chemin du
  créneau si c'est plus tôt** —, le porteur et les `manager` reçoivent
  `immo_visit_unanswered`, une seule fois (section 10).
- 🆕 ⚠️ **UN `agent` VOIT TOUS LES BIENS, NE MODIFIE QUE LES SIENS** (v4.131.0,
  décision du 11 nov.). Il pose ses annonces sur le stock de l'agence, mais ne
  peut ni modifier, ni archiver, ni toucher aux photos d'un bien qu'un collègue
  a saisi : `403 property_edit_reserved`. Le bien porte `created_by` (le compte
  qui l'a saisi) : sur un bien dont `created_by` n'est pas ce compte, **masquez**
  « modifier », « archiver » et les gestes de photo pour un `agent`. ⚠️ Un bien
  saisi avant le 11 novembre n'a pas de `created_by` : seuls le porteur et les
  `manager` le modifient.
- **Un collaborateur voit ses collègues**, et c'est voulu : il travaille avec
  eux. Il ne peut pas modifier la liste.

### 5.2 Rattacher — `POST /immo/publishers/me/members`

**Le porteur d'une agence, seul.** Le compte à rattacher doit **déjà exister**,
avec le rôle `publisher`, **dans le même pays**, et **sans fiche à lui**.

```json
POST /immo/publishers/me/members
{
  "phone":   "+22891223344",               // ⚠️ le numéro COMPLET, format international
  "role":    "agent",                      // manager | agent — OBLIGATOIRE
  "site_id": "6b0f3c1a9e2d4f5a6b7c8d55"    // facultatif : une implantation de l'agence
}
→ 201 {
  "id": "6b0f3c1a9e2d4f5a6b7c8e10", "publisher_id": "6b0f3c1a9e2d4f5a6b7c8d90",
  "user_id": "6b0f3c1a9e2d4f5a6b7c8d77", "country": "TG", "role": "agent",
  "site_id": "6b0f3c1a9e2d4f5a6b7c8d55", "name": "Kossi Agbeko",
  "phone": "+22891223344", "created_at": "…", "updated_at": "…"
}
```

- **`phone` ou `user_id`.** `user_id` (24 caractères hexadécimaux) sert à la
  console d'exploitation, qui a l'identifiant sous la main. ⚠️ **Depuis
  l'application, rattachez par téléphone** : c'est la seule voie où le serveur
  ne cherche **que** des comptes `publisher` **du pays** (voir l'encadré
  ci-dessous).
- ⚠️ **UNE SEULE RÉPONSE, OU AUCUNE.** La recherche par téléphone est « contient
  » : un fragment de numéro peut désigner plusieurs comptes, et rattacher « le
  premier » donnerait à quelqu'un les droits d'un autre. Zéro ou plusieurs
  comptes trouvés rendent `404 account_not_found`, avec `reason` = `"0"` ou
  `"2+"` et `fields: ["phone"]`. Pour `"2+"`, demandez le numéro **complet**.
- `name` et `phone` du collaborateur sont une **copie** prise au rattachement :
  quelqu'un qui change de nom au socle garde l'ancien ici. Ils sont absents
  quand le socle n'en avait pas ; `site_id` est absent sans implantation.

> 🆕 **PAR `user_id` COMME PAR TÉLÉPHONE, LE COMPTE DOIT ÊTRE UN ANNONCEUR DU
> PAYS DE L'AGENCE** (v4.124.0). Un compte client, ou d'un autre pays, rend
> `404 account_not_found` avec `fields` = la case remplie (`["user_id"]` ou
> `["phone"]`) — **le même refus que « personne »**, exprès : dire « ce compte
> existe, mais au Sénégal » renseignerait sur un compte qui n'est pas le vôtre.
> Jusqu'au 10 novembre, l'identifiant laissait passer un compte client, qui ne
> pouvait ensuite jamais ouvrir cette application.

| Refus | Code | Ce que l'écran fait |
|---|---|---|
| Ni `phone` ni `user_id` | `422 validation_failed`, `fields: ["user_id", "phone"]` | souligner le téléphone |
| Aucun ou plusieurs comptes pour ce numéro | `404 account_not_found` (`reason` `"0"` / `"2+"`) | « vérifiez le numéro complet ; la personne doit d'abord s'inscrire dans l'application Dira Immo Pro » |
| Identifiant inconnu (par `user_id`) | `404 user_not_found` (refus du socle) | idem |
| 🆕 Le compte n'est pas un annonceur, ou pas de ce pays | `404 account_not_found` (`fields` = la case remplie) | idem |
| Vous vous rattachez vous-même | `422 cannot_attach_yourself` | afficher le `message` |
| La personne a déjà **sa propre fiche** | `409 publisher_exists` | « ce compte publie déjà pour lui-même : il ne peut pas rejoindre une agence » |
| La personne est **déjà collaboratrice** (ici ou ailleurs), ou rattachement simultané | 🆕 `409 member_already_attached` (« Ce compte appartient déjà à un annonceur ») | afficher le `message` ; relire l'équipe. ⚠️ C'était `publisher_is_member`, dont la phrase parlait d'« ouvrir sa propre fiche » |
| Implantation inconnue | `422 unknown_site` | proposer la liste des sites |
| Vous êtes collaborateur, pas porteur | `403 not_the_agency_owner` | masquer le bouton |
| Vous êtes un particulier | `422 members_need_an_agency` | masquer le bouton ; dire que seule une agence a une équipe |
| Le socle ne répond pas | `503 core_unavailable` | réessayer plus tard |

### 5.3 Lister, modifier, détacher

```
GET    /immo/publishers/me/members          → 200 { "items": [ <collaborateur>, … ] }
PATCH  /immo/publishers/me/members/{id}     { "role"?: "manager", "site_id"?: "…" | "" }  → 200 <collaborateur>
DELETE /immo/publishers/me/members/{id}                                                   → 204
```

- `{id}` est l'**identifiant du rattachement** (`items[].id`), pas celui du
  compte (`user_id`).
- La liste : les plus récents d'abord, **500 au plus**, **sans pagination** ;
  `items` vaut `[]` pour une agence sans équipe et pour un particulier.
- `PATCH` : `role` change le rang **immédiatement** (un `agent` passé
  `manager` voit tout à la requête suivante). `"site_id": ""` retire
  l'implantation. 🆕 **`PATCH {}` est refusé** (`422 nothing_to_update`).
- **Porteur seul** pour `PATCH` et `DELETE` (`403 not_the_agency_owner`).
  Collaborateur introuvable ou d'une autre agence : `404 member_not_found`.

⚠️⚠️ **CE QUE LE DÉTACHEMENT NE FAIT PAS — À ÉCRIRE DANS LA CONFIRMATION :**

- **Les annonces de l'ancien collaborateur restent à l'agence.** Le porteur et
  les `manager` continuent de les voir et de les gérer. Bien.
- 🆕 **LES NOTIFICATIONS DE CES ANNONCES VONT AU PORTEUR DE LA FICHE** (v4.128.0) :
  demandes, annulations et rappels de visite, messages des chercheurs (9.7).
  Jusqu'au 10 novembre, elles continuaient d'aller à l'ancien collaborateur,
  qui ne pouvait plus agir — et l'agence n'en savait rien. Les visites et les
  fils gardent la trace de qui avait saisi l'annonce ; seul le destinataire
  change. Dites-le dans la confirmation : « ses visites et ses messages vous
  seront adressés ».
- Le compte détaché peut ensuite ouvrir **sa propre fiche** : il ne voit plus
  rien de l'agence.

---

## 6. Les biens

### 6.1 Le bien n'est pas l'annonce

**Le bien** (`property`) est ce qui reste quand l'offre change : son type, son
adresse, ses pièces, ses surfaces et leur provenance, ses équipements.
**L'annonce** (`listing`, section 8) est ce qu'on en propose, à quel prix. Le
même appartement se loue cette année et se vend l'an prochain : **un bien,
deux annonces**, et ses mesures ne se ressaisissent pas.

⚠️ **LE BIEN NE PORTE NI TITRE, NI TEXTE, NI PRIX.** Un titre est un argument de
vente, et il ne se dit pas pareil à la location et à la vente : il vit sur
l'annonce.

### 6.2 Créer — `POST /immo/properties`

```json
POST /immo/properties
{
  "type": "apartment",
  "address": {
    "label":        "12 rue des Hydrocarbures",
    "neighborhood": "Tokoin Doumassesse",
    "city":         "Lomé",
    "lat":          6.1517,
    "lng":          1.2186
  },
  "rooms": { "bedrooms": 2, "bathrooms": 1, "floor": 1, "year_built": 2015 },
  "amenities": ["running_water", "air_conditioning", "parking", "gated_compound"],
  "areas": [
    { "kind": "living", "square_meters": 84, "source": "declared" },
    { "kind": "built",  "square_meters": 96, "source": "declared" }
  ],
  "furnished": false,
  "documents": []
}
```

**Obligatoires** : `type`, `address.lat`, `address.lng`, et **la surface que
le type exige** (6.4). Tout le reste est facultatif.

⚠️ **QUATRE CHAMPS NE S'ENVOIENT JAMAIS** : `country`, `publisher_id`,
`verified`, `has_3d`. Le pays et l'annonceur viennent du jeton ; `verified` est
écrit par la modération, `has_3d` par la capture 3D. Les envoyer rend
`422 validation_failed` qui les **nomme** (`reason: unknown_field`). Une
application qui laisserait cocher « visite 3D » ferait vendre des visites qui
n'existent pas.

**La réponse** — `201`, le bien :

```json
{
  "id":           "6b1a0e5f2c3d4e5f6a7b8c01",
  "publisher_id": "6b0f3c1a9e2d4f5a6b7c8d90",
  "created_by":   "6b0f3c1a9e2d4f5a6b7c8d91",
  "country":      "TG",
  "type":         "apartment",
  "address":      { "label": "12 rue des Hydrocarbures", "neighborhood": "Tokoin Doumassesse",
                    "city": "Lomé", "lat": 6.1517, "lng": 1.2186 },
  "rooms":        { "bedrooms": 2, "bathrooms": 1, "floor": 1, "year_built": 2015 },
  "amenities":    ["running_water", "air_conditioning", "parking", "gated_compound"],
  "areas": [
    { "kind": "living", "square_meters": 84, "source": "declared",
      "accuracy_m2": null, "source_family": "declared" },
    { "kind": "built",  "square_meters": 96, "source": "declared",
      "accuracy_m2": null, "source_family": "declared" }
  ],
  "documents":    [],
  "furnished":    false,
  "has_3d":       null,
  "verified":     null,
  "archived_at":  null,
  "created_at":   "2026-11-10T09:00:00Z",
  "updated_at":   "2026-11-10T09:00:00Z"
}
```

- **Tous les champs sont toujours servis** ; `amenities`, `areas` et
  `documents` sont des listes, **jamais `null`**.
- `publisher_id` est **l'agence**, pas le collaborateur qui a saisi.
- ⚠️ **TROIS BOOLÉENS À TROIS ÉTATS** — `null` n'est pas `false` :

| Champ | `null` | `false` | `true` |
|---|---|---|---|
| `furnished` | personne n'a répondu | non meublé | meublé |
| `has_3d` | **aucune capture tentée** | une capture a tourné sans jumeau visitable | visite 3D disponible |
| `verified` | **pas encore regardé** par l'équipe | **regardé et refusé** | les papiers ont été vus |

⚠️ **N'ÉCRIVEZ JAMAIS « REFUSÉ » POUR `verified: null`.** Confondre « en
attente » et « refusé » est une accusation. Et aujourd'hui, aucune route
n'écrit `verified` ni `has_3d` sur un bien : ils restent `null`.

### 6.3 Les types de bien — un vocabulaire fermé

`apartment` · `house` · `villa` · `land` · `commercial` · `room`

Un type hors liste rend `422 property_type_unknown`, dont `reason` cite la
valeur reçue et la liste. Le serveur range la casse et les espaces
(`" Apartment "` devient `apartment`), mais ne traduit pas : « appartement » est
refusé.

### 6.4 Les surfaces — ⚠️ toute surface porte sa source

**C'est la règle centrale de cette surface.** Un écran qui affiche « 84 m² »
sans dire d'où vient le chiffre fait croire à une mesure là où quelqu'un a tapé
ce qui l'arrangeait. Une surface **sans provenance est refusée**
(`422 area_source_required`) — avant même que son chiffre soit regardé.

Une surface :

```json
{ "kind": "living", "square_meters": 84, "source": "declared", "accuracy_m2": null }
```

**`kind`** — trois genres, **chacun une fois au plus** (`422 area_kind_duplicated`) :

| `kind` | Sens |
|---|---|
| `living` | surface **habitable** (pour un local commercial : sa surface **utile**) |
| `built` | surface **construite, tous niveaux** — pas l'emprise au sol |
| `plot` | surface du **terrain** |

**Ce que chaque type exige et interdit** :

| `type` | Surface **exigée** | Surface **interdite** | `rooms` |
|---|---|---|---|
| `apartment` | `living` | `plot` (la parcelle est à l'immeuble) | oui |
| `house` | `living` | — | oui |
| `villa` | `living` | — | oui |
| `land` | `plot` | `living` (un `built` reste permis : une dalle, une annexe) | ⚠️ **aucune** |
| `commercial` | `living` | — | oui |
| `room` | `living` | `plot` | oui |

**`source`** — six provenances, une **famille** dérivée par le serveur :

| `source` | `source_family` | Possible sur `living` / `built` | Possible sur `plot` |
|---|---|---|---|
| `declared` | `declared` | oui | oui |
| `surveyed` (géomètre) | `surveyed` | oui | oui |
| `measured_lidar` | `measured` | oui | ⚠️ non — portée de quelques mètres |
| `measured_ar` | `measured` | oui | non |
| `measured_photogrammetry` | `measured` | oui | oui (par drone) |
| `gps_polygon` (contour marché) | `measured` | ⚠️ non — le GPS dérive à l'intérieur | oui |

🆕 ⚠️⚠️ **UNE SURFACE « MESURÉE » NE SE SAISIT PAS** (v4.124.0). Les `measured_*`
et `gps_polygon` sont réservés à une mesure réellement faite par un capteur —
l'application de capture, à venir (section 12). Posés par cette application, ils
sont **refusés** : `422 area_source_reserved`, `fields: ["areas"]`, `reason`
`"measured_lidar / living"`. Une étiquette « mesuré » posée sur un chiffre tapé
est exactement le mensonge que ce champ existe pour empêcher — et le serveur la
croyait jusqu'au 10 novembre.

- **Proposez `declared`** ; `surveyed` seulement si la personne a un plan de
  géomètre à joindre (le serveur l'accepte : rien ne le vérifie encore).
- ⚠️ **UNE MESURE DÉJÀ ÉCRITE, RENVOYÉE TELLE QUELLE, PASSE** : reposer les
  surfaces lues pour corriger le quartier ne fait rien refuser. C'est une
  mesure **nouvelle ou modifiée** (chiffre, précision, provenance) qui est
  refusée — une mesure qu'on corrige à la main redevient une saisie :
  renvoyez-la en `declared`.

**`source_family`** est **servie et jamais lue** : elle se calcule depuis
`source`. L'application qui repose ce qu'elle a lu peut la renvoyer — elle est
acceptée et ignorée. C'est `source` qui décide, toujours.

**Les bornes** :

| | Bornes | Refus |
|---|---|---|
| `square_meters`, `living` et `built` | 1 à 50 000 m² | `422 area_out_of_range` |
| `square_meters`, `plot` | 1 à 10 000 000 m² (mille hectares) | `422 area_out_of_range` |
| `accuracy_m2` (± m²) | absent ou `null` = « on ne sait pas » ; sinon ≥ 0 et **strictement inférieur** à la surface | `422 area_accuracy_out_of_range` |
| `accuracy_m2` sur une surface `declared` | ⚠️ **interdit** — rien n'a été mesuré, rien n'est précis | `422 area_accuracy_without_measure` |
| `living` > `built` | ⚠️ **interdit** — presque toujours deux chiffres inversés | `422 areas_inconsistent` |

`accuracy_m2: 0` veut dire « exact au mètre carré près ». Ce n'est pas
l'absence.

⚠️ **POURQUOI LES PLAFONDS DIFFÈRENT :** un hectare de terrain est banal en
périphérie de Lomé, dix mille mètres carrés habitables ne le sont pas. Un
appartement saisi en centimètres carrés (840 000 au lieu de 84) est refusé ;
dites « vérifiez l'unité » à côté de la case.

Le `reason` de chaque refus cite ce qui est en cause (`"living 84 > built 60"`,
`"land / living"`, `"measured_lidar / plot"`) : il sert à souligner la bonne
ligne. Le `message`, traduit, se montre à la personne.

### 6.5 Les pièces — `rooms`

```json
"rooms": { "bedrooms": 0, "bathrooms": null, "floor": 0, "year_built": 2027 }
```

Quatre nombres entiers, **chacun facultatif** — et `null` n'est pas `0` :

| Champ | Bornes | Ce que `0` veut dire |
|---|---|---|
| `bedrooms` | 0 à 100 | un **studio** — c'est une information |
| `bathrooms` | 0 à 100 | aucune salle d'eau |
| `floor` | −5 à 200 | le **rez-de-chaussée** ; un étage négatif est un sous-sol |
| `year_built` | 1800 à l'année en cours + 5 | — (un immeuble se vend sur plan) |

Hors bornes : `422 rooms_out_of_range`, `fields` nomme la case. ⚠️ **Un
terrain (`land`) n'a aucune pièce** : un seul des quatre renseigné rend
`422 rooms_not_for_type`, et `fields` liste **toutes** les cases à vider.

### 6.6 Les équipements — `amenities`

Un vocabulaire **fermé**, choisi pour l'Afrique de l'Ouest (un forage décide
d'une location à Lomé bien plus qu'un lave-vaisselle) :

`running_water` · `borehole` · `water_tank` · `hot_water` · `septic_tank` ·
`electricity` · `generator` · `solar_power` · `air_conditioning` ·
`ceiling_fan` · `fitted_kitchen` · `laundry_room` · `storage_room` · `parking` ·
`garage` · `elevator` · `wheelchair_access` · `gated_compound` ·
`perimeter_wall` · `security_guard` · `balcony` · `terrace` · `garden` ·
`swimming_pool` · `fiber_internet`

- Un mot inconnu rend `422 amenity_unknown`, et `reason` cite **tous** les mots
  refusés, pas le premier. Traduisez les libellés **dans l'application** ;
  n'envoyez que ces clés.
- Les doublons sont absorbés, la casse rangée.
- ⚠️ **« Meublé » n'est pas un équipement** : c'est le champ `furnished` à trois
  états (6.2). Ne mettez pas une case « meublé » dans la liste des équipements.

### 6.7 L'adresse

| Champ | |
|---|---|
| `label` | la rue, telle qu'elle se lit. ⚠️ **Jamais montrée au public pour un particulier** |
| `city` 🆕 | ⚠️ **obligatoire** (v4.128.0) : c'est ce que le chercheur tape, et le filtre de ville de la recherche ne trouve jamais un bien sans ville — `422 address_city_required`, `fields: ["address.city"]` |
| `neighborhood` | ce que le chercheur tape aussi. Le serveur ne l'exige pas : **demandez-le quand même**, une annonce sans quartier ne se trouve pas |
| `lat`, `lng` | **obligatoires** |

| Refus | Code | `reason` |
|---|---|---|
| `lat` ou `lng` absent | `422 address_invalid`, `fields: ["address.lat", "address.lng"]` | `… — missing` |
| hors de [−90, 90] / [−180, 180] | `422 address_invalid` | `… — out of range` |
| ⚠️ `(0, 0)` | `422 address_invalid` | `… — null island` |
| dans un autre pays connu | `422 address_outside_country` | `TG / GH (6.1, 1.1)` |

⚠️ **LE POINT (0, 0) EST LE PLUS FRÉQUENT DES FAUX.** C'est ce qu'envoie un
téléphone sans position GPS : un point au large du golfe de Guinée,
parfaitement valide, refusé exprès. **N'envoyez jamais une position par
défaut** : sans position, ne validez pas le formulaire.

### 6.8 Les titres de propriété — `documents`

Une liste de **références** `http://` ou `https://` (20 au plus, 2 048
caractères chacune ; `422 document_invalid` sinon). ⚠️ **Jamais une image en
`data:`** : un titre foncier photographié pèse quatre mégaoctets, et le bien
cesserait de s'enregistrer au sixième.

⚠️ **CETTE SURFACE NE SERT AUCUN ENVOI DE TITRE, ET PERSONNE NE LES LIT
AUJOURD'HUI.** Le champ existe, rien ne le vérifie. **Laissez-le à `[]`** et ne
dessinez pas d'écran « joindre votre titre » qui promettrait une vérification
qui n'a pas lieu (section 12).

### 6.9 Lister — `GET /immo/properties`

```
GET /immo/properties?archived=&limit=20&cursor=<id>

→ 200 { "items": [ <bien>, … ], "next_cursor": "6b1a0e5f2c3d4e5f6a7b8c01" }
```

- **Tous les biens de la fiche**, les plus récents d'abord — y compris pour un
  `agent` (section 0.1).
- `archived` : **absent** = les biens vivants · `only` = la corbeille · `all` =
  les deux. Toute autre valeur rend `422 validation_failed`
  (`fields: ["archived"]`) — elle n'est pas ignorée, sans quoi « la corbeille »
  aurait l'air vide.
- `limit` : 20 par défaut, 100 au plus.
- ⚠️ **`next_cursor` ABSENT = DERNIÈRE PAGE.** Il n'est servi que lorsque la
  page est **pleine** : une page pleine peut donc être suivie d'une page vide.
- ⚠️ **`items` VAUT `null` QUAND IL N'Y A RIEN.** Traitez-le comme `[]`.
- Un curseur illisible rend `422 validation_failed` (`fields: ["cursor"]`).

### 6.10 Lire — `GET /immo/properties/{id}`

→ `200`, le bien. Un bien d'une autre fiche, d'un autre pays, ou un identifiant
mal formé rendent tous `404 property_not_found` : on ne confirme pas
l'existence du bien d'un concurrent.

### 6.11 Modifier — `PATCH /immo/properties/{id}`

**La modification est partielle au premier niveau, et remplace à l'intérieur
d'un bloc.**

| Ce que vous envoyez | Ce qui se passe |
|---|---|
| un champ absent | inchangé |
| `type` | remplacé |
| `address` | ⚠️ le **bloc entier** est reposé : un champ omis dedans redevient vide — et `lat`/`lng` omis sont refusés |
| `rooms` | ⚠️ le **bloc entier** : un champ omis redevient `null` |
| `amenities`, `documents` | la **liste entière** ; `[]` = plus rien |
| `areas` | la **liste entière** ; ⚠️ `[]` est **refusé** (`area_required`) — c'est l'écriture la plus destructrice, et elle n'a l'air de rien |
| `furnished` | `true` ou `false` ; ⚠️ **impossible de revenir à `null`** |

⚠️⚠️ **LA VÉRIFICATION PORTE SUR LE BIEN FUSIONNÉ**, pas sur les seuls champs
envoyés. `PATCH {"type": "land"}` sur un appartement qui garde sa surface
habitable est **refusé** (`422 area_kind_not_for_type`, `reason: "land /
living"`), bien que la requête n'ait parlé d'aucune surface : c'est le
**résultat** qui serait impossible. Quand vous changez le type, reposez dans
la même requête les surfaces et les pièces qui vont avec.

- **Reposez ce que vous avez lu** pour les blocs et les listes : l'aller-retour
  d'un bien lu est accepté tel quel (`source_family` compris).
- ⚠️ **`PATCH {}` EST REFUSÉ** : `422 nothing_to_update`. Accepté, il aurait
  répondu « enregistré » à celui dont le champ s'était perdu en route.
- ⚠️ **UN BIEN ARCHIVÉ NE SE MODIFIE PLUS** : `409 property_archived`.
- 🆕 **Un `agent` ne modifie que les biens qu'il a saisis** : `403
  property_edit_reserved` (section 5.1). Il en va de même pour archiver.
- → `200`, le bien à jour. Ses annonces en ligne sont réindexées par le
  serveur : la recherche voit la nouvelle surface sans délai.

### 6.12 Archiver — `DELETE /immo/properties/{id}`

**Un bien ne se supprime pas : il s'archive.** Une annonce le désigne, et une
annonce dont le bien aurait disparu serait une page vide en ligne.

→ `200`, **le bien archivé** (avec `archived_at`) — pas un `204` : l'écran
affiche « archivé le … » sans redemander. **Idempotent** : archiver deux fois
rend le même bien, à la même date.

- ⚠️ **AUCUNE ROUTE NE DÉSARCHIVE.** Dites-le dans la confirmation.
- ⚠️⚠️ **LE SERVEUR NE CLÔT PAS LES ANNONCES DU BIEN.** Une annonce `published`
  reste `published` : elle sort des résultats de recherche, mais ses photos
  restent servies par leur adresse publique et ses créneaux restent ouverts aux
  demandes de visite. **Avant d'archiver, clôturez ses annonces** (section 8.6)
  — et proposez-le dans la confirmation.
- Ensuite : plus de nouvelle photo (`409 media_property_archived`), plus de
  nouvelle annonce (`409 property_archived`), plus de modification
  (`409 property_archived`). Les photos existantes se listent, se réordonnent
  et se retirent encore.

---

## 7. Les photos d'un bien

Les photos appartiennent **au bien**, pas à l'annonce : elles survivent au
passage de la location à la vente. `{id}` est **l'identifiant du bien** dans
toutes ces routes.

### 7.1 Envoyer — `POST /immo/publishers/me/properties/{id}/photos`

```
POST /immo/publishers/me/properties/{id}/photos
Content-Type: multipart/form-data

file = <la photo>                 ← OBLIGATOIRE, le champ s'appelle `file`
alt  = "Salon, vue sur le jardin" ← facultatif, 160 caractères au plus
```

| Règle | Valeur | Refus |
|---|---|---|
| Formats | **JPEG, PNG, WebP** | `415 media_format` |
| Poids | **15 Mio** au plus | `413 media_too_large` |
| Pixels | 50 millions au plus (une « bombe » de 30 000 × 30 000 pèse 1 Mo) | `413 media_too_large` |
| Petit côté | **480 px** au moins | `422 media_too_small` |
| Par bien | **30 photos** au plus | `409 media_photos_full` |
| Bien archivé | — | `409 media_property_archived` |
| Pas de champ `file` | — | `422 validation_failed`, `fields: ["file"]` |
| `alt` trop long | — | `422 validation_failed`, `fields: ["alt"]` |
| Bien d'une autre fiche | — | `404 property_not_found` |
| 🆕 Bien saisi par un collègue (pour un `agent`) | ajout, ordre, retrait, description | `403 property_edit_reserved` |

→ `201`, la photo :

```json
{
  "id":          "6b1b2c3d4e5f6a7b8c9d0e01",
  "property_id": "6b1a0e5f2c3d4e5f6a7b8c01",
  "order":       0,
  "alt":         "Salon, vue sur le jardin",
  "width":       1600,
  "height":      1200,
  "created_at":  "2026-11-10T09:05:00Z",
  "url":         "https://…?X-Amz-Signature=…",
  "thumb_url":   "https://…?X-Amz-Signature=…",
  "on_site":     "match"
}
```

⚠️⚠️ **LA PHOTO EST RÉENCODÉE, ET RIEN DE L'ORIGINAL NE SURVIT — C'EST VOULU.**
Le serveur la redresse (l'orientation du téléphone est lue, puis jetée), la
réduit et la réécrit en JPEG. **Aucune métadonnée ne sort**, dont la **position
GPS** de la prise de vue : pour un particulier qui photographie son salon, ce
serait l'adresse exacte de sa maison publiée à côté d'une annonce qui ne montre
qu'un cercle de 300 m. Ne promettez pas l'original à la personne.

🆕 ⚠️⚠️ **« PRISE SUR PLACE » — LA POSITION EST LUE AVANT D'ÊTRE JETÉE**
(v4.126.0). Le serveur lit la position GPS de la photo (JPEG), la compare à
l'adresse du bien, et **ne garde que le verdict** — jamais la position, ni dans
un fichier, ni en base, ni dans un journal :

| `on_site` | Ce que ça veut dire | Le public voit |
|---|---|---|
| `match` | prise à **moins de 150 m** de l'adresse du bien | le badge **« prise sur place »** |
| `mismatch` | prise ailleurs | rien — ⚠️ pas d'accusation publique : une vue de la plage voisine, un plan scanné au bureau sont honnêtes |
| `absent` | la photo ne dit pas où elle a été prise (PNG, WebP, position coupée, téléphone sans point) | rien |
| `unknown` | envoyée avant que le serveur ne lise la position | rien |

- ⚠️⚠️ **ENVOYEZ LA PHOTO AVEC SA POSITION** : c'est elle qui fait le badge. Une
  application qui réencode la photo avant l'envoi perd l'EXIF — et le badge avec
  lui (section 10 bis.3).
- La comparaison se fait contre `address.lat` / `address.lng` **au moment de
  l'envoi** : posez la position du bien **avant** d'ajouter ses photos.
- Dites à l'annonceur, sur `mismatch` ou `absent`, pourquoi la photo n'a pas le
  badge — et que ce n'est pas un refus.
- 🆕 ⚠️ **DEUX PHOTOS `mismatch` SUR UN MÊME BIEN METTENT SES ANNONCES DANS LA
  FILE DE L'ÉQUIPE** (v4.130.0). Une seule ne fait rien. L'annonce **reste en
  ligne** : un humain regarde, et ne la retire que s'il y a une fraude — une vue
  du quartier honnête se garde. Dès la deuxième, dites-le à l'annonceur
  (« l'équipe Dira regardera ces photos ») et proposez de **reprendre la photo
  sur place** ou de la retirer ; s'il la garde, il n'a rien d'autre à faire.

- **Deux tailles** : `url` (le plus grand côté ramené à **1 600 px**) et
  `thumb_url` (**480 px**). Une photo n'est jamais agrandie. `width` et
  `height` sont ceux de `url`.
- **Envoyez les photos une à une**, pas trente d'un coup : le serveur en traite
  deux à la fois, et un envoi en rafale fait attendre — puis expirer — les
  dernières. Montrez une progression par photo.
- ⚠️ **UN ENVOI REJOUÉ CRÉE UNE SECONDE PHOTO.** Après un délai dépassé,
  relisez la liste avant de renvoyer.
- `alt` vaut `""` quand il n'est pas renseigné (le champ est toujours servi).

### 7.2 Lister — `GET /immo/publishers/me/properties/{id}/photos`

→ `200 { "items": [ <photo>, … ] }` — **dans l'ordre d'affichage** ; la
première est la **couverture** de l'annonce. Jamais `null`, jamais paginé (30 au
plus).

⚠️ **FIEZ-VOUS À L'ORDRE DU TABLEAU, PAS AU NOMBRE `order`.** Après un retrait,
les valeurs de `order` gardent un trou, et une photo ajoutée ensuite peut
partager le nombre d'une autre. Le tableau, lui, est toujours dans l'ordre.

⚠️⚠️ **LES ADRESSES SIGNÉES VIVENT UNE HEURE.** `url` et `thumb_url` sont des
adresses temporaires, données à vous seul — elles montrent aussi les photos
d'un bien qui n'est pas en ligne. **Ne les gardez pas** : une galerie restée
ouverte, un cache d'images ou une liste mise de côté montreront des carrés
cassés au bout d'une heure. **Redemandez la liste** à l'ouverture de l'écran et
après une heure. Une adresse vide (signature impossible) se redemande aussi.

### 7.3 Réordonner — `PUT /immo/publishers/me/properties/{id}/photos/order`

```json
{ "ids": ["6b1b…0e03", "6b1b…0e01", "6b1b…0e02"] }
```

→ `200 { "items": [ … ] }`, la liste dans le nouvel ordre.

⚠️ **TOUTES LES PHOTOS DU BIEN, CHACUNE UNE FOIS.** Un ordre partiel mêlerait
les anciennes aux nouvelles — deux photos « en premier », et la couverture au
hasard. Un identifiant manquant, en trop, répété ou étranger rend
`422 validation_failed` (`fields: ["ids"]`) : relisez la liste et recommencez.

### 7.4 Décrire — `PATCH /immo/publishers/me/properties/{id}/photos/{photo}`

```json
{ "alt": "Chambre principale" }
```

→ `200`, la photo. `alt` est **obligatoire dans le corps** (`422
validation_failed`, `fields: ["alt"]`, sans lui) ; `""` efface la description.
160 caractères au plus. C'est ce que lisent les lecteurs d'écran et les moteurs
d'images ; sans description, le public lit « <titre de l'annonce> — photo 3 ».

### 7.5 Retirer — `DELETE /immo/publishers/me/properties/{id}/photos/{photo}`

→ `204`. Les deux fichiers sont effacés. `404 media_not_found` si la photo
n'est pas de ce bien.

### 7.6 Quand le public voit les photos

⚠️ **SEULEMENT TANT QU'UNE ANNONCE DU BIEN EST EN LIGNE** (`published`). Les
photos d'un brouillon ne sortent pas — l'intérieur d'un logement qu'on prépare
à louer n'a rien à faire en public —, et celles d'un bien loué cessent d'être
servies. Le public les lit par une adresse **stable**
(`…/api/v1/immo/media/<pays>/<photo>/display.jpg`), qui redirige vers une
adresse signée fraîche.

⚠️ **« CESSENT » N'EST PAS « INSTANTANÉMENT ».** Une page déjà ouverte, ou une
redirection gardée en cache, peut montrer encore les photos d'une annonce close
pendant **douze heures au plus**. Si un particulier demande que les photos de
son salon disparaissent « maintenant », **retirez les photos** (7.5) : c'est le
seul geste immédiat.

---

## 8. Les annonces (offres)

### 8.1 Quatre genres, et la périodicité qu'ils imposent

| `kind` | Offre | `price_period` imposée | Sur quels types de bien |
|---|---|---|---|
| `rent` | location **longue durée** | `month` | tous **sauf** `land` |
| `stay` | séjour **courte durée** | `night` | tous sauf `land` — ⚠️ voir section 12 |
| `sale` | vente | `total` | tous **sauf** `land` |
| `land` | vente de **terrain** | `total` | **seulement** `land` |

⚠️ **UN TERRAIN À VENDRE SE PROPOSE EN `land`, PAS EN `sale`.** `sale` sur un
bien `land` est refusé (`422 listing_kind_not_for_property`) : c'est ce qui le
range dans la bonne catégorie du site public. Et un terrain ne se loue pas
dans ce produit.

⚠️⚠️ **LA PÉRIODICITÉ EST LA RÈGLE QUI COMPTE.** « 485 000 » ne veut rien dire :
au mois c'est un loyer, à la nuit une suite, au total une erreur de saisie d'un
facteur cent. **Omettez `price_period`** : le serveur la déduit du genre. Si
vous l'envoyez, elle doit être celle du genre (`422 price_period_not_for_kind`,
`reason: "sale / night → total"`).

### 8.2 Le prix, les charges, la caution, la disponibilité

| Champ | Sens | Règle |
|---|---|---|
| `price` | entier, plus petite unité de la monnaie du pays | **obligatoire**, de 1 à 100 000 000 000 (`422 price_required` / `price_out_of_range`) |
| `currency` | la monnaie | ⚠️ **posée par le serveur** depuis le pays — ne s'envoie pas |
| `charges` | charges **mensuelles**, même unité | ⚠️ **location longue durée seulement** (`422 charges_not_for_kind`) ; 0 = aucune |
| `deposit_months` | caution, **en mois de loyer** | ⚠️ **location longue durée seulement** (`422 deposit_not_for_kind`) ; 0 à 24 (au-delà : 🆕 `422 deposit_out_of_range`, `fields: ["deposit_months"]`, `reason` `"36 — 0…24"`) |
| `negotiable` | prix négociable | facultatif ; `null` = « pas dit », qui n'est pas « non » |
| `available_from` | disponible à partir du | facultatif, **date locale** `YYYY-MM-DD` — jamais un horodatage (stocké en UTC, un jour glisserait) |

⚠️ **LES BORNES ATTRAPENT LA FAUTE DE FRAPPE, PAS LE CHER.** Elles sont larges
exprès pour que la Guinée (environ quatorze fois plus de francs) y entre. Un
loyer à 4 850 000 au lieu de 485 000 passe : **montrez le montant formaté avec
sa monnaie et sa périodicité** (« 485 000 F CFA / mois ») avant de valider.

### 8.3 Créer — `POST /immo/listings`

```json
POST /immo/listings
{
  "property_id":    "6b1a0e5f2c3d4e5f6a7b8c01",   // OBLIGATOIRE — un bien de VOTRE fiche
  "kind":           "rent",                       // OBLIGATOIRE
  "title":          "Appartement 2 chambres à Tokoin",  // OBLIGATOIRE
  "pitch":          "Lumineux, au calme, à cinq minutes du marché…",
  "price":          150000,
  "charges":        10000,
  "deposit_months": 3,
  "negotiable":     true,
  "available_from": "2026-12-01"
}
```

**`country`, `currency`, `status`, `sponsored`, `publisher_id` ne s'envoient
pas** : le serveur les pose (`422 validation_failed`, `unknown_field`, sinon).

- **Toute annonce naît `draft`** (brouillon), visible de la seule fiche.
- `title` : **140 caractères** au plus ; `pitch` : **2 000 caractères** au plus
  — des caractères, pas des octets (« Résidence à Bè, Lomé » en compte 20),
  vérifiés **à la création comme à la modification** (`422 validation_failed`,
  `fields`). (🔧 4.128.0 : ce paragraphe décrivait encore la règle d'avant
  dira-immo-api#74, en octets et à la modification seulement.)
- ⚠️ **Rappelez à un particulier, à côté du titre et du texte, de n'y écrire ni
  sa rue ni son numéro** (section 0.2).

→ `201`, l'annonce :

```json
{
  "id":             "6b2c3d4e5f6a7b8c9d0e1f01",
  "property_id":    "6b1a0e5f2c3d4e5f6a7b8c01",
  "publisher_id":   "6b0f3c1a9e2d4f5a6b7c8d90",
  "created_by":     "6b0f3c1a9e2d4f5a6b7c8d77",
  "country":        "TG",
  "kind":           "rent",
  "title":          "Appartement 2 chambres à Tokoin",
  "pitch":          "Lumineux, au calme, à cinq minutes du marché…",
  "status":         "draft",
  "price":          150000,
  "currency":       "XOF",
  "price_period":   "month",
  "charges":        10000,
  "deposit_months": 3,
  "negotiable":     true,
  "available_from": "2026-12-01",
  "sponsored":      false,
  "created_at":     "2026-11-10T09:10:00Z",
  "updated_at":     "2026-11-10T09:10:00Z"
}
```

- `publisher_id` est l'agence ; `created_by` est **le compte qui l'a saisie** —
  c'est lui qui la voit quand il est `agent`, et lui qui reçoit les
  notifications de visite.
- **Absents quand vides** : `pitch`, `available_from`, `published_at`,
  `closed_at`, `closed_reason`, `review_note`. **Toujours servis** : `sponsored`
  (une annonce payée doit pouvoir se marquer partout ; aujourd'hui toujours
  `false`), `charges`, `deposit_months`. `negotiable` vaut `null` quand rien
  n'a été dit.

| Refus | Code |
|---|---|
| `property_id`, `kind` ou `title` manquant ; `property_id` mal formé | `422 validation_failed` (`fields`) |
| Bien introuvable, d'un autre pays, ou 🆕 d'une **autre fiche** | `404 property_not_found` — ⚠️ c'était `403 listing_not_yours` pour un bien d'une autre agence, ce qui **confirmait** qu'il existait (v4.124.0) |
| Bien archivé | `409 property_archived` |
| Genre incompatible avec le type du bien | `422 listing_kind_not_for_property` (`reason: "sale / land"`) |
| Genre inconnu | `422 listing_kind_unknown` |
| Périodicité contraire au genre | `422 price_period_not_for_kind` |
| Prix absent ou nul / hors bornes | `422 price_required` / `422 price_out_of_range` |
| Charges ou caution hors location longue durée | `422 charges_not_for_kind` / `422 deposit_not_for_kind` |
| 🆕 Caution au-delà de 24 mois | `422 deposit_out_of_range` |
| `available_from` qui n'est pas `YYYY-MM-DD` | `422 validation_failed`, `fields: ["available_from"]` |

### 8.4 Lister — `GET /immo/publishers/me/listings`

```
GET /immo/publishers/me/listings?status=draft&limit=20&cursor=<id>

→ 200 { "items": [ <annonce>, … ], "next_cursor": "6b2c3d4e5f6a7b8c9d0e1f01" }
```

- Les plus récentes d'abord. Un `agent` ne reçoit **que les siennes**.
- `status` (facultatif) : `draft`, `in_review`, `published`, `paused`,
  `closed`. ⚠️ **Une valeur inconnue n'est pas refusée** : elle rend une liste
  vide. Ne l'envoyez que depuis la liste ci-dessus.
- 🆕 **LA PAGINATION DE LA PLATEFORME** (v4.128.0) — la même que les biens :
  `limit` 20 par défaut, 100 au plus (au-delà : 100) ; ⚠️ **`next_cursor`
  ABSENT = DERNIÈRE PAGE**. Jusqu'au 10 novembre, il valait `""` en fin de
  liste, avec 50 par page : une application qui testait « vide » doit tester
  « absent ».
- `items` vaut `[]` quand il n'y a rien — jamais `null`.

### 8.5 Lire et modifier

`GET /immo/publishers/me/listings/{id}` → `200`, l'annonce. Une annonce d'une
autre fiche — ou, pour un `agent`, d'un collègue — rend
`404 listing_not_found`.

```
PATCH /immo/publishers/me/listings/{id}
{ "title"?: "…", "pitch"?: "…", "price"?: 140000, "charges"?: 10000,
  "deposit_months"?: 2, "negotiable"?: false, "available_from"?: "2027-01-01" | "" }
```

→ `200`, l'annonce à jour.

- **Champ absent = inchangé.** `"available_from": ""` efface la date.
  `negotiable` ne revient pas à `null`.
- ⚠️ **`kind` NE SE MODIFIE PAS** (`422`, `unknown_field`). Passer de la
  location à la vente, c'est une **annonce neuve** sur le même bien : clôturez
  l'une (`withdrawn`), créez l'autre.
- ⚠️ **LA VÉRIFICATION PORTE SUR LE RÉSULTAT** : `{"price": 0}` est refusé, et
  `{"charges": 5000}` sur une vente aussi, même si la requête ne parle que d'un
  champ.
- `title` vide ou au-delà de 140 caractères, `pitch` au-delà de 2 000 caractères :
  `422 validation_failed` (`fields`).
- ⚠️ **UN CORPS QUI NE CHANGE RIEN EST REFUSÉ** — `PATCH {}`, ou un prix égal
  au prix actuel : `422 nothing_to_update`.
- Une annonce **close** ne se modifie plus : `409 listing_closed`.
- ⚠️ **UNE ANNONCE EN LIGNE SE MODIFIE SANS REPASSER PAR LA RELECTURE**, et le
  changement est visible du public tout de suite. Dites-le sur l'écran de
  modification d'une annonce `published`.
- **Un prix qui bouge écrit une ligne d'historique** (8.7).

### 8.6 Les états — et qui fait quoi

```
draft ──► in_review ──► published ◄──► paused
  ▲           │             │             │
  └───────────┘             └──► closed ◄─┘
```

`POST /immo/publishers/me/listings/{id}/status` — **un seul geste pour tous
les passages** :

```json
{ "status": "closed", "reason": "rented" }
```

→ `200`, l'annonce à jour.

| De | Vers | Qui | Ce que l'écran propose |
|---|---|---|---|
| `draft` | `in_review` | **l'annonceur** | « Soumettre à la relecture » |
| `in_review` | `published` | ⚠️ **l'équipe Dira seulement** | rien : attendre |
| `in_review` | `draft` | l'équipe (renvoi, avec une note) **ou** l'annonceur (retirer sa soumission) | « Retirer de la relecture » |
| `published` | `paused` | l'annonceur | « Mettre en pause » |
| `paused` | `published` | l'annonceur — ⚠️ **sans nouvelle relecture** | « Remettre en ligne » |
| `published` / `paused` | `closed` | l'annonceur, **avec `reason`** | « Clôturer » + le motif |
| `published` / `paused` | `closed` (`moderated`) | ⚠️ **l'équipe Dira seulement** — retrait par la modération, `review_note` porte le motif | « Retirée par l'équipe » + la note |
| `closed` | — | **terminal** | rien |

**Les motifs de clôture** (`reason`, obligatoire vers `closed`) : `rented`
(louée) · `sold` (vendue) · `booked` (séjour : plus de disponibilité) ·
`withdrawn` (retirée sans transaction) · `expired`. Ils ne sont pas
décoratifs : c'est de là que se tire le délai de location par quartier. Sans
motif, ou un motif hors liste : `422 close_reason_unknown`.

🆕 ⚠️⚠️ **CLÔTURER FERME LES VISITES À VENIR** (v4.125.0) : les créneaux **pas
encore commencés** sont retirés, leurs visites (demandées ou confirmées) passent
`cancelled` avec **`closed_by: "listing"`**, et **chaque chercheur est prévenu**
(`immo_visit_cancelled`). Louée le matin, une annonce gardait jusqu'ici sa visite
confirmée de l'après-midi, et le chercheur traversait la ville pour rien. Dites
dans la confirmation combien de visiteurs seront prévenus. Les créneaux **déjà
commencés** restent : leurs visites se constatent (9.6).

⚠️ **UNE PAUSE N'Y TOUCHE PAS** : elle ferme les nouvelles demandes (l'annonce
n'est plus en ligne), pas les rendez-vous déjà pris. Si l'annonceur veut les
annuler, c'est créneau par créneau (9.3) ou visite par visite (9.5) — proposez-le
dans la confirmation de la pause.

⚠️ **`moderated` est le motif de l'ÉQUIPE** : une annonce signalée par des
chercheurs (ou dont une photo a déjà servi chez un autre annonceur) peut être
**retirée** après examen par un humain — clôturée, pas mise en pause, parce
qu'une pause se reprend seule. Affichez-la comme « retirée par l'équipe Dira »
avec sa `review_note`. ⚠️ **Vous ne pouvez pas l'envoyer** vous-même :
🆕 `403 listing_moderated_by_staff` (« Seule l'équipe Dira retire une annonce
pour modération »). Republier le bien, c'est une
annonce **neuve**, qui repasse en relecture.

⚠️ **UNE PHOTO DÉJÀ PUBLIÉE PAR UN AUTRE ANNONCEUR EST MARQUÉE** pour l'équipe
(la même image, recadrée ou recompressée, se reconnaît). Rien n'est refusé à
l'envoi — deux agences peuvent photographier le même immeuble, un mandat se
partage — mais l'annonce passe sous les yeux d'un humain. Photographiez vos
biens vous-même.

| Refus | Code | Ce que l'écran fait |
|---|---|---|
| ⚠️ L'annonceur tente `in_review` → `published` | 🆕 `403 listing_published_by_staff` | afficher le `message` : « La publication est faite par l'équipe Dira, après relecture » |
| Passage non permis | `409 listing_status_transition` | `reason` liste les passages ouverts (`"draft → published ; possible : in_review"`) : proposez-les |
| Annonce close | `409 listing_closed` | rien : proposer une annonce neuve sur le même bien |
| `status` manquant | `422 validation_failed` | bug de l'application |

- ⚠️ **`closed` EST TERMINAL.** « Louée le 12 mars » suivie d'une annonce en
  ligne ne se comprend pas. Reproposer le bien, c'est une **annonce neuve** :
  le bien, lui, n'a pas bougé.
- ⚠️⚠️ **`review_note` EST LA RÉPONSE DU RELECTEUR.** Une annonce renvoyée en
  `draft` par l'équipe porte une note (« photos floues ») : **affichez-la en
  tête du brouillon.** Un renvoi muet se resoumet à l'identique. ⚠️ Et
  n'envoyez **jamais** `note` depuis l'application : retirer sa soumission
  (`in_review` → `draft`) **écrase la note de relecture** par ce que vous
  envoyez — par du vide si vous n'envoyez rien.
- 🆕 **LA DÉCISION DE L'ÉQUIPE PRÉVIENT L'ANNONCEUR** (v4.129.0) : publiée
  (`immo_listing_published`), renvoyée (`immo_listing_returned`), retirée
  (`immo_listing_moderated`) — section 10. ⚠️ **La note n'est pas dans la
  notification** (un écran verrouillé se lit par-dessus l'épaule) : ouvrez
  l'annonce et affichez `review_note`. Relisez aussi la liste au retour au
  premier plan : une notification peut se perdre.
- `published_at` est posé à la **première** publication et ne bouge plus (une
  reprise après pause le garde). `closed_at` et `closed_reason` sont posés à la
  clôture.
- ⚠️⚠️ **METTRE EN PAUSE OU CLÔTURER NE TOUCHE PAS AUX VISITES.** Les créneaux
  restent, les visites demandées ou confirmées aussi ; seules les **nouvelles**
  demandes sont refusées. Avant de clôturer, **annulez les visites à venir ou
  retirez les créneaux** (section 9) — et proposez-le dans la confirmation.
- **La relecture ne vérifie rien automatiquement** à la soumission (ni photos,
  ni surface) : c'est un humain qui regarde. Une annonce sans photo part en
  relecture et y sera probablement renvoyée.

### 8.7 L'historique des prix — `GET /immo/publishers/me/listings/{id}/price-history`

```json
→ 200 { "items": [
  { "id": "…", "listing_id": "6b2c…1f01", "country": "TG",
    "price": 140000, "currency": "XOF", "period": "month",
    "by": "6b0f3c1a9e2d4f5a6b7c8d77", "at": "2026-11-20T08:00:00Z" },
  { "id": "…", "listing_id": "6b2c…1f01", "country": "TG",
    "price": 150000, "currency": "XOF", "period": "month",
    "by": "6b0f3c1a9e2d4f5a6b7c8d77", "at": "2026-11-10T09:10:00Z" } ] }
```

Les plus récents d'abord, 200 au plus, jamais `null`. **La première ligne est
écrite à la création** : le prix d'origine — celui qui dit si le bien a baissé
— est toujours là. `by` est le compte qui a fixé ce prix.

### 8.8 Voir ce que voit le public — `GET /immo/listings/{ref}`

La lecture **publique** d'une annonce, celle du site et de l'application
chercheur, avec l'identifiant de l'annonce comme `{ref}`. Elle montre
exactement ce que voit un chercheur : le cercle de 300 m d'un particulier, la
rue vide, les photos, la complétude. ⚠️ **Elle ne répond que pour une annonce
`published`** (`404 listing_not_found` sinon) : c'est un aperçu de ce qui est
en ligne, pas d'un brouillon.

🆕 **Elle porte `share_url`** (v4.122.0) : l'adresse de l'annonce sur le site
public, composée par le serveur dans la langue de la requête — **c'est elle qui
se partage** (section 10 bis.6). Absente quand le déploiement ne connaît pas son
site : pas de bouton, alors. ⚠️ Ne la fabriquez pas.

---

## 9. Les créneaux et les visites

**Un chercheur ne demande pas « quand puis-je visiter ? » : il choisit un
créneau que vous avez proposé.** Il demande ; vous confirmez ou refusez ; on se
rappelle deux heures avant ; vous constatez sa venue.

⚠️⚠️ **LE TÉLÉPHONE N'EST RÉVÉLÉ QU'APRÈS CONFIRMATION — DANS LES DEUX SENS.**
Avant, vous ne connaissez du chercheur que l'heure qu'il a choisie et le mot
qu'il a joint ; lui ne connaît ni votre numéro ni, pour un particulier,
l'adresse exacte. Un numéro publié est un numéro moissonné.

### 9.1 Proposer un créneau — `POST /immo/publishers/me/listings/{id}/visit-slots`

`{id}` est **l'annonce**.

```json
{
  "starts_at":    "2026-11-14T15:00:00Z",   // un INSTANT, RFC 3339
  "duration_min": 30,
  "mode":         "in_person",
  "capacity":     1
}
→ 201 {
  "id": "6b3d4e5f6a7b8c9d0e1f2a01", "listing_id": "6b2c3d4e5f6a7b8c9d0e1f01",
  "starts_at": "2026-11-14T15:00:00Z", "duration_min": 30, "mode": "in_person",
  "capacity": 1, "booked": 0, "cancelled": false, "created_at": "2026-11-10T09:30:00Z"
}
```

| Champ | Bornes exactes |
|---|---|
| `starts_at` | **au moins une heure** après maintenant, **au plus 60 jours** ; ramené à la minute |
| `duration_min` | **15 à 240** minutes |
| `mode` | `in_person` (sur place) · `video` (à distance) |
| `capacity` | **1 à 20** visiteurs sur le créneau ; absent ou `0` = **1** |

- ⚠️ **SEULEMENT SUR UNE ANNONCE EN LIGNE** (`published`) :
  `409 visit_listing_not_live` sinon. On ne prépare donc pas ses créneaux
  pendant la relecture : proposez-les **après** la publication.
- **50 créneaux à venir au plus** par annonce : `409 visit_slots_full`.
- Un champ hors bornes : `422 validation_failed`, et `fields` liste **tous** les
  champs en cause (`["starts_at", "duration_min"]`) ; `reason` nomme le premier.
- ⚠️ **COMPOSEZ L'HEURE DANS LE FUSEAU DU PAYS**, pas dans celui du téléphone,
  et envoyez-la avec son décalage (`2026-11-14T16:00:00+01:00`) ou en UTC. La
  réponse est en UTC. Un agent en déplacement proposerait sinon une heure qui
  n'existe pas chez le visiteur.
- ⚠️ **LE SERVEUR NE REFUSE PAS DEUX CRÉNEAUX QUI SE CHEVAUCHENT.** Prévenez
  vous-même la personne.
- **`video`** dit seulement que la visite se fait à distance : ⚠️ **le serveur ne
  fournit aucun lien ni aucune salle.** Le lien s'échange par téléphone, une
  fois la visite confirmée.
- Un `agent` ne propose que sur **ses** annonces (`404 listing_not_found`
  sinon).

### 9.2 Lister les créneaux — `GET /immo/publishers/me/listings/{id}/visit-slots`

→ `200 { "items": [ <créneau>, … ] }` — les créneaux **à venir**, non retirés,
**complets compris**, du plus proche au plus lointain, 50 au plus. `booked` est
le nombre de places **tenues** (demandes en attente et visites confirmées).
⚠️ Un créneau passé **disparaît** de cette liste : ses visites se lisent dans la
liste des visites (9.4).

### 9.3 Retirer un créneau — `DELETE /immo/publishers/me/visit-slots/{id}`

`{id}` est **le créneau**. → `204`.

- ⚠️ **LES VISITES QUI Y TENAIENT UNE PLACE SONT ANNULÉES**, demandées comme
  confirmées (`status: cancelled`, `closed_by: "slot"`), et **chaque chercheur
  est prévenu** (`immo_visit_cancelled`). Dites-le dans la confirmation, avec le
  nombre (`booked`) : « 2 visiteurs seront prévenus ».
- 🆕 ⚠️ **UN CRÉNEAU COMMENCÉ NE SE RETIRE PLUS** (v4.125.0) : à partir de
  `starts_at`, `409 visit_slot_started`. Le retirer annulait la visite
  confirmée du chercheur qui était en train de visiter. Ses visites se
  **constatent** (9.6) ; une demande jamais tranchée expire seule. Masquez
  « retirer » sur un créneau commencé.
- Retirer deux fois rend `204` les deux fois.
- `404 visit_slot_not_found` : créneau inexistant, d'une autre fiche, ou — pour
  un `agent` — d'une annonce d'un collègue.

### 9.4 Les visites — `GET /immo/publishers/me/visits`

→ `200 { "items": [ <visite>, … ] }` — **toutes** les visites des annonces de
la fiche (un `agent` : celles de **ses** annonces), **de la plus lointaine à la
plus ancienne**, **200 au plus**, **sans pagination ni filtre**. Triez et
filtrez dans l'application (à venir / à trancher / passées).

```json
{
  "id":           "6b4e5f6a7b8c9d0e1f2a3b01",
  "slot_id":      "6b3d4e5f6a7b8c9d0e1f2a01",
  "listing":      { "id": "6b2c3d4e5f6a7b8c9d0e1f01",
                    "title": "Appartement 2 chambres à Tokoin",
                    "slug": "appartement-2-chambres-a-tokoin-6b2c3d4e5f6a7b8c9d0e1f01" },
  "starts_at":    "2026-11-14T15:00:00Z",
  "duration_min": 30,
  "mode":         "in_person",
  "status":       "confirmed",
  "note":         "Je viens avec ma sœur",
  "requested_at": "2026-11-10T10:02:00Z",
  "decided_at":   "2026-11-10T10:15:00Z",
  "seeker":       { "name": "Amina Diallo", "phone": "+22893001122" }
}
```

| `status` | Ce qui s'est passé | `seeker` |
|---|---|---|
| `requested` | demandée, **à trancher** par vous | `null` |
| `confirmed` | vous avez accepté | ⚠️ **rempli** : nom et téléphone du chercheur |
| `declined` | vous avez refusé | `null` |
| `cancelled` | annulée — `closed_by` : `seeker` (lui), `host` (vous), `slot` (créneau retiré), 🆕 `listing` (annonce clôturée) | `null` |
| `expired` | demandée, **jamais tranchée avant l'heure** — close par le serveur | `null` |
| `attended` | vous avez constaté sa venue | rempli |
| `no_show` | vous avez constaté son absence | ⚠️ `null` |

- **`listing` est une copie prise à la demande** (titre et `slug`) : elle ne
  suit pas un titre modifié ensuite.
- **`note`** : le seul texte que le chercheur vous envoie avant la
  confirmation (500 caractères au plus). Absent quand il n'a rien écrit.
- `decided_at` et `closed_by` sont **absents** quand ils n'ont pas de valeur.
- ⚠️⚠️ **`seeker` EST LA SEULE SOURCE DU NOM ET DU TÉLÉPHONE DU CHERCHEUR**, et
  seulement pour `confirmed` et `attended`. Une visite annulée — ou constatée
  `no_show` — les **referme** : ne les gardez pas sur l'appareil au-delà de la
  visite, et ne les recopiez pas dans un carnet de contacts.
- ⚠️ **IL N'Y A PAS DE ROUTE POUR UNE SEULE VISITE.** Une notification donne un
  `visit_id` : ouvrez la liste et cherchez-la par `id`.

### 9.5 Décider — `POST /immo/publishers/me/visits/{id}/decision`

```json
{ "decision": "confirm" }       // confirm | decline | cancel
```

→ `200`, la visite à jour (avec `seeker` rempli après `confirm`).

| `decision` | Depuis | Vers | Le chercheur reçoit |
|---|---|---|---|
| `confirm` | `requested` | `confirmed` | `immo_visit_confirmed` — l'adresse exacte et **votre** contact s'ouvrent dans sa visite |
| `decline` | `requested` | `declined` | `immo_visit_declined` |
| `cancel` | `confirmed` | `cancelled` (`closed_by: "host"`) | `immo_visit_cancelled` |

- ⚠️ **TOUJOURS AVANT L'HEURE** : à partir de `starts_at`, toute décision rend
  `409 visit_too_late`. Une demande restée sans réponse à l'heure devient
  `expired` toute seule — et le chercheur a attendu pour rien. 🆕 Les deux en
  sont prévenus (v4.129.0) : `immo_visit_expired` au chercheur,
  `immo_visit_expired_host` à vous — dans les dix minutes qui suivent l'heure
  du créneau (le passage est planifié toutes les dix minutes). **Mettez les
  demandes à trancher en tête de l'écran.**
- Depuis un autre état : `409 visit_status_transition` (relisez la liste :
  l'autre partie a peut-être annulé entre-temps).
- `decision` hors des trois valeurs : `422 validation_failed`
  (`fields: ["decision"]`).
- ⚠️ **CONFIRMER DONNE VOTRE NUMÉRO — CELUI DE LA FICHE.** Dites-le sur le
  bouton : « Confirmer — le visiteur recevra le téléphone de l'agence ».
  Pour un particulier, confirmer donne **aussi l'adresse exacte** de son bien.

### 9.6 Constater la présence — `POST /immo/publishers/me/visits/{id}/presence`

```json
{ "attended": true }
```

→ `200`, la visite : `attended` (`true`) ou `no_show` (`false`).

- ⚠️ **`attended` EST OBLIGATOIRE** : un corps vide ne constate rien
  (`422 validation_failed`, `fields: ["attended"]`).
- **Une fois l'heure venue seulement** : avant `starts_at`,
  `409 visit_not_started`.
- **Depuis `confirmed` seulement** : sinon `409 visit_status_transition`.
- ⚠️ **Constater `no_show` referme le contact du chercheur** (`seeker`
  redevient `null`). Si vous devez le rappeler, faites-le avant.
- 🆕 **`presence_at`** : l'heure du constat, servie sur la visite constatée
  (v4.125.0) — elle était écrite et jamais relue. Absente avant le constat.

| Refus communs aux visites | Code |
|---|---|
| Visite d'une autre fiche, ou — pour un `agent` — d'une annonce d'un collègue | `404 visit_not_found` |
| Pas de fiche | `403 not_a_publisher` |
| Session sans compte lisible | `401 account_unknown` — reconnecter |

### 9.7 🆕 Les messages des chercheurs — avant la visite (v4.127.0)

Un chercheur peut vous écrire depuis l'annonce, avant toute visite : **un fil
par annonce et par chercheur**.

```
GET  /immo/publishers/me/threads                     → { "items": [ <fil>, … ] }
GET  /immo/publishers/me/threads/{id}/messages        → { "items": [ <message>, … ], "unread": 1 }
POST /immo/publishers/me/threads/{id}/messages        { "body": "…" }  → 201 { "thread", "message", "masked" }
POST /immo/publishers/me/threads/{id}/read            → { "marked": 1 }
```

```json
{ "id": "6b5a1c2d3e4f5a6b7c8d9e01",
  "listing": { "id": "…", "title": "Villa à la Sablière", "slug": "…" },
  "preview": "Bonjour, la villa est-elle libre en décembre ?",
  "last_from": "seeker", "last_message_at": "2026-11-10T21:40:00Z",
  "created_at": "2026-11-10T21:40:00Z", "unread": 1 }
```

- **Qui lit, qui répond** : le compte qui a **saisi l'annonce**, et — pour une
  agence — le **porteur** et les **`manager`**, qui voient tous les fils et
  répondent au nom de l'agence. Un **`agent`** ne voit que les fils de **ses**
  annonces (`404 thread_not_found` pour celles d'un collègue).
- ⚠️ **LE CHERCHEUR EST ANONYME** : ni nom ni téléphone sur un fil. Son contact
  n'arrive qu'avec une visite **confirmée** (9.4). Écrivez « un chercheur ».
- ⚠️⚠️ **LES COORDONNÉES SONT MASQUÉES DANS LES DEUX SENS** tant qu'aucune visite
  de ce chercheur n'est confirmée : un numéro ou un e-mail que **vous** écrivez
  devient `•• •• •• ••` / `•••@•••`, et `masked: true` vous le dit. Ne contournez
  pas : proposez un créneau (9.1) — la visite confirmée donne votre numéro.
  Un montant n'est pas masqué.
- `message.from` : `seeker` ou `publisher` ; `mine` dit si c'est le vôtre. 1 à
  1 000 caractères (`422 validation_failed`) ; une menace ne part pas
  (`422 message_blocked`).

---

## 10. Les notifications

Elles arrivent par le socle : **poussée** sur le téléphone, et **boîte**
consultable.

```
POST   /me/devices                  { "token": "<jeton FCM/APNs>", "platform": "android" }
DELETE /me/devices/{token}
GET    /me/notifications[?limit=&cursor=]
POST   /me/notifications/read
POST   /me/notifications/{id}/read
```

⚠️ **DÉCLAREZ LE TÉLÉPHONE APRÈS LA CONNEXION, ET RETIREZ-LE À LA
DÉCONNEXION.** Un jeton laissé derrière continue de recevoir : la personne à
qui l'on prête le téléphone de l'agence lirait les demandes de visite.

**Ce que reçoit l'annonceur** — ⚠️ **le compte qui a saisi l'annonce**, et lui
seul (section 5.1) ; 🆕 **le porteur de la fiche** quand ce compte a été
détaché de l'agence (v4.128.0, section 5.3) :

| Clé | Quand | Texte (fr) |
|---|---|---|
| `immo_visit_requested` | un chercheur demande une visite | « Demande de visite » — « Une visite est demandée pour « [titre] », le [date] à [heure]. Confirmez-la ou refusez-la. » |
| `immo_visit_cancelled` | **le chercheur** annule | « Visite annulée » — « La visite de « [titre] » du [date] à [heure] est annulée. » |
| `immo_visit_reminder` | **deux heures avant** une visite confirmée (envoyé entre 1 h 50 et 2 h avant, jamais après le début) | « Visite à [heure] » — « Rappel : visite de « [titre] » aujourd'hui à [heure]. » |
| `immo_thread_message` 🆕 | un chercheur vous écrit (9.7) | « Message sur « [titre] » » — le message, **déjà masqué**. `data` : `type: "immo_thread"`, `thread_id`, `listing_id` → ouvrez le fil |
| `immo_visit_expired_host` 🆕 | une demande **jamais tranchée** a expiré à l'heure du créneau | « Demande expirée » — « La demande de visite de « [titre] » du [date] à [heure] a expiré sans réponse. » |
| `immo_visit_unanswered` 🆕 v4.131.0 | au **porteur** et aux **`manager`** : une demande de la fiche est toujours sans réponse **4 h** après avoir été faite, ou à **mi-chemin** du créneau si c'est plus tôt — une fois, jamais à celui qui devait répondre | « Demande sans réponse » — « La demande de visite de « [titre] » du [date] à [heure] attend toujours une réponse de votre équipe. » `data` comme les autres visites |
| `immo_listing_published` 🆕 | **l'équipe** a publié l'annonce soumise | « Annonce en ligne » — « « [titre] » est publiée : les chercheurs la voient. » |
| `immo_listing_returned` 🆕 | **l'équipe** l'a renvoyée en brouillon | « Annonce à revoir » — « L'équipe Dira vous renvoie « [titre] » : ouvrez-la pour lire sa note. » |
| `immo_listing_moderated` 🆕 | **l'équipe** l'a retirée (`closed_reason: "moderated"`) | « Annonce retirée » — « L'équipe Dira a retiré « [titre] ». Ouvrez-la pour lire le motif. » |
| `immo_member_attached` 🆕 | **ce compte** est rattaché à une agence | « Bienvenue chez [nom de l'agence] » — « Vous publiez désormais au nom de « [nom] ». » |
| `immo_member_detached` 🆕 | **ce compte** est détaché de son agence | « Rattachement terminé » — « Vous ne faites plus partie de « [nom] ». » |

🆕 (v4.129.0) Les trois clés `immo_listing_*` ne partent que pour une décision
**de l'équipe** : ce que l'annonceur fait lui-même (retirer sa soumission,
mettre en pause, clôturer « louée ») ne lui revient pas, pas plus qu'une pause
ou une clôture « louée » faite par l'équipe à sa demande. Les deux
`immo_member_*` partent au **collaborateur**, pas au porteur — c'est lui qui
vient de faire le geste.

**Les données** des visites, identiques pour les clés `immo_visit_*` :

```json
{ "type": "immo_visit", "visit_id": "6b4e5f6a7b8c9d0e1f2a3b01", "listing_id": "6b2c3d4e5f6a7b8c9d0e1f01" }
```

Routez `type: "immo_visit"` vers l'écran des visites, et ouvrez celle dont
l'`id` est `visit_id` (9.4).

🆕 (v4.129.0) Les deux autres familles :

```json
{ "type": "immo_listing", "listing_id": "6b2c3d4e5f6a7b8c9d0e1f01", "status": "draft" }
{ "type": "immo_membership", "publisher_id": "6b1a2b3c4d5e6f7a8b9c0d01" }
```

| `data.type` | Ouvrir |
|---|---|
| `immo_listing` | **l'annonce** — `GET /immo/publishers/me/listings/{listing_id}` (8.5), `review_note` en tête si elle est renvoyée ou retirée. `status` dit l'état **au moment de l'envoi** : relisez l'annonce, c'est elle qui fait foi |
| `immo_membership` | **la porte** — `GET /immo/publishers/me` (section 3), puis l'accueil qu'elle désigne. ⚠️ Après un détachement, les écrans de l'ancienne agence sont **fermés** : videz-les avant de rouvrir |

⚠️ **ROUTEZ SUR `data.type`, JAMAIS SUR LA CLÉ** — la clé est un gabarit que
l'exploitation peut renommer. Une valeur inconnue s'affiche dans la boîte sans
rien ouvrir.

- ⚠️ **L'HEURE DU TEXTE EST L'HEURE LOCALE DU PAYS** de la visite (date en
  `JJ/MM`, heure en `HH:MM`), composée par le serveur. Dans l'application,
  affichez `starts_at` dans ce même fuseau : un agent qui lit « 15:00 » dans la
  notification et « 14:00 » dans l'écran ne sait plus à quelle heure venir.
- ⚠️ **AUCUNE NOTIFICATION NE PORTE D'ADRESSE NI DE TÉLÉPHONE** : un écran
  verrouillé se lit par-dessus l'épaule.
- Elles sont rangées avec les **mises à jour** (catégorie `order_updates`) —
  🆕 sauf `immo_member_attached` / `immo_member_detached`, rangées en
  **sécurité** (`security`) : un changement d'accès **ne se coupe pas**.
  ⚠️ **Une catégorie coupée n'est ni poussée ni gardée dans la boîte** : un
  annonceur qui la coupe ne voit plus arriver **aucune** demande de visite, et
  elles expirent sans qu'il le sache. Si vous exposez ce réglage, dites-le à
  côté de l'interrupteur.

⚠️ **CE QUI NE PRÉVIENT PAS L'ANNONCEUR — à compenser par une relecture :**

🆕 (v4.131.0) **Plus rien.** La dernière ligne — une demande sur l'annonce d'un
**collègue**, pour le porteur et les `manager` — est couverte par
`immo_visit_unanswered` : ils ne sont pas prévenus de **chaque** demande (une
agence de dix agents recevrait tout), mais de celle qu'un agent laisse sans
réponse.

🆕 (v4.129.0) Les trois autres lignes de ce tableau — demande **expirée**,
annonce **publiée / renvoyée** par l'équipe, **rattachement / détachement** —
préviennent désormais (tableau ci-dessus). Jusqu'au 11 novembre, un annonceur
dont l'annonce était renvoyée la laissait en brouillon des jours, croyant la
relecture en cours.

---

## 10 bis. 🆕 📱 SUR LE TÉLÉPHONE DE L'AGENT — l'application des annonceurs (v4.122.0)

L'annonceur a **son application**, **Dira Immo Pro** — native, Android et iOS —,
celle qui se connecte avec `"app": "immo_pro"`. Elle sert les
trois rangs d'une agence (porteur, `manager`, `agent`) **et** le particulier :
c'est la **fiche** (section 3) qui dit lesquels de ses écrans existent, pas
l'application installée. Cette section dit ce qu'un téléphone ajoute aux
précédentes ; tout le reste vaut tel quel.

⚠️ **CE N'EST PAS DIRA IMMO, L'APPLICATION DU CHERCHEUR.** Un compte a un rôle,
un seul : le compte `publisher` qui publie n'est pas le compte `client` qui
cherche. Un agent qui cherche aussi un logement le fait dans **Dira Immo**,
**avec un compte client** — et `403 wrong_app` (`reason` nomme l'application à
ouvrir) l'y envoie s'il se trompe de porte.

### 10 bis.1 L'ouverture — trois appels, dans cet ordre

1. **La session** : jeton gardé dans le stockage **chiffré** du téléphone
   (Keystore, Trousseau) ; `POST /auth/refresh` si l'accès a expiré.
2. **La porte** — `GET /immo/publishers/me` : elle dit s'il faut créer la fiche,
   attendre une invitation d'agence ou ouvrir l'accueil, et **quel rang** porte
   ce compte (section 3). ⚠️ **Relisez-la à chaque retour au premier plan** :
   🆕 un détachement d'agence est notifié (`immo_member_detached`, section 10)
   **mais une notification peut se perdre**, et un agent détaché qui garde
   l'écran de son ancienne agence se fait refuser geste après geste.
3. **Le téléphone** — `POST /me/devices { "token": "…", "platform":
   "android" | "ios" }` après **chaque** connexion — **sans** `app` : ce champ
   ne sert qu'à séparer Dira Immo de l'application cliente, qui partagent un
   compte ; un compte `publisher` n'a que Dira Immo Pro —, et quand le système change
   le jeton de notification. ⚠️ **`DELETE /me/devices/{token}` AVANT
   `POST /auth/logout`** : la déconnexion **ne retire pas** le téléphone, et
   après elle la session n'a peut-être plus le droit de le faire. Oublié, le
   téléphone de l'agence prêté à quelqu'un d'autre continue de recevoir les
   demandes de visite.

### 10 bis.2 Une notification ouvre une visite

- `data.type: "immo_visit"` → **l'écran des visites**, puis celle dont l'`id`
  est `visit_id`. ⚠️ Il n'y a pas de route pour une seule visite :
  `GET /immo/publishers/me/visits` et cherchez-la par `id`.
- ⚠️ **Relisez avant d'afficher les boutons** : entre la notification et le
  toucher, le chercheur a pu annuler, et `POST …/decision` rendrait
  `409 visit_status_transition`.
- **Une demande à trancher se tranche d'un geste**, depuis la notification
  ouverte : « Confirmer — le visiteur recevra le téléphone de l'agence » /
  « Refuser ». Une demande qui attend expire **toute seule à l'heure** — et le
  chercheur a attendu pour rien.
- Une clé inconnue s'affiche dans la boîte (`GET /me/notifications`), sans rien
  ouvrir.

### 10 bis.3 ⚠️⚠️ Les photos, depuis l'appareil photo

C'est l'écran qui fait l'annonce, et celui qui coûte le plus de réseau. Les
règles de la section 7 restent la loi ; voici ce qu'un téléphone y ajoute.

- **RÉDUISEZ AVANT D'ENVOYER.** Le serveur ne garde que **1 600 px** sur le
  grand côté : envoyer les 12 mégapixels du capteur (3 à 8 Mo) fait payer au
  forfait de l'agent des pixels jetés à l'arrivée. Ramenez le grand côté à
  **1 600 px** en JPEG de qualité ~0,8 (300 à 600 Ko) : sur un réseau 3G, dix
  secondes au lieu de deux minutes. ⚠️ **Sauf si le petit côté passerait sous
  480 px** (un panorama) : `422 media_too_small` — réduisez moins.
- ⚠️ **LE HEIC N'EST PAS ACCEPTÉ** — c'est pourtant le format par défaut d'un
  iPhone : `415 media_format`. Convertissez en **JPEG sur le téléphone** avant
  l'envoi.
- **L'orientation** : si vous réencodez, **appliquez la rotation aux pixels**
  (les bibliothèques de redimensionnement le font) ; si vous envoyez
  l'original, le serveur lit l'orientation et redresse.
- 🆕 ⚠️⚠️ **SI VOUS RÉDUISEZ, RECOPIEZ LA POSITION** (v4.126.0). Presque toutes
  les bibliothèques de redimensionnement **jettent l'EXIF** — et avec lui la
  position, qui fait le badge « prise sur place » (section 7.1). Recopiez au
  moins les étiquettes GPS dans la photo réduite (Android : `ExifInterface` ;
  iOS : les métadonnées de `CGImageDestination`), ou envoyez l'original quand il
  fait moins de 15 Mio. **Rien d'autre à retirer** : le serveur jette tout après
  l'avoir lu.
- La permission de **localisation de l'appareil photo** est celle du système :
  si la personne l'a coupée, les photos arrivent `absent`. Dites-le une fois,
  sans insister — le badge est un plus, pas une condition.
- **Une à une, avec une progression par photo** — jamais trente en parallèle :
  le serveur en traite deux à la fois, et les dernières expireraient.
- ⚠️ **UNE FILE LOCALE DES PHOTOS NON ENVOYÉES EST PERMISE, ET SEULEMENT
  ELLE.** Une photo prise sans réseau attend sur le téléphone et part au
  retour du réseau. Mais **un envoi dont la réponse n'est pas arrivée a
  peut-être réussi** (section 7.1 : un envoi rejoué crée une seconde photo) :
  avant de le refaire, `GET …/photos` et comparez le nombre à celui d'avant
  l'envoi.
- **La permission** de l'appareil photo et de la galerie se demande **au
  geste**, pas au démarrage. Sur Android 13 et plus, le sélecteur de photos du
  système n'en demande aucune.
- ⚠️ **Les adresses signées vivent une heure** (section 7.2). Le cache d'images
  s'indexe par **l'`id` de la photo et sa taille**, jamais par `url` : la même
  photo change d'adresse à chaque lecture.

### 10 bis.4 Placer le bien — la position du téléphone

- **« Je suis devant le bien »** est la meilleure source de `address.lat` /
  `address.lng` : la position du téléphone, **avec sa précision affichée**.
  Proposez d'attendre une meilleure position quand la précision dépasse
  quelques dizaines de mètres, et laissez **déplacer l'épingle** sur la carte.
- ⚠️ **AUCUNE POSITION PAR DÉFAUT.** Sans position, le formulaire **ne se valide
  pas** : `(0, 0)` est refusé (`422 address_invalid`, `reason` « null island »),
  et le centre de la ville passerait — faux.
- `422 address_outside_country` : la position est dans un autre pays que celui
  du compte (`reason` les nomme). Dites-le avec les deux noms ; c'est souvent
  un agent frontalier qui saisit un bien de l'autre côté.
- **Le fond de carte** : le choix de la personne, sinon `maps.basemap` du pays
  (à la connexion et au rafraîchissement), sinon `dira`. ⚠️ Aucune clé de carte
  n'est servie : celle de Google appartient à l'application.
- ⚠️⚠️ **MONTREZ AU PARTICULIER CE QUE LE PUBLIC VERRA** : un **cercle** de
  300 m, pas son épingle. Il saisit l'adresse exacte de sa maison ; il doit voir
  sur le même écran que personne ne la lira avant une visite confirmée.

### 10 bis.5 Appeler un visiteur

- **Le bouton « appeler » n'existe que sur une visite `confirmed` ou
  `attended`** dont `seeker.phone` est servi : un lien `tel:`.
- ⚠️ **JAMAIS DANS LE CARNET DE CONTACTS DU TÉLÉPHONE**, et rien de gardé sur
  l'appareil au-delà de la visite : annulée ou constatée `no_show`, elle
  referme le contact (section 9.4). Le téléphone d'une agence passe de main en
  main ; un carnet rempli de numéros de visiteurs serait une fuite que personne
  ne verrait.
- **Rappeler un absent, c'est AVANT de constater `no_show`** : après, le numéro
  n'est plus servi. Mettez « appeler » au-dessus de « absent ».
- **`mode: "video"`** : aucun lien de visioconférence n'est servi. La visite
  confirmée donne le contact ; c'est par lui qu'on se joint.

### 10 bis.6 Partager une annonce en ligne — `share_url`

- Une annonce **`published`** se partage par **l'adresse du site public**, que
  le serveur compose : lisez-la dans la **vue publique**
  (`GET /immo/listings/{ref}`, section 8.8) → `share_url`, dans la langue de la
  requête. Passez-la à la feuille de partage du système — WhatsApp et
  Facebook en tête.
- ⚠️ **Avant la publication, il n'y a pas de lien** : la vue publique rend
  `404 listing_not_found` pour un brouillon, et un lien partagé trop tôt
  ouvrirait une page introuvable chez tous les contacts de l'agent.
- ⚠️ **`share_url` absent → pas de bouton.** Ne fabriquez pas l'adresse : l'hôte
  change d'un environnement à l'autre.

### 10 bis.7 Hors ligne, réseau faible — ce qui attend et ce qui n'attend pas

| Geste | Sans réseau |
|---|---|
| Lire ses biens, annonces et visites | la dernière lecture, **avec son heure** |
| Remplir le formulaire d'un bien | **gardé sur l'appareil**, envoyé au retour du réseau. ⚠️ `POST /immo/properties` n'est pas idempotent : un envoi sans réponse se vérifie par `GET /immo/properties` **avant** d'être refait, sinon le bien existe deux fois |
| Prendre des photos | la file locale de 10 bis.3 |
| ⚠️ Confirmer, refuser ou annuler une visite | **jamais en file** : la demande aura expiré ou le chercheur annulé, et l'autre partie aura été prévenue à tort |
| ⚠️ Constater une présence, changer l'état d'une annonce, rattacher un collaborateur | **jamais en file**, pour la même raison |

### 10 bis.8 Les heures

Toute heure s'affiche dans le **fuseau du pays du bien** (Lomé : UTC+0 ;
Libreville et N'Djamena : UTC+1), pas dans celui du téléphone : c'est l'heure
que le texte de la notification donne déjà, et un agent qui lit « 15:00 » dans
la notification et « 14:00 » dans l'écran ne sait plus à quelle heure venir.

---

## 11. Les réglages de publication — `GET /immo/settings/publication`

Ce que publier coûte **annoncé** dans le pays du compte. Lisible par tout
compte connecté.

```json
→ 200 {
  "free_listings":  2,      // annonces gratuites d'un PARTICULIER
  "listing_tokens": 500,    // prix d'une annonce au-delà, en jetons
  "boost_tokens":   1000,   // prix d'une mise en avant
  "boost_days":     7,      // durée d'une mise en avant
  "twin_tokens":    2000,   // prix du traitement 3D
  "updated_at":     "2026-11-02T09:00:00Z"
}
```

- Le quota gratuit ne vise que les **particuliers** : une agence paie dès la
  première annonce — c'est son métier.
- Un pays qui n'a rien réglé suit le réglage commun, puis les valeurs de
  départ ci-dessus. Le serveur relit toutes les trente secondes.
- ⚠️ **`updated_at` PEUT VALOIR `0001-01-01T00:00:00Z`** quand aucun réglage
  n'a jamais été écrit (les valeurs de départ). Ne l'affichez pas.

> ⚠️⚠️ **CE SONT DES PRIX ANNONCÉS, PAS DES PRIX APPLIQUÉS.** Vérifié dans le
> code au 10 novembre 2026 : **aucune route ne compte les annonces gratuites,
> aucune ne débite de jetons** — ni à la création, ni à la soumission, ni à la
> publication. La mise en avant et la 3D ne sont pas servies (section 12), et
> l'inscription d'un annonceur n'ouvre aucun portefeuille de jetons.
>
> **Ne dessinez donc ni solde, ni « il vous reste 1 annonce gratuite », ni
> écran de paiement.** Si vous montrez la grille, écrivez qu'il s'agit des
> tarifs annoncés — un annonceur qui lit « 500 jetons » et n'est jamais débité
> croira à une dette qui l'attend.

---

## 12. ⚠️ CE QUE CETTE SURFACE NE SERT PAS — et pourquoi

**N'en dessinez pas les écrans.** Certaines lignes sont des décisions,
d'autres des modules pas encore construits ; aucune n'est un oubli à combler
côté application.

| Absent | Pourquoi |
|---|---|
| **Le téléphone ou le nom d'un chercheur avant confirmation** | ⚠️ **délibérément absent**, dans les deux sens. Un numéro donné avant la visite est un numéro démarché |
| **Choisir le pays d'un bien ou d'une fiche** | ⚠️ **délibérément absent** : le pays est celui du compte (1 bis) |
| **Poser le badge `verified`** (fiche ou bien) | il dit qu'un humain de Dira a regardé l'identité, le mandat, le titre ; une case à cocher en ferait une affirmation de plus |
| **Se publier soi-même** | la relecture est faite par l'équipe (0.3) |
| **La mise en avant payée** (`sponsored`, `boost_*`) | pas servie : `sponsored` est toujours `false`, aucune route ne l'achète |
| **Le paiement, les jetons, le quota gratuit** | annoncés, jamais appliqués (section 11) |
| **Les séjours courte durée — réservation et paiement** | 🆕 (v4.131.0) **`stay` est ouvert** : une annonce à la nuit se crée, se publie, se cherche, se visite et se discute comme les autres. Ce qui n'existe **pas encore** : le calendrier des nuits, la réservation et le paiement. Proposez `stay` en le disant : « le voyageur vous contacte ; la réservation en ligne arrive ». |
| **La gestion locative** — bail, échéancier, loyers, quittances | pas encore ouverte |
| **La capture 3D, les mesures LiDAR, le contour de terrain au GPS** | l'**application de capture**, à venir. C'est elle qui écrira `has_3d` et les surfaces `measured_*` / `gps_polygon` ; cette application ne les saisit pas (6.4) |
| **La vidéo de visite** — filmée, envoyée, montrée | pas servie. Le mode de créneau `video` n'est qu'une étiquette : aucun lien, aucune salle (9.1) |
| **L'envoi et la vérification des titres de propriété** | le champ `documents` existe, aucune porte d'envoi ni aucune vérification (6.8) |
| **Supprimer un bien, une annonce ou une fiche** | un bien s'**archive** (6.12), une annonce se **clôt** (8.6) ; une fiche ne se supprime pas |
| **Désarchiver un bien, rouvrir une annonce close** | un bien archivé reste archivé ; une annonce close se repropose en annonce **neuve** |
| **Changer le genre d'une annonce, la forme d'une fiche** | une offre neuve sur le même bien ; la forme d'une fiche est définitive |
| **Réattribuer les annonces ou les visites d'un collaborateur** | aucune route : voir les pièges du détachement (5.3) |
| **Une visite seule, par son identifiant** | lire la liste (9.4) |
| **Un code par SMS ou WhatsApp pour se connecter** | réservé aux comptes clients (2.2) |
| **Ouvrir l'application depuis un lien d'annonce** | le site ne publie ni `assetlinks.json` (Android) ni `apple-app-site-association` (iOS) : `share_url` ouvre le **navigateur** |
| **Une route de synchronisation hors ligne** | aucune : la dernière lecture s'affiche avec son heure, et seules les photos et le formulaire d'un bien attendent le réseau (10 bis.7) |

> ⚠️ **SI L'UN DE CES CHAMPS APPARAÎT UN JOUR DANS UNE RÉPONSE, NE L'AFFICHEZ
> PAS — signalez-le.** Une interface qui l'affiche le rend définitif.

---

## 13. ⚠️ Ce qu'un écran ne doit PAS faire — récapitulatif de recette

- [ ] **Connexion** : `app: "immo_pro"` envoyé ; `403 wrong_app` affiche `reason` ; aucun parcours par code SMS.
- [ ] **Inscription** : `role: "publisher"` envoyé — sinon un compte client, définitivement.
- [ ] **Porte** : `404 publisher_not_found` ouvre **deux** portes ; la seconde (« je travaille pour une agence ») ne crée **rien**.
- [ ] **Fiche** : le téléphone demandé à la création, et dit comme « le numéro que recevront vos visiteurs » ; la case `name` vide refusée côté application.
- [ ] **Sites** : reposés **en entier**, avec leurs `id` ; aucun `id` inventé.
- [ ] **Équipe** : rattachement **par téléphone complet** ; la confirmation du détachement dit que les visites et les messages de ses annonces seront adressés au porteur.
- [ ] **Biens** : seule la provenance `declared` proposée à la saisie ; aucune position par défaut ; `(0, 0)` jamais envoyé ; « meublé » n'est pas un équipement.
- [ ] **Biens** : `verified: null` ne s'écrit jamais « refusé » ; un agent voit **tous** les biens, et l'écran ne dit pas le contraire.
- [ ] **Archivage** : la confirmation propose de clôturer les annonces du bien, et dit qu'on ne désarchive pas.
- [ ] 🆕 **Photos — « prise sur place »** : la position du bien posée **avant** les photos ; la photo réduite **garde ses étiquettes GPS** ; `mismatch` / `absent` expliqués, jamais présentés comme un refus ; à la **deuxième** `mismatch`, l'annonceur sait que l'équipe regardera (l'annonce reste en ligne).
- [ ] **Photos** : envoyées une à une ; les adresses signées jamais gardées plus d'une heure ; l'ordre lu dans le tableau, pas dans `order`.
- [ ] **Annonces** : `price_period` omis ; le montant formaté avec monnaie et périodicité avant validation ; `title` ≤ 140 caractères et `pitch` ≤ 2 000 caractères tenus côté application ; aucun bouton « publier » ; `review_note` en tête du brouillon ; `note` jamais envoyé.
- [ ] **Listes** : la fin d'une liste — biens comme annonces — = `next_cursor` **absent** ; `items` vaut `[]`, jamais `null`.
- [ ] 🆕 **Biens** : la ville obligatoire à la saisie (`address_city_required`).
- [ ] **Clôture / pause** : la clôture **dit** combien de visiteurs seront prévenus (le serveur annule) ; la pause **propose** d'annuler les visites à venir (le serveur n'y touche pas).
- [ ] 🆕 **Créneaux** : « retirer » masqué sur un créneau commencé.
- [ ] **Visites** : les demandes à trancher en tête ; l'heure affichée dans le fuseau du pays ; « Confirmer » dit qu'il donne le téléphone de la fiche (et l'adresse, pour un particulier) ; la présence constatée **avant** de retirer un créneau passé ; le contact du chercheur jamais gardé au-delà de la visite.
- [ ] **Notifications** : appareil déclaré à la connexion, retiré **avant** la déconnexion ; `type: "immo_visit"` ouvre la bonne visite, **relue** avant d'afficher ses boutons.
- [ ] 🆕 **Téléphone — la porte** relue à chaque retour au premier plan (un détachement notifié peut ne pas arriver).
- [ ] 🆕 **Notifications de l'annonceur** : `immo_listing` ouvre l'annonce (`review_note` en tête, l'annonce relue), `immo_membership` relit la porte ; routage sur `data.type`, jamais sur la clé.
- [ ] 🆕 **Téléphone — les photos** : réduites à 1 600 px sur le grand côté (jamais sous 480 px sur le petit) ; le HEIC converti en JPEG ; un envoi sans réponse vérifié par la liste avant d'être refait ; cache indexé par `id`, jamais par `url`.
- [ ] 🆕 **Téléphone — la position** : précision affichée, épingle déplaçable, aucune position par défaut ; le particulier voit le cercle que verra le public.
- [ ] 🆕 **Téléphone — appeler** : `tel:` sur une visite `confirmed` / `attended` seulement ; rien dans le carnet de contacts ; « appeler » au-dessus de « absent ».
- [ ] 🆕 **Téléphone — partager** : `share_url` lu sur la vue publique d'une annonce **en ligne** ; aucune adresse fabriquée.
- [ ] 🆕 **Téléphone — hors ligne** : aucune décision de visite, présence, changement d'état ni rattachement mis en file ; un bien envoyé sans réponse vérifié par la liste avant d'être refait.
- [ ] **Réglages** : aucun solde, aucun quota, aucun écran de paiement.
- [ ] **Erreurs** : le `message` de `validation_failed` jamais montré ; `fields` souligne les cases.

---

## 14. Les refus de cette surface

| Code | HTTP | Phrase (fr) |
|---|---|---|
| `validation_failed` | 422 | ⚠️ **non traduite** — ne l'affichez pas ; lisez `fields` et `reason` |
| `forbidden` | 403 | Vous n'êtes pas autorisé à effectuer cette action |
| `missing_token` | 401 | Authentification requise |
| `invalid_token` | 401 | Jeton invalide ou expiré |
| `rate_limited` | 429 | ⚠️ non traduite — « trop de requêtes, réessayez dans un instant » |
| `internal_error` | 500 | ⚠️ non traduite — citez le `X-Request-ID` |
| **Socle — entrer** | | |
| `wrong_app` | 403 | Ce compte n'est pas un compte de cette application. Ouvrez celle qui lui correspond — votre mot de passe est correct. (affichez `reason`) |
| `invalid_credentials` | 401 | Numéro de téléphone ou mot de passe invalide |
| `phone_taken` | 409 | Ce numéro de téléphone est déjà enregistré |
| `account_suspended` | 403 | Ce compte est suspendu |
| `account_closed` | 403 | Ce compte a été supprimé. Vous pouvez vous réinscrire avec ce même numéro, ou écrire au support si c'était une erreur. |
| `otp_not_available` | 403 | Ce compte se connecte avec un mot de passe. |
| **Fiche et équipe** | | |
| `publisher_not_found` | 404 | Annonceur introuvable |
| `publisher_exists` | 409 | Ce compte a déjà une fiche d'annonceur |
| `publisher_is_member` | 409 | Ce compte appartient à une agence : il ne peut pas ouvrir sa propre fiche (⚠️ à la **création** seulement ; au rattachement : `member_already_attached`) |
| `members_need_an_agency` | 422 | Seule une agence a des collaborateurs |
| `not_the_agency_owner` | 403 | Seul le compte qui porte la fiche de l'agence gère ses collaborateurs |
| `member_already_attached` | 409 | Ce compte appartient déjà à un annonceur |
| `member_not_found` | 404 | Collaborateur introuvable |
| `cannot_attach_yourself` | 422 | Vous portez déjà cette agence : vous n'êtes pas l'un de ses collaborateurs |
| `account_not_found` | 404 | Aucun compte de ce pays ne correspond |
| `user_not_found` | 404 | (refus du socle, rattachement par `user_id`) |
| `unknown_site` | 422 | Cette agence n'a pas cette implantation |
| `site_has_members` 🆕 | 409 | Des collaborateurs travaillent encore sur ce site : rattachez-les à un autre site avant de le retirer |
| `core_unavailable` | 503 | Le socle de la plateforme est injoignable |
| **Biens** | | |
| `not_a_publisher` | 403 | Ce compte n'a pas de profil d'annonceur. |
| `publisher_directory_unavailable` | 503 | L'annuaire des annonceurs est injoignable : réessayez dans un instant. |
| `property_not_found` | 404 | Ce bien n'existe pas. |
| `property_archived` | 409 | Ce bien est archivé : il ne se modifie plus. |
| `nothing_to_update` | 422 | Cette demande ne modifie rien. |
| `property_type_unknown` | 422 | Ce type de bien n'existe pas au catalogue. Les types acceptés sont indiqués dans le détail du refus. |
| `country_required` | 422 | Un bien est toujours rattaché à un pays, et celui-ci n'en a pas. (ne devrait jamais sortir d'une route) |
| `address_invalid` | 422 | Cette adresse n'a pas de coordonnées utilisables : vérifiez la latitude et la longitude. |
| `address_city_required` 🆕 | 422 | Le bien a besoin de sa ville : c'est elle que les chercheurs tapent |
| `address_outside_country` | 422 | Ces coordonnées tombent dans un autre pays que celui de votre compte. |
| `area_required` | 422 | Ce type de bien doit porter au moins une surface, et elle doit être nommée. |
| `area_source_required` | 422 | Chaque surface doit dire d'où vient son chiffre : déclarée, mesurée ou relevée par un géomètre. Une surface sans provenance ne peut pas être affichée. |
| `area_source_unknown` | 422 | Cette provenance de surface n'est pas reconnue. |
| `area_kind_unknown` | 422 | Ce genre de surface n'est pas reconnu : habitable, terrain ou construite. |
| `area_kind_duplicated` | 422 | Un bien ne porte qu'une seule surface de chaque genre. |
| `area_out_of_range` | 422 | Cette surface est invraisemblable pour son genre : vérifiez l'unité et le nombre. |
| `area_kind_not_for_type` | 422 | Ce genre de surface n'existe pas sur ce type de bien. |
| `area_source_not_for_kind` | 422 | Cette méthode de mesure ne peut pas produire ce genre de surface. |
| `area_accuracy_without_measure` | 422 | Une surface déclarée ne peut pas annoncer de précision de mesure : rien n'a été mesuré. |
| `area_accuracy_out_of_range` | 422 | Cette précision n'est pas utilisable pour cette surface. |
| `areas_inconsistent` | 422 | Ces surfaces se contredisent : la surface habitable ne peut pas dépasser la surface construite. |
| `rooms_not_for_type` | 422 | Ce type de bien n'a pas de pièces à décrire. |
| `rooms_out_of_range` | 422 | Ce nombre de pièces est invraisemblable. |
| `amenity_unknown` | 422 | Cet équipement n'est pas dans la liste reconnue. |
| `document_invalid` | 422 | Un titre de propriété doit être une référence de fichier, et il y a un maximum. |
| **Photos** | | |
| `media_not_found` | 404 | Photo introuvable |
| `media_photos_full` | 409 | Un bien porte 30 photos au plus : retirez-en une avant d'en ajouter |
| `media_property_archived` | 409 | Un bien archivé ne reçoit plus de photo |
| `media_format` | 415 | Envoyez une photographie en JPEG, PNG ou WebP |
| `media_too_large` | 413 | Cette photo est trop lourde ou trop grande |
| `media_too_small` | 422 | Cette photo est trop petite : 480 pixels au moins sur le petit côté |
| **Annonces** | | |
| `listing_not_found` | 404 | Annonce introuvable |
| ~~`listing_not_yours`~~ | — | 🆕 v4.124.0 — **n'est plus émis** : un bien d'une autre fiche rend `property_not_found`, l'auto-publication `listing_published_by_staff`, le motif `moderated` `listing_moderated_by_staff` |
| `listing_published_by_staff` 🆕 | 403 | La publication est faite par l'équipe Dira, après relecture |
| `listing_moderated_by_staff` 🆕 | 403 | Seule l'équipe Dira retire une annonce pour modération |
| `area_source_reserved` 🆕 | 422 | Une surface « mesurée » vient d'un capteur : saisie à la main, elle se déclare |
| `deposit_out_of_range` 🆕 | 422 | La caution se compte en mois de loyer, de 0 à 24 |
| `listing_kind_unknown` | 422 | Ce type d'offre n'existe pas |
| `listing_kind_not_for_property` | 422 | Ce type d'offre ne va pas avec ce type de bien |
| `price_period_not_for_kind` | 422 | Cette périodicité ne va pas avec ce type d'offre : un loyer se dit au mois, une nuitée à la nuit, une vente au total |
| `price_required` | 422 | Une offre a besoin d'un prix |
| `price_out_of_range` | 422 | Ce montant est hors des bornes acceptées |
| `charges_not_for_kind` | 422 | Seule une location longue durée porte des charges mensuelles |
| `deposit_not_for_kind` | 422 | Seule une location longue durée porte une caution |
| `listing_status_transition` | 409 | Cette annonce ne peut pas aller là depuis son état actuel |
| `listing_closed` | 409 | Une annonce close ne se modifie plus |
| `close_reason_unknown` | 422 | Dites pourquoi cette annonce se termine |
| **Créneaux et visites** | | |
| `visit_listing_not_live` | 409 | Les visites ne se proposent que sur une annonce en ligne |
| `visit_slots_full` | 409 | Cette annonce propose déjà le nombre maximal de créneaux |
| `visit_slot_not_found` | 404 | Ce créneau de visite n'existe pas ou a été retiré |
| `visit_not_found` | 404 | Visite introuvable |
| `visit_too_late` | 409 | Trop tard : la visite a commencé ou va commencer |
| `visit_status_transition` | 409 | Cette visite ne peut pas aller là depuis son état actuel |
| `thread_not_found` 🆕 | 404 | Conversation introuvable |
| `message_blocked` 🆕 | 422 | Ce message n'a pas été envoyé : il contient une menace ou une insulte que nous ne relayons pas |
| `visit_not_started` | 409 | La présence se constate une fois la visite commencée |
| `visit_slot_started` 🆕 | 409 | Ce créneau a commencé : il ne se retire plus — constatez la présence des visiteurs |
| `account_unknown` | 401 | Cette session ne désigne aucun compte : reconnectez-vous |

---

## 15. Journal

### 4.128.0 — 10 novembre 2026

🔧 **LE DÉTACHÉ N'EST PLUS PRÉVENU** (§5.3, §10) : les visites et les messages des
annonces d'un collaborateur détaché vont au **porteur** de la fiche.

🔧 **LA VILLE EST OBLIGATOIRE** (§6.7) : `422 address_city_required`.

🔧 **UNE SEULE PAGINATION** (§8.4) : les annonces comme les biens — 20 par défaut,
100 au plus, `next_cursor` **absent** en fin (il valait `""`).

🔧 Deux restes de la 4.121.0 corrigés : `items` n'est jamais `null` (§8.4), et les
bornes du titre et du texte se comptent en **caractères** (§13).

### 4.127.0 — 10 novembre 2026

🟢 **LES MESSAGES DES CHERCHEURS** (§9.7) : `/immo/publishers/me/threads`. Le porteur
et les `manager` voient tous les fils, un `agent` ceux de ses annonces ; le
chercheur reste anonyme ; les coordonnées sont masquées dans les deux sens tant
qu'aucune visite n'est confirmée. Notification `immo_thread_message`.

### 4.126.0 — 10 novembre 2026

🟢 **« PRISE SUR PLACE »** (§7.1, §10 bis.3) : le serveur lit la position GPS de la
photo, la compare à l'adresse du bien (150 m), et garde le **verdict** seul —
`on_site` : `match`, `mismatch`, `absent`, `unknown`. Le public ne voit que
« prise sur place ». ⚠️ Une photo réduite sur le téléphone doit **garder ses
étiquettes GPS**, sinon elle arrive `absent`.

### 4.125.0 — 10 novembre 2026

🟢 **CLÔTURER FERME LES VISITES À VENIR** (§8.6) : créneaux pas encore commencés
retirés, visites annulées avec `closed_by: "listing"`, chercheurs prévenus. Une
pause n'y touche pas.

🔧 **UN CRÉNEAU COMMENCÉ NE SE RETIRE PLUS** (§9.3) : `409 visit_slot_started` — il
annulait la visite en cours. **`presence_at`** est servi (§9.6).

🔧 §4.3 : la 4.124.0 promettait `reason` sur un nom vide ; il n'y est que pour un
nom fait d'espaces.

### 4.124.0 — 10 novembre 2026

🔧 **SEPT REFUS QUI DISENT ENFIN LA BONNE CHOSE** (dira-immo-api#72, second lot) :

- **Rattacher** (§5.2) : par `user_id` comme par téléphone, le compte doit être un
  **annonceur du pays de l'agence** — sinon `404 account_not_found`, le même refus
  que « personne ». Un collaborateur déjà pris rend `409 member_already_attached`.
- **La fiche** (§4.3) : `name` vide refusé (il effaçait le nom) ; un `id` de site
  inventé ou répété refusé (`422 unknown_site`) ; un site retiré sous ses
  collaborateurs refusé (`409 site_has_members`) ; `PATCH {}` refusé
  (`422 nothing_to_update`), ici et sur un collaborateur (§5.3).
- **Les surfaces** (§6.4) : `measured_*` et `gps_polygon` saisis à la main sont
  refusés (`422 area_source_reserved`) — une mesure déjà écrite, renvoyée telle
  quelle, passe.
- **Les annonces** (§8) : le bien d'une autre agence est `404 property_not_found`
  (le `403` confirmait qu'il existait) ; publier depuis la relecture rend
  `403 listing_published_by_staff`, le motif `moderated` `403
  listing_moderated_by_staff` ; une caution au-delà de 24 mois
  `422 deposit_out_of_range`. `listing_not_yours` n'est plus émis.

### 4.123.0 — 10 novembre 2026

🔧 **L'application se nomme Dira Immo Pro**, et un agent qui cherche un logement le
fait dans **Dira Immo** (l'application du chercheur), plus dans l'application
cliente. `POST /me/devices` reste **sans** `app` ici (§10 bis.1).

### 4.122.0 — 10 novembre 2026

🟢 **SUR LE TÉLÉPHONE DE L'AGENT** (§10 bis) : l'application des annonceurs, Dira
Immo Pro (`app: "immo_pro"`) — l'ouverture en trois appels, le téléphone retiré **avant** la
déconnexion, la notification qui ouvre une visite, les photos depuis l'appareil
photo (réduire à 1 600 px, convertir le HEIC, la seule file permise), la position
du bien sans position par défaut, l'appel au visiteur jamais gardé dans les
contacts, le partage d'une annonce en ligne, ce qui attend le réseau et ce qui
n'attend pas.

🟢 **`share_url`** sur la vue publique (§8.8).

### 4.121.0 — 10 novembre 2026

**Première version.** L'entrée (§2), la porte (§3), la fiche (§4) et l'équipe
d'une agence (§5), les biens et la provenance de leurs surfaces (§6), les
photos réencodées (§7), les annonces, leurs états et l'historique des prix
(§8), les créneaux et les visites côté annonceur (§9), les notifications
(§10), les réglages de publication — annoncés, non appliqués (§11). Ce qui
n'est pas servi, et pourquoi : §12.
