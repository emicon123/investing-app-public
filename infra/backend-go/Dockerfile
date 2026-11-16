# syntax=docker/dockerfile:1
# Build context is the project root (set in infra/compose.yml), matching infra/backend/Dockerfile's
# convention — needed because backend-go's go.mod resolves common-go via a relative `replace`
# directive (`../../../packages/common/go/common-go` from apps/main/backend-go), so both modules
# must be copied into the image at the same relative depth they have on disk.

# ── Stage 1: Build the static Go binary ─────────────────────────────────────────
FROM golang:1.27.4-alpine AS builder
WORKDIR /app

# Copy go.mod/go.sum for both modules first so `go mod download` is cached until a
# dependency actually changes (mirrors the Maven go-offline layer-caching trick in
# infra/backend/Dockerfile).
COPY packages/common/go/common-go/go.mod packages/common/go/common-go/go.sum ./packages/common/go/common-go/
COPY apps/main/backend-go/go.mod apps/main/backend-go/go.sum ./apps/main/backend-go/

# Deliberately NOT using `go.work` here — each backend-go module already carries its own
# `replace` directive to common-go, so plain module-mode `go build` inside backend-go/ resolves
# it without needing go.work/go.work.sum in the build context at all. Keeps this image's build
# graph scoped to exactly the two modules it ships, not all 4 workspace modules.
WORKDIR /app/apps/main/backend-go
RUN go mod download

WORKDIR /app
COPY packages/common/go/common-go/ ./packages/common/go/common-go/
COPY apps/main/backend-go/ ./apps/main/backend-go/

WORKDIR /app/apps/main/backend-go
ENV CGO_ENABLED=0 GOOS=linux GOARCH=arm64
RUN go build -trimpath -ldflags="-s -w" -o /out/backend-go ./cmd/server

# ── Stage 2: Minimal runtime image ──────────────────────────────────────────────
# distroless/static-debian12:nonroot — no shell, no package manager, runs as the built-in
# 65532:65532 nonroot user out of the box. Chosen over `alpine` per ADR-094 §2's either/or: this
# binary is CGO_ENABLED=0/fully static (no libc dependency, so alpine's musl buys nothing) and the
# whole point of this migration is minimizing footprint/attack surface, so the smaller,
# shell-less image is the better default. Trade-off, noted rather than hidden: no shell means no
# wget/curl-based `HEALTHCHECK`, so the container healthcheck is the binary's own `healthcheck`
# subcommand (exec form, declared in infra/compose.yml — ADR-110).
FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=builder /out/backend-go /app/backend-go
USER nonroot:nonroot
EXPOSE 8091
ENTRYPOINT ["/app/backend-go"]
