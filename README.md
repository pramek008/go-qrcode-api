<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22-00ADD8?logo=go" alt="Go 1.22">
  <img src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License">
</p>

# QR Service

A self-hosted, batteries-included QR code generation API — compatible with the **goqr.me** API format. Generate QR codes as PNG, SVG, JPEG, or WebP, with logo overlays, custom colors, gradient fills, custom module shapes, content-type helpers (WiFi, vCard, …), and optional persistent history.

> **Two modes:** run with just `go run` for instant stateless generation, or add a PostgreSQL database to unlock persistent history and API key management.

> Spins up in seconds with `docker compose up`. No external database required.

## Features

- **Multiple formats** — PNG, SVG, JPEG, and **native WebP** (real encoding, not fake)
- **Content-type helpers** — Encode WiFi, vCard, MeCard, email, tel, SMS, geo, event, WhatsApp — no manual payload formatting
- **Styling engine** — Custom module shapes (`square`, `rounded`, `dot`, `circle`), eye styles, separate eye color, linear/radial gradients
- **Logo overlay** — Embed a brand logo centered on the QR; configurable size, shape (`square`/`circle`), and safety-zone margin
- **Custom colors** — Hex (`#rrggbb`, `#rgb`), decimal (`r-g-b`), or `transparent` background
- **Controllable margins** — `padding` (px) and `qzone` (quiet-zone modules) for exact whitespace control
- **ETag / 304 caching** — Output is deterministic, so identical requests get a `304 Not Modified` for free
- **Output modes** — Return raw image bytes (`image`), `base64` string, or full `json` with dataUri
- **Error correction** — L / M / Q / H levels for print durability or logo tolerance
- **Persistent history** — Save generated QRs to PostgreSQL, list, search, filter, and download
- **Stateless mode** — Generate on-the-fly without storing anything
- **Multi-key management** — Issue per-client API keys with individual rate limits and usage quotas
- **Security built in** — Rate limiting, CORS, API key auth, graceful shutdown
- **Zero-dependency DB** — Bundled PostgreSQL in docker-compose, auto-migration on startup

## 🚀 Quick Start

### Stateless — no setup required

```bash
git clone https://github.com/pramek008/go-qrcode-api.git
cd go-qrcode-api
go run ./cmd/main.go
```

```bash
curl 'http://localhost:8080/v1/create-qr-code?data=Hello%20World' --output qr.png
curl 'http://localhost:8080/health'
# → {"status":"ok","mode":"stateless"}
```

### Full — with PostgreSQL (Docker Compose)

```bash
# 1. Clone
git clone https://github.com/pramek008/go-qrcode-api.git
cd go-qrcode-api

# 2. Configure (optional — defaults work out of the box)
cp .env.example .env

# 3. Start everything (app + PostgreSQL)
docker compose up -d
```

The API is ready at `http://localhost:9080`.

```bash
curl 'http://localhost:9080/v1/create-qr-code?data=Hello%20World&format=png' --output qr.png
curl 'http://localhost:9080/health'
# → {"status":"ok","mode":"full"}
```

---

## 📖 Table of Contents

- [Quick Start](#-quick-start)
- [Modes](#-modes)
- [Getting Started](#-getting-started)
  - [Prerequisites](#prerequisites)
  - [Option A: Docker Compose (recommended)](#option-a-docker-compose-recommended)
  - [Option B: External PostgreSQL](#option-b-external-postgresql)
- [API Reference](#-api-reference)
  - [Generate QR (stateless)](#generate-qr-stateless)
  - [Management Endpoints](#management-endpoints)
  - [Health & Metrics](#health--metrics)
- [API Key Management](#-api-key-management)
  - [Admin Endpoints](#admin-endpoints)
  - [Client Usage](#client-usage)
- [Environment Variables](#-environment-variables)
- [QR Features](#-qr-features)
  - [Padding & Margins](#padding--margins)
  - [Logo Overlay](#logo-overlay)
  - [WebP Support](#webp-support)
  - [Error Correction Levels](#error-correction-levels)
- [Architecture](#-architecture)
- [Security](#-security)
- [Local Development](#-local-development)
- [Contributing](#-contributing)
- [License](#-license)

---

---

## ⚙️ Modes

The service detects its run mode automatically from the presence of `DATABASE_URL`.

| | Stateless mode | Full mode |
|---|---|---|
| **How to start** | No env vars needed | Set `DATABASE_URL` |
| **`GET /v1/create-qr-code`** | ✅ | ✅ |
| **`?save` flag** | ❌ 503 (no DB) | ✅ |
| **`/v1/qr/*` management** | ❌ 503 | ✅ |
| **`/v1/admin/*` API keys** | ❌ 503 | ✅ |
| **`/health`** | ✅ `{"status":"ok","mode":"stateless"}` | ✅ with DB ping |
| **`/metrics`** | ✅ `{"mode":"stateless","db":null}` | ✅ with pool stats |

### Stateless mode — zero dependencies

No Docker, no database, no env file:

```bash
go run ./cmd/main.go
# → {"mode":"stateless"} on startup
# → GET /v1/create-qr-code works immediately
```

Or with Docker:

```bash
docker run -p 8080:8080 ghcr.io/pramek008/go-qrcode-api
```

### Full mode — with persistence

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/qrservice go run ./cmd/main.go
# → connects to DB, runs migrations, enables all endpoints
```

Or with Docker Compose (recommended for production):

```bash
docker compose up -d
```

---

## 🛠 Getting Started

### Prerequisites

- **Docker** and **Docker Compose** v2+
- That's it — no Go toolchain, no PostgreSQL install needed.

### Option A: Docker Compose (recommended)

The `docker-compose.yml` bundles everything:

| Service | What it does |
|---|---|
| `qrservice` | QR generation API (port `9080`) |
| `db` | PostgreSQL 16 with auto-created `qrservice` database |

```bash
# Default setup
docker compose up -d

# Custom external port
PORT_EXTERNAL=8090 docker compose up -d

# Custom DB credentials
POSTGRES_USER=admin POSTGRES_PASSWORD=s3cret docker compose up -d
```

Data persists in two Docker volumes:
- `pgdata` — PostgreSQL data
- `qr_storage` — Generated QR image files

### Option B: External PostgreSQL

If you already run PostgreSQL elsewhere (e.g., on a `shared_net` Docker network):

```bash
# 1. Configure
cp .env.example .env
# Edit .env — set your DATABASE_URL

# 2. Use the standalone production compose (no bundled DB)
docker compose -f docker-compose.prod.yml up -d --build
```

This compose file connects to `shared_net` (external Docker network) and skips the
bundled database container — exactly your existing VPS setup.

Or run without Docker at all (see [Local Development](#-local-development)).

---

## 📡 API Reference

### Try in this Playground

https://ekanovation.my.id/tools/qr-service

### You can try with this base url
```
https://qrcode.ekanovation.my.id
```

### Generate QR (stateless)

```
GET /v1/create-qr-code
```

Generate a QR code on the fly. Nothing is stored unless `?save` is added.

#### Basic / Data params

| Query | Default | Description |
|---|---|---|
| `data` | *required* | Text or URL to encode (use `type` instead for structured content) |
| `type` | `text` | Content type — see [Content Types](#content-types) below |
| `size` | `150x150` | Output dimensions — `WxH` (e.g. `400x200`) or `300` → `300x300` |
| `width` / `height` | — | Alternative to `size`; override individual dimension |
| `format` | `png` | `png`, `svg`, `jpeg`, `webp` |
| `output` | `image` | Response mode: `image` (bytes), `base64`, or `json` (with dataUri) |
| `download` | *(none)* | Set `Content-Disposition` filename, e.g. `download=qr.png` |
| `save` | *(flag)* | Persist to history (no value needed) |

#### Color params

| Query | Default | Description |
|---|---|---|
| `color` | `000000` | Foreground — hex (`rrggbb` or `rgb`), or decimal `r-g-b` |
| `bgcolor` | `ffffff` | Background — same formats, or `transparent` for alpha PNG/SVG/WebP |
| `eye_color` | *(= color)* | Finder-pattern color — lets eyes stand out from body modules |

#### Margin / quiet-zone params

| Query | Default | Description |
|---|---|---|
| `padding` / `margin` | `4` | Outer whitespace in **px** inside the canvas |
| `qzone` | `0` | Additional quiet-zone in **modules** (added around the QR pattern) |

#### Error correction

| Query | Default | Description |
|---|---|---|
| `recovery` / `ecc` | `M` | `L`, `M`, `Q`, `H` — alias `ecc` compatible with goqr.me |

#### Styling params

| Query | Default | Options | Description |
|---|---|---|---|
| `style` | `square` | `square`, `rounded`, `dot`, `circle` | Body module shape |
| `eye_style` | `square` | `square`, `rounded`, `circle` | Finder-pattern (eye) shape |
| `gradient` | `none` | `none`, `linear`, `radial` | Gradient on body modules |
| `gradient_from` | *(= color)* | hex / `r-g-b` | Gradient start color |
| `gradient_to` | *(= color)* | hex / `r-g-b` | Gradient end color |
| `gradient_angle` | `0` | 0–360 | Angle in degrees (linear only) |

#### Logo params

| Query | Default | Description |
|---|---|---|
| `logo` | *(none)* | Base64-encoded image (raw or `data:image/png;base64,...` URI) |
| `logo_size` | `22` | Logo size as **percent** of QR (1–50) |
| `logo_shape` | `square` | `square` or `circle` (circular crop) |
| `logo_margin` | `2` | White safety-zone thickness in px |

> When `logo` is provided, error correction is automatically forced to **High**.

**Examples:**

```bash
# Basic PNG
GET /v1/create-qr-code?data=https://example.com&size=300x300

# WiFi QR (no manual WIFI: string!)
GET /v1/create-qr-code?type=wifi&ssid=HomeNet&password=secret&encryption=WPA

# vCard
GET /v1/create-qr-code?type=vcard&name=John+Doe&phone=%2B6281234&email=john@example.com

# Rounded dots, gradient, custom eye color
GET /v1/create-qr-code?data=styled&style=dot&eye_style=circle&eye_color=ff0000&gradient=linear&gradient_from=3a1c71&gradient_to=ffaf7b

# Transparent PNG background
GET /v1/create-qr-code?data=hi&bgcolor=transparent&format=png

# JSON output with dataUri (for frontend embedding)
GET /v1/create-qr-code?data=hi&output=json

# Force download with filename
GET /v1/create-qr-code?data=hi&download=my-qr.png

# SVG with circular logo, 25% size
GET /v1/create-qr-code?data=branded&format=svg&logo=iVBORw0KGgo...&logo_size=25&logo_shape=circle

# Save to history
GET /v1/create-qr-code?data=https://example.com&save
```

#### Content Types

Use `type=` instead of encoding payloads manually:

| `type` | Required params | Example |
|---|---|---|
| `text` / `url` | `data` | `?data=Hello` |
| `wifi` | `ssid`, `encryption` (`WPA`/`WEP`/`nopass`) | `?type=wifi&ssid=Net&password=pass&encryption=WPA` |
| `vcard` | `name` | `?type=vcard&name=Jane&phone=%2B62...` |
| `mecard` | `name` | `?type=mecard&name=Jane&phone=...` |
| `email` | `to` | `?type=email&to=x@y.com&subject=Hi` |
| `tel` | `number` | `?type=tel&number=%2B6281234` |
| `sms` | `number` | `?type=sms&number=123&message=Hello` |
| `geo` | `lat`, `lng` | `?type=geo&lat=-6.2&lng=106.8` |
| `event` | `title` | `?type=event&title=Launch&start=20260617T090000Z` |
| `whatsapp` | `number` | `?type=whatsapp&number=%2B6281234&message=Hi` |

### Management Endpoints

These endpoints require an API key — see [API Key Management](#-api-key-management). Authenticate via `X-API-Key: <key>` header or `?api_key=<key>` query param.

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/qr` | Generate QR + save to history |
| `GET` | `/v1/qr` | List history (paginated, searchable, filterable) |
| `GET` | `/v1/qr/:id` | Get single QR record details |
| `GET` | `/v1/qr/:id/download` | Download QR image file |
| `DELETE` | `/v1/qr/:id` | Delete QR from history and storage |

**POST /v1/qr** — Request body:

```json
{
  "data": "https://example.com",
  "size": 300,
  "width": 400,
  "height": 200,
  "format": "png",
  "color": "#000000",
  "bgcolor": "#ffffff",
  "recovery": "H",
  "padding": 10,
  "qzone": 2,
  "logo": "iVBORw0KGgo...",
  "logo_size": 22,
  "logo_shape": "circle",
  "logo_margin": 3,
  "style": "rounded",
  "eye_style": "circle",
  "eye_color": "#ff0000",
  "gradient": "linear",
  "gradient_from": "#000000",
  "gradient_to": "#0000ff",
  "gradient_angle": 45
}
```

> Use `size` for square, or `width`/`height` for rectangular. If both are given, `width`/`height` win.
> All styling fields (`style`, `eye_style`, `eye_color`, `gradient*`, `logo_*`) are optional and default to the same values as the stateless endpoint.

**Response:**

```json
{
  "qr": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "data": "https://example.com",
    "format": "png",
    "width": 400,
    "height": 200,
    "size": 200,
    "color": "#000000",
    "bgcolor": "#ffffff",
    "file_path": "/app/storage/qrcodes/550e8400...png",
    "created_at": "2026-05-03T12:00:00Z"
  },
  "download": "/v1/qr/550e8400-e29b-41d4-a716-446655440000/download"
}
```

**GET /v1/qr** — Query parameters:

| Query | Default | Description |
|---|---|---|
| `limit` | `20` | Max 100 |
| `offset` | `0` | Pagination offset |
| `search` | *(none)* | Case-insensitive search on `data` field (ILIKE) |
| `format` | *(none)* | Filter by output format (`png`, `svg`, `jpeg`, `webp`) |

**Examples:**

```bash
# List page 1
GET /v1/qr?limit=20&offset=0

# Search for QRs containing "example"
GET /v1/qr?search=example

# Filter SVGs + search
GET /v1/qr?format=svg&search=hello

# Authenticated
GET /v1/qr -H "X-API-Key: your-key"
```

### Health & Metrics

```bash
GET /health
# → 200 { "status": "ok" }
# → 503 { "status": "unhealthy", "error": "database unreachable" }

GET /metrics
# → { "db": { "total_conns": 5, "idle_conns": 3, "acquired_conns": 2 } }
```

---

## 🔑 API Key Management

The management endpoints (`/v1/qr/*`) require an API key. Instead of a single static key, you create **per-client keys** via admin endpoints — each with its own rate limit and monthly quota.

### Admin Endpoints

Protected by `ADMIN_KEY` env var (passed via `X-Admin-Key` header or `?admin_key` query param).

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/admin/keys` | Create a new API key for a client |
| `GET` | `/v1/admin/keys` | List all API keys |
| `GET` | `/v1/admin/keys/:id` | Get single key details |
| `DELETE` | `/v1/admin/keys/:id` | Revoke (deactivate) a key |
| `POST` | `/v1/admin/keys/:id/rotate` | Generate a new key string |

**Create a key:**

```bash
curl -X POST http://localhost:9080/v1/admin/keys \
  -H "X-Admin-Key: your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{"name": "Client A", "rate_limit": 100, "rate_limit_window": 60, "quota": 5000}'
```

**Response:**

```json
{
  "key": {
    "id": "uuid",
    "name": "Client A",
    "key": "abc123...",
    "rate_limit": 100,
    "rate_limit_window": 60,
    "quota": 5000,
    "quota_used": 0,
    "is_active": true
  }
}
```

| Body field | Default | Description |
|---|---|---|
| `name` | *required* | Human-readable label for the client |
| `rate_limit` | `30` | Max requests per window |
| `rate_limit_window` | `60` | Window duration in seconds |
| `quota` | `0` | Total request quota (`0` = unlimited) |

### Client Usage

Clients use their key via `X-API-Key` header or `?api_key` query param:

```bash
curl -H "X-API-Key: abc123..." \
  http://localhost:9080/v1/qr?limit=10
```

Each key's rate limit and quota are enforced per-request. Keys can be revoked without restarting the service.

---

## 🔧 Environment Variables

| Variable | Default | Required | Description |
|---|---|---|---|
| `DATABASE_URL` | *(none)* | No¹ | PostgreSQL connection string — omit to run in stateless-only mode |
| `PORT` | `8080` | No | Server listen port |
| `STORAGE_DIR` | `/app/storage/qrcodes` | No | QR file storage directory |
| `MIGRATIONS_DIR` | `./migrations` | No | Path to SQL migration files |
| `ADMIN_KEY` | *(empty)* | No | Master key for `/v1/admin/*` key management endpoints |
| `CORS_ORIGINS` | `*` | No | Comma-separated allowed origins |
| `RATE_LIMIT_MAX` | `30` | No | Max requests per IP per window |
| `RATE_LIMIT_EXPIRATION` | `60s` | No | Rate limit window duration |
| `DB_MAX_CONNS` | `20` | No | PostgreSQL connection pool max |
¹ Required only for full mode (persistence + API key management).

| `POSTGRES_USER` | `qruser` | No | (compose) DB username |
| `POSTGRES_PASSWORD` | `qrpass` | No | (compose) DB password |
| `POSTGRES_DB` | `qrservice` | No | (compose) DB name |
| `PORT_EXTERNAL` | `9080` | No | (compose) External port mapping |

---

## 🎨 QR Features

### Content Types

Instead of constructing payload strings manually, set `type=` and supply structured fields. The service builds the correct encoded string for you:

```bash
# WiFi — encodes to "WIFI:T:WPA;S:MyNet;P:pass;H:false;;"
?type=wifi&ssid=MyNet&password=pass&encryption=WPA

# vCard — encodes to BEGIN:VCARD ... END:VCARD
?type=vcard&name=Jane+Doe&phone=%2B6281234&email=jane@example.com

# WhatsApp deep-link — strips spaces/dashes from number
?type=whatsapp&number=%2B62+812-3456&message=Hello
```

### Styling Engine

All styling is rendered from the raw QR bitmap — no library restrictions.

**Module shapes** (`style=`):
- `square` — classic solid squares *(default)*
- `rounded` — squares with rounded corners
- `dot` — small filled circles (~84% of cell)
- `circle` — full circles

**Eye (finder-pattern) styles** (`eye_style=`):
- `square`, `rounded`, `circle`
- Can use a separate color (`eye_color=`) to make eyes pop

**Gradients** (`gradient=linear` or `gradient=radial`):
- `gradient_from` / `gradient_to` — start and end colors
- `gradient_angle` — rotation in degrees (linear only)

```bash
# Teal gradient, dot modules, rounded eyes
?data=hi&style=dot&eye_style=rounded&gradient=linear&gradient_from=00d2ff&gradient_to=3a7bd5

# Radial gradient in SVG (vector quality, scales infinitely)
?data=hi&format=svg&gradient=radial&gradient_from=ffcc00&gradient_to=cc0000
```

### Padding & Margins

Two independent controls:
- `padding` — whitespace in **pixels** inside the canvas (default 4)
- `qzone` — quiet-zone in **modules** added around the QR pattern (default 0)

The library's built-in quiet zone is disabled — you have full control.

```
padding=0          padding=4 (default)       padding=30
┌──────────┐       ┌──────────┐              ┌──────────┐
│██████████│       │ ████████ │              │          │
│██ ██ ████│       │ ██ ██ ██ │              │ ████████ │
│████ █████│       │ ████ ███ │              │ ██ ██ ██ │
│██████████│       │ ████████ │              │ ████████ │
└──────────┘       └──────────┘              └──────────┘
edge-to-edge       thin margin               generous space
```

### Logo Overlay

Supports any image format that Go's `image` package can decode (PNG, JPEG, GIF). The logo is:

- Scaled to `logo_size`% of the QR area using Catmull-Rom interpolation (default **22%**)
- Cropped to a circle if `logo_shape=circle`
- Centered with a white safety zone of `logo_margin` px (default **2px**)
- Auto-triggers **High** error correction to keep the QR scannable

The `logo` parameter accepts both raw base64 and full `data:image/png;base64,...` URIs.

```bash
# Square logo, default size
?logo=iVBORw0KGgo...

# Circular logo, 25% size, 4px safety zone
?logo=iVBORw0KGgo...&logo_shape=circle&logo_size=25&logo_margin=4
```

> **Tip:** For large logos, use `POST /v1/qr` with the logo in the JSON body to avoid URL length limits.

### Output Modes

| `output=` | Response | Use Case |
|---|---|---|
| `image` *(default)* | Binary image bytes | `<img src>` / curl download |
| `base64` | JSON `{base64, dataUri}` | Lightweight frontend embed |
| `json` | JSON `{format, mime, width, height, base64, dataUri}` | Full metadata |

### ETag / 304 Caching

QR output is deterministic for identical params. The API sets an `ETag` header on every response. Browsers and CDNs that send `If-None-Match` on repeat requests receive a `304 Not Modified` with no body — free bandwidth savings for hot URLs.

### WebP Support

WebP encoding uses the `cwebp` CLI tool (`libwebp-tools`), installed automatically in the Docker image. For local development:

```bash
# Debian / Ubuntu
sudo apt install webp

# macOS
brew install webp
```

### Error Correction Levels

| Level | Recovery | Use Case |
|---|---|---|
| `L` (Low) | ~7% | Maximum data density, minimal damage tolerance |
| `M` (Medium) | ~15% | General use *(default)* |
| `Q` (Quartile) | ~25% | Balanced density and durability |
| `H` (High) | ~30% | Logo overlays, print media *(auto-selected with logo)* |

---

## 🏗 Architecture

```
cmd/main.go                  — Entry point, config, server lifecycle
internal/
  content/
    build.go                 — Content-type payload builders (WiFi, vCard, …)
    build_test.go
  handler/qr_handler.go      — HTTP handlers (ETag, output modes, param parsing)
  service/
    qr_service.go            — Business logic (QR gen, logo, encoding, color parsing)
    render.go                — Unified raster + SVG renderer (shapes, gradients, eyes)
    qr_service_test.go       — Unit tests (generation, styling, colors, data length)
    render_test.go           — Renderer unit tests (shapes, gradients, transparency)
    errors.go                — Sentinel error types
  repository/
    qr_repository.go         — PostgreSQL persistence (CRUD + search/filter)
  migration/
    migrate.go               — Auto-migration runner (idempotent)
migrations/
  001_init.sql               — Initial schema
  002_add_dimensions.sql     — Width/height columns
  003_add_api_keys.sql       — Per-client API keys
```

**Tech stack:** Go 1.22 · Fiber v2 · pgx v5 · go-qrcode · slog · golang.org/x/image

---

## 🔒 Security

| Feature | Detail |
|---|---|
| **Rate limiting** | 30 req / 60s per IP (configurable) |
| **CORS** | Configurable origins via `CORS_ORIGINS` |
| **API key auth** | Optional for management endpoints |
| **Path sanitization** | Output filenames validated and sanitized |
| **Graceful shutdown** | SIGINT/SIGTERM with 10s drain timeout |

---

## 💻 Local Development

```bash
# Prerequisites: Go 1.22+, PostgreSQL, cwebp (libwebp-tools)

# 1. Start PostgreSQL (or use Docker)
docker compose up -d db

# 2. Set up environment
cp .env.example .env
# Edit DATABASE_URL to point to localhost, e.g.:
# DATABASE_URL=postgres://qruser:qrpass@localhost:5432/qrservice?sslmode=disable

# 3. Run
go run ./cmd/main.go

# 4. Test
go test ./... -count=1
```

---

## 🤝 Contributing

Contributions are welcome! Areas that could use attention:

- Handler integration tests (currently only unit tests exist)
- OpenAPI / Swagger spec generation
- Additional output formats (PDF, EPS, TIFF)
- Multi-language QR data support

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run `go test ./...` to verify
5. Open a pull request

---

## 📄 License

MIT — see [LICENSE](./LICENSE) for details.
