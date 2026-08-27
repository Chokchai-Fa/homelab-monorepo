# Shared multi-stage Dockerfile for ALL Go services.
# Per-service specifics are passed as build-args from services.yaml:
#   GO_VERSION  base golang image tag (default 1.25)
#   SERVICE_DIR service source dir, relative to the repo root (e.g.
#               services/line-webhook) - the build context is the REPO ROOT,
#               not the service dir, so that shared local modules under libs/
#               are reachable. Services `replace` them by relative path
#               (../../libs/natsutil), which only resolves if libs/ is inside
#               the context.
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

ARG SERVICE_DIR
WORKDIR /src

# Shared local modules first: they are `replace` targets, so `go mod download`
# needs them present, and they change far less often than service code — which
# keeps this layer cached across most builds.
COPY libs/ ./libs/

# Download modules — cached in /go/pkg/mod across builds.
# Layer cache still helps when go.mod/go.sum haven't changed.
COPY ${SERVICE_DIR}/go.mod ${SERVICE_DIR}/go.sum ./${SERVICE_DIR}/
WORKDIR /src/${SERVICE_DIR}
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY ${SERVICE_DIR}/ ./

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
