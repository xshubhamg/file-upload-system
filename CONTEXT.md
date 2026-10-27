# file-upload — Project Context

> Scope: this file is the single source of context for the `file-upload` project.
> Last updated: 2026-10-27. Everything outside this directory is out of scope.

## 1. What this is

An authenticated file-storage API. Users register or log in, initiate an upload
to obtain a presigned S3-compatible URL, PUT the bytes directly to object
storage, then confirm the upload. They can list, inspect, download, and delete
their own files. Metadata lives in Postgres 16; bytes live in object storage.
Legacy `/api/*` clients keep working through deprecated aliases.

**Current goal:** production readiness — correctness, security, operability,
tests, CI, and documentation.

## 2. Stack

- Go 1.27.x (installed: go1.27.3; `go.mod` declares 1.27.1)
- Gin v1.12 (HTTP) and GORM v1.31 with `gorm.io/driver/postgres`
- pgx v5 (driver and migration connection)
- `golang-jwt/jwt/v5` (HS256 access tokens)
- `golang.org/x/crypto/bcrypt` (configurable cost 10–14)
- `golang-migrate/migrate/v4` with embedded versioned SQL migrations
- `minio/minio-go/v7` for S3-compatible storage and presigning
- Prometheus client, `google/uuid`, `golang.org/x/time/rate`, `godotenv`
- Postgres 16 + S3-compatible storage; `adobe/s3mock` locally and in CI
- Docker/Compose for the local stack; CGO-free static binary on Alpine

SQLite, CGO, local disk storage, and GORM AutoMigrate were removed.

## 3. Current layout

```
cmd/server/main.go                 # config, DB, migrations, servers, janitor, shutdown
internal/config/                   # env parsing + validation
internal/database/                 # GORM/pgx pool, embedded migrations
internal/domain/                   # User, File, RefreshToken
internal/repository/               # interfaces + GORM implementations
internal/service/                  # auth and file use cases
internal/storage/                  # Store interface, S3 implementation, memory fake
internal/api/                      # router, middleware, handlers, response envelope
internal/observability/            # slog logger, Prometheus metrics
internal/version/                  # build metadata
internal/testutil/                 # in-memory fakes and test config
docs/openapi.yaml                  # API contract (embedded in the binary)
docs/operations.md                 # deploy, storage, backup, monitoring, incidents
docker-compose.yml                 # postgres + s3mock + api
.github/workflows/ci.yml           # vet + race tests + binary/container builds
```

## 4. API surface

Canonical base: `/api/v1`. Protected routes accept
`Authorization: Bearer <access_token>` or the `accessToken` cookie.

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/api/v1/auth/register` | no | username, email, password |
| POST | `/api/v1/auth/login` | no | username or email + password; returns token pair and sets cookies |
| POST | `/api/v1/auth/refresh` | no | rotates an opaque refresh token (body or cookie) |
| POST | `/api/v1/auth/logout` | no* | revokes the current refresh family; idempotent |
| POST | `/api/v1/auth/logout-all` | yes | revokes every refresh family for the user |
| GET | `/api/v1/auth/me` | yes | current user |
| POST | `/api/v1/files/uploads` | yes | creates a pending row + presigned PUT URL |
| POST | `/api/v1/files/{id}/complete` | yes | validates object and activates the file |
| GET | `/api/v1/files` | yes | active files, keyset pagination (`limit`, `cursor`) |
| GET | `/api/v1/files/{id}` | yes | metadata + presigned download URL |
| GET | `/api/v1/files/{id}/download` | yes | 302 redirect to presigned download URL |
| DELETE | `/api/v1/files/{id}` | yes | deletes object then row |

Legacy aliases keep their original paths and success shapes, and set
`Deprecation: true` plus a `Warning`/`Link` header:

| Method | Path |
|---|---|
| POST | `/api/auth/register`, `/api/auth/login`, `/api/auth/refresh`, `/api/auth/logout`, `/api/auth/logout-all` |
| GET | `/api/auth/me` |
| POST | `/api/file/upload` (multipart field `file`, server-mediated) |
| GET | `/api/file` and `/api/file/` |
| GET / DELETE | `/api/file/:filename` |

Operational endpoints:

- `/healthz` and `/readyz` are available on both the API and admin listeners.
- `/metrics` is admin-only and is not registered when `METRICS_ENABLED=false`.
- `/api/v1/openapi.yaml` serves the embedded contract; `/api/v1/docs` serves
  Swagger UI.

## 5. Current behavior and guarantees

- Passwords are bcrypt-hashed and never serialized.
- Access tokens are HS256 JWTs with `iss=file-upload`, `aud=file-upload-api`,
  a 15-minute default TTL, and a 30-second validation leeway.
- Refresh tokens are 256-bit opaque values. Only an HMAC-SHA-256 digest keyed
  with `JWT_REFRESH_SECRET` is stored. Rotation is atomic; replaying a consumed
  token revokes the whole family (`TOKEN_REUSE_DETECTED`).
- Logout revokes the refresh family; logout-all revokes every family for the
  user. Access tokens remain valid until expiry (stateless by design).
- Presigned uploads and downloads are generated with a separate public-endpoint
  client, because SigV4 signatures are host-specific.
- Upload completion re-checks object existence, non-empty size, the configured
  maximum size, and the actual stored content type. Rejected objects are
  deleted.
- Pending uploads older than `PENDING_UPLOAD_TTL` are removed by a janitor every
  `CLEANUP_INTERVAL`.
- File access is owner-scoped. Another user's file returns 404 rather than
  revealing its existence.
- IDs are UUID strings; legacy numeric IDs are a documented breaking change.
- Timestamps are RFC3339 JSON values. Errors use a stable envelope:
  `{ "error": { "code", "message", "details", "request_id" } }`.
- Rate limiting is per-process and per-IP, split into auth and file buckets.
- Request IDs, structured logs, Prometheus metrics, security headers, CORS,
  body limits, timeouts, health/readiness, migrations, and graceful shutdown are
  wired in.
- The Docker image is a static CGO-free Go binary on Alpine, running as UID
  10001. Compose host ports are configurable via `.env`.

## 6. Original audit gaps (all closed)

The pre-rebuild audit found: no tests, SQLite/local-disk storage, no refresh or
revocation, no rate limiting, no MIME allow-list, no request body limits, no
security headers/CORS, no health/readiness, no structured logging/request IDs,
no metrics, no graceful shutdown, no CI, no migrations, no versioned API, no
stable error codes, no OpenAPI, and no operations docs.

Each item is now addressed by the code and docs above, with unit/HTTP tests and
Postgres/S3 integration tests. The remaining deliberate limitations are listed
in section 7 and in `docs/operations.md`.

## 7. Non-goals (unless we change them)

- Multi-node horizontal scaling with shared local disk (storage is S3 now, but
  rate limits are still per-process).
- Video transcoding, thumbnails, antivirus scanning, or content moderation.
- Billing, quotas, team/shared folders, or an admin UI.
- Revoking already-issued access JWTs before expiry.
- Production use of S3Mock. It is a test double that does not validate
  signatures, expiry, or verbs.

## 8. Agreed production architecture (2026-10-27)

Decisions made with the project owner:

1. **Data + storage:** PostgreSQL 16 for metadata; S3-compatible object storage
   (S3Mock locally for tests, AWS S3 or compatible services in production) for
   file bytes. Uploads/downloads use presigned URLs so bytes never transit the
   API in the primary flow. MinIO images are no longer published on Docker Hub,
   so local Compose uses `adobe/s3mock`; S3Mock accepts presigned URLs without
   validating signatures and is not production storage.
2. **Auth:** short-lived access JWT + rotating opaque refresh tokens with reuse
   detection, per-session revocation, and logout-all. Refresh tokens are stored
   as HMAC-SHA-256 digests (peppered with `JWT_REFRESH_SECRET`) in Postgres.
3. **API contract:** `/api/v1` is the canonical surface. Legacy `/api/*` paths
   remain as deprecated aliases during a migration window.
4. **Delivery scope:** full hardening — tests (unit + integration + HTTP), CI,
   rate limiting, security headers/CORS, MIME allow-list, health/readiness,
   structured logs + request IDs, Prometheus metrics, graceful shutdown,
   versioned SQL migrations, Docker Compose, OpenAPI, and operations docs.
5. **Stack:** keep Gin + GORM. Add pgx (Postgres driver), minio-go, golang-migrate,
   prometheus client, google/uuid, x/time/rate. Drop SQLite/CGO.
6. **Migrations:** versioned SQL embedded in the binary; `AUTO_MIGRATE` defaults
   to true only in development. Production should run migrations as a release
   job and start replicas with `AUTO_MIGRATE=false`.
7. **Observability:** health, readiness, and metrics are served on a separate
   admin listener (`ADMIN_PORT`, default 9090). Health probes are also exposed
   on the public listener for simple container/load-balancer checks; metrics are
   admin-only.

### Compatibility

- Legacy routes keep their paths and success semantics.
- Legacy numeric IDs become UUID strings (documented breaking change); v1 is the
  recommended surface.
- Legacy multipart upload remains server-mediated for old clients; v1 uses
  presigned direct-to-S3 uploads.

## 9. Working agreements

- Keep Gin + GORM unless we explicitly agree to migrate.
- Every behavior change gets a test.
- No secrets in the repo; `.env` stays ignored.
- Update this file whenever scope or architecture changes.
