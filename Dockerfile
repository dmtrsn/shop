# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/sweetshop \
    ./cmd/server

FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 sweetshop \
    && adduser -S -D -H -u 10001 -G sweetshop sweetshop

WORKDIR /app

COPY --from=builder --chown=sweetshop:sweetshop /out/sweetshop ./sweetshop
COPY --chown=sweetshop:sweetshop web ./web
COPY --chown=sweetshop:sweetshop migrations ./migrations

ENV HTTP_ADDR=:8080 \
    WEB_DIR=/app/web \
    MIGRATIONS_DIR=/app/migrations \
    RUN_MIGRATIONS=true

USER sweetshop

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["/app/sweetshop"]
