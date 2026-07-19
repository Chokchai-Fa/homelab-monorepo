---
sidebar_position: 6
title: Secrets bootstrap
---

# Secrets bootstrap

Secrets are the **one thing not in Git**. Every other piece of cluster state is
declarative and version-controlled, but credentials are created directly
in-cluster with `kubectl create secret` and deliberately never committed. This
page is the runbook for standing them up (e.g. on a fresh cluster).

:::warning
Without these secrets, the data services and application pods will fail to
start (CrashLoopBackOff on missing `secretKeyRef`). Create them **before** (or
right as) Flux first reconciles the `core` and `apps` layers.
:::

## Secrets inventory

| Secret | Namespace(s) | Keys | Consumed by |
|--------|--------------|------|-------------|
| `postgres-secret` | `core` | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Postgres StatefulSet |
| `nats-auth` | `core`, `default` | `username`, `password` | NATS + every NATS client |
| `redis-auth` | `core`, `default` | `username`, `password` | Redis + every Redis client |
| `consumer-llm-processor-secret` | `default` | `DATABASE_URL`, `GEMINI_API_KEY`, `GROQ_API_KEY`*, `OPENROUTER_API_KEY`*, `CF_ACCOUNT_ID`*, `CF_API_TOKEN`* | consumer-llm-processor, consumer-reminder, worker-*, subscriber-* (all reuse `DATABASE_URL`) |
| `line-webhook-secret` | `default` | `LINE_CHANNEL_SECRET`, `LINE_CHANNEL_ACCESS_TOKEN` | line-webhook, consumer-reply-line-user |

<small>* optional — enable extra providers / image generation.</small>

Note that `nats-auth` and `redis-auth` are needed in **both** `core` (the
servers) and `default` (the clients), so they're created in both namespaces.
`DATABASE_URL` lives once in `consumer-llm-processor-secret` and is reused by
every service that touches Postgres.

## Commands

```bash
# --- core namespace: the data services ---
kubectl -n core create secret generic postgres-secret \
  --from-literal=POSTGRES_USER=homelab \
  --from-literal=POSTGRES_PASSWORD='<pg-password>' \
  --from-literal=POSTGRES_DB=homelab

kubectl -n core create secret generic nats-auth \
  --from-literal=username=homelab \
  --from-literal=password='<nats-password>'

kubectl -n core create secret generic redis-auth \
  --from-literal=username=homelab \
  --from-literal=password='<redis-password>'

# --- default namespace: the app clients ---
# nats-auth / redis-auth copied in so clients can authenticate
kubectl -n default create secret generic nats-auth \
  --from-literal=username=homelab --from-literal=password='<nats-password>'
kubectl -n default create secret generic redis-auth \
  --from-literal=username=homelab --from-literal=password='<redis-password>'

# DATABASE_URL points at the in-cluster Postgres; add provider keys as needed
kubectl -n default create secret generic consumer-llm-processor-secret \
  --from-literal=DATABASE_URL='postgres://homelab:<pg-password>@postgres.core.svc.cluster.local:5432/homelab?sslmode=disable' \
  --from-literal=GEMINI_API_KEY='<gemini-key>' \
  --from-literal=GROQ_API_KEY='<groq-key>' \
  --from-literal=OPENROUTER_API_KEY='<openrouter-key>' \
  --from-literal=CF_ACCOUNT_ID='<cf-account>' \
  --from-literal=CF_API_TOKEN='<cf-token>'

kubectl -n default create secret generic line-webhook-secret \
  --from-literal=LINE_CHANNEL_SECRET='<line-channel-secret>' \
  --from-literal=LINE_CHANNEL_ACCESS_TOKEN='<line-access-token>'
```

## The cloudflared tunnel credential

The tunnel credential is **not** a Kubernetes secret — it's a host file mounted
via `hostPath` at `/home/chokchai/.cloudflared/<tunnel-uuid>.json`. Provision it
by running `cloudflared tunnel login` + `cloudflared tunnel create` on the Pi
once; the resulting JSON is what the deployment mounts. See
[Networking](/infrastructure/networking).

## Rotation

To rotate a credential: update the upstream (DB password, LINE token, provider
key), re-run the matching `kubectl create secret … --dry-run=client -o yaml |
kubectl apply -f -`, then restart the consuming pods
(`kubectl rollout restart deploy/<name>`). Since secrets aren't in Git, Flux
won't touch them — the rotation is a manual, out-of-band operation by design.
