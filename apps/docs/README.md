# homelab-docs

Docusaurus site documenting the whole homelab: the GitOps/k3s infrastructure
layer (`homelab-flux-controller`), the core data services (NATS, Postgres,
Redis), and the application layer (LINE AI chatbot + reminder system in
`homelab-monorepo/services`). Architecture and behavior are drawn as Mermaid
diagrams.

## Local development

```bash
npm install
npm run start      # dev server with hot reload at http://localhost:3000
npm run build      # static build into ./build (fails on broken internal links)
npm run serve      # serve the built site locally
```

## Deployment

Built and shipped like every other service in this monorepo: the GitHub Actions
pipeline (driven by `services.yaml`) builds a multi-arch image
`chokchaifa/docs:v<VERSION>-<run>.<sha>` using `build/docusaurus/Dockerfile`
(Node build → nginx static serve). FluxCD image-automation then rolls it out to
the k3s cluster, served at `https://docs.chokchai-dev.xyz` via the cloudflared
tunnel. See `docs/runbooks/` and `docs/infrastructure/cicd-pipeline.md`.

## Structure

- `docs/` — the documentation content (one folder per layer).
- `docusaurus.config.ts` — site config; Mermaid + docs-only mode.
- `sidebars.ts` — explicit sidebar / reading order.
