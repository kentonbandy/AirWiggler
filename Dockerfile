# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependencies before copying source
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o airwiggler .

# ── Final stage ───────────────────────────────────────────────────────────────
FROM scratch

# Copy CA certificates for any future HTTPS needs
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /build/airwiggler /airwiggler

# Music library (read-only recommended)
VOLUME ["/music"]

EXPOSE 8080

ENV APP_PORT=8080 \
    SITE_TITLE="My Music Library" \
    DEFAULT_QUALITY=medium \
    MUSIC_DIR=/music \
    RESCAN_ON_START=true \
    RESCAN_INTERVAL_MINUTES=15

ENTRYPOINT ["/airwiggler"]
