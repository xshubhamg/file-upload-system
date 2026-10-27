# Operations runbook

This document is for running `file-upload` outside a laptop. It complements the
README: configuration and API behavior live there; this file covers deployment,
storage policy, backups, monitoring, and incident response.

## 1. Deployment topology

```
Internet / ingress (TLS)
        │
        ▼
   API :8080          Admin :9090 (private)
        │                 │
        ├── Postgres 16 ──┤
        └── S3-compatible ─┘
```

- Expose only the API listener (`PORT`, default 8080) publicly.
- Keep the admin listener (`ADMIN_PORT`, default 9090) on a private network. It
  serves `/healthz`, `/readyz`, and `/metrics` without authentication.
- Terminate TLS at the ingress or load balancer. Forward `X-Forwarded-For` and
  `X-Forwarded-Proto`, and set `TRUSTED_PROXIES` to the ingress CIDRs. Do not set
  it to `0.0.0.0/0`.
- Set `COOKIE_SECURE=true` when the public URL is HTTPS.
- Use a dedicated Postgres database and role. Do not reuse the Compose
  credentials.

### Required production settings

```dotenv
APP_ENV=production
DATABASE_URL=postgres://fileupload:...@postgres.internal:5432/file_upload?sslmode=require
AUTO_MIGRATE=false                 # run migrations as a release job
JWT_ACCESS_SECRET=<32+ random chars>
JWT_REFRESH_SECRET=<32+ different random chars>
COOKIE_SECURE=true
CORS_ALLOWED_ORIGINS=https://app.example.com
TRUSTED_PROXIES=10.0.0.0/8
S3_ENDPOINT=s3.us-east-1.amazonaws.com
S3_PUBLIC_ENDPOINT=s3.us-east-1.amazonaws.com
S3_REGION=us-east-1
S3_BUCKET=file-upload-prod
S3_USE_SSL=true
S3_PUBLIC_USE_SSL=true
S3_AUTO_CREATE_BUCKET=false
S3_ACCESS_KEY=...
S3_SECRET_KEY=...
LOG_FORMAT=json
LOG_LEVEL=info
```

`AUTO_MIGRATE=false` is supported only after the schema has been migrated. The
service exits if the schema version is 0 or dirty, which prevents serving
traffic against a half-initialized database.

## 2. Object storage

### Bucket

Create the bucket before starting the service. Enable:

- Block Public Access.
- Default encryption (SSE-S3 or SSE-KMS).
- Versioning if you need recovery from accidental deletes.
- Lifecycle rules for incomplete multipart uploads and any temporary prefixes.

The service writes keys under:

```
users/<user-uuid>/<random-uuid>/<filename>
```

It never writes user-supplied paths. A filename is included for operator
readability but ownership comes from the first two UUID segments and the
database row.

### IAM policy

Scope credentials to the one bucket:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::file-upload-prod/users/*"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::file-upload-prod"
    }
  ]
}
```

`ListBucket` is only needed for the bucket-exists readiness check. If your
provider supports `HeadBucket` under a different action, adjust accordingly.

### Bucket CORS

Browsers uploading to a presigned PUT URL are making a cross-origin request to
the bucket. Without bucket CORS, uploads fail even though the API returned a
valid URL. Example for `https://app.example.com`:

```json
[
  {
    "AllowedOrigins": ["https://app.example.com"],
    "AllowedMethods": ["GET", "PUT"],
    "AllowedHeaders": ["Content-Type"],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3600
  }
]
```

Apply the equivalent rule in MinIO (`mc cors set`) or your S3-compatible
provider. S3Mock does not implement bucket CORS; use real storage for browser
upload tests. Presigned URLs expire after `PRESIGN_UPLOAD_TTL` (default 15m) and
`PRESIGN_DOWNLOAD_TTL` (default 5m).

### Public endpoint

SigV4 signatures include the host. If the API reaches object storage through an
internal address but clients reach it through a public address, set
`S3_PUBLIC_ENDPOINT` to the public host. Otherwise presigned URLs will point at
an unreachable address or fail signature validation.

## 3. Migrations

Migrations are embedded in the binary and use `golang-migrate` with its own
`schema_migrations` table and a Postgres advisory lock.

Release job (recommended):

```bash
AUTO_MIGRATE=true ./server
```

Run it once as a one-shot task before rolling out the new application version,
then start replicas with `AUTO_MIGRATE=false`. Concurrent replicas are safe if
you do leave auto-migrate enabled: golang-migrate serializes them.

Check the version from the application log (`database migrations applied`) or:

```sql
SELECT * FROM schema_migrations;
```

A dirty schema must be fixed manually. Back up first, inspect the failing
migration, repair the schema, and force the version only after understanding the
partial state.

## 4. Backups and restore

### Postgres

Take daily logical backups plus continuous WAL archiving if your provider
supports it:

```bash
pg_dump --format=custom --no-owner --file=file_upload.dump "$DATABASE_URL"
```

Restore into an empty database:

```bash
pg_restore --clean --if-exists --no-owner --dbname="$DATABASE_URL" file_upload.dump
```

Then start the service with `AUTO_MIGRATE=false` and verify `/readyz`.

### Object storage

Enable bucket versioning or replication. The database is the source of truth
for metadata; object bytes are not reconstructible from Postgres.

A consistent restore requires:

1. Restore Postgres to time T.
2. Restore object storage to the same point in time, or accept that objects
   created after T are missing and will 404 until re-uploaded.
3. Run the pending-upload cleanup and verify file counts.

### Secrets

Store `DATABASE_URL`, both JWT secrets, and S3 credentials in a secret manager.
Do not put them in the image, Compose file, or repository.

## 5. Monitoring

Scrape `http://<admin>:9090/metrics`. Alert on:

| Signal | Suggested threshold |
|---|---|
| `up{job="file-upload"} == 0` | 1 minute |
| `/readyz` non-200 | 2 consecutive checks |
| `http_requests_total{status=~"5.."}` rate | > 1% of requests for 5m |
| `http_request_duration_seconds` p95 | > 2s for 10m |
| `file_uploads_total{status="failed"}` | any sustained increase |
| `storage_operations_total{status="error"}` | any sustained increase |
| Postgres connections | > 80% of `DB_MAX_OPEN_CONNS` |
| Disk / bucket growth | capacity threshold |

Useful log fields: `request_id` (also returned to clients), `route`, `status`,
`duration_ms`, `user_id`, and `error`. Logs are JSON when `LOG_FORMAT=json`.

### Probes

- Liveness: `GET /healthz` on the API or admin listener.
- Readiness: `GET /readyz` checks Postgres and bucket existence.
- Do not use the public API listener for admin scraping if you can avoid it.

## 6. Scaling

The API is stateless; scale replicas horizontally behind a load balancer.

- Postgres is the shared state. Size the pool with
  `DB_MAX_OPEN_CONNS × replicas < Postgres max_connections`.
- Presigned URLs are generated per request and require no shared cache.
- Rate limiting is in-process and per replica. If you need global limits, put
  them at the ingress or use a shared store.
- The cleanup janitor runs in every replica. It is idempotent, but you can move
  it to a single worker if you prefer. Duplicate work only causes extra deletes
  that ignore missing objects.
- Access tokens are stateless. Revocation applies to refresh tokens and takes
  effect on the next refresh, not on an already-issued access token.

## 7. Incident response

### Suspected refresh-token theft

The service detects reuse automatically and revokes the session family. To
revoke everything for one user:

```bash
# No dedicated CLI yet; run against Postgres:
psql "$DATABASE_URL" -c \
  "UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = '<uuid>' AND revoked_at IS NULL;"
```

To revoke every session globally, rotate `JWT_REFRESH_SECRET` and restart. This
also invalidates stored refresh hashes. Rotate `JWT_ACCESS_SECRET` at the same
time to invalidate access tokens immediately.

### Object storage outage

`/readyz` returns 503 and new uploads/downloads fail. Existing metadata remains
intact. Do not delete rows manually. Restore storage access, then verify a
sample of objects with `Stat` (or the provider console). The janitor only
removes pending rows and their objects, so active files are not touched.

### Database outage

The API cannot serve authenticated requests. Keep the process running so its
readiness probe fails and the orchestrator stops routing traffic, or let the
orchestrator replace it. Reconnect after Postgres returns; the pool recovers.

### Accidental deletion

If bucket versioning is enabled, restore the object version. The database row is
deleted after the object, so a failed row delete means the object may already be
gone; re-upload is required. If the row still exists but the object is gone,
`GET /download` returns 404 from storage.

### Stuck pending uploads

Pending uploads are removed after `PENDING_UPLOAD_TTL` by a janitor running
every `CLEANUP_INTERVAL`. To trigger a check, restart or wait for the next
interval. A failed delete is logged and retried on the next cycle.

## 8. Rate limits and abuse

Per-IP token buckets are applied to auth routes and file routes separately:

- `AUTH_RATE_LIMIT_RPS` / `AUTH_RATE_LIMIT_BURST` for register/login/refresh.
- `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` for file endpoints.

Behind a proxy, set `TRUSTED_PROXIES`; otherwise every request appears to come
from the proxy IP and one client can exhaust the bucket for everyone. If you
need distributed limits, enforce them at the ingress (e.g. per-IP or per-token)
and keep these as a backstop.

## 9. Maintenance checklist

Weekly:

- Check error-rate and latency dashboards.
- Review 5xx logs by `request_id`.
- Confirm Postgres backups completed and test one restore quarterly.
- Check bucket lifecycle rules and storage growth.

Before a release:

- `go test -race ./...`
- `go vet ./...`
- Build the image with a pinned version and commit.
- Run migrations as a one-shot job.
- Roll out one replica, verify `/readyz`, then continue.
- Keep the previous image available for rollback.

After a secret rotation:

- Restart every replica so both JWT secrets are consistent.
- Expect all users to log in again (refresh tokens are invalidated).
- Verify `/readyz` and a login/upload smoke test.
