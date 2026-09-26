# File Upload API (Go + Gin + GORM)

A minimal authenticated file-upload backend. Register/login with JWT (cookie or Bearer token), upload files up to 100MB, list/download/delete your own files. Storage is local disk (`./uploads/<userID>/`) with metadata in SQLite via GORM.

## Features

- Auth: register, login, logout, `me` — bcrypt passwords, HS256 JWT (15 min expiry)
- Files: multipart upload, list, download, delete — scoped per user
- Safety: filename sanitization, per-user dirs keyed by user ID, byte-exact 100MB limit, orphan cleanup on DB failure, 409 on duplicates/races
- Persistence: SQLite + AutoMigrate (`User`, `File`)
- Deployable: `Makefile`, `Dockerfile` (CGO/sqlite-ready), `.env.example`

## Tech stack

- Go 1.27, Gin, GORM + SQLite driver, golang-jwt v5, godotenv, bcrypt

## Project structure

```text
.
├── main.go                    # env load, JWT + DB init, route setup
├── controllers/
│   ├── auth.controllers.go    # login / register / logout / me
│   └── file.controllers.go    # upload / list / get / delete
├── middlewares/
│   └── auth.middleware.go     # cookie-or-Bearer JWT gate
├── models/
│   ├── user.model.go
│   └── file.model.go
├── routes/
│   ├── init.go                # mounts /api
│   ├── auth.routes.go
│   └── file.routes.go
├── utils/
│   ├── db.go                  # SQLite connect + migrate
│   ├── token.go               # HS256 issue/validate, issuer check
│   ├── auth_helpers.go        # cookies, TokenFromRequest, unique-error helper
│   ├── current_user.go        # CurrentUser(c) helper
│   ├── storage.go             # UserDir, SafeFilename, FileURL, limits
│   └── validation.go          # username / password rules
├── Dockerfile
├── Makefile
├── .env.example
└── uploads/                   # runtime only, gitignored
```

## Quickstart

Requirements: Go 1.27+.

```bash
git clone <your-repo-url> file-upload
cd file-upload

cp .env.example .env
# edit .env — set a strong JWT_SECRET, e.g.:
# openssl rand -base64 48

go mod tidy
go run .
# or: make run
```

Server defaults to `:8080` (`PORT` overrides).

## Configuration

| Var | Required | Default | Notes |
|-----|----------|---------|-------|
| `PORT` | No | `8080` | Listen port |
| `JWT_SECRET` | Yes | — | HMAC secret; app panics if unset. Generate with `openssl rand -base64 48` |

SQLite file `db.sqlite` is created on first run. Uploads go to `./uploads/<userID>/`.

## API reference

Base URL: `http://localhost:8080/api`

Auth: login sets an HttpOnly `accessToken` cookie. Every protected route also accepts `Authorization: Bearer <token>`.

### Auth

#### `POST /api/auth/register`

```bash
curl -s http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo_user","email":"demo@example.com","password":"secret123"}'
```

- `201` → `{"id":1,"username":"demo_user","email":"demo@example.com"}`
- `400` → missing fields, bad username (`3-32 chars, [a-zA-Z0-9_-]`), bad password (`8-72 chars`)
- `409` → username or email taken (includes concurrent-register race)

#### `POST /api/auth/login`

```bash
curl -si http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo_user","password":"secret123"}'
```

- `200` → `{"message":"Logged in successfully!"}` + `Set-Cookie: accessToken=...`
- `401` → invalid credentials (generic message either way)

Save the cookie or copy the token for Bearer use below.

#### `POST /api/auth/logout` (auth)

```bash
curl -s -X POST http://localhost:8080/api/auth/logout \
  -H "Authorization: Bearer $TOKEN" -c cookies.txt -b cookies.txt
```

Clears the cookie. `200` → `{"message":"Logout successfully!"}`.

#### `GET /api/auth/me` (auth)

```bash
curl -s http://localhost:8080/api/auth/me -H "Authorization: Bearer $TOKEN"
```

- `200` → `{"id":1,"username":"demo_user","email":"demo@example.com"}`
- `401` / `404` → no/invalid token, user deleted

### Files

All file routes require auth.

#### `POST /api/file/upload`

Multipart field name must be `file`. Limit is exactly `100 * 1024 * 1024` bytes.

```bash
curl -s http://localhost:8080/api/file/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F file=@./notes.pdf
```

- `201` → `{"message":"File uploaded successfully","url":"/api/file/notes.pdf"}`
- `400` → no file, too large, invalid filename
- `409` → file with this name already exists (re-upload returns conflict, disk file cleaned up)
- Response `url` is relative to host: `GET http://localhost:8080/api/file/notes.pdf` with auth

#### `GET /api/file` and `GET /api/file/`

```bash
curl -s http://localhost:8080/api/file -H "Authorization: Bearer $TOKEN"
```

- `200` → array of `File` rows (`id`, `user_id`, `filename`, `path`, `size`, timestamps)
- Both `/api/file` and `/api/file/` work

#### `GET /api/file/:filename`

```bash
curl -OJ http://localhost:8080/api/file/notes.pdf -H "Authorization: Bearer $TOKEN"
```

- `200` → file bytes
- `400` → invalid filename (`.`, `..`, separators)
- `404` → not found

#### `DELETE /api/file/:filename`

```bash
curl -s -X DELETE http://localhost:8080/api/file/notes.pdf \
  -H "Authorization: Bearer $TOKEN"
```

- `200` → `{"message":"File deleted successfully!"}`
- `404` → no such record for this user

## Development

```bash
make vet    # go vet ./...
make build  # builds ./server
make test   # go test ./...
make clean  # removes ./server, db.sqlite, uploads/
```

Cookie expiry (`utils.AccessTokenTTLSeconds`) must stay in sync with `GenerateToken` (15 min).

## Docker

```bash
cp .env.example .env   # set real JWT_SECRET
docker build -t file-upload .
docker run --rm -p 8080:8080 --env-file .env \
  -v $(pwd)/uploads:/app/uploads file-upload
# or: make docker-build && make docker-run
```

The image is multi-stage: Go 1.27-alpine builder with `gcc/musl-dev/sqlite-dev` for `mattn/go-sqlite3` (CGO), slim Alpine runtime with `sqlite-libs`. `PORT` defaults to `8080`.

## Security notes

- Filenames pass through `SafeFilename` (`filepath.Base` + reject `.`/`..`/`/`/`\`); storage is `./uploads/<userID>/`, never raw username.
- JWT must be exactly HS256 with issuer `file-upload`; missing/invalid/expired tokens are 401.
- Passwords are bcrypt-hashed, never serialized (`json:"-"`).
- Do not commit `.env`, `*.sqlite`, or `uploads/` — all gitignored. Rotate `JWT_SECRET` if ever leaked.

## License

MIT — see [LICENSE](LICENSE).
