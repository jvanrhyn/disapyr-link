# Deploying disapyr-link to Virtuozzo

## Why it fails on Virtuozzo but works locally

The local `docker-compose.yml` uses Compose's built-in shell interpolation when it reads `.env`. So this line in `docker-compose.yml`:

```
DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
```

…is expanded by Compose *before* the value is passed to the container. The `${…}` placeholders become real values.

Your `.env` file however contains:

```
DATABASE_URL=postgres://disapyr:${POSTGRES_PASSWORD}@${DATABASE_HOST}:5432/disapyr?sslmode=disable
```

**Docker's `--env-file` flag and every platform UI/config system passes this literally — it does NOT expand `${POSTGRES_PASSWORD}`.** The binary receives the string `${POSTGRES_PASSWORD}` as the password, which is not a valid credential, so the database connection fails.

The fix is simple: `DATABASE_URL` must always be a fully-expanded literal string with no `${…}` references.

---

## The application reads environment variables

The binary reads all configuration from environment variables at startup via `os.Getenv`. It does **not** read any `.env` file from disk. The `.env` file is only used as a convenience file by `docker-compose` and your local shell.

Variables the app requires:

| Variable | Required | Example |
|---|---|---|
| `DATABASE_URL` | **Yes** | `postgres://user:pass@host:5432/dbname?sslmode=require` |
| `PORT` | No (default `8080`) | `8080` |
| `BASE_URL` | No (default `http://localhost:8080`) | `https://your.domain.com` |
| `MAX_SECRET_BYTES` | No (default `10485760`) | `10485760` |
| `LOG_LEVEL` | No (default `info`) | `info` |
| `LOG_DB_MIN_LEVEL` | No (default `warn`) | `warn` |
| `RATE_LIMIT_CREATE_PER_MIN` | No (default `10`) | `10` |
| `RATE_LIMIT_REVEAL_PER_MIN` | No (default `20`) | `20` |
| `RATE_LIMIT_HEALTH_PER_MIN` | No (default `5`) | `5` |
| `TRUST_PROXY` | No (default `false`) | `true` |
| `ADMIN_USER` | No | `admin` |
| `ADMIN_PASSWORD` | No | `strong-password` |

---

## Step 1 — Get the PostgreSQL connection details from Virtuozzo

When Virtuozzo provisions a PostgreSQL database for your application it exposes the connection details as environment variables **in your container**. The exact names depend on the Virtuozzo plan/version. Common patterns:

| Virtuozzo variable | Maps to |
|---|---|
| `DATABASE_URL` | Use directly if it is a full `postgres://…` URI |
| `POSTGRES_URL` | Same — use directly |
| `PG_HOST` / `PG_PORT` / `PG_USER` / `PG_PASSWORD` / `PG_DATABASE` | Assemble into `DATABASE_URL` |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | Assemble into `DATABASE_URL` |

**How to find the exact names on Virtuozzo:**

1. Open your application in the Virtuozzo dashboard.
2. Go to **Environment** or **Config Vars** (exact name varies by panel version).
3. Note every variable injected by the PostgreSQL add-on.

If the platform provides a full URI in a variable named anything other than `DATABASE_URL`, add an alias:

```
DATABASE_URL=<paste the full URI here>
```

---

## Step 2 — Build the Docker image

The `Dockerfile` at the repo root is already correct. It produces a statically compiled Go binary in a minimal Alpine image. No changes are needed.

```bash
# Tag with your registry and a version
docker build -t ghcr.io/jvanrhyn/disapyr-link:latest .

# Or tag with a specific version
docker build -t ghcr.io/jvanrhyn/disapyr-link:v1.0.0 .
```

### Authenticate to GitHub Container Registry (GHCR)

```bash
# Create a personal access token at https://github.com/settings/tokens
# with the 'write:packages' scope, then:
echo "<your-token>" | docker login ghcr.io -u jvanrhyn --password-stdin
```

### Push the image

```bash
docker push ghcr.io/jvanrhyn/disapyr-link:latest
```

---

## Step 3 — Configure environment variables on Virtuozzo

In the Virtuozzo dashboard for your application, set the following variables. **All values must be literal strings — no `${…}` syntax.**

### Minimum required set

```
DATABASE_URL=postgres://DB_USER:DB_PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require
PORT=8080
BASE_URL=https://your.domain.com
TRUST_PROXY=true
```

Replace `DB_USER`, `DB_PASSWORD`, `DB_HOST`, `DB_NAME` with the actual values from Step 1.

> **`sslmode=require`** — Virtuozzo managed PostgreSQL almost always requires SSL. If you use `sslmode=disable` the connection will be refused. Use `sslmode=require` unless the platform documentation explicitly says otherwise.

### Recommended additional variables

```
LOG_LEVEL=info
LOG_DB_MIN_LEVEL=warn
RATE_LIMIT_CREATE_PER_MIN=10
RATE_LIMIT_REVEAL_PER_MIN=20
RATE_LIMIT_HEALTH_PER_MIN=5
ADMIN_USER=admin
ADMIN_PASSWORD=<strong-random-password>
```

### What NOT to set

Do not set `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, or `DATABASE_HOST` as separate variables. The app does not read them — only `DATABASE_URL` is used for the database connection.

---

## Step 4 — Verify the configuration before starting

The binary has a `--print-config` flag that prints all resolved configuration without starting the server or connecting to the database. Use it to confirm the platform is injecting the right values.

In the Virtuozzo console/SSH:

```bash
./disapyr --print-config
```

Or from outside (run the image with your env vars set):

```bash
docker run --rm \
  -e DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require" \
  -e PORT=8080 \
  -e BASE_URL=https://your.domain.com \
  ghcr.io/jvanrhyn/disapyr-link:latest \
  --print-config
```

Expected output:

```
=== disapyr-link configuration ===
DATABASE_URL           = postgres://user:xxxxx@host:5432/db?sslmode=require
PORT                   = 8080
BASE_URL               = https://your.domain.com
...
ADMIN_PASSWORD         = ****
```

If `DATABASE_URL` shows `${POSTGRES_PASSWORD}` literally, the platform is not expanding shell references — go back to Step 3 and enter the fully expanded connection string.

---

## Step 5 — Run database migrations

The app requires two SQL migrations to be applied to the database before the first start. These are in `internal/db/migrations/`:

- `001_init.sql` — creates the `secrets` table
- `002_app_logs.sql` — creates the `app_logs` table

### Option A — Run migrations via psql (recommended for first deploy)

From any machine with `psql` and network access to your Virtuozzo PostgreSQL:

```bash
psql "postgres://DB_USER:DB_PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require" \
  -f internal/db/migrations/001_init.sql

psql "postgres://DB_USER:DB_PASSWORD@DB_HOST:5432/DB_NAME?sslmode=require" \
  -f internal/db/migrations/002_app_logs.sql
```

### Option B — Run migrations from inside the container

```bash
# Start a temporary container with psql
docker run --rm \
  -e PGPASSWORD=DB_PASSWORD \
  postgres:16-alpine \
  psql -h DB_HOST -U DB_USER -d DB_NAME -f /dev/stdin < internal/db/migrations/001_init.sql

# Repeat for 002_app_logs.sql
```

> Migrations are idempotent (`CREATE TABLE IF NOT EXISTS`) so re-running them is safe.

---

## Step 6 — Deploy the container

In the Virtuozzo dashboard:

1. Set the image to `ghcr.io/jvanrhyn/disapyr-link:latest` (or a pinned version tag).
2. Set the container port to `8080`.
3. Confirm all environment variables from Step 3 are set.
4. Deploy / restart the container.
5. Check the logs. A successful start looks like:

```json
{"time":"...","level":"INFO","msg":"database connected"}
{"time":"...","level":"INFO","msg":"server starting","addr":":8080"}
```

A failed database connection looks like:

```json
{"time":"...","level":"ERROR","msg":"connect to database","err":"..."}
```

---

## Troubleshooting

### "connect to database" error

1. Run `--print-config` and confirm `DATABASE_URL` does not contain `${…}` literals.
2. Confirm `sslmode=require` (not `disable`) in the URL.
3. Check the Virtuozzo firewall — the container must be allowed to reach the PostgreSQL host on port 5432.
4. Try connecting with `psql` using the same URL from outside the container to rule out a network issue.

### "relation does not exist" error

Migrations have not been applied. Run Step 5.

### App starts but returns 500 errors

Check logs. The admin health endpoint at `/health` (requires `ADMIN_USER`/`ADMIN_PASSWORD` Basic Auth) shows recent errors:

```
GET https://your.domain.com/health
Authorization: Basic <base64 of user:password>
```

### Rate limit errors immediately

If running behind an ingress/proxy, confirm `TRUST_PROXY=true` is set. Without it the app sees every request as coming from the proxy IP and rate-limits the proxy itself.

---

## Updating the deployment

1. Build a new image with a version tag: `docker build -t ghcr.io/jvanrhyn/disapyr-link:v1.x.x .`
2. Push: `docker push ghcr.io/jvanrhyn/disapyr-link:v1.x.x`
3. Update the image tag in the Virtuozzo dashboard and redeploy.

New migrations (if any) must be applied before or immediately after the new image starts. Check `internal/db/migrations/` for any files added since the last deploy.

---

## Summary of what docker-compose does that the platform doesn't

| What | docker-compose | Platform / `--env-file` |
|---|---|---|
| Expands `${VAR}` in values | ✅ Yes, during `up` | ❌ No — passes literal string |
| Reads `.env` automatically | ✅ Yes | ❌ No — you set vars in the UI |
| Starts postgres for you | ✅ Yes | ✅ Yes (managed service) |
| Runs migrations | ❌ No — you run them | ❌ No — you run them |

The binary itself is identical in both environments. The only thing that changes is how you provide the configuration.
