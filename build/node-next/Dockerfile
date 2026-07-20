# Shared multi-stage Dockerfile for ALL Next.js (standalone output) frontends.
# Requires next.config with `output: 'standalone'` in the app.
# Per-service specifics are passed as build-args from services.yaml:
#   NODE_VERSION  base node image tag (default 18)
#   PORT          port the app listens on (default 3000)
#
# Multi-arch strategy (mirrors build/go/Dockerfile): the dependency and build
# stages are pinned to the BUILD platform ($BUILDPLATFORM) so `npm ci` and
# `next build` run natively on the CI runner instead of under QEMU emulation
# for every target arch. This is safe because Next.js standalone output is
# architecture-independent JavaScript: the SWC compiler's native binary
# (@next/swc-*) is used only during `next build` and is NOT traced into
# .next/standalone, so nothing arch-specific reaches the runtime image. Only
# the final `runner` stage is built per target arch (it just runs node).
#
# Caveat for future apps: if an app adds a dependency with a native addon that
# IS required at runtime (e.g. sharp, better-sqlite3), that binary would be
# built for $BUILDPLATFORM and break on a different target arch. Such an app
# must install/rebuild those deps on the target platform instead.
#
# node:*-alpine ships amd64 + arm64 manifests, so the runner resolves per arch.
ARG NODE_VERSION=18

# ---- Dependencies: native to the build host, npm cache persisted -----------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS deps
# libc6-compat may be needed on alpine, see nodejs/docker-node notes
RUN apk add --no-cache libc6-compat
WORKDIR /app
COPY package.json package-lock.json* ./
# Cache mount keeps npm's download cache across builds so unchanged deps are
# not re-fetched; the layer cache still short-circuits when the lockfile is
# unchanged.
RUN --mount=type=cache,target=/root/.npm \
    npm ci

# ---- Build: native to the build host, Next.js cache persisted --------------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS builder
# libc6-compat: the native SWC compiler runs here during `next build`.
RUN apk add --no-cache libc6-compat
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Persist Next.js's incremental build cache (.next/cache) across builds to
# speed up recompilation; it is regenerated when sources change.
RUN --mount=type=cache,target=/app/.next/cache \
    npm run build

# ---- Runtime: built PER TARGET arch (no --platform pin) --------------------
# Only this stage runs on the cluster, and it does nothing but `node server.js`
# over the arch-independent standalone bundle.
FROM node:${NODE_VERSION}-alpine AS runner
# No apk/native deps here: the standalone runtime is pure JS (verified: no
# *.node files are traced into .next/standalone), so the runner keeps its
# work minimal - only the cheap adduser/mkdir below run on the target arch.
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs \
 && adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public

# Correct permission for prerender cache
RUN mkdir .next && chown nextjs:nodejs .next

# Leverage Next.js standalone output to reduce image size
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

ARG PORT=3000
ENV PORT=${PORT}
ENV HOSTNAME="0.0.0.0"
EXPOSE ${PORT}

# server.js is produced by `next build` under standalone output
CMD ["node", "server.js"]
