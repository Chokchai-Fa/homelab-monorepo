---
sidebar_position: 1
title: k3s cluster
---

# k3s cluster

The entire homelab runs on **one Raspberry Pi 4 (arm64, 3.7 GiB RAM)** as a
**single-node k3s cluster**. There is no HA, no second node, and no cloud
fallback — every design decision downstream is shaped by that constraint.

## Cluster facts

| Property | Value |
|----------|-------|
| Distribution | [k3s](https://k3s.io) (lightweight Kubernetes) |
| Kubernetes channel | `v1.35` (pinned) |
| Architecture | arm64 |
| Nodes | 1 (the Pi itself, control-plane + worker) |
| Ingress controller | **Traefik disabled** — cloudflared tunnel is the only ingress |
| StorageClass | `local-path` (k3s built-in, backs the node's disk) |
| RAM | 3.7 GiB total |

### Bootstrap

k3s is installed with Traefik disabled (the cluster uses a cloudflared tunnel
instead) and a world-readable kubeconfig:

```bash
curl -sfL https://get.k3s.io | sudo \
  INSTALL_K3S_CHANNEL=v1.35 \
  INSTALL_K3S_EXEC="--disable=traefik --write-kubeconfig-mode=644" \
  sh -
```

After that, the cluster is bootstrapped into GitOps by installing FluxCD and
pointing it at `homelab-flux-controller` — see
[GitOps with FluxCD](/infrastructure/gitops-fluxcd).

## Living within a single Pi

The single-node, SD-card-backed Pi drives several recurring patterns you'll see
throughout the manifests:

- **No `nodeSelector` / `tolerations` anywhere.** With one node there's nothing
  to schedule around, so workloads are unconstrained.
- **In-memory data services where durability isn't required.** NATS runs core
  (no JetStream) and Redis runs with no persistent volume — both avoid writing
  to the slow SD card. Only Postgres and Portainer have PVCs
  (`local-path`, 5 GiB and 2 GiB).
- **Tight resource limits.** NATS is capped at 256 Mi, Redis at 128 Mi (plus a
  hard `--maxmemory 64mb`), cloudflared at 128 Mi. Application services run with
  requests as low as 20 m CPU / 32 Mi.
- **Widened Flux liveness probes.** Slow SD-card I/O made the default 1-second
  controller probes time out and CrashLoopBackOff; the flux-system kustomization
  patches every Flux Deployment to `initialDelaySeconds: 30`,
  `periodSeconds: 30`, `failureThreshold: 6`.
- **Wave-based rollout.** Flux brings up infrastructure before apps, and a cold
  start is staged so the node isn't hit with every image pull at once — see the
  [rollout waves runbook](/runbooks/rollout-waves).

:::tip
When adding a workload, copy the resource sizing of an existing peer rather than
guessing. The Pi has no headroom to absorb an over-provisioned pod.
:::
