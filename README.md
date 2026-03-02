# disapyr-link

A production-ready, zero-knowledge one-time secret sharing application. Share text or files via a single-use link that self-destructs after the first view — and the server **never** sees the plaintext.

---

## How it works

1. **Submit** — paste text or drop a file into the browser. A 256-bit AES-GCM key is generated in your browser and the secret is encrypted **client-side** before anything leaves your device.
2. **Get a link** — the server stores only the ciphertext, nonce, and a random one-time token. The encryption key is never transmitted to or stored on the server.
3. **Share** — the retrieval link includes the key in the URL **fragment** (`#key=…`). Browsers never send the fragment to the server, so the key remains client-only.
4. **Retrieve** — the recipient opens the link. The server returns the ciphertext, the browser decrypts it with the key from the fragment, and then **atomically deletes** the record. Any subsequent request returns 404.

```
https://your-domain/s/{token}#key={base64url-key}
                    ^^^^^^^^                ^^^^^^^^^^^^^^^^^^
                 stored in DB              never stored anywhere
```

---

## Security model

| Property | Guarantee |
|---|---|
| Encryption | AES-256-GCM via the Web Crypto API |
| Key storage | Key exists only in the URL fragment — never logged, never sent to the server |
| Server knowledge | Server stores: ciphertext, nonce, one-time token, expiry. Nothing else. |
| One-time access | Fetch and delete are a single atomic database transaction |
| Expired secrets | Indexed on `expires_at`; expired records return 404 and are eligible for cleanup |
| HTTP security headers | `Content-Security-Policy` (strict), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` |
| CSP enforcement | No inline scripts or styles; all JS is served as external files |
| File safety | Client-side blocklist rejects executable file types before encryption (`.exe`, `.msi`, `.bat`, `.sh`, `.jar`, etc.) |

> **Note on server-side AV scanning**: because the server only ever receives encrypted ciphertext, server-side antivirus scanning is architecturally impossible by design. The client-side extension blocklist is the safety layer for executable types.

---

## Features

- **Text secrets** — multi-line plain text with safe rendering (no HTML injection)
- **File secrets** — any file type up to the configured maximum; downloaded by the recipient after decryption
- **Expiry options** — no expiry, 1 hour, 24 hours, or 7 days
- **Copy-to-clipboard** — one-click copy of the retrieval link on creation; copy button on revealed text secrets
- **Drag-and-drop** — drop a file onto the upload zone
- **Light / Dark / System theme** — three-state toggle with FOUC-free initialisation
- **Destruction confirmation** — "This secret has been destroyed" shown after retrieval
- **Configurable size limit** — default 10 MB, set via `MAX_SECRET_BYTES`

---

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.25+ |
| HTTP | `net/http` (stdlib) |
| Templates | `html/template` (stdlib) |
| Database | PostgreSQL 16 |
| DB driver | `pgx/v5` |
| Client crypto | Web Crypto API (AES-GCM 256) |
| Frontend | Vanilla JS + Go templates (no SPA framework) |
| Container | Docker (multi-stage) + docker-compose |
| Logging | `log/slog` (stdlib) |

---

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/)
- PostgreSQL 16 (or use the bundled docker-compose service)

---

## Quick start (Docker)

```bash
# 1. Clone
git clone https://github.com/jvanrhyn/disapyr-link.git
cd disapyr-link

# 2. Configure (edit as needed)
cp .env.example .env

# 3. Start everything
docker-compose up --build

# App is now at http://localhost:8080
```

The `postgres` service automatically runs the schema migration on first start via `001_init.sql`. No manual migration step is required.

---

## Quick start (local development)

```bash
# 1. Start only the database
docker-compose up -d postgres

# 2. Copy and edit env
cp .env.example .env
# Edit DATABASE_URL if needed, e.g.:
# DATABASE_URL=postgres://disapyr:disapyr@localhost:5432/disapyr?sslmode=disable

# 3. Apply the schema
psql "$DATABASE_URL" -f internal/db/migrations/001_init.sql

# 4. Run
go run ./cmd/server
```

---

## Configuration

All configuration is via environment variables (`.env` file or shell):

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `PORT` | `8080` | HTTP listen port |
| `BASE_URL` | `http://localhost:8080` | Public base URL (used in retrieval links) |
| `MAX_SECRET_BYTES` | `10485760` (10 MB) | Maximum encrypted payload size in bytes |
| `LOG_LEVEL` | `info` | Logging level (`debug`, `info`, `warn`, `error`) |

---

## Project structure

```
disapyr-link/
├── cmd/server/          # Entry point — wires dependencies and starts HTTP server
├── internal/
│   ├── config/          # Environment variable parsing
│   ├── db/
│   │   ├── db.go        # pgx connection pool
│   │   └── migrations/  # SQL schema (001_init.sql)
│   ├── handler/         # HTTP handlers, CSP headers, template rendering
│   ├── model/           # Domain types (Secret, CreateSecretInput)
│   ├── repository/      # Database access (Store, FetchAndDelete)
│   └── service/         # Business logic (Create, Retrieve, token generation)
└── web/
    ├── static/
    │   ├── css/style.css        # Design system — light/dark tokens, component styles
    │   └── js/
    │       ├── crypto.js        # AES-GCM encrypt/decrypt, file validation, drag-drop, copy
    │       ├── effects.js       # UI effects, theme toggle state machine
    │       └── theme-init.js    # Synchronous FOUC-prevention script (no defer)
    └── templates/
        ├── base.html            # Shared layout, nav, theme controls
        ├── index.html           # Secret creation form
        └── retrieve.html        # Reveal / download page
```

---

## Database schema

```sql
CREATE TABLE secrets (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token       TEXT        NOT NULL UNIQUE,   -- one-time retrieval token
    ciphertext  BYTEA       NOT NULL,          -- AES-GCM encrypted envelope (metadata + payload)
    nonce       BYTEA       NOT NULL,          -- 12-byte GCM nonce
    expires_at  TIMESTAMPTZ,                   -- NULL = no expiry
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

The `ciphertext` contains an encrypted envelope of `{content_type, filename}` JSON header + raw file bytes, so the server never sees plaintext metadata either.

---

## API

The application exposes only two endpoints beyond static files:

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Secret creation form |
| `POST` | `/` | Submit encrypted secret; returns JSON `{"token":"…"}` |
| `GET` | `/s/{token}` | Retrieve page (serves ciphertext to browser for decryption) |

---

## Building

```bash
# Build binary
go build -o disapyr ./cmd/server

# Build Docker image
docker build -t disapyr-link .
```

---

## Blocked file types

The following file extensions are rejected client-side before encryption:

`exe` `msi` `bat` `cmd` `com` `pif` `scr` `vbs` `vbe` `wsh` `wsf` `ps1` `ps2` `jar` `app` `dmg` `pkg` `command` `sh` `run`

---

## License

MIT
