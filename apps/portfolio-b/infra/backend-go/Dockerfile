# syntax=docker/dockerfile:1
# Build context is the project root (matches apps/portfolio-b/infra/backend/Dockerfile) —
# needed because this module's go.mod resolves common-go via `replace
# investing-app/common-go => ../../../packages/common/go/common-go`, three levels up from
# apps/portfolio-b/backend-go, so the image must mirror that same on-disk depth.

# ── Stage 1: Build the static Go binary ─────────────────────────────────────────
FROM golang:1.27.4-alpine AS builder
WORKDIR /app

COPY packages/common/go/common-go/go.mod packages/common/go/common-go/go.sum ./packages/common/go/common-go/
COPY apps/portfolio-b/backend-go/go.mod apps/portfolio-b/backend-go/go.sum ./apps/portfolio-b/backend-go/

# Not using go.work — this module's own `replace` directive is enough in plain module mode.
WORKDIR /app/apps/portfolio-b/backend-go
RUN go mod download

WORKDIR /app
COPY packages/common/go/common-go/ ./packages/common/go/common-go/
COPY apps/portfolio-b/backend-go/ ./apps/portfolio-b/backend-go/

WORKDIR /app/apps/portfolio-b/backend-go
ENV CGO_ENABLED=0 GOOS=linux GOARCH=arm64
RUN go build -trimpath -ldflags="-s -w" -o /out/backend-go ./cmd/server

# ── Stage 2: Minimal runtime image ──────────────────────────────────────────────
# See infra/backend-go/Dockerfile (root app) for the distroless-vs-alpine reasoning — identical
# here: fully static binary, no libc need, minimize footprint. The container healthcheck is the
# binary's own `healthcheck` subcommand (exec form, declared in infra/compose.yml — ADR-110).
FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=builder /out/backend-go /app/backend-go
USER nonroot:nonroot
EXPOSE 8092
ENTRYPOINT ["/app/backend-go"]
