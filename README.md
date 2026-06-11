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
| Rate limiting | Per-IP token-bucket limiter on write endpoints (`POST /` and `POST /s/{token}/reveal`) and admin endpoints; configurable per route via env vars; responds with `Retry-After` header on 429 |

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
- **Per-IP rate limiting** — token-bucket limiter on write and admin endpoints; configurable per route with `Retry-After` response headers
- **Admin `/health` page** — system status (uptime, DB pool stats), secret activity counters, and paginated log browser; disabled by default, enabled with `ADMIN_USER` + `ADMIN_PASSWORD`
- **Application log persistence** — structured logs at or above `LOG_DB_MIN_LEVEL` are written to the `app_logs` DB table and browsable from the admin page
- **Background expiry cleanup** — a goroutine runs every 15 minutes to hard-delete expired secrets from the database

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
| Frontend | Vanilla JS + Go templates; [htmx](https://htmx.org) for admin partial updates |
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

# 3. Apply the schema (three migrations)
psql "$DATABASE_URL" -f internal/db/migrations/001_init.sql
psql "$DATABASE_URL" -f internal/db/migrations/002_app_logs.sql
psql "$DATABASE_URL" -f internal/db/migrations/003_secret_events.sql

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
| `RATE_LIMIT_CREATE_PER_MIN` | `10` | Per-IP rate limit for secret creation (`POST /`), req/min; `0` = disabled |
| `RATE_LIMIT_REVEAL_PER_MIN` | `20` | Per-IP rate limit for secret reveal (`POST /s/{token}/reveal`), req/min; `0` = disabled |
| `RATE_LIMIT_HEALTH_PER_MIN` | `5` | Per-IP rate limit for admin `/health` endpoints, req/min; `0` = disabled |
| `TRUST_PROXY` | `false` | When `true`, reads client IP from `X-Forwarded-For` (use only behind a trusted reverse proxy) |
| `TRUSTED_PROXIES_COUNT` | `1` | Number of trusted proxies in front of the application to skip when parsing `X-Forwarded-For` |
| `LOG_DB_MIN_LEVEL` | `warn` | Minimum slog level written to the `app_logs` DB table (`debug`, `info`, `warn`, `error`) |
| `ADMIN_USER` | *(empty)* | Admin username for `/health` HTTP Basic Auth; both must be set to enable the admin interface |
| `ADMIN_PASSWORD` | *(empty)* | Admin password for `/health` HTTP Basic Auth |

---

## Project structure

```
disapyr-link/
├── cmd/server/          # Entry point — wires dependencies and starts HTTP server
├── internal/
│   ├── config/          # Environment variable parsing
│   ├── db/
│   │   ├── db.go        # pgx connection pool
│   │   └── migrations/  # SQL schema (001_init.sql, 002_app_logs.sql, 003_secret_events.sql)
│   ├── handler/         # HTTP handlers, CSP headers, rate limiter, admin interface
│   ├── logger/          # DB log handler — fans out slog records to stdout + app_logs table
│   ├── model/           # Domain types (Secret, CreateSecretInput)
│   ├── repository/      # Database access (Store, FetchAndDelete)
│   └── service/         # Business logic (Create, Retrieve, token generation)
└── web/
    ├── static/
    │   ├── css/style.css        # Design system — light/dark tokens, component styles
    │   ├── fonts/               # JetBrains Mono and Syne web fonts
    │   └── js/
    │       ├── crypto.js        # AES-GCM encrypt/decrypt, file validation, drag-drop, copy
    │       ├── effects.js       # UI effects, theme toggle state machine
    │       ├── htmx.min.js      # htmx (used for admin log browser partial updates)
    │       └── theme-init.js    # Synchronous FOUC-prevention script (no defer)
    └── templates/
        ├── base.html                    # Shared layout, nav, theme controls
        ├── health.html                  # Admin status + log browser page
        ├── health_logs_partial.html     # HTMX partial for log filtering/pagination
        ├── index.html                   # Secret creation form
        └── retrieve.html                # Reveal / download page
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

## `app_logs` schema

Application log records written by the structured logger (levels at or above `LOG_DB_MIN_LEVEL`):

```sql
CREATE TABLE app_logs (
    id     BIGSERIAL    PRIMARY KEY,
    ts     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    level  TEXT         NOT NULL,
    msg    TEXT         NOT NULL,
    attrs  JSONB
);
```

Indexed on `ts DESC` and `level` for the admin log browser queries. Applied by `002_app_logs.sql`.

---

## `secret_events` schema

Tracks secret lifecycle events for the health page activity counters:

```sql
CREATE TABLE secret_events (
    id         BIGSERIAL    PRIMARY KEY,
    event_type TEXT         NOT NULL CHECK (event_type IN ('created', 'retrieved', 'expired')),
    count      BIGINT       NOT NULL DEFAULT 1,
    ts         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
```

`count > 1` is used by batch expiry cleanup runs so one row is emitted per cleanup cycle rather than one row per expired secret. Applied by `003_secret_events.sql`.

---

## API

The application exposes the following endpoints beyond static files:

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Secret creation form |
| `POST` | `/` | Submit encrypted secret; returns JSON `{"token":"…"}` (rate limited) |
| `GET` | `/s/{token}` | Retrieve page (serves ciphertext to browser for decryption) |
| `POST` | `/s/{token}/reveal` | Fetch-and-delete the encrypted payload; browser decrypts it client-side (rate limited) |
| `GET` | `/health` | Admin status page + log browser (HTTP Basic Auth required) |
| `GET` | `/health/logs` | HTMX partial for log filtering and pagination (Basic Auth required) |
| `POST` | `/health/logs/clear` | Delete application logs, optionally filtered by age (Basic Auth required) |

---

## Building

```bash
# Build binary
go build -o disapyr ./cmd/server

# Build Docker image
docker build -t disapyr-link .
```

The server accepts a `--print-config` flag to print the resolved configuration (with sensitive values redacted) and exit:

```bash
./disapyr --print-config
# or without building:
go run ./cmd/server --print-config
```

---

## Blocked file types

The following file extensions are rejected client-side before encryption:

`exe` `msi` `bat` `cmd` `com` `pif` `scr` `vbs` `vbe` `wsh` `wsf` `ps1` `ps2` `ps1xml` `ps2xml` `psc1` `psc2` `jar` `app` `dmg` `pkg` `command` `sh` `run`

---

## License

MIT
