---
sidebar_position: 2
title: Repository layout
---

# Repository layout

The homelab lives in two repositories. Knowing which one owns what is the
fastest way to find anything.

## `homelab-monorepo` — application source & CI

```text
homelab-monorepo/
├── apps/                       Frontend / Node apps (one dir each, + VERSION)
│   ├── portfolio-web/            Next.js portfolio
│   └── docs/                     This Docusaurus site
├── services/                   Backend Go services (one dir each, + VERSION)
│   ├── line-webhook/
│   ├── consumer-llm-processor/
│   ├── consumer-reply-line-user/
│   ├── consumer-reminder/
│   ├── worker-reminder-scheduler/
│   └── subscriber-reminder-notifier/
├── build/                      Shared, per-STACK Dockerfiles
│   ├── go/Dockerfile             every Go service
│   ├── node-next/Dockerfile      every Next.js standalone frontend
│   └── docusaurus/Dockerfile     this docs site (static → nginx)
├── services.yaml               Service registry — single source of truth for CI
├── scripts/select-services.sh  Change detection → build matrix
└── .github/workflows/build-and-push.yml
```

Each buildable unit has a `VERSION` file (semver core) and an entry in
`services.yaml`. The CI pipeline is **registry-driven**: adding a service means
dropping source in a folder, adding a `VERSION`, and appending one block to
`services.yaml` — no workflow edits. See the
[CI/CD pipeline](/infrastructure/cicd-pipeline) page.

## `homelab-flux-controller` — desired cluster state (GitOps)

```text
homelab-flux-controller/
├── clusters/homelab/           The cluster's entrypoint for Flux
│   ├── flux-system/              Flux's own components + GitRepository/Kustomization
│   └── apps/                     Flux Kustomization objects (infrastructure, apps, …)
│       └── image-automation/     ImageRepository/Policy/UpdateAutomation per service
├── infrastructure/             Platform layer, deployed before apps
│   ├── core/                     namespace + postgres + nats + redis
│   ├── networking/               cloudflared tunnel (ingress)
│   └── monitoring/               weave-gitops (+ portainer, independent)
└── apps/                        Application workloads (Deployments/Services)
    ├── portfolio-web/
    ├── line-webhook/
    ├── consumer-*/  worker-*/  subscriber-*/
    └── docs/
```

Note the deliberate mirror: for every service under `homelab-monorepo/services`
(or `apps`), there is a matching workload under `homelab-flux-controller/apps`
**and** an image-automation entry under
`clusters/homelab/apps/image-automation`. The
[GitOps](/infrastructure/gitops-fluxcd) page explains how these tie together.

## Where to look for…

| I want to change… | Go to… |
|-------------------|--------|
| Service behavior / business logic | `homelab-monorepo/services/<name>` |
| How an image is built | `homelab-monorepo/build/<stack>/Dockerfile` + `services.yaml` |
| What runs in the cluster (replicas, env, resources) | `homelab-flux-controller/apps/<name>/deployment.yaml` |
| A data service (NATS/Postgres/Redis) config | `homelab-flux-controller/infrastructure/core/<name>` |
| A public hostname / tunnel route | `homelab-flux-controller/infrastructure/networking/ingress/configmap-tunnel.yaml` |
| How auto-deploy of new images works | `homelab-flux-controller/clusters/homelab/apps/image-automation/<name>` |
