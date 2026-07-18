# Shared multi-stage Dockerfile for ALL Go services.
# Per-service specifics are passed as build-args from services.yaml:
#   GO_VERSION  base golang image tag (default 1.25)
#   MAIN_PATH   package to build (default ./)
#   PORT        port the service listens on (default 8080)
#
# Multi-arch strategy: the builder stage is pinned to the host platform
# ($BUILDPLATFORM) so Go cross-compiles natively — no QEMU emulation needed.
# TARGETOS / TARGETARCH are injected by buildx and passed to the Go compiler.
ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

# Only ca-certificates needed: CGO_ENABLED=0 means no gcc/build-base,
# and go mod download uses HTTPS (not git).
RUN apk add --no-cache ca-certificates

WORKDIR /src

# Download modules — cached in /go/pkg/mod across builds.
# Layer cache still helps when go.mod/go.sum haven't changed.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG MAIN_PATH=./
# /go/pkg/mod        — module cache (shared with download step)
# /root/.cache/go-build — incremental build cache, keyed per arch to
#                         avoid amd64/arm64 cache collisions
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-${TARGETARCH} \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w" -o /out/app ${MAIN_PATH}

# Runtime image: small Alpine with CA certs for the target platform.
# CA certs are copied from the builder instead of running apk — this avoids
# any RUN on the target platform so QEMU is not needed at all.
FROM alpine:3.21
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

WORKDIR /
COPY --from=builder /out/app /app

ARG PORT=8080
ENV PORT=${PORT}
EXPOSE ${PORT}

ENTRYPOINT ["/app"]
