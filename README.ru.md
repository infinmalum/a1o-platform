<div align="center">
  <img src="docs/logo.png" alt="Логотип A1O Platform" width="120" />

  # A1O Platform

  **Шлюз для моделей ИИ с поддержкой API OpenAI, Anthropic и Gemini.**

  [![CI](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/infinmalum/a1o-platform/actions/workflows/ci.yml)
  [![License](https://img.shields.io/badge/License-AGPL_v3.0-blue.svg)](LICENSE)
  ![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
  ![Node.js](https://img.shields.io/badge/Node.js-24-339933?logo=nodedotjs&logoColor=white)

  [English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Français](README.fr.md) · Русский · [Español](README.es.md)
</div>

---

A1O Platform — независимо поддерживаемый шлюз для моделей ИИ на основе [One API](https://github.com/songquanpeng/one-api). Один экземпляр даёт доступ к разным поставщикам моделей и поддерживает нативные форматы OpenAI Chat Completions, OpenAI Responses, Anthropic Messages и Gemini generateContent. Пользовательский интерфейс Studio дополняется консолью управления каналами, пользователями, токенами, квотами и журналами использования. One API создали JustSong и другие участники проекта.

> [!NOTE]
> A1O Platform выпускается и поддерживается отдельно. Релизы, образы Docker, демонстрационные сайты и трекер One API относятся к исходному проекту.

## Основные возможности

- 🔌 **Четыре нативных формата API** — OpenAI Chat Completions, OpenAI Responses (с возможностью проверять состояние и отменять фоновые задачи), Anthropic Messages и Gemini `generateContent`. Тот же токен подходит для API эмбеддингов, изображений, аудио и модерации.
- 🌉 **Преобразование протоколов** — шлюз преобразует запросы и ответы, включая потоковую передачу. Клиент OpenAI может обращаться к каналам Anthropic, Gemini и Responses, а клиент Anthropic — к каналам OpenAI Chat и Gemini.
- 🔀 **Маршрутизация запросов** — балансировка нагрузки между каналами, сопоставление имён моделей, коэффициенты для групп, подключение аккаунтов подписки через OAuth (ChatGPT, Claude Code, Gemini CLI), приоритет таких аккаунтов в правилах маршрутизации и автоматические повторы до отправки первого байта клиенту.
- 🤖 **Встроенная среда для диалогов** — Chat и Work используют общий цикл вызова инструментов [Norma](https://github.com/infinmalum/norma-e). Поддерживаются сохраняемые сеансы, изолированное выполнение кода Python и Node (ограничения ресурсов rlimits и сетевая изоляция), а также серверы инструментов MCP (Model Context Protocol).
- 🧮 **Квоты и оплата** — квоты для отдельных токенов, журналы использования, коды пополнения, пополнение баланса и расчёт стоимости каждого запроса по настроенным тарифам.
- 🖥️ **Веб-интерфейсы** — в Studio по адресу `/` доступны Chat с многопротокольным Playground, фоновые задачи Work, каталог агентов, закрытые библиотеки документов и инструменты для изображений, видео и аудио. В консоли `/admin/` можно управлять каналами, аккаунтами поставщиков, пользователями, токенами, журналами и настройками.
- 💾 **Гибкое хранение** — PostgreSQL по умолчанию в Docker Compose, с поддержкой SQLite и MySQL, Redis для кэша и синхронизации. Можно развернуть основной и дополнительные узлы.
- 🛠️ **Технологии** — сервер на Go и веб-интерфейсы на TypeScript с Vite. Перед каждой сборкой интерфейсов выполняется проверка типов.

Поддержка моделей и возможностей зависит от типа канала. Проверьте его настройки и протестируйте модель перед использованием в рабочей среде.

## Работа в Studio

Настройте канал поставщика и включите нужную модель для своей группы в консоли управления. Войдите в Studio и выберите модель. Playground и инструменты для медиа используют сеанс Studio. Вызовы моделей расходуют квоту аккаунта.

| Раздел | Возможности |
| --- | --- |
| **Чат → Playground** | Отправляйте запросы в нативных форматах OpenAI Chat Completions, OpenAI Responses, Anthropic Messages и Gemini. Просматривайте исходный JSON или события потока, сравнивайте ответы двух моделей на один запрос и копируйте пример cURL. |
| **Рабочая область → Библиотеки знаний** | Загружайте закрытые файлы TXT, Markdown, CSV, TSV, PDF, DOCX, XLSX, PPTX, JPEG, PNG, GIF и WebP. В чате можно искать по извлечённому тексту и открывать цитаты. По умолчанию доступно 16 МиБ на файл, 256 МиБ и до 64 файлов на библиотеку. Администратор может изменить ограничения размера. См. [руководство по библиотекам](docs/knowledge-libraries.md). |
| **Генерация изображений → Изображение / Видео** | Создавайте изображения с помощью поддерживаемых моделей, включая Seedream в Volcengine Ark и Gemini. Просматривайте и скачивайте оригиналы, прикрепляйте их к чатам и задачам Work. Seedream и совместимые модели Gemini для изображений принимают образцы в PNG, JPEG и WebP. Seedance в Ark создаёт видео в 720p длительностью 5–8 с по тексту и при необходимости по первому кадру. Gemini `gemini-omni-1.2-flash` создаёт видео в 360p длительностью 3–8 с по тексту. |
| **Аудио** | Используйте совместимую модель для синтеза речи, расшифровки аудио или перевода речи на английский. Воспроизводите и скачивайте аудио или экспортируйте расшифровку. |
| **Аккаунт → Пополнение** | Активируйте код или купите разовый пакет квоты через Stripe Checkout, Airwallex, EPay V1/V2, NOWPayments или GMPay Edge в зависимости от настроек сайта. |

Администратор задаёт тарифы моделей Ark через `ARK_MEDIA_PRICES`, а цены пакетов — через `PAYMENT_PACKAGES`. Для Stripe, Airwallex, NOWPayments и GMPay цены задаются в центах США. Для EPay цена указывается отдельно в фэнях. Валюты и сети NOWPayments включаются через `NOWPAYMENTS_PAY_CURRENCIES`, а способы получения платежей GMPay настраиваются в GMPay Edge. Настройка и тестирование описаны в [руководстве по медиа и платежам](docs/media-and-payments.md).

В верхней панели Studio доступны десять языков интерфейса. Для арабского и персидского используется направление справа налево. В консоли управления можно отдельно выбрать китайский или английский язык.

Один экземпляр может работать с SQLite без Redis и векторной базы данных. При резервном копировании и переносе сохраняйте базу данных и том данных. Вложения чатов и созданные изображения и видео хранятся на диске.

После входа каталог показывает включённые модели и позволяет фильтровать их по группе, категории и названию.

## Быстрый запуск в Docker

Приведённые ниже команды собирают приложение из исходного кода. Чтобы использовать готовый образ для Linux amd64, следуйте [инструкции по развёртыванию через GHCR](docs/database-deployment.md#deploy-a-prebuilt-image-from-ghcr).

Для новой установки выполните следующие команды в среде с Docker Compose и OpenSSL.

```sh
git clone https://github.com/infinmalum/a1o-platform.git
cd a1o-platform
umask 077
cp .env.example .env
printf 'POSTGRES_PASSWORD=%s\nSESSION_SECRET=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> .env
docker compose up -d
```

При первом запуске откройте <http://localhost:3000/admin/setup> и создайте учётную запись root. Для автоматической настройки передайте `INITIAL_ROOT_PASSWORD` при первом запуске контейнера. После настройки Studio доступна по адресу <http://localhost:3000/>, а консоль управления — по адресу <http://localhost:3000/admin/>. При замене контейнера сохраняйте том данных. Для публичного развёртывания настройте HTTPS и постоянное значение `SESSION_SECRET`.

Compose по умолчанию запускает PostgreSQL и хранит его данные в `./data/postgres`. Для SQLite или существующей базы PostgreSQL либо MySQL используйте `docker compose -f docker-compose.external.yml up -d` и задайте `SQL_DSN` для внешней базы. Redis необязателен.

При обновлении старой установки со встроенным MySQL сохраните каталог проекта и учётные данные, затем запустите `docker compose -f docker-compose.mysql.yml up -d`. Перед сменой базы сделайте резервную копию, перенесите и проверьте данные. Порядок переключения и отката описан в [руководстве по базам данных](docs/database-deployment.md).

## Сборка из исходного кода

Используйте Node.js 24, Go 1.27.1 и компилятор C для драйвера SQLite с CGO. Выполните команды из корня репозитория.

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm run build --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
npm run build --prefix web/admin
go build -o a1o-platform .
./a1o-platform --port 3000
```

Vite сохраняет приложения в `web/build/studio` и `web/build/admin`. Бинарный файл Go включает оба приложения. Norma выполняет вызовы моделей и инструментов для Chat и Work. A1O предоставляет разрешённые инструменты и управляет маршрутизацией, оплатой, хранением данных и планированием задач.

## Настройка и использование шлюза

Задайте переменные окружения до запуска сервера.

| Переменная | Назначение |
| --- | --- |
| `PORT` | Порт HTTP, по умолчанию `3000`. |
| `SQL_DSN` | DSN MySQL или URL `postgres://`. Если переменная отсутствует, используется SQLite. |
| `SQLITE_PATH` | Путь к файлу базы SQLite. |
| `SESSION_SECRET` | Постоянный секрет сеансов, в том числе для перезапусков и реплик. |
| `REDIS_CONN_STRING` | URL подключения к Redis для кэша. |
| `SYNC_FREQUENCY` | Интервал синхронизации кэша в секундах. |
| `NODE_TYPE` | Роль `master` или `slave` для конфигурации с несколькими узлами. |
| `FRONTEND_BASE_URL` | Необязательный URL перенаправления веб-интерфейса для дополнительного узла. |

Если экземпляров несколько, подключите все узлы к общей базе MySQL или PostgreSQL, используйте одинаковый `SESSION_SECRET` и настройте роли узлов и синхронизацию кэша. Поддерживаемые настройки перечислены в пакете [`common/config`](common/config).

После входа добавьте ключ поставщика в разделе **Каналы** и создайте токен доступа в разделе **Токены**. Один токен подходит для всех перечисленных ниже API.

| Формат клиента | Путь API |
| --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses` (фоновые задачи: `GET /v1/responses/{id}`, `POST /v1/responses/{id}/cancel`) |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{model}:generateContent` или `:streamGenerateContent` (также доступно под `/v1`) |
| OpenAI Embeddings | `POST /v1/embeddings` |
| Изображения, аудио и модерация OpenAI | `POST /v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/audio/translations`, `/v1/moderations` |
| Оценка SystemOne | `POST /v1/systemone` |

Преобразование форматов позволяет клиенту Anthropic обращаться к каналу OpenAI Chat или Gemini. Запросы OpenAI Chat могут использовать аккаунты ChatGPT с подпиской через формат Responses.

В клиенте, совместимом с OpenAI, укажите базовый URL `http://localhost:3000/v1` и используйте токен в качестве ключа API.

```sh
curl http://localhost:3000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'
```

## Документация

- [Разработка веб-интерфейсов](web/README.md)
- [Конфиденциальность аккаунта, память и удаление данных](docs/privacy.md)

## Разработка

- Проверьте типы для изменённого интерфейса командой `npm run typecheck --prefix web/studio` или `npm run typecheck --prefix web/admin`. Чтобы включить актуальные ресурсы в бинарный файл, сначала пересоберите оба приложения.
- При изменениях на сервере запустите `go test ./...`.

Сообщайте об ошибках и предлагайте изменения в [трекере этого репозитория](https://github.com/infinmalum/a1o-platform/issues).

## Происхождение и лицензии

A1O Platform основан на [One API](https://github.com/songquanpeng/one-api), авторские права © 2023 JustSong и другие участники. Исходная лицензия MIT и уведомление сохранены в [`LICENSE.upstream`](LICENSE.upstream). Оригинальные изменения в A1O Platform распространяются по [лицензии GNU Affero General Public v3.0 (AGPL-3.0)](LICENSE). Сведения об авторстве и области применения приведены в [`NOTICE`](NOTICE). Уведомления о лицензиях отдельных файлов и сторонних зависимостей сохраняют силу.
