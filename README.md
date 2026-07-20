# homelab-monorepo

Monorepo for homelab **backend services** (Go/Rust/…) and **frontend apps**
(Node-based, various frameworks). One GitHub Actions pipeline builds Docker
images for every service and pushes them to Docker Hub (`chokchaifa/<image>`).

**CI here, CD elsewhere.** Deployment is handled by
[`homelab-flux-controller`](https://github.com/Chokchai-Fa/homelab-flux-controller):
Flux image-automation watches Docker Hub and auto-rolls the k3s cluster whenever
a new tag matching `^v[0-9]+\.[0-9]+\.[0-9]+.*$` appears. This repo's job ends at
"push a correctly-tagged image".

## Layout

```
build/            Shared, per-STACK Dockerfiles (reused across services)
  go/Dockerfile          every Go service
  node-next/Dockerfile   every Next.js (standalone) frontend
  docusaurus/Dockerfile  the docs site
apps/             Frontend services (one dir each) + a VERSION file
services/         Backend services (one dir each) + a VERSION file
services.yaml     Service registry — single source of truth
scripts/          select-services.sh — change detection + build matrix
.github/workflows/build-and-push.yml
```

## How builds are triggered

| Trigger | What builds |
| --- | --- |
| **Push / merge to `main`** | Only services whose files changed (change detection) |
| **Pull request to `main`** | Nothing — the *test* workflow runs `go vet` + unit tests for every service instead |
| **Manual run** (Actions → *build-and-push* → *Run workflow*) | Pick a branch/tag + `all` or specific service(s) — **no merge needed** |

Every build pushes `docker.io/chokchaifa/<image>:v<VERSION>-<run>.<shortsha>`,
e.g. `v1.0.0-142.9fa46c8`. The `<VERSION>` core comes from the service's
`VERSION` file; the monotonic run number guarantees Flux always deploys the
newest build. Bump `VERSION` for a meaningful release.

## Adding a new service

1. Put the source under `services/<name>/` (backend) or `apps/<name>/` (frontend).
2. Add a `VERSION` file (e.g. `1.0.0`) in that dir.
3. Reuse a shared Dockerfile — **no new Dockerfile** for an existing stack.
   (New stack? Add one shared `build/<stack>/Dockerfile`.)
4. Append a block to [`services.yaml`](services.yaml) pointing at the shared
   Dockerfile, with `image`, `context`, `platforms`, `buildArgs`, `paths`.
5. To make Flux deploy it, add the app's image-automation + deployment manifests
   in `homelab-flux-controller` (follow an existing app there).

## Services

| Service | Stack | Image | Port | Notes |
| --- | --- | --- | --- | --- |
| `portfolio-web` | Next.js 14 (standalone) | `chokchaifa/chokchai-portfolio` | 3000 | migrated from `chokchai-portfolio` |
| `docs` | Docusaurus | `chokchaifa/docs` | 80 | this documentation site; `npm run build` fails the image on broken links or MDX errors |
| `line-webhook` | Go 1.25 (Echo) | `chokchaifa/line-webhook` | 8080 | only HTTP ingress, never replies to LINE directly; needs `LINE_CHANNEL_SECRET` + `LINE_CHANNEL_ACCESS_TOKEN` at runtime (k8s Secret in flux repo) |
| `consumer-llm-processor` | Go 1.25 | `chokchaifa/consumer-llm-processor` | — | NATS consumer for LLM-backed AI replies (Gemini/Groq/OpenRouter/CF provider chain); needs `GEMINI_API_KEY` + `DATABASE_URL` |
| `consumer-reply-line-user` | Go 1.25 | `chokchaifa/consumer-reply-line-user` | — | only egress; NATS consumer for LINE delivery (reply token first, push fallback); needs channel credentials |
| `consumer-reminder` | Go 1.25 | `chokchaifa/consumer-reminder` | — | owns the reminder conversation flow + `line_users`/`reminders` tables; never calls an LLM |
| `worker-reminder-scheduler` | Go 1.25 | `chokchaifa/worker-reminder-scheduler` | — | gocron loop; arms due reminders as Redis expiry keys, no NATS |
| `subscriber-reminder-notifier` | Go 1.25 | `chokchaifa/subscriber-reminder-notifier` | — | fires on Redis key-expiry and publishes the reminder reply |

See [docs.chokchai-dev.xyz](https://docs.chokchai-dev.xyz) for architecture,
per-service env vars, and sequence diagrams.

## Required GitHub secrets

- `DOCKER_USERNAME` — Docker Hub user (`chokchaifa`, also the image namespace)
- `DOCKER_PASSWORD` — Docker Hub access token

## Local checks

```bash
# Which services would build for a given diff?
EVENT_NAME=push BEFORE_SHA=<sha> AFTER_SHA=HEAD bash scripts/select-services.sh

# Build a service image locally (arm64, the cluster arch)
docker buildx build -f build/go/Dockerfile --build-arg MAIN_PATH=./ \
  --platform linux/arm64 services/line-webhook
docker buildx build -f build/node-next/Dockerfile \
  --platform linux/arm64 apps/portfolio-web
```
