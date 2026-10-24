# PARTENAIRE — PROPRIÉTAIRE DE FLOTTE PRIVÉE — contrat d'API

> **Version 4.73.0** · 24 octobre 2026
> Socle : `https://api-staging.dira.llc/api/v1` · Courses : `https://api-staging.dira.llc/api/v1/vtc`
> Console en service : `https://partner-staging.dira.llc`
>
> ⚠️ **CE CONTRAT SERT DEUX SURFACES : la console web ET l'application
> mobile.** Les routes, les refus et les règles de périmètre sont les mêmes ;
> **la section 1 ter dit tout ce qu'un téléphone ajoute** — la connexion avec
> son appareil, le fond de carte, le verrou, les notifications, le compte, le
> hors ligne et le battement de la carte. Lisez-la avant de dessiner un
> écran.

---

## 0. ⚠️⚠️ LIRE CECI AVANT TOUT LE RESTE

**Cette surface n'est pas une console d'administration réduite.** Elle sert un
**tiers extérieur** — une société de transport — qui a des pouvoirs sur **le
travail d'autres personnes** :

- il pose une **limite de dette** sur un chauffeur, ce qui **coupe ses appels** ;
- il **reprend sa voiture**, ce qui met son conducteur **hors ligne** ;
- il enregistre les **termes de sa rémunération**.

Chaque fois qu'on ajoute quelque chose ici, la question n'est donc pas « est-ce
techniquement disponible ? » mais « **qu'est-ce que ça lui donne sur une
personne ?** ». La section 2 liste ce qui est **délibérément absent** — et ce
n'est pas une liste de manques à combler, c'est le contrat.

⚠️ **TOUT CE QUI EST SERVI ICI VIT SOUS `/partner/...`.** Si une interface
partenaire appelle une route d'exploitation (`/admin/...`), le serveur répond
`403` — mais le code aura appris à la demander, et c'est ainsi qu'une console
devient autre chose que ce qu'elle est.

---

## 1. Deux bases d'URL, aucun socket

| | Rôle | Base |
|---|---|---|
| **Socle** (`dira-core-api`) | connexion, **ma flotte**, **mon personnel**, **le matériel de la société** | `…/api/v1/` |
| **Courses** (`dira-vtc-api`) | **le parc**, **les conducteurs**, **les courses**, **la paie**, **les papiers**, **le support** | `…/api/v1/vtc/` |

> **Le jeton est le même des deux côtés.** Une seule connexion, au socle.

⚠️ **AUCUN TEMPS RÉEL PAR SOCKET, et c'est un choix.** La carte des véhicules se
rafraîchit par un **appel HTTP toutes les dix secondes** (`GET
/vtc/partner/live`). Un socket aurait demandé à maintenir un flux par
propriétaire connecté pour une information qui n'a pas besoin d'être à la
seconde : une voiture se déplace, elle n'apparaît pas.

⚠️ **DEUX APPELS POUR L'ACCUEIL, PAS UN.** `GET /vtc/partner/dashboard` porte
les totaux (rechargé au **changement de période**) et `GET /vtc/partner/live`
porte la carte (toutes les **dix secondes**). Les fondre fait **réagréger un
mois de courses six fois par minute**, pour déplacer trois marqueurs.

---

## 1 bis. Conventions

| | |
|---|---|
| Auth | `Authorization: Bearer <access_token>` — le même jeton pour le socle et pour les courses |
| Erreurs | `{ "error": { "code": "snake_case", "message": "…", "fields"?: ["…"], "reason"?: "…" } }` |
| Pagination | `?limit=20&cursor=<id>` → `{ "items": [...], "next_cursor": "…" }` — `next_cursor` absent = dernière page |
| Montants | **entiers**, en XOF (`…_xof`). Jamais de flottant. |
| Dates | ISO 8601 UTC (`2026-10-10T10:41:23Z`). Une **date seule** s'écrit `YYYY-MM-DD`. |
| Coordonnées | `[lng, lat]`, dans cet ordre, partout |
| Langue | `Accept-Language: fr` ou `en` — les messages d'erreur suivent |
| Pays | **rien à envoyer** : voir la section *Le pays* |
| Fil | `X-Request-ID` — sur chaque réponse ; à l'aller, facultatif mais recommandé |

**Traitez le `code`, pas le message.** Le message est traduit et peut changer ;
le code est le contrat.

⚠️⚠️ **ET AFFICHEZ LE `message` MOT POUR MOT.** Les refus de cette surface
**disent quoi faire** — « demandez à son chauffeur de le déclarer dans
l'application Dira Chauffeur », « demandez un accès *gérant* à son
propriétaire ». Les récrire perd la phrase qui évite l'appel au support. Le
`code` sert à **décider**, le `message` à **montrer**.

**`401`** = jeton expiré : `POST /auth/refresh`, puis rejouer la requête ; si le
rafraîchissement échoue, revenir à la connexion.
**`403`** = ce rôle, ou cette personne, n'a pas accès — **ne pas réessayer**.

⚠️ **UN SEUL RAFRAÎCHISSEMENT À LA FOIS.** Le jeton de rafraîchissement **tourne**
(l'ancien est consommé). Trois listes chargées ensemble rendent trois `401` : sans
verrou, deux des trois rejouent avec un jeton déjà dépensé et la session tombe au
**premier chargement de page**. Un seul appel en vol, les autres l'attendent.

### 🧵 Le fil d'une requête — `X-Request-ID`

Chaque réponse porte un `X-Request-ID` : le nom sous lequel cette requête est
écrite dans nos journaux, **y compris sur un refus et y compris sur un `500`**.
Gardez-le avec l'erreur que vous enregistrez — un ticket qui cite le fil se lit
en trente secondes.

⚠️ **Un build qui tourne dans un navigateur ne lit cet en-tête que si la façade
l'expose.** Raison de plus d'**envoyer le vôtre** : l'en-tête est accepté à
l'aller et repris tel quel. Forme : **64 caractères au plus**, dans
`a-z A-Z 0-9 - _ .` ; hors de ces règles il est **refusé, pas tronqué**, et le
serveur tire le sien (la réponse dit lequel a été retenu).

⚠️ **Jamais de donnée personnelle dedans** : ce mot est recopié dans les
journaux de plusieurs services, et une base de journaux ne se purge pas comme un
compte se supprime.

### ⚠️ Le pays — rien à envoyer

Dira s'installe pays par pays et **toute donnée est bornée par pays**. Sur cette
surface, il n'y a **rien à faire** : le pays vient du **compte**, inscrit dans le
jeton, et le serveur borne chaque lecture. Un propriétaire ne change pas de pays
en changeant un en-tête.

⚠️ **N'envoyez pas `X-Dira-Country`** pour « voir un autre pays » : l'en-tête
qui nomme un pays **non servi** par le déploiement est **ignoré, pas refusé**, et
l'écriture se rangerait dans le pays du compte sans que rien ne le signale. La
réponse porte le pays **retenu** dans le même en-tête : si vous le lisez,
c'est pour l'afficher, pas pour le choisir.

⚠️ **Une société ne possède des voitures que dans UN pays.** Un contrat par
pays, un compte par contrat. Si le même groupe opère au Togo et au Sénégal, ce
sont **deux flottes et deux accès** — et c'est voulu : la monnaie, les
commissions et les règles d'appel diffèrent.

---

## 📱 1 ter. SUR UN TÉLÉPHONE — ce qui change, et ce qui ne change pas (v4.64.0)

Ce document a d'abord décrit une **console web**. Il décrit maintenant aussi une
**application**, et tout ce qui précède reste vrai : mêmes routes, mêmes refus,
mêmes règles de périmètre. Ce qui suit est ce qu'un téléphone ajoute.

> ⚠️⚠️ **LE PUBLIC NE CHANGE PAS, LE MOMENT SI.** Un propriétaire de flotte
> ouvre une console le soir, assis, pour régler la paie. Il ouvre une
> application **debout, dans une cour, entre deux voitures** — pour savoir si
> quelque chose cloche et qui appeler. Les écrans qui comptent sur un téléphone
> sont donc **l'accueil, la carte et les conducteurs** ; la paie et le grand
> livre s'y consultent, ils ne s'y règlent pas confortablement.
>
> **N'en déduisez pas qu'il faut les retirer** : un gérant qui n'a que son
> téléphone doit pouvoir tout faire. Déduisez-en l'ORDRE des écrans.

### 📱 1 ter.1 La connexion depuis une application

Le corps gagne trois champs que la console n'envoyait pas :

```json
POST /auth/login
{ "email": "…", "password": "…",
  "app": "partner",
  "platform": "android",                      // android | ios | web
  "device_id": "<identifiant d'installation>",
  "device_name": "Tecno Spark 10 · Android 13" }
```

⚠️ **`app: "partner"` EST OBLIGATOIRE, et il l'était déjà.** Sans lui, un
chauffeur se connecte ici, reçoit un jeton parfaitement valide, puis se fait
refuser écran par écran — et croit l'application cassée plutôt que de comprendre
qu'il s'est trompé d'application. Avec lui, le socle refuse **avant** d'émettre
le jeton (`403 wrong_app`), et son `reason` **nomme l'application à ouvrir** :
affichez-le tel quel.

⚠️ **`device_id` EST L'IDENTIFIANT D'INSTALLATION, pas celui du matériel.** Il
doit survivre à une mise à jour et mourir avec une réinstallation. N'utilisez ni
l'IMEI, ni l'identifiant publicitaire, ni rien qui suive la personne d'une
application à l'autre.

⚠️⚠️ **PLUSIEURS APPAREILS SONT PERMIS, ET LE PLUS ANCIEN EST CHASSÉ.** Au-delà
du plafond, la session la plus ancienne est fermée **en silence côté serveur** :
son prochain appel rendra `401`. L'application doit donc traiter un `401` comme
« ma session a été fermée ailleurs » et **revenir à la porte sans drame**, pas
comme une panne.

⚠️ **LE PLAFOND EST UN RÉGLAGE DE PAYS — n'en codez AUCUN nombre en dur, et
n'en affichez aucun.** Le défaut est de trois, mais un pays peut le changer, et
le serveur ne vous dit pas lequel s'applique. Un écran qui annoncerait « 3
appareils maximum » mentirait le jour où ce pays passe à deux — et la personne
chercherait le bug chez elle.

⚠️ **ET UN PARTENAIRE N'EST PAS UN CHAUFFEUR.** La règle de **l'appareil
unique** — un seul téléphone par compte, le précédent chassé à chaque connexion
— ne vise **que le rôle `driver`**. Si vous reprenez du code de l'application
chauffeur, ne reprenez pas celui-là : un propriétaire a souvent un téléphone et
une tablette, et les faire se chasser l'un l'autre rendrait l'application
inutilisable.

Un gérant et un propriétaire qui **partagent un compte** — ça arrive — se
chassent en revanche mutuellement ; c'est la raison de la section 12 (un compte
par personne).

#### ⚠️ La réponse porte deux blocs que la console ignorait

```json
{ "access_token": "…", "refresh_token": "…", "user": { … },
  "maps":     { "basemap": "dira" },
  "app_lock": { "mode": "optional", "biometrics": true,
                "pin_length": 4, "grace_seconds": 120, "max_attempts": 5 } }
```

Ils sont servis **à tous les comptes**, partenaire compris. La console web n'en
faisait rien ; une application, si.

### 🗺️ 1 ter.2 LE FOND DE CARTE — `maps`

Cette surface a une **carte en direct** (section 5.2). Sur un téléphone, c'est
l'écran qu'on vient voir.

| Valeur | Ce que l'application affiche |
|---|---|
| `"dira"` | **notre** fond, tuiles OSM — aucune clé, aucun compte |
| `"google"` | Google Maps, avec **la clé embarquée dans l'application** |

⚠️⚠️ **LE SERVEUR NE SERT AUCUNE CLÉ, NULLE PART.** Ni le socle, ni la console,
ni ce bloc. `basemap` dit seulement **lequel des deux fonds** ce pays a choisi ;
la clé Google, si elle est nécessaire, vit dans l'application et se restreint
chez Google par signature d'application. Un serveur qui servirait une clé la
servirait à tout porteur d'un jeton — c'est-à-dire à quiconque sait se
connecter.

⚠️ **C'EST UN RÉGLAGE DE PAYS, PAS DE COMPTE.** Il peut changer d'un
déploiement à l'autre : relisez-le **à chaque connexion et à chaque
rafraîchissement du jeton**, et n'en faites pas une constante de compilation.

### 🔒 1 ter.3 LE VERROU DE L'APPLICATION — `app_lock`

| Champ | Sens |
|---|---|
| `mode` | `off` · `optional` · `required` |
| `biometrics` | l'empreinte ou le visage sont acceptés |
| `pin_length` | la longueur du code |
| `grace_seconds` | délai avant de redemander après un passage en arrière-plan |
| `max_attempts` | essais avant de fermer la session |

⚠️⚠️ **CE VERROU EST LOCAL, ET IL NE REMPLACE PAS LE MOT DE PASSE.** Il protège
contre quelqu'un qui prend le téléphone posé sur un comptoir, pas contre
quelqu'un qui connaît l'identifiant. `required` **n'autorise pas** à garder la
session ouverte éternellement — le jeton expire comme ailleurs.

⚠️ **ET IL EST PARTICULIÈREMENT JUSTIFIÉ ICI.** Cette application pose des
limites de dette et reprend des voitures : un téléphone déverrouillé posé dans
une cour donne ces pouvoirs à qui le ramasse. Si le pays sert `optional`,
**proposez-le à la première ouverture** plutôt que de l'enterrer dans les
réglages.

### 🔔 1 ter.4 LES NOTIFICATIONS — et l'avertissement qui compte

```
POST   /me/devices                 { "token": "<jeton FCM/APNs>", "platform": "android" }  → 201
DELETE /me/devices/{token}                                                                  → 204
GET    /me/notifications[?limit=&cursor=]  → { "items": [...], "unread": 0, "next_cursor": "…" }
POST   /me/notifications/read                                                               → 200
POST   /me/notifications/{id}/read                                                          → 200
```

⚠️ **DÉCLAREZ LE TÉLÉPHONE APRÈS LA CONNEXION, ET RETIREZ-LE À LA
DÉCONNEXION.** Un jeton laissé derrière continue de recevoir : le propriétaire
suivant du téléphone — ou la personne à qui on l'a prêté — lirait les alertes
d'une société qui n'est pas la sienne.

⚠️ **LE CENTRE DE NOTIFICATIONS EXISTE PARCE QU'UNE POUSSÉE NE LAISSE AUCUNE
TRACE.** Un téléphone éteint, une bannière balayée, et le message n'existe plus
nulle part. `GET /me/notifications` est l'endroit où il reste.

> ### ⚠️⚠️ AUJOURD'HUI, RIEN NE POUSSE VERS UN PROPRIÉTAIRE DE FLOTTE
>
> **Vérifié au 11 octobre 2026, dans le code :** les deux seules notifications
> liées à une flotte — `vehicle_assigned` et `vehicle_taken_back` — partent vers
> **le chauffeur**, pas vers la société. Aucune autre ne vise un compte
> `partner`.
>
> **Conséquence pour vous : la boîte d'un partenaire est vide, et elle le
> restera tant que le serveur n'aura pas changé.** `GET /me/notifications` rend
> `{"items": [], "unread": 0}` — ce n'est ni un bug, ni un problème de jeton, ni
> un filtre mal posé. **Ne cherchez pas.**
>
> ⚠️ **Et n'en faites pas un écran vide qui a l'air cassé.** Si vous dessinez un
> centre de notifications, écrivez-y ce qui est vrai — « vous serez prévenu ici
> dès que Dira enverra des alertes à votre société » — plutôt qu'un « aucune
> notification » qui se lit comme une panne.
>
> **Ce qui mériterait une poussée, quand le serveur l'enverra** (c'est la
> demande à porter côté serveur, pas une promesse de ce contrat) : une pièce qui
> expire sous 30 jours, un chauffeur qui atteint sa limite de dette, une voiture
> immobilisée depuis plus de 24 h, un ticket de support répondu.
>
> **En attendant, les alertes se lisent dans `GET /vtc/partner/dashboard`**
> (section 5.1), au premier plan. C'est la seule source, et c'est pour ça que
> l'accueil s'ouvre dessus.

### 👤 1 ter.5 MON COMPTE — et le mot de passe (v4.64.0)

```
GET    /me                        → le compte
PATCH  /me                        { "name"?, "first_name"?, "last_name"?, "email"?, "avatar_url"? }
PATCH  /me/preferences            { "locale"?, "theme"?, … }
POST   /me/password               { "current_password", "new_password" }   → 204
```

> ### ⚠️⚠️ `POST /me/password` EST NOUVEAU, ET IL COMBLE UN TROU ORDINAIRE
>
> Cette route **n'existait pas** avant la v4.64.0. Les accès de cette plateforme
> sont **ouverts par quelqu'un d'autre**, qui choisit le mot de passe et le
> transmet de vive voix : Dira ouvre celui d'un partenaire, le propriétaire
> ouvre celui de son gérant. Sans ce geste, **celui qui a créé le compte en
> connaît le mot de passe pour toujours**, et la personne qui s'en sert n'a
> aucun moyen de reprendre la main.
>
> **Mettez-le sur le chemin, pas au fond des réglages.** C'est le premier geste
> à faire après une première connexion.

| Refus | Code | Ce que l'écran en fait |
|---|---|---|
| Mauvais mot de passe actuel | `current_password_wrong` | souligner **ce champ-là** ; la phrase dit déjà d'aller demander à qui a ouvert l'accès |
| Le même qu'avant | `password_unchanged` | souligner le nouveau |
| Moins de 8 / plus de 120 | `password_too_short` · `password_too_long` | **la phrase porte le nombre** : affichez-la, n'écrivez pas « trop court » |
| Compte sans mot de passe | `account_has_no_password` | ne peut pas arriver ici — un partenaire entre par mot de passe |

⚠️ **L'ANCIEN MOT DE PASSE EST EXIGÉ, et c'est ce qui sépare « changer son mot
de passe » de « prendre un compte ».** Un téléphone laissé déverrouillé
suffirait sinon à verrouiller quelqu'un **hors** de son propre compte, en y
gardant l'accès.

⚠️ **LE TÉLÉPHONE NE SE CHANGE PAS ICI.** `PATCH /me` ne porte pas de champ
`phone`, et le serveur ne l'accepterait pas : le numéro **identifie** le compte.
Montrez-le en lecture, et dites où il se change (auprès de Dira).

⚠️ **ET ON NE SUPPRIME PAS SON COMPTE DEPUIS CETTE APPLICATION.** Le socle sert
bien `DELETE /me` (RGPD, en deux temps), mais ici ce serait un piège : l'accès
d'un partenaire vient d'un **contrat**, pas d'une inscription. Un propriétaire
qui effacerait son compte laisserait sa flotte **sans signataire** — ses
voitures, ses conducteurs et ses papiers derrière une porte que plus personne
n'ouvre. **Écrivez-le** plutôt que de vous taire : quelqu'un qui cherche ce
bouton et ne le trouve pas écrit au support.

### 📴 1 ter.6 HORS LIGNE — ce qu'on garde, ce qu'on ne garde jamais

Il n'y a **aucune route de synchronisation** sur cette surface, et ce n'est pas
un oubli : un propriétaire **consulte**, il ne produit pas d'opérations au bord
de la route. Les gestes qu'il pose — une limite de dette, une reprise de
voiture — **touchent quelqu'un d'autre** et ne doivent pas partir d'une file
rejouée plus tard, alors que la situation a changé.

| | |
|---|---|
| **À garder** pour l'affichage | le dernier `dashboard`, le dernier parc, la dernière liste de conducteurs, le dernier relevé |
| **Jamais en file d'attente** | la limite de dette, la reprise d'une voiture, le rattachement, les termes de paie, les écritures du grand livre |

⚠️ **UN CHIFFRE GARDÉ DOIT PORTER SON HEURE.** « 142 500 F » sans date se lit
comme le total de maintenant. Affichez « il y a 3 h » à côté, et **grisez** ce
qui n'a pas été rafraîchi.

⚠️ **ET LA CARTE NE SE GARDE PAS.** Une position de voiture vieille de dix
minutes est pire qu'une carte vide : on appelle un chauffeur en croyant savoir
où il est. Hors réseau, la carte affiche **« positions indisponibles »**, pas
les dernières connues.

### ⏱️ 1 ter.7 Le battement de dix secondes s'arrête en arrière-plan

`GET /vtc/partner/live` est appelé **toutes les dix secondes** tant que la carte
est à l'écran — et **seulement** là.

⚠️ **ARRÊTEZ-LE DÈS QUE L'APPLICATION PASSE EN ARRIÈRE-PLAN**, et dès que la
carte n'est plus l'onglet actif. Six appels par minute laissés en fond, c'est la
batterie d'un propriétaire qui descend pendant qu'il ne regarde rien — et il
désinstalle sans jamais dire pourquoi.

⚠️ **NE RATTRAPEZ PAS LES BATTEMENTS MANQUÉS** au retour au premier plan :
faites **un** appel, et reprenez le rythme. Rejouer trente appels d'un coup ne
rend pas une carte plus à jour, elle rend juste le serveur plus occupé.

---

## 2. ⚠️ CE QUE CETTE SURFACE NE SERT PAS — et pourquoi

**N'en dessinez pas les écrans.** Chaque ligne est une décision, pas un retard.

| Absent | Pourquoi |
|---|---|
| **Suspendre un chauffeur** | ce serait se servir de notre dispatch comme d'un **outil de pression sur un travailleur**. Le propriétaire peut **reprendre son bien** — même effet pratique, et c'est honnête : la voiture est à lui, la personne reste chauffeur Dira |
| **Le temps en ligne d'un chauffeur** | ce chiffre n'existe que pour **peser sur quelqu'un**. Le serveur ne le sert pas à cette surface |
| **Le taux d'acceptation d'un chauffeur** | même raison. Et un bonus assis dessus a été **refusé à l'écriture** ailleurs sur la plateforme, pour la même raison |
| **Le passager d'une course** | nom, numéro, note : rien. Un propriétaire n'a aucune raison de connaître les clients de son chauffeur |
| **Le départ et l'arrivée d'une course** | ⚠️ **une adresse de départ EST l'adresse de quelqu'un**, et l'historique des arrivées **est son carnet d'adresses**. Les courses sont servies **sans aucun point géographique** |
| **Le grand livre d'un chauffeur** | son matériel, ses bonus, ses prélèvements : c'est **sa** relation avec Dira. La flotte voit ce que **ses voitures** ont produit, pas le compte de la personne |
| **Les tickets de support d'un chauffeur** | ⚠️ **ne jamais ouvrir ça** : un chauffeur doit pouvoir écrire « mon propriétaire prend 90 % et ne répare pas les freins ». Le guichet borne au **participant** |
| **Valider les pièces d'un chauffeur** | la conformité est arbitrée par Dira. Le propriétaire **dépose** les papiers de ses voitures, il ne les approuve pas |
| **Un prix, une commission, un réglage d'appel** | la tarification est une politique de pays. Une société ne fixe pas le prix d'une course Dira |
| **Détacher une voiture de sa flotte** | ⚠️ **c'est l'exploitation Dira qui le fait**, sur demande et avec un motif. Les écrans d'argent d'une flotte sont calculés sur les véhicules qu'elle possède **aujourd'hui** : une société qui détacherait elle-même ferait **disparaître de ses propres relevés** le mois qu'elle a encaissé avec cette voiture — et les espèces qui vont avec |
| **Créer un compte chauffeur** | le rattachement **ne crée aucun compte**, sinon un partenaire enrôlerait des conducteurs sans pièce d'identité ni casier judiciaire |
| **S'inscrire comme partenaire** | `POST /auth/register` **refuse** le rôle `partner`. Une seule porte : l'exploitation ouvre l'accès depuis la fiche du contrat |
| **Notifications poussées** | c'est une console web. Les alertes sont **calculées à la lecture** (`dashboard.alerts`), pas envoyées |

> ⚠️ **SI L'UN DE CES CHAMPS APPARAÎT UN JOUR DANS UNE RÉPONSE, NE L'AFFICHEZ
> PAS — signalez-le.** C'est un défaut côté serveur, et une interface qui
> l'affiche le rend définitif.

---

## 3. La porte : se connecter, puis lire SA flotte

### 3.1 La connexion

```
POST /auth/login
{
  "email":    "kodjo@teranga.sn",
  "password": "…",
  "app":      "partner",        ← ⚠️ OBLIGATOIRE
  "platform": "web"
}

→ 200 { "access_token": "…", "refresh_token": "…", "user": { … } }
```

⚠️⚠️ **`app: "partner"` EST OBLIGATOIRE, et c'est ce qui protège les deux
côtés.** Le serveur refuse un compte qui n'est pas partenaire **avant même
d'émettre un jeton** :

```
→ 403 { "error": { "code": "wrong_app", "reason": "driver",
        "message": "Ce compte est un compte chauffeur : ouvrez l'application Dira Chauffeur" } }
```

Sans ce champ, un chauffeur se connecterait ici et tomberait sur des `403`
**écran par écran**, sans jamais comprendre.

⚠️ **`error.reason` ET NON `error.meta`** — c'est `reason` qui porte le rôle réel.
⚠️ **UNE VALEUR DE `reason` INCONNUE N'EST PAS UNE ERREUR, ET SURTOUT PAS UN
BLANC.** La liste s'allonge (`client`, `driver`, `merchant`, `courier`,
`partner`…) : une interface qui traduit `reason` en nom d'application affiche
« Ouvrez l'application&nbsp;» — phrase coupée. **Repli : `error.message`, déjà
traduit.**

Puis `POST /auth/refresh` (rotation), `POST /auth/logout` (`{ refresh_token }`),
et `GET /me` pour le compte nu.

### 3.2 ⚠️ `GET /partner/me` est LA PORTE — pas un détail de l'en-tête

```
GET /partner/me        (socle)

→ 200 {
  "id":             "6ac8c5be57f350bd9013280d",
  "name":           "Transports Teranga",
  "contact_name":   "Kodjo Mensah",
  "contact_phone":  "+221771999001",
  "contact_email":  "kodjo@teranga.sn",
  "contract_ref":   "CT-2026-DEMO",
  "commission_bp":  1800,        ← taux négocié, en points de base ; absent/null = le taux de la plateforme
  "status":         "active",    ← active | suspended
  "role":           "owner",     ← MON rôle dans cette flotte (voir §4)
  "created_at":     "2026-10-09T10:45:18Z"
}
```

**Appelez-la en premier, après la connexion, et traitez ses refus comme des
écrans pleins — pas comme des pannes :**

| Code | Statut | Ce que l'écran doit dire |
|---|---|---|
| `partner_no_fleet` | `403` | « Aucune flotte n'est encore rattachée à ce compte. Demandez à Dira d'y rattacher votre contrat. » |
| `partner_fleet_suspended` | `403` | « Le contrat de cette flotte est suspendu : contactez Dira. **Vos chauffeurs, eux, continuent de travailler.** » |
| 🆕 `partner_fleet_internal` | `403` | « Cette flotte appartient à Dira : elle se gère depuis la flotte interne, pas depuis cette console. » (v4.73.0) |

### 🆕 ⚠️ `partner_fleet_internal` — la flotte de Dira n'est pas une société partenaire (v4.73.0)

Dira possède désormais des véhicules **en propre**, confiés à des chauffeurs
contre une **redevance**. Ils vivent dans une flotte marquée *interne*, créée une
fois par pays, **sans contrat et sans commission**.

⚠️ **UN COMPTE PARTENAIRE RATTACHÉ À CETTE FLOTTE SE VOIT REFUSER TOUTE LA
SURFACE**, et pas seulement un écran : la porte elle-même refuse, donc chaque
route de cette console répond `403`. **Traitez-le comme `partner_no_fleet`** —
un écran plein, une phrase, aucun bouton — et surtout **ne réessayez pas en
boucle**.

⚠️ **CE REFUS VAUT PLUS QU'UN CLOISONNEMENT D'ÉCRANS.** Cette surface porte
`PUT /vtc/partner/drivers/{driverID}/pay`, le **seul** chemin d'écriture des termes de
paie. Ouverte sur une flotte interne, elle aurait fait prélever à Dira **une
part de chaque course en plus de la redevance du véhicule** — deux fois le même
revenu, et rien pour le signaler puisque les deux nombres sont plausibles. Le
revenu d'un véhicule de Dira est **sa redevance, et elle seule**.

⚠️ **ET IL PASSE AVANT LE CONTRÔLE DE SUSPENSION** : une flotte interne est
`active`, donc le contrôle de statut l'aurait laissée entrer.

⚠️ **LA SECONDE PHRASE DE `partner_fleet_suspended` N'EST PAS DÉCORATIVE.** Un
propriétaire qui lit « contrat suspendu » croit que ses voitures sont arrêtées et
appelle ses chauffeurs pour les faire rentrer. Une suspension de contrat ferme
**la console**, pas le travail.

⚠️ **Un écran vide à la place de ces phrases fait appeler le support** — et le
support répondra ce que la phrase disait déjà.

⚠️ **LA FLOTTE N'EST JAMAIS DANS UN CHEMIN D'URL.** Aucune route de cette
surface ne porte d'identifiant de flotte : elle est **résolue à chaque requête**
à partir du jeton. Si une route vous en demandait un, c'est un défaut côté
serveur — **ne le contournez pas en l'envoyant**. (Conséquence utile : un contrat
suspendu prend effet **à la requête suivante**, pas à l'expiration du jeton.)

---

## 4. Trois rôles — et le défaut est « ne peut pas »

`role`, servi par `GET /partner/me`, vaut `owner`, `manager` ou `viewer`.

| Rôle | Lit | Écrit | Gère le personnel |
|---|---|---|---|
| `owner` — le **signataire du contrat** | ✅ | ✅ | ✅ |
| `manager` — le **répartiteur** | ✅ | ✅ | ❌ |
| `viewer` — le **comptable** | ✅ | ❌ | ❌ |

⚠️⚠️ **NE DESSINEZ PAS UN BOUTON QUE LE SERVEUR REFUSERA — et dites pourquoi il
manque.** Un bouton affiché puis refusé a déjà **promis** quelque chose. Lisez
le rôle **avant** de rendre l'écran, et remplacez le geste par la phrase qui
mène à la bonne personne.

⚠️ **LE DÉFAUT EST « NE PEUT PAS ».** Un `role` **absent** (un socle plus ancien,
une réponse tronquée) doit **couper** le pouvoir, jamais l'ouvrir. On ferme un
bouton par excès de prudence ; on n'ouvre pas une limite de dette par accident.

⚠️ **`owner` NE SE DONNE PAS, IL SE SIGNE.** Le serveur refuse `owner` à
l'écriture (`fleet_role_unknown` : « Le rôle doit être *gérant* ou *lecteur* »),
et on ne retire pas le propriétaire — **pas même lui-même**.

Les refus que vous verrez si l'écran devance le rôle :

```
→ 403 { "code": "partner_read_only",
        "message": "Votre rôle sur cette flotte est en lecture seule : demandez un accès « gérant » à son propriétaire" }

→ 403 { "code": "fleet_not_owner",
        "message": "Seul le signataire du contrat peut gérer le personnel de la flotte" }
```

⚠️ **Les deux ne disent pas la même chose** : le premier envoie vers le
propriétaire de la flotte, le second dit que **même un gérant** n'y touche pas.
Les fondre en « Interdit » envoie tout le monde au support.

---

## 5. L'ACCUEIL — ce qui cloche, qui travaille, où, et combien

⚠️⚠️ **L'ORDRE DE CETTE PAGE EST SA SEULE DÉCISION DE DESIGN**, et il suit ce
qu'un propriétaire se demande en l'ouvrant :

1. **est-ce que quelque chose cloche ?** → les alertes
2. **est-ce que mes voitures travaillent ?** → les compteurs
3. **où sont-elles ?** → la carte
4. **combien ça a rapporté ?** → les totaux

**L'argent est en DERNIER, délibérément.** C'est le chiffre qu'on regarde une
fois par jour ; les trois premiers demandent un geste **tout de suite**. Un
tableau de bord qui ouvre sur un total se referme sans que personne n'ait appelé
le chauffeur dont le téléphone est muet.

### 5.1 `GET /vtc/partner/dashboard` — les totaux, les compteurs, les alertes

```
GET /vtc/partner/dashboard?from=2026-10-01&to=2026-10-10&tz=Africa/Dakar

→ 200 {
  "fleet": "Transports Teranga",
  "from":  "2026-10-01",            ← la fenêtre RETENUE (voir §13)
  "to":    "2026-10-10",
  "totals": {                       ← ⚠️ ABSENT, et non à zéro, si indisponible
    "rides":           75,
    "fare_xof":        344400,      ← le prix CLIENT encaissé
    "commission_xof":  63259,       ← la part de Dira
    "driver_xof":      281141,      ← ce qui reste à partager entre la société et son chauffeur
    "distance_m":      1044466,
    "duration_s":      165721,
    "cash_xof":        175350,      ← le PLEIN TARIF des courses payées en espèces
    "cancelled_rides": 4
  },
  "counts": {
    "vehicles": 3, "on_ride": 0, "online": 1, "stale": 0, "offline": 2,
    "drivers": 3, "debt_xof": -3396, "over_limit": 0,
    "withdrawn": 0, "maintenance": 0
  },
  "vehicles": [ … voir §5.2 … ],
  "alerts":  [ { "kind": "no_vehicle", "severity": "warn", "subject": "DK-8740-EE" } ]
}
```

⚠️⚠️ **UN CHIFFRE INDISPONIBLE S'AFFICHE « — », JAMAIS 0.** `totals` est
**absent** quand l'agrégation n'a pas pu être faite. Sur un tableau de bord
d'argent, **un zéro faux est la pire réponse possible : on le croit**, et on en
tire une conclusion sur sa journée.

⚠️⚠️ **UNE DETTE NÉGATIVE EST UN CRÉDIT — CHANGEZ LE MOT, PAS LE SIGNE.**
`counts.debt_xof` peut être négatif (le chauffeur a payé d'avance).
« -3 396 F » sous le mot « dette » se lit comme un bug ; affichez
« 3 396 F d'avance ».

⚠️ **`cash_xof` EST DE L'ARGENT À RÉCLAMER, PAS À VERSER** — et c'est le **plein
tarif**, commission de Dira comprise. C'est ce que le chauffeur a **réellement eu
en main**. Servir sa seule part aurait fait réclamer moins qu'il ne détient.

### 5.2 `GET /vtc/partner/live` — la carte, toutes les dix secondes

```
GET /vtc/partner/live

→ 200 { "items": [ {
  "vehicle_id":     "6ab371373317b63a136b7272",
  "plate":          "DK-8740-EE",
  "class_key":      "eco",
  "brand":          "Hyundai",  "model": "i20",
  "status":         "service",            ← service | maintenance | withdrawn
  "state":          "on_ride",            ← on_ride | online | stale | offline
  "driver_id":      "6ab37137b46bacb400cc9ef0",
  "driver_user_id": "6ab3713774c82fdf276f8909",
  "driver_name":    "Astou Diouf",
  "driver_phone":   "+221771000118",      ← pour l'APPELER : c'est le geste de la plupart des alertes
  "geo":            [-17.4467, 14.6937],  ← [lng, lat] — ABSENT si jamais vu
  "last_seen_at":   "2026-10-10T09:58:12Z",
  "ride":           { "id": "…", "status": "on_board", "since": "2026-10-10T09:41:00Z",
                      "distance_m": 4200, "fare_xof": 2500 },
  "debt_xof":       -935,
  "over_limit":     false
} ] }
```

#### ⚠️ Quatre états, et les confondre coûte un appel téléphonique

| `state` | Mot | Ton | Le geste |
|---|---|---|---|
| `on_ride` | **En course** | ok | rien — il travaille |
| `online` | **Disponible** | ok | rien — il attend un appel |
| `stale` | **Plus vu** | ⚠️ alerte | **APPELEZ-LE** : il se croit disponible et ne reçoit plus d'appels |
| `offline` | **Hors ligne** | neutre | rien — il a fini sa journée |

⚠️⚠️ **« PLUS VU » N'EST PAS « HORS LIGNE ».** L'un appelle un coup de téléphone,
l'autre n'appelle rien. Les fondre a fait chercher des pannes de réseau là où
quelqu'un avait rangé son téléphone — et laissé un chauffeur se croire
disponible toute une matinée.

⚠️⚠️ **UNE VOITURE EN COURSE RESTE EN COURSE, MÊME SI SON TÉLÉPHONE S'EST TU.**
`on_ride` l'emporte sur `stale` : un passager à bord qu'on ne voit plus **ne doit
pas disparaître de la carte** — c'est précisément le moment où on le cherche.

#### ⚠️ La carte ne se recrée pas à chaque battement

Recréer l'objet de carte recharge les tuiles et **ramène la vue au centre**,
annulant le zoom qu'on venait de faire. Créez-la **une fois**, **déplacez** les
marqueurs, et ne **recadrez qu'au premier** jeu de positions.

⚠️ **`geo` ABSENT N'EST PAS `[0, 0]`.** Une voiture jamais vue n'a pas de
position : comptez-la dans un bandeau (« 2 véhicules sans position ») au lieu de
la poser au large du golfe de Guinée.

⚠️ **AFFICHEZ « DEPUIS COMBIEN DE TEMPS », PAS UN HORODATAGE.** « À bord depuis
2 h 05 » fait repérer une voiture bloquée ; `2026-10-10T09:41:00Z` non.

#### Les six alertes — et c'est le GESTE qui est servi, pas la phrase

```json
{ "kind": "debt_blocked", "severity": "high", "subject": "Kofi" }
{ "kind": "driver_stale", "severity": "warn", "count": 3 }
```

| `kind` | Ce que la phrase doit dire |
|---|---|
| `debt_blocked` | « *{subject}* ne reçoit plus d'appels : sa dette a franchi la limite » |
| `papers_missing` | ⚠️ **DÉCLARÉ, PAS ENCORE SERVI** — voir l'avertissement sous le tableau |
| `vehicle_withdrawn` | « *{subject}* est repris : rattachez-le à un chauffeur pour le remettre au travail » |
| `vehicle_maintenance` | « *{subject}* est immobilisé par Dira — contactez le support » |
| `driver_stale` | « *{count}* téléphone(s) ne donnent plus de position : appelez vos chauffeurs » |
| `no_vehicle` | « *{subject}* n'a aucun conducteur : rattachez-le depuis « Mon parc » » |

⚠️⚠️ **LE SERVEUR SERT LE GENRE ET LE SUJET, PAS LA PHRASE** — une phrase
traduite côté serveur aurait figé la langue de la console. Écrivez-les **en un
seul endroit** de votre code : trois écrans qui montrent les mêmes alertes
finissent par dire trois choses, et c'est celle qu'on lit le moins qui garde
l'ancienne.

⚠️⚠️ **L'ALERTE PORTE LE GESTE, PAS LE CONSTAT.** « 3 voitures sans papiers » est
un constat : on le lit une fois puis on l'ignore. « Déposez l'assurance de
AB-1234 » est un geste.

⚠️⚠️ **`papers_missing` EST DÉCLARÉ ET N'EST PAS ENCORE LEVÉ (v4.62.3).** Le
tableau de bord ne lit pas la conformité : prévoyez la ligne, ne comptez pas sur
elle, et **ne remplacez pas l'écran « Papiers » par une alerte qui n'arrivera
pas**. Les cinq autres sont servies.

⚠️ **UN `kind` INCONNU NE SE JETTE PAS** : affichez-le brut plutôt que de perdre
une alerte dont la gravité est servie (`severity`).

---

## 6. MON PARC — rattacher une voiture, la reprendre

### 6.1 `GET /vtc/partner/vehicles` — le parc

```
GET /vtc/partner/vehicles?limit=50&cursor=<id>

→ 200 { "items": [ {
  "id":            "6ab371373317b63a136b7272",
  "license_plate": "DK-8740-EE",
  "class_key":     "eco",
  "brand":         "Hyundai",  "model": "i20",  "color": "blanc",
  "seats":         4,
  "description":   "Hyundai i20 blanche",     ← GÉNÉRÉE par le serveur
  "status":        "service",                 ← service | maintenance | withdrawn
  "driver_id":     "6ab37137b46bacb400cc9ef0",
  "driver_name":   "Astou Diouf",
  "is_active":     true,                      ← la voiture que le conducteur a choisie aujourd'hui
  "photo_url":     "…",  "images": [ "…" ],
  "fleet_id":      "…",  "fleet_name": "Transports Teranga",
  "usage":         "vtc"
} ], "next_cursor": "…" }
```

#### ⚠️ Trois états, et le troisième ne se dit PAS comme les autres

| `status` | Mot à afficher | Qui a décidé | Ce que l'écran propose |
|---|---|---|---|
| `service` | **En service** | — | reprendre la voiture |
| `maintenance` | **Immobilisé par Dira** | **Dira** | *rien* — « contactez le support » |
| `withdrawn` | **Reprise** | **le propriétaire** | la rattacher à un conducteur |

⚠️⚠️ **`maintenance` ET `withdrawn` NE SE DISENT PAS PAREIL, PARCE QUE
L'INTERLOCUTEUR N'EST PAS LE MÊME.** « Immobilisé » envoie chercher un garage ;
une voiture `withdrawn` roule très bien — elle n'est simplement plus confiée à
personne. Et c'est cette ligne qu'on relit quand un chauffeur appelle pour
demander pourquoi il ne reçoit plus d'appels.

⚠️⚠️ **CETTE ROUTE NE PREND QUE `limit` ET `cursor` — AUCUN FILTRE.** Filtrez
**dans la page** (en service / repris / immobilisé, avec ou sans conducteur) :
un parc de société se compte en dizaines, pas en milliers. **Un paramètre qu'on
invente est un paramètre toujours ignoré**, et c'est le pire des défauts : la
liste répond `200`, l'écran affiche tout, et personne ne voit que le filtre ne
filtre rien. Le tableau des paramètres acceptés, route par route, est en §13.

⚠️ **UN ÉTAT INCONNU SE NOMME, IL NE PREND PAS LE CHEMIN RASSURANT.** N'écrivez
pas « immobilisé, **sinon** en service » : le jour où un quatrième état arrive,
cette écriture l'affiche en vert alors que la voiture ne prend aucune course —
et le compteur de la même page, lui, comptera juste. Deux chiffres qui se
contredisent sur un écran : on croit celui qui a l'air d'une phrase.

### 6.2 ⚠️ `POST /vtc/partner/vehicles/assign` — PLAQUE + TÉLÉPHONE, jamais des identifiants

```
POST /vtc/partner/vehicles/assign
{ "plate": "DK-8740-EE", "driver_phone": "+221771000118" }

→ 200 { …le véhicule à jour, avec son nouveau conducteur… }
```

⚠️⚠️ **DEUX CHOSES QU'UN PROPRIÉTAIRE CONNAÎT ET QU'IL NE PEUT PAS DEVINER.**
Avec des identifiants, il aurait balayé le parc d'un pays en changeant un
chiffre. **N'offrez donc aucune liste de véhicules « disponibles » ni de
chauffeurs à choisir** : il n'y en a pas, et c'est la protection.

⚠️ **ÉGALITÉ EXACTE SUR LA PLAQUE.** « AB1234 » ne ramène pas « AB12345 » :
rattacher la voiture du voisin serait indétectable des deux côtés. Si deux
voitures portent la même plaque, le serveur **refuse** au lieu de choisir
(`vehicle_plate_ambiguous`).

#### Les cinq refus de ce geste, et ce que l'écran doit en faire

| Code | Statut | Le message dit déjà quoi faire |
|---|---|---|
| `vehicle_plate_unknown` | `404` | « Aucun véhicule avec cette plaque : **demandez à son chauffeur de le déclarer dans l'application Dira Chauffeur** » |
| `driver_phone_unknown` | `404` | « Ce numéro n'est pas celui d'un chauffeur Dira : **la personne doit d'abord s'inscrire dans l'application Dira Chauffeur** » |
| `vehicle_in_another_fleet` | `409` | « Ce véhicule appartient déjà à une autre flotte » |
| `vehicle_plate_ambiguous` | `409` | « Deux véhicules portent cette plaque : **le support Dira doit corriger** avant le rattachement » |
| `driver_in_another_country` | `422` | « Ce chauffeur est enregistré dans un autre pays que le véhicule » |

⚠️ **`vehicle_in_another_fleet` NE DIT PAS LAQUELLE, et c'est voulu** : un
partenaire n'a pas à apprendre, en tapant des plaques, quelle société possède
quoi.

⚠️ **Ces deux `404` ne sont pas des erreurs techniques — ce sont les deux cas
les plus fréquents du geste.** Affichez-les **sous le champ concerné**, pas dans
une bannière rouge : la personne doit savoir si c'est la plaque ou le numéro
qu'elle doit corriger.

#### ⚠️ Ce que le geste fait d'autre — à écrire dans la confirmation

1. La voiture passe en **`service`** (un rattachement remet au travail une
   voiture reprise).
2. **Si le chauffeur n'avait aucun véhicule actif, celle-ci le devient.** Sans
   cela, l'écran du partenaire dirait « rattaché » et le dispatch « aucun
   véhicule » — l'écart se règle par un appel au support.
3. **Le chauffeur est prévenu** (`vehicle_assigned`). On ne lui demande pas son
   accord — c'est la voiture du partenaire —, donc **cette notification est son
   seul contre-pouvoir**.
4. ⚠️ **Si la voiture change de conducteur, l'ANCIEN est traité comme une
   reprise** : sorti du vivier, prévenu (`vehicle_taken_back`). Le laisser en
   ligne aurait envoyé des appels vers une voiture qui roule pour quelqu'un
   d'autre.

### 6.3 `POST /vtc/partner/vehicles/{vehicleID}/take-back` — reprendre son bien

```
POST /vtc/partner/vehicles/{vehicleID}/take-back
(aucun corps)

→ 200 { …le véhicule, status: "withdrawn"… }
```

⚠️ **UN POST SANS CORPS, ET PAS UN `PATCH` D'ÉTAT.** Le partenaire ne **choisit**
pas l'état du véhicule : il fait **un geste**, dont nous décidons les
conséquences. Un champ `status` dans un corps aurait laissé un propriétaire
écrire « maintenance » et croire avoir repris sa voiture.

**La confirmation doit porter les conséquences, dans cet ordre :**

> **« {plaque} sera reprise à {conducteur}.**
> Il passera **hors ligne** s'il conduisait cette voiture, et recevra un
> message. **Il reste chauffeur Dira** et peut conduire un autre véhicule. »

⚠️⚠️ **« IL RESTE CHAUFFEUR DIRA » N'EST PAS UNE POLITESSE.** C'est la
différence entre reprendre un bien et suspendre quelqu'un — et le propriétaire
doit savoir qu'il fait le premier, parce que c'est le seul des deux qu'il peut
faire.

⚠️ **Le véhicule GARDE son `driver_id`** : c'est la trace de qui l'avait, la
première question d'un litige. Ne l'affichez pas comme « conducteur actuel »
sur une voiture `withdrawn` — écrivez « repris à {nom} ».

⚠️ **Reprendre une voiture qui n'était PAS la voiture active du chauffeur ne le
met pas hors ligne** (il roule avec une autre). N'annoncez pas une conséquence
qui n'aura pas lieu : lisez `is_active`.

⚠️ **Il n'y a pas de « remettre en service » : on RATTACHE.** Une voiture reprise
revient au travail par `assign` (§6.2), avec un conducteur nommé. Un bouton
« remettre en service » aurait rendu une voiture à personne.

---

## 7. MES CONDUCTEURS — et la limite qui coupe les appels

### 7.1 `GET /vtc/partner/drivers`

```
GET /vtc/partner/drivers

→ 200 { "items": [ {
  "driver_id":    "6ab37137b46bacb400cc9ef0",
  "user_id":      "6ab3713774c82fdf276f8909",
  "name":         "Astou Diouf",
  "phone":        "+221771000118",
  "status":       "active",
  "online":       true,
  "rides_count":  41,          ← SES COURSES AVEC VOS VOITURES, pas toutes ses courses
  "rating_avg":   4.7,
  "plates":       [ "DK-8740-EE" ],
  "vehicle_ids":  [ "…" ],
  "debt_xof":     -935,
  "debt_cap_xof": 25000,       ← VOTRE limite ; absente = aucune
  "debt_cap_by":  "Transports Teranga",   ← le nom inscrit dessus
  "over_limit":   false,
  "pay":          { "mode": "commission", "share_bp": 7000, "by": "…", "at": "…" }
} ] }
```

⚠️ **CE SONT « VOS CONDUCTEURS », PAS « DES CHAUFFEURS ».** Quelqu'un apparaît
ici **parce qu'il conduit une de vos voitures** — pas parce qu'il travaille pour
vous. S'il rend la voiture, il disparaît de la liste, et c'est correct.

⚠️ **`rides_count` EST BORNÉ À VOS VÉHICULES.** Un chauffeur peut conduire sa
propre voiture le matin et la vôtre l'après-midi.

⚠️ **NI TEMPS EN LIGNE, NI TAUX D'ACCEPTATION** — voir §2. Ils ne sont pas
servis ; si vous en voyez un, signalez-le.

### 7.2 ⚠️ `PUT /vtc/partner/drivers/{driverID}/debt-cap` — le geste le plus lourd de la console

```
PUT /vtc/partner/drivers/{driverID}/debt-cap
{ "cap_xof": 25000 }        ← 0 RETIRE la limite

→ 200 { …le chauffeur à jour… }
```

⚠️⚠️ **CETTE LIMITE COUPE LES APPELS DE QUELQU'UN.** Au-delà, le dispatch ne lui
envoie plus de course : c'est son travail qui s'arrête. La confirmation doit le
dire, et dire aussi qui en répond :

> **« Au-delà de 25 000 F de dette, {nom} ne recevra plus d'appels.**
> **Votre nom de société sera inscrit sur cette limite.** »

⚠️ **LE NOM VIENT DE LA FLOTTE, PAS D'UN CHAMP.** Le corps ne porte **aucun
auteur** : un champ libre aurait laissé écrire « l'exploitation » sur une
décision de société. « Qui a posé 5 000 ? » doit avoir une réponse le jour où le
chauffeur la demande — et c'est ce jour-là qu'on en a besoin.

⚠️ **`0` RETIRE VOTRE LIMITE, CE N'EST PAS « ZÉRO FRANC ».** Et dites-le : **le
plafond du PAYS continue de s'appliquer.** Ce n'est pas une porte ouverte, c'est
le retrait d'un resserrement. `debt_cap_by` est alors vidé — l'auteur d'une levée
n'a pas à rester sur la fiche.

⚠️ `amount_out_of_range` (`422`) : « Ce montant doit être compris entre 0 et
100 000 000 F : **vérifiez le nombre de zéros** ». Ce montant se tape à la main,
et l'erreur n'est jamais « j'ai voulu cent millions ».

### 7.3 Les deux refus de périmètre

| Code | Statut | |
|---|---|---|
| `driver_not_in_your_fleet` | `403` | « Ce chauffeur ne conduit aucun de vos véhicules » |
| `vehicle_not_in_your_fleet` | `403` | « Ce véhicule n'appartient pas à votre flotte » |

⚠️ **`403` ET NON `404`.** Un `404` ferait croire à un partenaire dont on vient
de détacher un véhicule qu'il **a disparu de la base** — et il appellerait pour
le faire « recréer ».

⚠️ **Chaque objet est revérifié contre la flotte À CHAQUE REQUÊTE**, pas
seulement filtré dans la liste. N'en concluez pas qu'un identifiant lu ailleurs
« marchera » : il faut le taper, et il sera refusé.

---

## 8. LES COURSES DE LA FLOTTE — sans passager, sans adresse

```
GET /vtc/partner/rides?from=2026-10-01&to=2026-10-10&tz=Africa/Dakar&limit=50&cursor=<id>

→ 200 { "items": [ {
  "id":              "6abaeea5e57931887160b55c",
  "status":          "completed",
  "mode":            "standard",       ← standard | rental | pool …
  "rental_hours":    4,                ← location : la durée vendue
  "share_pct":       70,               ← course partagée : la PART du tarif ordinaire
  "driver_user_id":  "6ab37137b46bacb400cc9ed0",
  "driver_name":     "Sokhna Mbengue",
  "vehicle_id":      "6ab371373317b63a136b7270",
  "plate":           "DK-6528-CC",
  "class_key":       "confort",
  "created_at":      "2026-09-28T14:11:28Z",
  "ended_at":        "2026-09-28T14:39:02Z",
  "distance_m":      8420,
  "distance_source": "gps",            ← gps | route | estimate
  "duration_s":      1654,
  "fare_xof":        3100,             ← le prix CLIENT
  "commission_xof":  558,              ← la part de Dira
  "driver_xof":      2542,             ← ce qui reste à partager (société + chauffeur)
  "payment_method":  "cash",
  "paid":            true,
  "settled":         true
} ], "next_cursor": "…" }
```

⚠️⚠️ **NI PASSAGER, NI DÉPART, NI ARRIVÉE — ET CE N'EST PAS UN OUBLI.** Une
adresse de départ **est l'adresse de quelqu'un** ; l'historique des arrivées d'un
chauffeur **est le carnet d'adresses de ses clients**. N'affichez pas un espace
vide intitulé « Trajet » : écrivez ce que la ligne contient (une plaque, une
durée, une distance, un montant).

⚠️⚠️ **LA BORNE EST LE VÉHICULE, PAS LE CHAUFFEUR.** Sont servies les courses
faites **avec vos voitures**. Les courses que le même chauffeur fait avec sa
propre voiture **ne vous regardent pas** — et ne sont pas là.

⚠️ **`driver_user_id` ET NON `driver_id`** sur cette route : c'est l'identifiant
de **compte**. Ne le croisez pas avec `driver_id` de `/vtc/partner/drivers`, qui
est l'identifiant de **profil de chauffeur** ; `user_id` y est servi pour faire
le lien.

⚠️ **`distance_source` CHANGE CE QU'ON PEUT DIRE DU CHIFFRE.** `estimate` n'est
pas une mesure : ne le présentez pas comme un relevé de compteur.

### ⚠️ Filtrer : `vehicle`, `driver`, `payment` (v4.63.0)

| | |
|---|---|
| `vehicle` | une voiture **de votre parc**. Une autre est **refusée** (`403 vehicle_not_in_your_fleet`), pas rendue vide : « ce n'est pas votre voiture » se corrige, « aucune course » fait appeler le support |
| `driver` | le **compte** du chauffeur (`user_id` de `/vtc/partner/drivers`, `driver_user_id` d'une ligne) — **pas** son `driver_id`, qui est son profil |
| `payment` | `cash`, `wallet`, `online`. ⚠️ `cash` répond à la question qui amène le plus souvent sur cet écran : **ce que mes chauffeurs ont en main** |

⚠️⚠️ **CES FILTRES RESSERRENT, ILS N'ÉLARGISSENT JAMAIS** : la borne du parc
reste posée quoi qu'il arrive. Si une réponse contient une course d'une voiture
qui n'est pas la vôtre, c'est un défaut côté serveur — signalez-le.

⚠️ **UN TOTAL CALCULÉ SUR LA PAGE AFFICHÉE N'EST PAS LE TOTAL DE LA PÉRIODE.**
La liste est paginée : si vous additionnez les lignes à l'écran, **dites-le**
(« totaux des courses affichées »). Les totaux de la période sont sur
`/vtc/partner/dashboard`, et eux seuls font foi.

⚠️ **LES COURSES ANNULÉES SONT COMPTÉES À PART** (`totals.cancelled_rides`), pas
mêlées au nombre de courses. Un propriétaire qui additionne les deux croit avoir
perdu de l'argent sur des courses qui n'ont jamais eu lieu.

---

## 9. LA FINANCE — commission ou salaire, et le relevé

### 9.1 ⚠️⚠️ CE QUE CET ÉCRAN N'EST PAS

> **CET ARGENT N'EST PAS CELUI DE DIRA.** Nous **enregistrons les termes** que
> vous avez convenus avec votre chauffeur et nous **calculons un relevé**. Nous
> ne prélevons rien, nous ne versons rien.

⚠️ **ÉCRIVEZ CETTE PHRASE EN HAUT DE L'ÉCRAN, pas dans une aide.** Sans elle, un
propriétaire attend un virement, et un chauffeur attend d'être payé par Dira.
Les deux appellent le support la même semaine.

### 9.2 `PUT /vtc/partner/drivers/{driverID}/pay` — les termes convenus

```
PUT /vtc/partner/drivers/{driverID}/pay
{ "mode": "commission", "share_bp": 7000 }      ← 7000 = 70 % POUR LE CHAUFFEUR
   ou
{ "mode": "salary",     "salary_xof": 120000 }

→ 200 { "mode": "commission", "share_bp": 7000, "by": "Transports Teranga", "at": "…" }
```

⚠️⚠️ **`share_bp` EST LA PART DU CHAUFFEUR, pas celle de la société.** Les deux
conventions existent sur le terrain, et une inversion ferait verser 30 % à
quelqu'un qui devait en toucher 70 — **sans que rien ne le signale**, puisque les
deux nombres sont plausibles. Écrivez-le sur le champ : « ce que **le chauffeur**
garde ».

⚠️ **L'ARRONDI VA AU CHAUFFEUR.** Un franc perdu cent fois est perdu par la même
personne.

⚠️ **EN SALAIRE, LE SALAIRE EST DÛ À PART** — y compris une semaine sans course.
Ne l'affichez pas comme une part des recettes.

⚠️ **ON N'ÉCRIT QUE LE CHAMP DU MODE CHOISI.** Le serveur efface l'autre : une
fiche portant « 150 000 F de salaire » à côté de « 50 % de commission » aurait
laissé la prochaine lecture **deviner** lequel fait foi.

#### Les quatre refus — et chacun propose une sortie

| Code | Le message |
|---|---|
| `pay_zero_share` | « Un chauffeur qui ne garde rien n'est pas en commission : **enregistrez plutôt un salaire, avec son montant** » |
| `pay_share_range` | « La part du chauffeur doit être comprise entre **0,01 % et 100 %** » |
| `pay_salary_needs_amount` | « Un salaire doit porter son montant » |
| `pay_mode_unknown` | « La rémunération est soit une commission, soit un salaire » |

⚠️⚠️ **`pay_zero_share` EST UN CHOIX DE PRODUIT, PAS UNE VALIDATION.** « Le
chauffeur garde 0 % » veut dire qu'il travaille **sans rien toucher** ; si c'est
l'accord, il s'appelle un salaire et il porte un montant. Laisser passer ce zéro
aurait permis d'enregistrer, dans nos données, un travail non rémunéré — et de
s'en servir comme d'une preuve d'accord. **Affichez la phrase : elle dit par où
sortir.**

⚠️ `amount_out_of_range` s'applique aussi au salaire (« vérifiez le nombre de
zéros »).

### 9.3 `GET /vtc/partner/payroll` — le relevé, chauffeur par chauffeur

```
GET /vtc/partner/payroll?from=2026-10-01&to=2026-10-10&tz=Africa/Dakar

→ 200 { "items": [ {
  "driver_id":        "…",  "user_id": "…",
  "name":             "Astou Diouf",  "phone": "+221771000118",
  "plates":           [ "DK-8740-EE" ],
  "rides":            24,
  "earned_xof":       96800,     ← produit AVEC VOS VOITURES (part hors commission Dira)
  "cash_xof":         41200,     ← ⚠️ À RÉCLAMER, pas à verser : le plein tarif encaissé en espèces
  "terms":            { "mode": "commission", "share_bp": 7000 },
  "driver_share_xof": 67760,     ← sa part, selon les termes
  "fleet_share_xof":  29040,     ← la vôtre
  "salary_xof":       0,
  "debt_xof":         -935,
  "over_limit":       false
} ] }
```

⚠️⚠️ **`cash_xof` EST LE PLEIN TARIF DES COURSES PAYÉES EN ESPÈCES.** C'est ce
que le chauffeur **détient** : la commission de Dira est dedans (elle devient sa
dette), et votre part aussi. **Ne l'affichez jamais dans la même colonne que
`driver_share_xof`** — l'un est à encaisser, l'autre à verser, et les aligner
fait additionner deux choses opposées.

⚠️ **LE RELEVÉ EST BORNÉ À VOS VÉHICULES.** Un défaut corrigé : il agrégeait
**toutes** les courses du chauffeur, personnelles comprises, si bien que ses
gains propres entraient dans ce que la société lui réclame. **C'est de l'argent
qu'on demande à quelqu'un** — la borne n'est pas cosmétique.

⚠️ **AUCUNE LIGNE DU GRAND LIVRE DU CHAUFFEUR** n'est servie (ses locations de
matériel, ses bonus, ses prélèvements). N'inventez pas un « solde » : affichez
`debt_xof` sous le mot *dette envers Dira*, et rien de plus.

### 9.4 `GET /vtc/partner/earnings` — la vue courte

Même fenêtre, une ligne par chauffeur : `rides`, `earned_xof`, `debt_xof`,
`over_limit`, `plates`.

⚠️ **`earned_xof` EST LA PART HORS COMMISSION DIRA, PAS LE PRIX CLIENT.** Servir
le prix client ferait croire au propriétaire qu'on lui doit la différence.

---

## 10. LES PAPIERS DE SES VOITURES

⚠️⚠️ **CE CHAPITRE EXISTE PARCE QU'ON RÉCLAMAIT AU CHAUFFEUR LA CARTE GRISE
D'UNE VOITURE QUI N'EST PAS LA SIENNE** — et la relance le lui redisait tous les
trois jours. Les papiers d'un véhicule de société se déposent par **la société**.

```
GET /vtc/partner/compliance

→ 200 {
  "compliant": false,
  "vehicles": [ {
    "vehicle_id": "6ab371373317b63a136b7270",
    "plate":      "DK-6528-CC",
    "compliant":  false,
    "missing":    [ "insurance", "inspection" ],
    "documents":  [ {
      "id":         "…",
      "kind":       "registration",
      "vehicle_id": "6ab371373317b63a136b7270",
      "file_url":   "…",
      "state":      "valid",       ← pending | valid | expiring | expired | rejected
      "expires_at": "2027-03-31",
      "by_fleet":   true,          ← déposée par la SOCIÉTÉ (et non par le conducteur)
      "updated_at": "…"
    } ]
  } ]
}
```

```
POST /vtc/partner/compliance/documents
{
  "kind":       "insurance",
  "vehicle_id": "6ab371373317b63a136b7270",
  "file_url":   "https://files.dira.llc/…",   ← déposé au préalable
  "expires_at": "2027-03-31"
}
```

🆕 ⚠️ **`file_url` VIENT DE `POST /uploads?kind=document` (v4.73.0), ET C'EST
NOUVEAU.** Ce genre **n'existait pas** : les pièces partaient sous un genre
prévu pour des **photographies**, donc **sans PDF possible** — alors qu'une
attestation d'assurance ou une carte grise scannée est un PDF neuf fois sur dix.

### 🆕 LES FORMATS DE TÉLÉVERSEMENT — un genre, ses formats, son plafond (v4.73.0)

`POST /uploads?kind=…` n'accepte plus « des images » : **chaque genre a ses
formats et son plafond**, et le refus nomme ceux **de ce genre-là**.

| Genre | Formats acceptés | Plafond |
|---|---|---|
| `avatar` · `vehicle` · `dish` · `store` · `brand` · `banner` · `equipment` | jpeg, png, webp | **5 Mio** |
| `feed` | jpeg, png, webp — vidéos : mp4, webm, mov (⚠️ **≤ 60 s**) | **60 Mio** |
| `receipt` · `document` | jpeg, png, webp, **pdf** | **15 Mio** |
| `class` · `marker` (pictogrammes, **exploitation seule**) | png, webp, **svg** | **512 Kio** |

**Ce que cette application envoie : `document` (les pièces de la société et de ses véhicules), `vehicle` (les photos d'une voiture), `avatar` (la photo du compte).**

🆕 ⚠️ **`kind=document` MANQUAIT, ET C'EST LE CHANGEMENT QUI VOUS CONCERNE.** Les
pièces de conformité n'avaient aucun genre à elles : elles passaient par un
genre prévu pour des **photographies**, donc **sans PDF possible** — alors
qu'une attestation d'assurance est un PDF neuf fois sur dix. Envoyez désormais
toute pièce de conformité avec `?kind=document`.

⚠️ **LE PDF N'EST ACCEPTÉ QUE LÀ** — `receipt` (pièce comptable) et `document`
(pièce de conformité). Nulle part ailleurs : une photo de profil en PDF ne
s'affiche dans aucune liste.

⚠️ **LE SVG N'EST PLUS ACCEPTÉ HORS PICTOGRAMMES.** Il l'était partout, par une
règle unique écrite pour toute la plateforme. Un SVG peut porter du script, et
il n'a aucune raison d'être une photo.

### 🆕 Les trois refus de la porte d'envoi, NOMMÉS (v4.73.0)

| Code | Statut | Ce que la phrase porte | `fields` |
|---|---|---|---|
| `upload_kind_unknown` | `422` | la liste des genres acceptés | `kind` |
| `upload_format_rejected` | `422` | les formats de **ce** genre | `file` |
| `upload_too_large` | `422` | le plafond de **ce** genre, en Mio | `file` |

⚠️ **AFFICHEZ LA PHRASE SERVIE, N'ÉCRIVEZ PAS LA LISTE EN DUR** : une liste
recopiée dans l'application vieillit en silence au premier format ajouté.

> ## ⚠️⚠️ CES TROIS CODES SONT NEUFS PARCE QUE LE REFUS ÉTAIT MUET
>
> Jusqu'au 24 octobre 2026, les trois répondaient
> `{"code":"validation_failed","message":"Requête invalide"}` — **en français
> comme en anglais**. Le détail (« kind must be one of… », « accepted: jpeg,
> png, webp, pdf ») vivait dans le message de l'erreur, et **l'enveloppe
> remplace ce message par la phrase traduite du code** : tout ce qui n'y figure
> pas est perdu en route. C'était du texte mort, lisible seulement dans les
> journaux du serveur, pendant que quelqu'un au comptoir cherchait pourquoi son
> attestation d'assurance ne passait pas.
>
> **Si votre écran affiche encore « Requête invalide » sur un envoi refusé,
> c'est qu'il lit l'ancien code.**

⚠️ **`upload_too_large` COUVRE AUSSI UN CORPS MULTIPART ILLISIBLE**, et le dit
comme une taille — c'est indiscernable côté serveur. Entre « votre fichier est
trop gros » sur un corps cassé et « requête invalide » sur un fichier trop gros,
**la première se corrige**.

⚠️ **UN `kind` INCONNU EST REFUSÉ.** N'en inventez pas, et ne réutilisez pas
celui d'un autre écran parce qu'il « passe » : le plafond et les formats suivent
le genre.

---


### ⚠️ Six types, et SIX SEULEMENT

| Type | |
|---|---|
| `registration` | carte grise |
| `insurance` | assurance |
| `inspection` | contrôle technique |
| `vehicle_front` · `vehicle_rear` · `vehicle_side` | les trois photos — **trois types, pas une galerie** |

⚠️⚠️ **UNE SOCIÉTÉ NE DÉPOSE JAMAIS LA PIÈCE D'IDENTITÉ DE QUELQU'UN.** Permis,
carte d'identité, casier judiciaire, selfie : **refusés** sur cette route. Ne les
proposez pas — la confusion aurait rangé le permis d'un chauffeur dans le dossier
d'une entreprise.

⚠️ **UNE PIÈCE DE VÉHICULE EXIGE SON `vehicle_id`**, et un véhicule non membre de
la flotte est refusé (`vehicle_not_in_your_fleet`).

> ## ⚠️ LES TROIS REFUS D'UN DÉPÔT, ET LEURS CODES (v4.62.2)
>
> | Code | Statut | Ce que ça veut dire |
> |---|---|---|
> | `unknown_document_kind` | `422` | ce type n'existe pas — vérifiez l'orthographe |
> | `document_needs_vehicle` | `422` | cette pièce est celle d'un véhicule : **ajoutez `vehicle_id`** |
> | `document_refuses_vehicle` | `422` | cette pièce est celle de la personne : **retirez `vehicle_id`** |
>
> ⚠️⚠️ **ET SI VOTRE APPLICATION A DÉJÀ REÇU « Requête invalide » SUR UN SELFIE,
> UN CASIER, UN CONTRÔLE TECHNIQUE OU UNE DES TROIS PHOTOS : C'ÉTAIT NOUS.** La
> route n'acceptait que quatre types (`licence`, `id_card`, `registration`,
> `insurance`) alors que `missing` en réclamait neuf — donc des papiers demandés,
> redits tous les trois jours par la relance, et **refusés à l'envoi**. Corrigé
> en 4.62.2 : **tous les types servis dans `missing` sont acceptés.**
>
> ⚠️ Avant cette version, ces trois refus portaient `validation_failed`, donc le
> message « Requête invalide » : **un code à vous est ce qui vous permet de
> souligner le bon champ.**

⚠️ **ICI, `vehicle_id` EST TOUJOURS EXIGÉ** : une société n'a que des papiers de
voiture. `document_refuses_vehicle` ne devrait donc jamais vous arriver — et
`document_needs_vehicle` signifie que votre requête a oublié le champ.

> ## ⚠️⚠️ `missing` CONTIENT LES PIÈCES DÉPOSÉES MAIS PAS ENCORE VALIDÉES (v4.62.3)
>
> « Manquant » veut dire **« non couvert par une pièce valable »**, et une pièce
> `pending` ne couvre rien tant qu'un humain ne l'a pas regardée. Une pièce
> déposée ce matin est donc **dans `documents` ET dans `missing`**.
>
> ⚠️ **NE LES AFFICHEZ PAS L'UN APRÈS L'AUTRE.** La console le faisait : douze
> lignes pour six pièces, « en cours d'examen » puis « Absent » **pour le même
> papier**. Un propriétaire qui vient de tout déposer et lit « Absent »
> **redépose**.
>
> **La règle : « Absent » ne s'affiche que pour un `kind` dont il n'existe AUCUN
> dépôt** (`missing` moins les `documents[].kind`). Les autres sont dites par
> l'état de leur pièce.
>
> ⚠️ **ET L'EN-TÊTE D'UNE VOITURE A TROIS ÉTATS, PAS DEUX :**
>
> | | |
> |---|---|
> | `compliant: true` | **En règle** |
> | il reste un `kind` sans aucun dépôt | **Dossier incomplet** |
> | tout est déposé, rien n'est encore validé | **En cours d'examen chez Dira** |
>
> « Il n'y a plus rien à faire de ce côté » est une information : « incomplet »
> fait chercher quoi déposer encore.

### ⚠️ Les quatre cas d'une pièce, et ce que l'écran propose

| `state` | Ce que l'écran fait |
|---|---|
| `pending` | « en cours d'examen » — **aucun bouton** |
| `valid` | la date d'expiration, et rien d'autre |
| `expiring` | ⚠️ **à montrer bien visible** : c'est ce qui fait renouveler avant un contrôle de police |
| `expired` / `rejected` | **remplacer**, avec `rejected_reason` affiché mot pour mot |

⚠️ **`expiring` ET `expired` SONT CALCULÉS À LA LECTURE**, pas stockés : ils
basculent tout seuls. N'attendez pas un événement.

⚠️⚠️ **`missing` N'EST PAS LA LISTE DU CHAUFFEUR.** La plateforme sert deux
listes distinctes — ce qui manque **à la personne** (son permis) et ce qui manque
**à la société** (l'assurance d'une voiture) — et **il est interdit de les
concaténer**. Le destinataire change, pas la réalité : une voiture sans assurance
roule sans papiers dans les deux cas, mais ce n'est pas la même personne qui doit
agir.

⚠️ **LE DÉPÔT PAR LE CONDUCTEUR RESTE ACCEPTÉ** : les papiers sont souvent dans
la boîte à gants. Une pièce sans `by_fleet` n'est pas une anomalie — affichez-la
comme les autres, sans proposer de la remplacer si elle est `valid`.

---

## 11. LE MATÉRIEL DE LA SOCIÉTÉ — **SOCLE** (sans `/vtc`)

Gilets, casques, téléphones : une société en prend pour ses chauffeurs.

```
GET /partner/equipment/catalogue      (socle)

→ 200 { "items": [ {
  "id":                  "6ab2533ebdafb23b6d19f1b1",
  "kind":                "phone",
  "name":                "Téléphone portable Dira",
  "description":         "Smartphone Android avec l'application installée…",
  "photos":              [ ],
  "sale_price_xof":      0,          ← 0 ou absent = CETTE FORMULE N'EST PAS PROPOSÉE
  "rental_daily_xof":    500,
  "rental_weekly_xof":   3000,
  "rental_monthly_xof":  10000,
  "deposit_xof":         15000,
  "stock":               20,
  "active":              true
} ] }
```

⚠️⚠️ **LES CHAMPS SONT `sale_price_xof` ET `rental_{daily,weekly,monthly}_xof`.**
Il n'y a **ni `rent_xof`, ni `modes[]`**. Une console les a inventés : le
catalogue s'est affiché **sans aucun prix, sans aucune erreur** — un champ absent
vaut `undefined`. **Un champ qu'on invente est un champ toujours vide, et c'est
le pire des défauts : rien ne crie.**

⚠️ **NE PROPOSEZ QUE LES FORMULES QUI ONT UN PRIX.** Offrir « Achat » sur un
article dont `sale_price_xof` vaut 0 fait cliquer pour lire « cette formule n'est
pas proposée ». Un pays peut ne vendre qu'une formule.

⚠️ **AFFICHEZ LE LOYER MENSUEL** (repli : semaine, puis jour) : on n'équipe pas
une flotte à la journée.

```
POST /partner/equipment/requests
{ "item_id": "6ab2533ebdafb23b6d19f1b1", "quantity": 3, "mode": "rental", "note": "…" }

→ 201 { "id": "…", "item_name": "Téléphone portable Dira", "quantity": 3,
         "mode": "rental", "status": "requested",
         "price_xof": 9000, "deposit_xof": 45000,
         "fleet_name": "Transports Agoè" }
```

⚠️ **`mode` VAUT `sale`, `rental` OU `loan` — PAS `rent`.** Les deux mots sont
plausibles, et celui qu'on invente échoue à l'envoi (`422` sur le champ `mode`).

```
GET /partner/equipment       → { "items": [ …contrats de la société… ] }
```

Statuts : `requested` → `draft` → `accepted` → `active` (remis) → `returned` /
`completed`, ou `cancelled` / `defaulted`.

⚠️⚠️ **UNE SOCIÉTÉ RÈGLE SUR FACTURE — il n'y a pas de grand livre de flotte.**
Tous les canaux de recouvrement sont **fermés** sur un contrat de société : rien
n'est prélevé sur des gains, rien sur un portefeuille, et **aucun blocage pour
retard**. N'affichez donc ni échéancier de prélèvement, ni « bloqué » : ces
champs existent dans la réponse (structure commune avec les contrats d'une
personne) et valent `false`/`0` ici.

⚠️ **LE PORTEUR N'EST PAS UNE PERSONNE** : `user_id` est **vide** sur un contrat
de société, et `fleet_name` porte le nom. N'affichez pas « attribué à — ».

---

## 12. LE PERSONNEL — **SOCLE** (sans `/vtc`)

⚠️⚠️ **POURQUOI CET ÉCRAN EXISTE : un seul compte par société = un mot de passe
partagé**, et plus rien ne dit **qui** a posé la limite qui a coupé quelqu'un.

```
GET /partner/members           (socle — ⚠️ lisible par TOUT le personnel, viewer compris)

→ 200 { "items": [ {
  "user_id":  "6ac924d6b9c3193bbf5dfbfa",
  "role":     "owner",        ← owner | manager | viewer
  "name":     "Kodjo Mensah",
  "phone":    "+221771999001",
  "email":    "kodjo@teranga.sn",
  "owner":    true,
  "added_by": "…",  "added_at": "…"
} ] }

POST /partner/members          (⚠️ le PROPRIÉTAIRE seul)
{ "name": "Ndèye Fall", "phone": "+221777000001", "email": "compta.sn@dira.llc",
  "role": "viewer", "password": "…" }
→ 201 { …le membre… }

DELETE /partner/members/{userID}    (⚠️ le PROPRIÉTAIRE seul)
→ 204
```

⚠️ **LA LISTE EST LISIBLE PAR TOUS, Y COMPRIS UN `viewer`** — et ce n'est pas un
pouvoir, c'est le contraire : savoir qui a accès est ce qui permet de signaler un
compte qui ne devrait plus être là.

⚠️ **LE MOT DE PASSE EST EXIGÉ, et le propriétaire le transmet de vive voix.** Un
tirage du serveur affiché masqué finit **recopié de travers**.

⚠️⚠️ **« DÉTACHER » N'EST PAS « SUPPRIMER ».** Le compte n'est pas effacé : c'est
peut-être le compte personnel de la personne, et une société n'a pas à pouvoir
effacer quelqu'un qu'elle congédie. Écrivez-le dans la confirmation.

⚠️ **ON NE RETIRE PAS LE PROPRIÉTAIRE — pas même lui-même.** N'affichez pas le
bouton sur sa ligne (`owner: true`).

### Les refus de cet écran

| Code | Le message |
|---|---|
| `fleet_not_owner` | « Seul le signataire du contrat peut gérer le personnel de la flotte » |
| `fleet_role_unknown` | « Le rôle doit être « gérant » ou « lecteur » » |
| `fleet_already_member` | « Ce compte fait déjà partie du personnel de cette flotte » |
| `fleet_member_not_found` | le membre n'est pas dans cette flotte |
| `phone_belongs_to_another_role` | « Ce numéro appartient déjà à un compte Dira qui n'est pas un partenaire : **utilisez un autre numéro** » |

⚠️⚠️ **`phone_belongs_to_another_role` EST LE PIÈGE LE PLUS FRÉQUENT, et il a une
cause banale : le gérant d'une flotte est très souvent DÉJÀ un passager Dira.**
Un compte = un rôle = une application. Le refus **nomme le geste** : un autre
numéro. Sans cette phrase, l'écran afficherait « accès ouvert » pendant que la
connexion répondrait `403 wrong_app`.

---

## 13. LA FENÊTRE DE DATES — `from`, `to`, `tz`

Quatre routes en dépendent : `dashboard`, `rides`, `payroll`, `earnings`.

```
?from=2026-10-01&to=2026-10-10&tz=Africa/Dakar
```

| | |
|---|---|
| `from`, `to` | `YYYY-MM-DD`, **tous deux inclusifs**. Absents → **les sept derniers jours** |
| `tz` | nom de fuseau IANA. Absent → `Africa/Lome` |

⚠️⚠️ **`to` EST INCLUSIF.** Qui demande « du 1er au 31 » veut le 31. Le serveur
s'en charge ; ne retirez pas un jour « pour compenser », et ne recopiez pas la
borne haute dans un filtre exclusif côté interface.

⚠️⚠️ **ENVOYEZ LE FUSEAU DU NAVIGATEUR.** Un serveur en UTC ne sait pas à quelle
heure une journée de Lomé commence, et une période décalée d'une heure **fait
manquer les courses du soir** — celles qui font le chiffre. La réponse porte la
fenêtre **retenue** (`from`, `to`) : affichez-la.

⚠️ **LA RÉPONSE EST LA SOURCE DE VÉRITÉ DE LA PÉRIODE AFFICHÉE.** Mettez
l'étiquette (« ce mois ») à jour **depuis** `from`/`to`, jamais depuis ce que
l'interface croit avoir demandé.

### Les trois refus de la fenêtre

| Code | Le message |
|---|---|
| `window_too_long` | « Une période ne peut pas dépasser **93 jours** : resserrez les dates, un mois par exemple » |
| `window_backwards` | « La date de début est après la date de fin : **inversez-les** » |
| `window_unknown_tz` | « Votre appareil a envoyé un fuseau horaire que ce serveur ne connaît pas : **vérifiez son réglage de date et d'heure** » |

⚠️⚠️ **LES TROIS SONT ATTEIGNABLES DÈS QUE VOUS OFFREZ DES SÉLECTEURS DE DATE
LIBRES** — et c'est le cas de Finance, Gains et Courses. Affichez le `message` :
il porte la limite **et** le geste. (Ces refus ont longtemps sorti en « Requête
invalide », faute d'un code à eux ; si vous revoyez cette phrase ici, c'est une
régression à signaler.)

⚠️ **LA BORNE DE 93 JOURS EST UNE PROTECTION DE LA BASE**, pas une politique de
confidentialité : l'agrégation balaie les courses du parc. « Depuis le début »
sur cinquante voitures est la requête qu'on lance une fois par curiosité et qui
met la base à genoux pour tout le monde.

### ⚠️ Les paramètres acceptés, route par route

| Route | Paramètres |
|---|---|
| `GET /vtc/partner/live` | *aucun* |
| `GET /vtc/partner/dashboard` | `from` `to` `tz` |
| `GET /vtc/partner/vehicles` | `limit` `cursor` |
| `GET /vtc/partner/drivers` | *aucun* |
| `GET /vtc/partner/rides` | `from` `to` `tz` `limit` `cursor` `vehicle` `driver` `payment` |
| `GET /vtc/partner/payroll` | `from` `to` `tz` |
| `GET /vtc/partner/earnings` | `from` `to` `tz` |
| `GET /vtc/partner/compliance` | *aucun* |
| `GET /vtc/partner/ledger` | **`driver` (obligatoire)** `from` `to` `tz` |
| `GET /vtc/tickets` | `status` `category` `limit` `cursor` |
| `GET /partner/me` · `/partner/members` · `/partner/equipment` · `/partner/equipment/catalogue` | *aucun* |

⚠️ **UN PARAMÈTRE QU'ON INVENTE EST UN PARAMÈTRE TOUJOURS IGNORÉ** — la réponse
reste `200`, et le filtre ne filtre rien sans que personne le voie. Tout ce qui
n'est pas dans cette table se filtre **dans la page**.

---

## 13 bis. LE GRAND LIVRE — ce qu'un chauffeur doit, ce qu'on lui doit (v4.63.0)

```
GET  /vtc/partner/ledger?driver=<compte>&from=&to=&tz=
POST /vtc/partner/ledger                 { driver_user_id, kind, amount_xof, reason?, at? }
POST /vtc/partner/ledger/{id}/reverse    { reason? }
```

> ## ⚠️⚠️ DIRA N'ENCAISSE ET NE VERSE RIEN ICI
>
> Ce livre **enregistre** ce que la société déclare avoir versé à son chauffeur
> et encaissé de lui. L'argent ne passe pas par la plateforme : elle tient le
> compte, pas la caisse.
>
> **Écrivez-le en haut de l'écran**, pas dans une aide. Sans cette phrase, un
> propriétaire attend un virement et un chauffeur attend d'être payé par Dira —
> les deux appellent le support la même semaine.

### ⚠️⚠️ UN LIVRE PAR CHAUFFEUR, JAMAIS UN SOLDE GLOBAL

`driver` est **obligatoire**, et ce n'est pas une limitation : une société a
autant de comptes que de conducteurs. « Mon chauffeur me doit 42 000 » se
règle ; « ma flotte est à +180 000 » ne se règle avec personne — c'est
l'addition de créances sur des gens différents, et le nombre a l'air de vouloir
dire quelque chose. Sans `driver`, la route refuse (`ledger_driver_required`).

⚠️ **C'est le COMPTE (`user_id` de `/vtc/partner/drivers`), pas le profil
(`driver_id`).** Les deux sortent de la même liste et ont la même forme : se
tromper rend un livre vide, qui se lit « ce chauffeur ne doit rien ».

### La réponse

```json
{
  "driver_user_id": "…", "driver_name": "Astou Diouf",
  "from": "2026-09-11", "to": "2026-10-10",
  "opening_xof": 12500,
  "closing_xof": -7000,
  "items": [
    { "at": "2026-09-28T14:39:02Z", "kind": "ride", "amount_xof": 2460,
      "balance_xof": 14960, "ride_id": "…", "plate": "DK-6528-CC", "payment": "cash" },
    { "id": "…", "at": "2026-10-02T12:00:00Z", "kind": "payout", "amount_xof": -15000,
      "balance_xof": -40, "reason": "Part de la semaine", "recorded_by": "Kodjo Mensah" },
    { "id": "…", "at": "2026-10-05T12:00:00Z", "kind": "reversal", "amount_xof": 15000,
      "balance_xof": 14960, "reverses_id": "…", "reason": "Montant erroné" }
  ]
}
```

### ⚠️⚠️ LE SENS DU SIGNE, UNE FOIS POUR TOUTES

```
POSITIF → le chauffeur DOIT à la société
NÉGATIF → la société DOIT au chauffeur
```

⚠️ **ET LE SIGNE SE DIT EN MOTS, JAMAIS EN « − ».** « −11 000 » sous le mot
« solde » ne dit pas **qui doit à qui**, et c'est la seule chose qu'on vient
lire. Écrivez « il vous doit » / « vous lui devez ».

| `kind` | Qui l'écrit | Effet sur le solde |
|---|---|---|
| `ride` | **la plateforme**, au règlement de la course | **+** la part de la société |
| `payout` | la société | **−** elle a donné de l'argent |
| `cash_in` | la société | **−** il a rendu les espèces qu'il détenait |
| `adjustment` | la société | **+ ou −**, le seul genre qui va dans les deux sens |
| `reversal` | la société, par `/reverse` | l'inverse exact de l'écriture annulée |

⚠️⚠️ **UN VERSEMENT ET UNE REMISE D'ESPÈCES VONT DANS LE MÊME SENS**, et c'est
contre-intuitif assez longtemps pour devoir être écrit à l'écran : le premier
réduit ce qu'il doit parce qu'on lui a donné de l'argent, le second parce qu'il
a rendu celui qu'il détenait.

⚠️ **LE MONTANT S'ENVOIE POSITIF ET LE SENS VIENT DU GENRE.** Un `-15000` envoyé
« par prudence » sur un `payout` est ramené à `-15000` par le serveur, jamais
inversé. Seule une correction porte son signe, et l'écran doit alors le demander
explicitement (« il me doit » / « je lui dois »).

### ⚠️ ON NE MODIFIE NI NE SUPPRIME JAMAIS

Il n'y a ni `PATCH` ni `DELETE`, et il n'y en aura pas : **un livre qu'on peut
récrire ne prouve rien**, et c'est précisément quand il y a litige qu'on vient
le lire. La correction est une **écriture de plus** (`/reverse`), qui dit ce
qu'elle annule.

- l'écriture annulée porte `reversed: true` et `reversed_by_id` ;
- **gardez-la visible, barrée** : la faire disparaître laisserait un trou dans
  un livre dont tout l'intérêt est d'être continu ;
- une annulation ne s'annule pas (`ledger_not_reversible`), et deux fois la même
  écriture non plus (`ledger_already_reversed`).

### ⚠️ LE REPORT À NOUVEAU — `opening_xof`

**Affichez-le à côté du solde.** Montrer les écritures d'octobre sans dire ce
qui restait au 30 septembre donne un solde de fin de mois que personne ne peut
rapprocher.

### ⚠️ LA DATE EST CELLE DE L'ÉVÉNEMENT

`at` est facultatif et vaut « maintenant ». Un versement fait lundi et noté
jeudi appartient à **lundi** : sans ce champ, le livre raconte l'ordre dans
lequel on a tapé, pas celui dans lequel les choses sont arrivées.

### Les refus

| Code | |
|---|---|
| `ledger_driver_required` | « Le grand livre se tient par chauffeur : dites lequel » |
| `ledger_kind_unknown` | une ligne de **course** ne s'écrit pas à la main — ce serait fabriquer une dette qu'aucune course ne justifie |
| `ledger_amount_zero` | « Une écriture de zéro n'enregistre rien » |
| `ledger_reason_required` | une **correction** doit dire pourquoi ; un versement se comprend tout seul |
| `driver_not_in_your_fleet` | `403` — on n'écrit pas sur le compte de quelqu'un qui ne conduit aucune de vos voitures |
| `partner_read_only` | un lecteur lit le livre, il n'y écrit pas |

### ⚠️ CE QUE LE LIVRE NE CONTIENT PAS, ET QU'IL FAUT DIRE

**Les courses d'avant la mise en place du livre n'y figurent pas.** Leur part
n'a pas été figée au règlement, et la recalculer aux termes d'aujourd'hui
donnerait une écriture dont le montant change le jour où les termes changent.
Un livre qui tait son point de départ se lit comme complet.

(Le **relevé** de Finance, lui, sait recalculer — et dit combien de courses il a
dû recalculer, via `recomputed_rides`.)

---

## 14. LE SUPPORT — ses fils, et SEULEMENT les siens

```
POST /vtc/tickets
{ "category": "vehicle", "message": "Les pièces de deux de nos voitures arrivent à échéance…",
  "priority": "normal" }
→ 201 { "id": "…", "reference": "…", "category": "vehicle", "status": "open", … }

GET  /vtc/tickets?status=open&category=vehicle&limit=20&cursor=<id>
GET  /vtc/tickets/{id}
POST /vtc/tickets/{id}/messages        { "body": "…" }
```

Catégories utiles ici : `vehicle` (une voiture immobilisée, des papiers
refusés), `payment` (un montant), `account` (l'accès à la console), `other`.
Statuts : `open`, `in_progress`, `waiting`, `resolved`, `closed`.

⚠️⚠️ **LE PARTENAIRE NE VOIT QUE SES PROPRES FILS — ET IL NE FAUT JAMAIS OUVRIR
ÇA.** Le guichet borne au **participant** pour tout compte non administrateur. Un
chauffeur doit pouvoir écrire « mon propriétaire prend 90 % et ne répare pas les
freins » sans que son propriétaire le lise.

⚠️ **ÉCRIVEZ-LE EN TÊTE D'ÉCRAN** (« Vous voyez vos échanges avec Dira. Les
demandes de vos chauffeurs leur appartiennent. ») : sans cette phrase, l'écran
se lit comme une liste incomplète, et quelqu'un finit par demander « l'accès aux
tickets de mes chauffeurs ».

⚠️ Le champ du message s'appelle **`message`** à la création et **`body`** dans
une réponse au fil. Deux routes, deux noms : c'est le guichet commun de la
plateforme.

---

## 15. ⚠️ Ce qu'un écran ne doit PAS faire — récapitulatif de recette

À essayer avant de considérer une interface partenaire prête.

**La porte**
1. Se connecter **sans** `app: "partner"` → vérifier que l'interface ne le fait jamais.
2. Un compte chauffeur sur cette console → `403 wrong_app`, et la phrase du serveur s'affiche.
3. Un compte partenaire **sans flotte** → `partner_no_fleet` en **écran plein**.
4. Un contrat **suspendu** → `partner_fleet_suspended`, **avec** « vos chauffeurs continuent de travailler ».

**Les rôles**
5. Ouvrir la console en `viewer` → **aucun bouton d'écriture dessiné**, et une phrase qui dit à qui demander.
6. Forcer un `PUT` en `viewer` → `partner_read_only` affiché mot pour mot.
7. Un `manager` sur l'écran du personnel → pas de bouton, `fleet_not_owner` si forcé.
8. Supprimer `role` de la réponse (bouchon) → l'interface doit **couper** les boutons, pas les ouvrir.

**La carte**
9. Un véhicule **sans `geo`** → compté dans un bandeau, **pas posé à `[0,0]`**.
10. Un véhicule `on_ride` dont `last_seen_at` est vieux → **reste « En course »**.
11. Zoomer, attendre deux battements → **la vue ne revient pas au centre**.
12. `stale` et `offline` côte à côte → **deux mots différents**, et seul le premier porte une alerte.

**L'argent**
13. `totals` absent → « — » partout, **jamais 0**.
14. `debt_xof` négatif → « 3 396 F d'avance », pas « -3 396 F de dette ».
15. `cash_xof` → libellé « à réclamer », dans une colonne séparée de la part à verser.
16. Commission à 0 % → la phrase `pay_zero_share` s'affiche, qui **propose le salaire**.
17. Part à 150 % → `pay_share_range` (« entre 0,01 % et 100 % »), pas « Requête invalide ».
18. Salaire avec trois zéros de trop → `amount_out_of_range` (« vérifiez le nombre de zéros »).

**Les dates**
19. Demander du 1er janvier au 31 décembre → `window_too_long`, et la phrase propose de resserrer.
20. Inverser les dates → `window_backwards`.
21. Comparer l'étiquette affichée aux `from`/`to` **de la réponse**.

**Les gestes qui touchent une personne**
22. Rattacher une plaque inconnue → la phrase renvoie vers **l'application Dira Chauffeur**.
23. Rattacher avec le numéro d'un client → `driver_phone_unknown`, même renvoi.
24. Reprendre la voiture **active** d'un chauffeur → la confirmation annonce « hors ligne » **et** « il reste chauffeur Dira ».
25. Reprendre une voiture **non active** → la confirmation **n'annonce pas** « hors ligne ».
26. Poser une limite de dette → la confirmation nomme la conséquence **et** dit que le nom de la société sera inscrit.
27. Poser `cap_xof: 0` → l'écran dit « limite retirée — **le plafond du pays s'applique toujours** ».

**Le matériel et les papiers**
28. Un article sans `sale_price_xof` → la formule « Achat » **n'est pas proposée**.
29. Demander un `mode: "rent"` → vérifier que l'interface envoie bien `rental`.
30. Déposer un permis sur la route de la société → refusé ; vérifier que l'interface ne le propose pas.
31. Une pièce `rejected` → `rejected_reason` affiché mot pour mot, avec le bouton « remplacer ».
32. `missing` et la liste du chauffeur → **jamais concaténées**.

---

## 16. ⚠️ Une note sur les exemples de ce document

**Les chemins portent `{vehicleID}`, `{driverID}`, `{id}` — jamais un
identifiant réel**, même dans un bloc d'exemple qui, par ailleurs, montre des
valeurs vraies.

⚠️ **Deux raisons, et la seconde est la plus sérieuse.** La première : un
identifiant recopié d'une spec appelle **la voiture de quelqu'un d'autre**. La
seconde : les routes citées ici sont **confrontées automatiquement aux routes
réellement montées** par les services — c'est ce qui empêche une équipe de
câbler un chemin ou un verbe qui n'existe pas. Un identifiant littéral dans le
chemin **sort la citation du contrôle** : elle ne ressemble plus à la route
montée, et plus personne ne vérifie qu'elle existe.

Les **corps** et les **réponses**, eux, portent des valeurs réalistes : ils ne
sont pas des chemins, et un exemple avec `"id": "…"` ne dit pas de quoi la
réponse a l'air.

---

## 17. Journal

### 4.64.0 — 11 octobre 2026

📱 **CE CONTRAT SERT AUSSI UNE APPLICATION MOBILE** (§1 ter). Les routes, les
refus et les règles de périmètre ne changent pas ; ce qui s'ajoute est **ce
qu'un téléphone demande** et que la console web ignorait.

🆕 **La connexion déclare son appareil** : `platform`, `device_id`,
`device_name`. ⚠️ **Trois appareils au plus, le plus ancien est chassé en
silence** — un `401` veut donc dire « ma session a été fermée ailleurs », pas
« panne ».

🆕 **`maps` et `app_lock` étaient DÉJÀ servis** à tous les comptes, partenaire
compris : la console n'en faisait rien, une application, si. ⚠️ **Aucune clé de
carte n'est servie, nulle part** — `basemap` dit seulement lequel des deux fonds
ce pays a choisi.

🆕 **`POST /me/password`** — changer son mot de passe. ⚠️ **Cette route
n'existait pas**, et c'était un trou ordinaire : les accès sont ouverts par
quelqu'un d'autre, qui en connaissait donc le mot de passe pour toujours.

> ⚠️⚠️ **ET UN AVERTISSEMENT QUI VOUS ÉVITERA UNE JOURNÉE** : aujourd'hui,
> **rien ne pousse de notification vers un propriétaire de flotte**. Vérifié
> dans le code : `vehicle_assigned` et `vehicle_taken_back` partent vers **le
> chauffeur**. La boîte d'un partenaire est vide, et ce n'est ni un bug, ni un
> jeton, ni un filtre. **Ne cherchez pas** — et n'en faites pas un écran vide
> qui a l'air cassé. Les alertes se lisent dans `GET /vtc/partner/dashboard`.

⚠️ **La carte ne se garde pas hors ligne**, et son battement **s'arrête en
arrière-plan** : une position vieille de dix minutes fait appeler un chauffeur
en croyant savoir où il est, et six appels par minute en fond vident une
batterie sans que personne ne regarde.

⚠️ **On ne supprime pas son compte depuis cette application** — l'accès vient
d'un contrat, et un propriétaire qui l'effacerait laisserait sa flotte sans
signataire. La page doit le **dire**, pas se taire.

### 4.63.0 — 11 octobre 2026

🆕 **LE GRAND LIVRE D'UNE SOCIÉTÉ AVEC SES CHAUFFEURS** (§13 bis) — et les
**filtres** des courses (§8).

⚠️ **Dira n'encaisse et ne verse rien** : le livre enregistre ce que la société
déclare avoir versé et encaissé. La phrase va **en haut de l'écran**.

⚠️ **Un livre par chauffeur**, jamais un solde global : `driver` est obligatoire.
« Mon chauffeur me doit 42 000 » se règle ; un total toutes personnes confondues
ne se règle avec personne.

⚠️ **Le signe se dit en mots.** « −11 000 » sous « solde » ne dit pas qui doit à
qui — et c'est la seule chose qu'on vient lire. Un **versement** et une **remise
d'espèces** vont dans le **même sens**, ce qui est assez contre-intuitif pour
devoir être écrit à l'écran.

⚠️ **On ne modifie ni ne supprime jamais** : ni `PATCH`, ni `DELETE`. La
correction est une écriture de plus, et l'écriture annulée **reste visible**.

⚠️ **Le livre ne contient pas les courses d'avant sa mise en place** — leur part
n'a pas été figée, et la recalculer donnerait une écriture dont le montant
change. **Dites-le** : un livre qui tait son point de départ se lit comme
complet.

**Au passage**, le relevé de Finance porte `recomputed_rides` : les courses
qu'il a dû partager aux termes **d'aujourd'hui**, faute de part figée. Avant
cela, changer un pourcentage réévaluait le mois écoulé — y compris les semaines
déjà payées.

---

### 4.62.3 — 10 octobre 2026

🔧 **DEUX CORRECTIONS DE CE DOCUMENT, trouvées en vérifiant le correctif
précédent sur staging** — en déposant les six pièces d'une voiture pour de vrai :

- **`missing` contient les pièces déposées mais pas encore validées**, et la
  spec ne le disait pas. L'écran affichait donc douze lignes pour six pièces,
  « en cours d'examen » puis « Absent » pour le même papier. La règle
  d'affichage, et le troisième état de l'en-tête d'une voiture, sont maintenant
  en §10.
- **`papers_missing` était annoncé parmi les alertes de l'accueil et n'est levé
  par personne** : le tableau de bord ne lit pas la conformité. ⚠️ Une spec qui
  promet une alerte qui n'arrive jamais fait remplacer un écran par elle.

---

### 4.62.2 — 10 octobre 2026

🔧 **LE DÉPÔT D'UN PAPIER DE SOCIÉTÉ NE MARCHAIT PAS, et ce document le promettait
dès sa première ligne.** Trouvé en rédigeant §10, en essayant de déposer les six
pièces pour de vrai. Trois défauts sur la même requête :

- **quatre des six types étaient refusés** à l'envoi (`inspection` et les trois
  photos) : la route n'en acceptait que quatre sur les neuf que `missing`
  réclame ;
- **la marque de société n'était écrite nulle part**, si bien qu'une pièce
  acceptée **n'apparaissait jamais** dans l'écran de la société — un dépôt
  invisible, à refaire ;
- **chaque dépôt rendait `500`** — sur un papier pourtant déjà enregistré. Un
  `500` qui mentait, donc un propriétaire qui recommençait.

Rien à changer dans votre code : §10 décrivait le comportement attendu, et c'est
le serveur qui a été corrigé. **Les trois refus du dépôt portent désormais leur
propre code** (tableau en §10) — avant, ils sortaient tous en « Requête
invalide ».

---

### 4.62.1 — 10 octobre 2026

✏️ **Correction de rédaction.** Trois chemins de ce document portaient un
identifiant réel au lieu de leur paramètre. Ce n'est pas une coquille : les
routes citées ici sont confrontées automatiquement aux routes réellement montées
par les services, et **un identifiant littéral sort la citation de ce contrôle**.
La règle, avec sa raison, est en §16.

---

### 4.62.0 — 10 octobre 2026

🆕 **PREMIÈRE PUBLICATION.** La console du propriétaire de flotte est en
service ; ce document est son contrat. Elle couvre dix écrans — accueil (carte en
direct), parc, conducteurs, courses, finance, gains, papiers, matériel,
personnel, support — sur **deux bases d'URL** et **sans aucun socket**.

Ce qui vaut d'être lu avant de câbler quoi que ce soit :

- **§2 — ce que cette surface ne sert PAS**, et pourquoi. Treize lignes qui sont
  le contrat, pas une liste de manques.
- **§4 — trois rôles, et le défaut est « ne peut pas »** : un `role` absent coupe
  le pouvoir.
- **§5 — quatre états de véhicule** : « plus vu » n'est pas « hors ligne », et une
  voiture en course le reste même si son téléphone s'est tu.
- **§6 — rattacher se fait par PLAQUE + TÉLÉPHONE**, et le geste a trois effets
  de bord qui doivent figurer dans la confirmation.
- **§9 — cet argent n'est pas celui de Dira** : on enregistre des termes, on ne
  verse rien. Et `cash_xof` est à **réclamer**, pas à verser.
- **§13 — la fenêtre de dates** et ses trois refus, atteignables dès qu'on offre
  un sélecteur libre.
- **§15 — trente-deux points de recette.**
