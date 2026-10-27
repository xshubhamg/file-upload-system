.PHONY: help run build test test-race test-integration vet fmt tidy clean coverage \
	docker-build docker-up docker-down compose-up compose-down

APP        := server
PKG        := ./cmd/server
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X github.com/xshubhamg/file-upload/internal/version.Version=$(VERSION) \
	-X github.com/xshubhamg/file-upload/internal/version.Commit=$(COMMIT) \
	-X github.com/xshubhamg/file-upload/internal/version.BuildDate=$(BUILD_DATE)

TEST_DATABASE_URL ?= postgres://fileupload:fileupload@localhost:5432/file_upload_test?sslmode=disable
TEST_S3_ENDPOINT  ?= localhost:9091
TEST_S3_ACCESS_KEY ?= s3mock-access
TEST_S3_SECRET_KEY ?= s3mock-secret
TEST_S3_BUCKET     ?= file-upload-test

help:
	@echo "Targets:"
	@echo "  run              run the API locally (requires Postgres + S3Mock)"
	@echo "  build            build ./$(APP) with version metadata"
	@echo "  test             unit + HTTP tests"
	@echo "  test-race        unit + HTTP tests with the race detector"
	@echo "  test-integration run all tests against local Postgres + S3Mock"
	@echo "  vet / fmt / tidy / clean / coverage"
	@echo "  compose-up       start Postgres + S3Mock + API via docker compose"
	@echo "  compose-down     stop the compose stack"

run:
	go run $(PKG)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(APP) $(PKG)

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" \
	TEST_S3_ENDPOINT="$(TEST_S3_ENDPOINT)" \
	TEST_S3_ACCESS_KEY="$(TEST_S3_ACCESS_KEY)" \
	TEST_S3_SECRET_KEY="$(TEST_S3_SECRET_KEY)" \
	TEST_S3_BUCKET="$(TEST_S3_BUCKET)" \
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal docs

tidy:
	go mod tidy

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f $(APP) coverage.out coverage.html

docker-build:
	docker build -t file-upload:$(VERSION) .

docker-up: compose-up

docker-down: compose-down

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down
