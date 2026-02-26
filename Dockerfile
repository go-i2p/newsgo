# Dockerfile — newsgo serving container
# Drop-in replacement for i2p.newsxml/Dockerfile
#
# Two-stage build:
#   Stage 1 (builder) — compiles the newsgo binary from source.
#   Stage 2 (runtime) — minimal image that serves build/ on port 9696 using
#                        newsgo's built-in HTTP server.  No lighttpd required.
#
# Run docker-news.sh (or news.sh) first to produce the build/ directory,
# then run docker-newsxml.sh to build and start this container.

# ── Stage 1: build newsgo binary ─────────────────────────────────────────────
FROM golang:latest AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /newsgo .

# ── Stage 2: serving runtime ─────────────────────────────────────────────────
FROM debian:stable-slim

COPY --from=builder /newsgo /usr/local/bin/newsgo

# Copy the pre-built feed directory produced by docker-news.sh / news.sh.
COPY build /build

EXPOSE 9696

# Serve the build/ directory on all interfaces so Docker can forward the port.
CMD ["newsgo", "serve", "--host", "0.0.0.0", "--port", "9696", "--newsdir", "/build"]
