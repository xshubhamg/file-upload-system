# syntax=docker/dockerfile:1.7

FROM golang:1.27-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/xshubhamg/file-upload/internal/version.Version=${VERSION} \
      -X github.com/xshubhamg/file-upload/internal/version.Commit=${COMMIT} \
      -X github.com/xshubhamg/file-upload/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app
COPY --from=builder /out/server ./server

USER 10001:10001

ENV PORT=8080 \
    ADMIN_PORT=9090

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/server"]
