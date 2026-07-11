# Shared multi-stage Dockerfile for ALL Next.js (standalone output) frontends.
# Requires next.config with `output: 'standalone'` in the app.
# Per-service specifics are passed as build-args from services.yaml:
#   NODE_VERSION  base node image tag (default 18)
#   PORT          port the app listens on (default 3000)
# Multi-arch clean: node:*-alpine ships amd64 + arm64 manifests.
ARG NODE_VERSION=18
FROM node:${NODE_VERSION}-alpine AS base

# Install dependencies only when needed
FROM base AS deps
# libc6-compat may be needed on alpine, see nodejs/docker-node notes
RUN apk add --no-cache libc6-compat
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci

# Rebuild the source code only when needed
FROM base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

# Production image
FROM base AS runner
WORKDIR /app
ENV NODE_ENV=production

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
