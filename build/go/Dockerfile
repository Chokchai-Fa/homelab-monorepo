# Shared multi-stage Dockerfile for ALL Go services.
# Per-service specifics are passed as build-args from services.yaml:
#   GO_VERSION  base golang image tag (default 1.23)
#   MAIN_PATH   package to build (default ./)
#   PORT        port the service listens on (default 8080)
# Multi-arch: buildx sets TARGETARCH per requested platform (linux/amd64, linux/arm64).
ARG GO_VERSION=1.23
FROM golang:${GO_VERSION}-alpine AS builder

# Packages needed for fetching modules and building
RUN apk add --no-cache git build-base ca-certificates

WORKDIR /src

# Cache modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the sources
COPY . .

# Build a static, stripped binary for the target architecture
ARG TARGETARCH
ARG MAIN_PATH=./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w" -o /out/app ${MAIN_PATH}

# Runtime image: small Alpine with CA certs
FROM alpine:3.18
RUN apk add --no-cache ca-certificates

WORKDIR /
COPY --from=builder /out/app /app

ARG PORT=8080
ENV PORT=${PORT}
EXPOSE ${PORT}

ENTRYPOINT ["/app"]
