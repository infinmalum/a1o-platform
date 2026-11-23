<div align="center">
  <img src="docs/logo.png" alt="Logo d’A1O Platform" width="120" />

  # A1O Platform

  **Une passerelle IA compatible avec les API OpenAI, Anthropic et Gemini.**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · Français · [Русский](README.ru.md) · [Español](README.es.md)
</div>

---

A1O Platform est une passerelle IA maintenue de manière indépendante, issue de [One API](https://github.com/songquanpeng/one-api). Une seule instance donne accès à plusieurs fournisseurs de modèles et prend en charge les formats natifs OpenAI Chat Completions, OpenAI Responses, Anthropic Messages et Gemini generateContent. Studio est destiné aux utilisateurs. La console d’administration gère les canaux, les utilisateurs, les jetons, les quotas et les journaux d’utilisation. One API a été créé par JustSong et ses contributeurs.

> [!NOTE]
> A1O Platform dispose de ses propres versions et canaux d’assistance. Les versions, images Docker, sites de démonstration et canaux d’assistance de One API relèvent du projet d’origine.

## Fonctionnalités principales

- 🔌 **Quatre formats d’API natifs** — OpenAI Chat Completions, OpenAI Responses (avec exécutions en arrière-plan consultables et annulables), Anthropic Messages et Gemini `generateContent`. Le même jeton donne accès aux API d’embeddings, d’images, d’audio et de modération.
- 🌉 **Conversion entre protocoles** — la passerelle convertit les requêtes et les réponses, y compris en streaming. Les clients OpenAI accèdent aux canaux Anthropic, Gemini ou Responses. Les clients Anthropic accèdent aux canaux OpenAI Chat ou Gemini.
- 🔀 **Routage des requêtes** — répartition de la charge, correspondances entre noms de modèles, coefficients par groupe, comptes ChatGPT par abonnement connectés via OAuth, priorité configurable aux abonnements et nouvelles tentatives avant le premier octet envoyé au client.
- 🤖 **Moteur conversationnel intégré** — Chat et Work partagent la boucle d’outils de [Lapua](https://github.com/infinmalum/Lapua), avec sessions persistantes, exécution de code Python et Node en environnement isolé (rlimits et isolation réseau) et serveurs d’outils MCP (Model Context Protocol).
- 🧮 **Quotas et facturation** — quotas par jeton, journaux d’utilisation, codes de recharge, recharges en ligne et calcul du coût de chaque requête selon les tarifs configurés.
- 🖥️ **Interfaces Web** — Studio, à l’adresse `/`, propose Chat et son Playground multiprotocole, les tâches Work, un catalogue d’agents, des bibliothèques de documents privées et des outils d’image, de vidéo et d’audio. La console `/admin/` gère les canaux, les comptes fournisseurs, les utilisateurs, les jetons, les journaux et les paramètres.
- 💾 **Stockage flexible** — SQLite par défaut, MySQL ou PostgreSQL pour le stockage partagé et Redis pour le cache et la synchronisation. Déploiement multinœud possible avec un nœud principal et des nœuds secondaires.
- 🛠️ **Technologies** — serveur Go et interfaces TypeScript avec Vite. Chaque compilation des interfaces commence par une vérification des types.

Les modèles et fonctionnalités disponibles dépendent du type de canal. Vérifiez sa configuration et testez le modèle avant la mise en production.

## Utiliser Studio

Dans la console d’administration, configurez un canal et activez les modèles pour votre groupe. Connectez-vous à Studio et sélectionnez un modèle. Le Playground et les outils multimédias utilisent votre session Studio. Les appels aux modèles sont déduits du quota du compte.

| Écran | Utilisation |
| --- | --- |
| **Chat → Playground** | Envoyez des requêtes natives OpenAI Chat Completions, OpenAI Responses, Anthropic Messages ou Gemini. Consultez le JSON brut ou les événements du flux, comparez deux modèles avec la même requête et copiez un exemple cURL. |
| **Espace de travail → Bibliothèques de connaissances** | Importez des fichiers TXT ou Markdown privés en UTF-8. Sélectionnez une bibliothèque dans Chat et ouvrez les passages cités. La recherche par mots-clés utilise la base de données existante, sans embeddings ni base vectorielle. Limites de 256 Kio par fichier, 64 fichiers et 2 Mio au total par bibliothèque. |
| **Génération d’images → Image / Vidéo** | Générez des images avec les modèles compatibles, dont Seedream sur Volcengine Ark et Gemini. Prévisualisez ou téléchargez les originaux et joignez-les à Chat ou Work. Seedream et les modèles d’image Gemini compatibles acceptent des références PNG, JPEG et WebP. Seedance sur Ark génère des vidéos de 5 à 8 s en 720p à partir de texte, avec une première image facultative. Gemini `gemini-omni-1.2-flash` génère des vidéos de 3 à 8 s en 360p à partir de texte. |
| **Audio** | Utilisez un modèle compatible pour la synthèse vocale, la transcription ou la traduction de la parole vers l’anglais. Écoutez et téléchargez l’audio ou exportez la transcription. |
| **Compte → Recharge** | Utilisez un code ou achetez un forfait de quota à l’unité via Stripe Checkout ou NOWPayments, selon les moyens activés sur le site. Pour le paiement en cryptomonnaie, choisissez un couple devise et réseau activé par l’exploitant du site. |

L’exploitant définit les tarifs des modèles Ark avec `ARK_MEDIA_PRICES` et les forfaits à prix fixe en dollars américains avec `PAYMENT_PACKAGES`. Pour accepter les cryptomonnaies, configurez les identifiants du prestataire et activez une ou plusieurs options avec `NOWPAYMENTS_PAY_CURRENCIES`. Par exemple, `usdtsol,usdcmatic,btc` active l’USDT sur Solana, l’USDC sur Polygon et le BTC sur Bitcoin. Le [guide des médias et des paiements](docs/media-and-payments.md) détaille la configuration, les montants minimums et les couples pris en charge.

Studio propose dix langues d’interface dans son en-tête, avec écriture de droite à gauche pour l’arabe et le persan. La console d’administration dispose de son propre choix entre le chinois et l’anglais.

Une instance unique peut utiliser SQLite sans Redis ni base vectorielle. Conservez la base de données et le volume de données lors d’une sauvegarde ou d’une migration. Les pièces jointes des conversations et les images et vidéos générées sont enregistrées sur disque.

## Démarrage rapide avec Docker

Construisez l’image depuis la racine du dépôt.

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
docker build -t a1o-platform:local .
docker run -d --name a1o-platform --restart unless-stopped -p 3000:3000 -v "$(pwd)/data:/data" a1o-platform:local
```

Au premier démarrage, ouvrez <http://localhost:3000/admin/setup> pour créer le compte root. Pour une installation sans intervention, fournissez `INITIAL_ROOT_PASSWORD` au conteneur avant son premier démarrage. Une fois l’initialisation terminée, ouvrez Studio à l’adresse <http://localhost:3000/> ou la console à l’adresse <http://localhost:3000/admin/>. Conservez le volume de données lors du remplacement du conteneur. Pour un déploiement public, configurez HTTPS et un `SESSION_SECRET` persistant.

Le fichier `docker-compose.yml` compile l’image localement et lui attribue le tag `a1o-platform:local`. La base et le répertoire de données de l’application utilisent le nom `a1o-platform`. Si votre ancien fichier Compose utilise les noms hérités de `one-api`, migrez la base et le répertoire `data/` avant de changer de configuration.

## Compiler depuis les sources

Utilisez Node.js 24, Go 1.27.1 et un compilateur C pour le pilote SQLite avec CGO. Exécutez les commandes depuis la racine du dépôt.

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm run build --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
npm run build --prefix web/admin
go build -o a1o-platform .
./a1o-platform --port 3000
```

Vite écrit les applications dans `web/build/studio` et `web/build/admin`. Le binaire Go intègre les deux. Lapua pilote les modèles et la boucle d’outils de Chat et Work. A1O fournit les outils autorisés et assure le routage, la facturation, la persistance et la planification.

## Configurer et utiliser la passerelle

Définissez les variables d’environnement avant de démarrer le serveur.

| Variable | Rôle |
| --- | --- |
| `PORT` | Port HTTP, `3000` par défaut. |
| `SQL_DSN` | DSN MySQL ou URL `postgres://`. SQLite est utilisé si la variable est absente. |
| `SQLITE_PATH` | Chemin du fichier de base SQLite. |
| `SESSION_SECRET` | Secret stable des sessions, notamment pour les redémarrages et les répliques. |
| `REDIS_CONN_STRING` | URL de connexion Redis pour le cache. |
| `SYNC_FREQUENCY` | Intervalle de synchronisation du cache, en secondes. |
| `NODE_TYPE` | `master` ou `slave` pour un déploiement multinœud. |
| `FRONTEND_BASE_URL` | URL de redirection facultative de l’interface pour un nœud secondaire. |

Avec plusieurs instances, reliez tous les nœuds à la même base MySQL ou PostgreSQL, partagez le même `SESSION_SECRET` et configurez le rôle de chaque nœud ainsi que la synchronisation du cache. Pour les paramètres pris en charge, consultez le paquet [`common/config`](common/config).

Après connexion, ajoutez une clé de fournisseur dans **Canaux**, puis créez un jeton dans **Jetons**. Le même jeton authentifie les clients sur tous les points d’accès ci-dessous.

| Format du client | Point d’accès |
| --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses` (arrière-plan : `GET /v1/responses/{id}`, `POST /v1/responses/{id}/cancel`) |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{model}:generateContent` ou `:streamGenerateContent` (également sous `/v1`) |
| OpenAI Embeddings | `POST /v1/embeddings` |
| Images, audio et modération OpenAI | `POST /v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/audio/translations`, `/v1/moderations` |
| Évaluation SystemOne | `POST /v1/systemone` |

La conversion permet à un client Anthropic d’utiliser un canal OpenAI Chat ou Gemini. Les requêtes OpenAI Chat peuvent aussi passer par des comptes ChatGPT par abonnement via le format Responses.

Configurez `http://localhost:3000/v1` comme URL de base de votre client compatible OpenAI et utilisez le jeton comme clé API.

```sh
curl http://localhost:3000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'
```

## Documentation

- [Développement des interfaces Web](web/README.md)

## Développement

- Vérifiez les types de l’application modifiée avec `npm run typecheck --prefix web/studio` ou `npm run typecheck --prefix web/admin`. Reconstruisez les deux applications avant de compiler un binaire intégrant les dernières ressources.
- Pour les modifications du serveur, lancez `go test ./...`.

Signalez les problèmes et proposez des modifications dans le [suivi de ce dépôt](https://github.com/infinmalum/a1o-platform/issues).

## Origine et licences

A1O Platform est dérivé de [One API](https://github.com/songquanpeng/one-api), copyright © 2023 JustSong et les autres contributeurs. La licence MIT et la notice amont sont conservées dans [`LICENSE.upstream`](LICENSE.upstream). Les modifications originales d’A1O Platform sont distribuées sous [licence Apache 2.0](LICENSE). L’attribution et la portée sont détaillées dans [`NOTICE`](NOTICE). Les mentions de licence propres aux fichiers et aux dépendances tierces restent applicables.
