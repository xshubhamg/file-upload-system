# SQLite via mattn/go-sqlite3 needs CGO, so the builder carries gcc.
FROM golang:1.27-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o server .

FROM alpine:latest

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app
COPY --from=builder /app/server ./server

# Render / Fly / most hosts inject PORT; default matches .env.example.
ENV PORT=8080
EXPOSE 8080

VOLUME ["/app/uploads"]

CMD ["./server"]
