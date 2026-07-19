# Shared multi-stage Dockerfile for Docusaurus static sites.
# Unlike build/node-next (which runs a Next.js server), Docusaurus emits a
# fully static site, so the runtime stage is just nginx serving files.
# Per-site specifics are passed as build-args from services.yaml:
#   NODE_VERSION  base node image tag for the build stage (default 20)
#   PORT          port nginx listens on (default 80)
# Multi-arch clean: both node:*-alpine and nginx:*-alpine ship amd64 + arm64.
ARG NODE_VERSION=20

# --- build stage: produce the static site into /app/build ---
FROM node:${NODE_VERSION}-alpine AS builder
WORKDIR /app

# Install deps first for layer caching (only re-runs when lockfile changes).
COPY package.json package-lock.json* ./
RUN npm ci

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
