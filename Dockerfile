# ── Build stage ─────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Download dependencies first (layer cache).
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/disapyr ./cmd/server

# ── Runtime stage ────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

# Non-root user.
RUN addgroup -S app && adduser -S -G app app
USER app

WORKDIR /app
COPY --from=builder /app/disapyr .

EXPOSE 8080
ENTRYPOINT ["/app/disapyr"]
