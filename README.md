<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22-00ADD8?logo=go" alt="Go 1.22">
  <img src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License">
</p>

# QR Service

A self-hosted, batteries-included QR code generation API — compatible with the **goqr.me** API format. Generate QR codes as PNG, SVG, JPEG, or WebP, with logo overlays, custom colors, error correction tuning, and persistent history.

> Spins up in seconds with `docker compose up`. No external database required.

## Features

- **Multiple formats** — PNG, SVG, JPEG, and **native WebP** (real encoding, not fake)
- **Logo overlay** — Embed a brand logo centered on the QR with automatic high error correction
- **Custom colors** — Pick any foreground and background hex color
- **Controllable margins** — Fine-grained `padding` parameter (0 = edge-to-edge)
- **Error correction** — L / M / Q / H levels for print durability or logo tolerance
- **Persistent history** — Save generated QRs to PostgreSQL, list, search, filter, and download
- **Stateless mode** — Generate on-the-fly without storing anything
- **Security built in** — Rate limiting, CORS, optional API key auth, graceful shutdown
- **Zero-dependency DB** — Bundled PostgreSQL in docker-compose, auto-migration on startup

## 🚀 Quick Start

```bash
# 1. Clone
git clone https://github.com/ekanovation/qrservice.git
cd qrservice

# 2. Configure (optional — defaults work out of the box)
cp .env.example .env

# 3. Start everything (app + PostgreSQL)
docker compose up -d
```

That's it. The API is ready at `http://localhost:9080`.

Try it:

```bash
curl 'http://localhost:9080/v1/create-qr-code?data=Hello%20World&format=png'
curl 'http://localhost:9080/health'
```

---

## 📖 Table of Contents

- [Quick Start](#-quick-start)
- [Getting Started](#-getting-started)
  - [Prerequisites](#prerequisites)
  - [Option A: Docker Compose (recommended)](#option-a-docker-compose-recommended)
  - [Option B: External PostgreSQL](#option-b-external-postgresql)
- [API Reference](#-api-reference)
  - [Generate QR (stateless)](#generate-qr-stateless)
  - [Management Endpoints](#management-endpoints)
  - [Health & Metrics](#health--metrics)
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

### Generate QR (stateless)

```
GET /v1/create-qr-code
```

Generate a QR code on the fly. Nothing is stored unless `?save` is added.

| Query | Default | Description |
|---|---|---|
| `data` | *required* | Text or URL to encode |
| `size` | `150x150` | Output dimensions — `WxH` (e.g. `400x200`) or single value `300` → `300x300` |
| `format` | `png` | `png`, `svg`, `jpeg`, `webp` |
| `color` | `000000` | Foreground hex (without `#`) |
| `bgcolor` | `ffffff` | Background hex (without `#`) |
| `recovery` | `M` | Error correction: `L`, `M`, `Q`, `H` |
| `logo` | *(none)* | Base64-encoded image (raw base64 or `data:image/png;base64,...` URI) |
| `padding` | `4` | Inner margin (px) around QR within the canvas. `0` = edge-to-edge |
| `save` | *(flag)* | Add `?save` to persist the QR to history |

**Examples:**

```bash
# Basic PNG
GET /v1/create-qr-code?data=https://example.com&size=300x300&format=png

# SVG with custom colors
GET /v1/create-qr-code?data=Hello&format=svg&color=4ECCA3&bgcolor=1a1a2e

# Rectangular with high error correction
GET /v1/create-qr-code?data=hi&size=400x200&format=jpeg&recovery=H

# Tight QR with 20px breathing room
GET /v1/create-qr-code?data=compact&size=200x200&padding=20

# Edge-to-edge, no margin
GET /v1/create-qr-code?data=tight&size=200x200&padding=0

# With logo overlay + save
GET /v1/create-qr-code?data=branded&format=png&logo=iVBORw0KGgo...&save
```

> When `logo` is provided, error correction is automatically forced to **High** for scanability.

### Management Endpoints

When `API_KEY` is set, these endpoints require `X-API-Key: <key>` header or `?api_key=<key>` query param.

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
  "logo": "iVBORw0KGgo..."
}
```

> Use `size` for square, or `width`/`height` for rectangular. If both are given, `width`/`height` win.

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

## 🔧 Environment Variables

| Variable | Default | Required | Description |
|---|---|---|---|
| `DATABASE_URL` | (compose default) | **Yes** | PostgreSQL connection string |
| `PORT` | `8080` | No | Server listen port |
| `STORAGE_DIR` | `/app/storage/qrcodes` | No | QR file storage directory |
| `MIGRATIONS_DIR` | `./migrations` | No | Path to SQL migration files |
| `API_KEY` | *(empty)* | No | Protects `/v1/qr/*` endpoints when set |
| `CORS_ORIGINS` | `*` | No | Comma-separated allowed origins |
| `RATE_LIMIT_MAX` | `30` | No | Max requests per IP per window |
| `RATE_LIMIT_EXPIRATION` | `60s` | No | Rate limit window duration |
| `DB_MAX_CONNS` | `20` | No | PostgreSQL connection pool max |
| `POSTGRES_USER` | `qruser` | No | (compose) DB username |
| `POSTGRES_PASSWORD` | `qrpass` | No | (compose) DB password |
| `POSTGRES_DB` | `qrservice` | No | (compose) DB name |
| `PORT_EXTERNAL` | `9080` | No | (compose) External port mapping |

---

## 🎨 QR Features

### Padding & Margins

The `padding` parameter controls white space around the QR inside the canvas. The library's built-in quiet zone is **disabled** — you have full control.

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

- Scaled to **22%** of the QR area using Catmull-Rom interpolation
- Centered with a **2px white safety zone** behind it
- Auto-triggers **High** error correction to keep the QR scannable

The `logo` parameter accepts both raw base64 and full `data:image/png;base64,...` URIs.

```bash
# Raw base64
?logo=iVBORw0KGgo...

# Data URI (from browser / export tools)
?logo=data:image/png;base64,iVBORw0KGgo...
```

> **Tip:** For large logos, use `POST /v1/qr` with the logo in the JSON body to avoid URL length limits.

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
  handler/qr_handler.go      — HTTP handlers (request → response)
  service/
    qr_service.go            — Business logic (QR gen, logo, encoding)
    qr_service_test.go       — 21 unit tests
    errors.go                — Sentinel error types
  repository/
    qr_repository.go         — PostgreSQL persistence (CRUD + search/filter)
  migration/
    migrate.go               — Auto-migration runner (idempotent)
    migrate_test.go          — Migration tests
migrations/
  001_init.sql               — Initial schema
  002_add_dimensions.sql     — Width/height columns
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
