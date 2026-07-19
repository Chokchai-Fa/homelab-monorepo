# Shared multi-stage Dockerfile for Docusaurus static sites.
# Unlike build/node-next (which runs a Next.js server), Docusaurus emits a
# fully static site, so the runtime stage is just nginx serving files.
#
# Multi-arch strategy: the build stage is pinned to $BUILDPLATFORM, so in a
# `buildx --platform linux/amd64,linux/arm64` build it runs ONCE, natively on
# the build host (amd64 on GitHub runners), instead of once per target arch.
# Static HTML/JS/CSS is architecture-independent, and running npm + webpack
# under QEMU arm64 emulation is what made this build slow. Only the tiny
# nginx runtime stage is per-arch, and nginx:*-alpine ships both manifests.
#
# Per-site specifics are passed as build-args from services.yaml:
#   NODE_VERSION  base node image tag for the build stage (default 20)
#   PORT          port nginx listens on (default 80)
ARG NODE_VERSION=20

# --- build stage: produce the static site into /app/build (native arch) ---
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS builder
WORKDIR /app

# Install deps first for layer caching (only re-runs when lockfile changes).
# The cache mount keeps npm's download cache across builds even then, and
# audit/fund add network round-trips that mean nothing in CI.
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline --no-audit --no-fund

COPY . .
RUN npm run build

# --- runtime stage: nginx serving the static output ---
FROM nginx:1.27-alpine AS runner

# SPA-style config: gzip + fallback to index.html for client-side routes.
COPY --from=builder /app/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/build /usr/share/nginx/html

ARG PORT=80
ENV PORT=${PORT}
EXPOSE ${PORT}

# nginx runs in the foreground (its base image already sets this CMD, repeated
# here for clarity).
CMD ["nginx", "-g", "daemon off;"]
