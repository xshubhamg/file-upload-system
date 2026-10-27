# file-upload

Production-shaped, authenticated file storage API written in Go.

Users register and log in, then upload files **directly to S3-compatible object
storage** using short-lived presigned URLs. The API stores metadata in Postgres,
issues rotating refresh tokens, and never proxies file bytes in the primary
upload/download flow.

<p align="center">
  <img src="docs/assets/file-upload-hero.png" alt="file-upload — authenticated direct-to-S3 file storage API in Go" width="100%">
</p>

<p align="center">
  <img src="docs/assets/file-upload-infographic.png" alt="file-upload architecture infographic: a client uploads directly to S3 with a presigned URL while a Go API manages authentication and Postgres metadata" width="520">
</p>

```
┌─────────┐   1. initiate upload (JWT)     ┌──────────────┐
│ Client  │ ─────────────────────────────► │  Go API      │
│         │ ◄───────────────────────────── │  Gin + GORM  │
│         │   2. presigned PUT URL         └──────┬───────┘
│         │                                       │ metadata
│         │   3. PUT bytes directly               ▼
│         │ ─────────────────────────────► ┌──────────────┐
│         │                                │  Postgres    │
│         │   4. complete (JWT)            └──────────────┘
│         │ ─────────────────────────────►
│         │                                ┌──────────────┐
│         │   5. GET presigned URL         │ S3-compatible│
│         │ ◄───────────────────────────── │ object store │
└─────────┘                                └──────────────┘
```

## Features

- **Auth** — bcrypt passwords, short-lived HS256 access JWT, opaque refresh
  tokens with rotation, reuse detection, per-session and logout-all revocation.
  Tokens are returned in JSON and set as HttpOnly cookies.
- **Files** — initiate → presigned PUT → complete → list/get/download/delete.
  Metadata is owned by the user and isolated by user ID.
- **Storage** — S3-compatible object storage. S3Mock for local development and
  tests, AWS S3 (or MinIO/other compatible services) in production. Separate
  internal/public endpoints so presigned URLs work both inside Docker and from
  browsers. S3Mock accepts presigned URLs without validating signatures, so it
  is test-only.
- **Safety** — MIME allow-list, filename sanitization, size limits verified
  again after upload, pending-upload cleanup, object cleanup on DB failures.
- **Operations** — Postgres migrations, structured JSON logs with request IDs,
  Prometheus metrics, liveness/readiness probes, graceful shutdown, request
  timeouts, per-IP rate limits, security headers, explicit CORS.
- **Contract** — OpenAPI 3 at `/api/v1/openapi.yaml` and Swagger UI at
  `/api/v1/docs`. Legacy `/api/*` routes remain as deprecated aliases.
- **Quality** — unit, HTTP, repository, and object-storage integration tests,
  race detector, `go vet`, and GitHub Actions CI.

## Stack

Go 1.27 · Gin · GORM · Postgres 16 (pgx) · S3-compatible storage ·
golang-migrate · Prometheus · slog

## Quick start (Docker Compose)

Requirements: Docker with Compose v2.

```bash
cp .env.example .env
# Optional: replace the two JWT secrets in .env with:
#   openssl rand -base64 48

docker compose up --build -d
docker compose ps
```

The API is at `http://localhost:8080`, and the admin listener is at
`http://localhost:9090`. S3Mock listens on `http://localhost:9091`; it has no
web console. The Compose stack creates the `file-upload` bucket automatically.

S3Mock is a test double: it accepts presigned URLs without validating the
signature, expiry, or HTTP verb. That is fine for local development, but use
real S3-compatible storage in production.

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:9090/readyz
```

Create an account and log in:

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo_user","email":"demo@example.com","password":"correct horse battery"}'

curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo_user","password":"correct horse battery"}'
```

Store the returned `access_token` and `refresh_token` securely. The same values
are also set as `accessToken` / `refreshToken` cookies.

### Upload a file (presigned flow)

```bash
TOKEN=...  # access_token from login

# 1. Initiate. The response contains file.id and upload.url.
curl -s -X POST http://localhost:8080/api/v1/files/uploads \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"filename":"hello.txt","content_type":"text/plain","size_bytes":5}'

# 2. PUT the bytes to upload.url with the returned Content-Type header.
curl -s -X PUT "<upload.url>" \
  -H 'Content-Type: text/plain' \
  --data-binary 'hello'

# 3. Complete. The API stats the object, verifies size/type, and activates it.
curl -s -X POST "http://localhost:8080/api/v1/files/<file.id>/complete" \
  -H "Authorization: Bearer $TOKEN"

# 4. List / get / download / delete.
curl -s "http://localhost:8080/api/v1/files?limit=20" -H "Authorization: Bearer $TOKEN"
curl -s "http://localhost:8080/api/v1/files/<file.id>" -H "Authorization: Bearer $TOKEN"
curl -s -L "http://localhost:8080/api/v1/files/<file.id>/download" -H "Authorization: Bearer $TOKEN" -o hello.txt
curl -s -X DELETE "http://localhost:8080/api/v1/files/<file.id>" -H "Authorization: Bearer $TOKEN"
```

`complete` is idempotent. Objects that are missing, empty, too large, or of a
disallowed MIME type are rejected; rejected objects are deleted from storage.

## Local development without containerizing the API

Start dependencies only:

```bash
cp .env.example .env
docker compose up -d postgres s3mock
go run ./cmd/server
```

Or run everything in Compose (`docker compose up --build`) and use the host
ports from `.env`.

Useful targets:

```bash
make help
make test                 # unit + HTTP tests
make test-race            # race detector
make test-integration     # also requires the Compose dependencies
make vet
make build
make coverage
make compose-up / make compose-down
```

## Configuration

Configuration is environment-only and validated at startup. The process exits
on missing or invalid values. See `.env.example` for a complete template.

### Core

| Variable | Required | Default | Notes |
|---|---|---|---|
| `APP_ENV` | no | `development` | `development`, `test`, `production` |
| `APP_NAME` | no | `file-upload` | Used in logs |
| `PORT` | no | `8080` | Public API listener |
| `ADMIN_PORT` | no | `9090` | Health + metrics listener |
| `LOG_LEVEL` | no | `debug` (dev), `info` (prod) | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | no | `text` (dev), `json` (prod) | `text` or `json` |
| `REQUEST_TIMEOUT` | no | `30s` | Per-request context deadline |
| `SHUTDOWN_TIMEOUT` | no | `15s` | Graceful drain deadline |

### Database

| Variable | Required | Default | Notes |
|---|---|---|---|
| `DATABASE_URL` | **yes** | — | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `DB_MAX_OPEN_CONNS` | no | `25` | Pool size |
| `DB_MAX_IDLE_CONNS` | no | `5` | Idle connections |
| `DB_CONN_MAX_LIFETIME` | no | `30m` | Connection recycling |
| `AUTO_MIGRATE` | no | `true` (dev), `false` (test/prod) | Run embedded migrations at startup |

### Authentication

| Variable | Required | Default | Notes |
|---|---|---|---|
| `JWT_ACCESS_SECRET` | **yes** | — | ≥32 chars; signs access JWTs |
| `JWT_REFRESH_SECRET` | **yes** | — | ≥32 chars; HMAC pepper for refresh hashes |
| `ACCESS_TOKEN_TTL` | no | `15m` | Keep short |
| `REFRESH_TOKEN_TTL` | no | `720h` | 30 days |
| `BCRYPT_COST` | no | `12` | 10–14 |
| `COOKIE_SECURE` | no | `false` (dev), `true` (prod) | Set true behind HTTPS |
| `COOKIE_DOMAIN` | no | empty | Host-only cookies |

### Object storage

| Variable | Required | Default | Notes |
|---|---|---|---|
| `S3_ENDPOINT` | **yes** | `localhost:9091` | Internal endpoint (Put/Stat/Delete) |
| `S3_PUBLIC_ENDPOINT` | no | same as `S3_ENDPOINT` | Host clients use for presigned URLs |
| `S3_REGION` | no | `us-east-1` | Signing region |
| `S3_BUCKET` | **yes** | `file-upload` | Must exist unless auto-create is on |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | **yes** | — | Use an IAM user limited to the bucket |
| `S3_USE_SSL` / `S3_PUBLIC_USE_SSL` | no | `false` | Enable for AWS S3 |
| `S3_AUTO_CREATE_BUCKET` | no | `true` (dev), `false` (prod) | Create bucket at startup |
| `PRESIGN_UPLOAD_TTL` | no | `15m` | Maximum 1h |
| `PRESIGN_DOWNLOAD_TTL` | no | `5m` | Maximum 1h |

### Uploads and cleanup

| Variable | Required | Default | Notes |
|---|---|---|---|
| `MAX_UPLOAD_BYTES` | no | `104857600` (100 MiB) | Initiate + complete enforce this |
| `MAX_JSON_BODY_BYTES` | no | `1048576` | JSON request body cap |
| `ALLOWED_MIME_TYPES` | no | common images, PDF, text, CSV, JSON, ZIP, MP4, MP3 | Comma-separated exact MIME types |
| `PENDING_UPLOAD_TTL` | no | `1h` | Abandoned uploads older than this are removed |
| `CLEANUP_INTERVAL` | no | `10m` | Janitor cadence |

### HTTP hardening

| Variable | Required | Default | Notes |
|---|---|---|---|
| `CORS_ALLOWED_ORIGINS` | **prod** | `http://localhost:3000` (dev) | Comma-separated; `*` is rejected |
| `TRUSTED_PROXIES` | no | `127.0.0.1,::1` (dev) | CIDRs/IPs allowed to set `X-Forwarded-For` |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | no | `20` / `40` | Per-IP general limit |
| `AUTH_RATE_LIMIT_RPS` / `AUTH_RATE_LIMIT_BURST` | no | `5` / `10` | Per-IP auth limit |
| `METRICS_ENABLED` | no | `true` | When false, metric collection is disabled and `/metrics` is not registered |

> `S3_PUBLIC_ENDPOINT` matters: SigV4 URLs are host-specific. In Docker the API
> reaches S3Mock at `s3mock:9090`, but a browser must use `localhost:9091`, so
> presigning uses the public client.

## API

Canonical base: `/api/v1`. All protected routes accept
`Authorization: Bearer <access_token>` or the `accessToken` cookie.

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/v1/auth/register` | no | Create account |
| POST | `/api/v1/auth/login` | no | Get access + refresh tokens |
| POST | `/api/v1/auth/refresh` | no | Rotate refresh token (body or cookie) |
| POST | `/api/v1/auth/logout` | no* | Revoke current session family |
| POST | `/api/v1/auth/logout-all` | yes | Revoke every session |
| GET | `/api/v1/auth/me` | yes | Current user |
| POST | `/api/v1/files/uploads` | yes | Initiate presigned upload |
| POST | `/api/v1/files/{id}/complete` | yes | Validate and activate upload |
| GET | `/api/v1/files` | yes | List active files (`limit`, `cursor`) |
| GET | `/api/v1/files/{id}` | yes | Metadata + presigned download URL |
| GET | `/api/v1/files/{id}/download` | yes | 302 redirect to presigned URL |
| DELETE | `/api/v1/files/{id}` | yes | Delete object and row |

\* `logout` is idempotent and accepts a refresh token from the body or cookie.

Legacy aliases (same handlers, `Deprecation: true` response header):

| Method | Path |
|---|---|
| POST | `/api/auth/register`, `/api/auth/login`, `/api/auth/refresh`, `/api/auth/logout`, `/api/auth/logout-all` |
| GET | `/api/auth/me` |
| POST | `/api/file/upload` (multipart field `file`, server-mediated) |
| GET | `/api/file` and `/api/file/` |
| GET / DELETE | `/api/file/:filename` |

### Error envelope

Every error uses one shape, including a request ID that also appears in the
`X-Request-ID` header:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "username must be 3-32 characters",
    "details": { "username": "must be 3-32 characters" },
    "request_id": "8f9c1d..."
  }
}
```

Stable codes include `VALIDATION_ERROR`, `UNAUTHORIZED`,
`INVALID_ACCESS_TOKEN`, `INVALID_REFRESH_TOKEN`, `TOKEN_REUSE_DETECTED`,
`TOKEN_EXPIRED`, `FORBIDDEN`, `NOT_FOUND`, `USERNAME_OR_EMAIL_TAKEN`,
`FILE_ALREADY_EXISTS`, `UPLOAD_NOT_FOUND`, `UPLOAD_NOT_COMPLETED`,
`EMPTY_UPLOAD`, `PAYLOAD_TOO_LARGE`, `UNSUPPORTED_MEDIA_TYPE`,
`TOO_MANY_REQUESTS`, `REQUEST_TIMEOUT`, `INTERNAL_ERROR`.

### Auth semantics

- Access tokens are stateless JWTs (`iss=file-upload`, `aud=file-upload-api`,
  HS256, 15 minutes by default). Logout cannot revoke an already-issued access
  token; the short TTL bounds the window.
- Refresh tokens are random 256-bit values. Only an HMAC-SHA-256 digest is
  stored. Each refresh rotates the token; replaying a consumed token revokes
  the entire session family (`TOKEN_REUSE_DETECTED`).
- Each login starts a new family. `logout` revokes the family; `logout-all`
  revokes every family for the user.
- Cookies are HttpOnly and `SameSite=Lax`; set `COOKIE_SECURE=true` behind TLS.
  Cross-site browser clients should use Bearer tokens (or deploy on the same
  site) because `SameSite=None` is not enabled by default.

### Pagination

`GET /api/v1/files?limit=20&cursor=...` uses keyset pagination ordered by
`created_at DESC, id DESC`. Pass the returned `next_cursor` unchanged. Limit is
clamped to 100.

## Migrations

SQL migrations are embedded and applied by `golang-migrate` with a Postgres
advisory lock:

```bash
AUTO_MIGRATE=true go run ./cmd/server
```

In production you can disable `AUTO_MIGRATE` and run migrations as a release
job, but the process refuses to start when the schema version is 0 or dirty.
The service uses its own `schema_migrations` table.

## Observability

| Endpoint | Listener | Purpose |
|---|---|---|
| `/healthz` | API and admin | Liveness; process is running |
| `/readyz` | API and admin | Postgres ping + bucket existence |
| `/metrics` | admin only | Prometheus metrics |

JSON logs include `request_id`, `method`, `path`, `route`, `status`,
`duration_ms`, `bytes`, `client_ip`, and `user_id` when authenticated. Tokens,
passwords, and file contents are never logged.

## Testing

```bash
make test          # unit + HTTP, no external services
make test-race
make test-integration  # Postgres + S3Mock via Compose
```

Integration tests skip unless `TEST_DATABASE_URL` and/or `TEST_S3_ENDPOINT` are
set. `make test-integration` supplies defaults for the Compose stack.

`make test-integration` uses the default Compose ports. If your `.env` changes
`POSTGRES_HOST_PORT` or `S3MOCK_HOST_PORT`, override the matching `TEST_*`
variables (or edit the Makefile defaults).

CI (`.github/workflows/ci.yml`) runs formatting checks, `go vet`, race tests,
and a build on every push and pull request, with Postgres and S3Mock services.

## Docker

```bash
make docker-build VERSION=v1.0.0
# Adjust host ports to match your .env; the dev file points at localhost, which
# is the container itself, so the dependency addresses are overridden here.
docker run --rm -p 8080:8080 -p 9090:9090 \
  --add-host=host.docker.internal:host-gateway \
  --env-file .env \
  -e DATABASE_URL='postgres://fileupload:fileupload@host.docker.internal:5432/file_upload?sslmode=disable' \
  -e S3_ENDPOINT='host.docker.internal:9091' \
  -e S3_PUBLIC_ENDPOINT='localhost:9091' \
  file-upload:v1.0.0
```

The image is a static Go binary on Alpine, runs as UID 10001, and has a
`HEALTHCHECK` against `/healthz`. Compose additionally runs it read-only with
all capabilities dropped and `/tmp` on tmpfs.

## Migration from the legacy SQLite version

- Numeric IDs became UUID strings. Legacy endpoints keep working, but clients
  that parse `id` or `user_id` as numbers must treat them as strings.
- Metadata moved from SQLite to Postgres. There is no automatic data import;
  migrate rows separately if you need to preserve existing files.
- New uploads go to S3-compatible storage. Legacy `POST /api/file/upload` still accepts
  multipart and uploads server-side, but stores the object in S3.
- `JWT_SECRET` is replaced by `JWT_ACCESS_SECRET` and `JWT_REFRESH_SECRET`.
- Access tokens are no longer valid across a secret rotation; refresh tokens
  are invalidated when `JWT_REFRESH_SECRET` changes.

## Security notes

- Terminate TLS in front of the API and set `COOKIE_SECURE=true` and correct
  `TRUSTED_PROXIES`.
- Restrict the S3 credentials to the bucket and the operations used:
  `GetObject` (which also authorizes `HeadObject`), `PutObject`,
  `DeleteObject`, and `ListBucket` if you use bucket checks.
- Configure bucket CORS for browser presigned PUT/GET from your web origin.
- Keep `MAX_UPLOAD_BYTES` and the MIME allow-list aligned with product needs.
  Complete re-validates size and content type because presigned PUT cannot
  enforce headers by itself.
- The API does not scan for malware. Add a scanner hook before `complete` if
  you accept untrusted content.
- Rotate both JWT secrets if either leaks; this invalidates all sessions.
- Never commit `.env`; it is ignored.

## License

MIT — see [LICENSE](LICENSE).
