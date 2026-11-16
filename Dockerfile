FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/ticketing-api     ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/ticketing-migrate  ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/ticketing-worker   ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/ticketing-seed     ./cmd/seed

# ──────────────────────────────────────────
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/ticketing-api     /app/ticketing-api
COPY --from=builder /bin/ticketing-migrate /app/ticketing-migrate
COPY --from=builder /bin/ticketing-worker  /app/ticketing-worker
COPY --from=builder /bin/ticketing-seed    /app/ticketing-seed
COPY scripts/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 4009 9090

USER nobody:nobody

ENTRYPOINT ["/app/entrypoint.sh"]
