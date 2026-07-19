---
sidebar_position: 4
title: Networking & ingress
---

# Networking & ingress

Traefik is disabled. The cluster has **no traditional ingress controller and no
open inbound ports** on the Pi. Instead, a **Cloudflare Tunnel** (`cloudflared`)
dials *out* to Cloudflare and receives traffic for a set of hostnames, forwarding
each to an in-cluster Service. This is how LINE's webhooks and every dashboard
reach the cluster without port-forwarding or a public IP.

```mermaid
flowchart LR
  internet([Internet / LINE platform])
  cfedge[[Cloudflare edge]]

  subgraph pi["Raspberry Pi — k3s"]
    subgraph ingressns["ns: ingress"]
      cfd["cloudflared<br/>2024.4.0"]
    end
    subgraph defaultns["ns: default"]
      pf[portfolio-web :3000]
      lw[line-webhook :8080]
      docs[docs :80]
    end
    subgraph fluxns["ns: flux-system"]
      wg[weave-gitops :9001]
    end
    subgraph co ["ns: core"]
      natsm[nats :8222]
      minio["minio :9000/:9001"]
    end
    subgraph portns["ns: portainer"]
      port[portainer :9000]
    end
  end

  internet <--> cfedge
  cfedge <-->|outbound tunnel| cfd
  cfd --> pf
  cfd --> lw
  cfd --> docs
  cfd --> wg
  cfd --> natsm
  cfd --> minio
  cfd --> port
```

## cloudflared deployment

| Property | Value |
|----------|-------|
| Namespace | `ingress` |
| Image | `cloudflare/cloudflared:2024.4.0` |
| Replicas | 1 |
| Resources | limits `cpu: 100m`, `memory: 128Mi` |
| Credentials | mounted from the host via **hostPath** (`/home/chokchai/.cloudflared/<tunnel-uuid>.json`) |
| Config | `ConfigMap` `cloudflared-config` mounted at `/etc/cloudflared/tunnel-config.yml` |

The tunnel credentials are deliberately **not** in Git — they're a host file
mounted into the pod, consistent with the single-node design.

## Hostname → Service map

The tunnel config (`infrastructure/networking/ingress/configmap-tunnel.yaml`)
maps public hostnames to cluster-internal Service DNS names:

| Hostname | Target Service |
|----------|----------------|
| `portfolio.chokchai-dev.xyz` | `portfolio-web.default.svc.cluster.local:3000` |
| `line-webhook.chokchai-dev.xyz` | `line-webhook.default.svc.cluster.local:8080` |
| `docs.chokchai-dev.xyz` | `docs.default.svc.cluster.local:80` |
| `weaver-gitops.chokchai-dev.xyz` | `weave-gitops.flux-system.svc.cluster.local:9001` |
| `portainer.chokchai-dev.xyz` | `portainer.portainer.svc.cluster.local:9000` |
| `codeserver.chokchai-dev.xyz` | `code-server.code-server.svc.cluster.local:8080` |
| `nats.chokchai-dev.xyz` | `nats.core.svc.cluster.local:8222` (monitoring UI) |
| `minio.chokchai-dev.xyz` | `minio.core.svc.cluster.local:9001` (MinIO console) |
| `s3.chokchai-dev.xyz` | `minio.core.svc.cluster.local:9000` (MinIO S3 API) |
| *(catch-all)* | `http_status:404` |

:::info Adding a hostname
Two steps: (1) add a `- hostname: … / service: http://<svc>.<ns>.svc.cluster.local:<port>`
entry to the tunnel ConfigMap **above the catch-all 404**, and (2) create a
Cloudflare DNS CNAME for that hostname pointing at the tunnel — either in the
Cloudflare dashboard or from the Pi, where the origin cert lives:
`cloudflared tunnel route dns <tunnel-uuid> <hostname>`. Restart the
`cloudflared` deployment after ConfigMap changes; Flux applies the ConfigMap
but does not bounce the pod.
:::

## CoreDNS hairpin for `s3.chokchai-dev.xyz`

One hostname resolves differently inside the cluster than outside: a
`coredns-custom` ConfigMap (`infrastructure/networking/coredns/`) rewrites
`s3.chokchai-dev.xyz` to `minio.core.svc.cluster.local`, so pods talking to
the public S3 hostname reach MinIO directly instead of looping out through
the Cloudflare edge — whose proxy corrupts S3 `aws-chunked` request
signatures. This is what makes MinIO console share links publicly valid.
Full explanation in [MinIO](../data-services/minio.md). k3s supports this
out of the box: its CoreDNS imports `/etc/coredns/custom/*.override` from
that ConfigMap if it exists.

## Why a tunnel instead of ingress + LoadBalancer?

- **No public IP required.** The Pi sits behind a home router; the tunnel makes
  outbound connections only.
- **TLS is terminated at Cloudflare's edge**, so no cert management on-cluster.
- **Nothing is exposed by accident** — only the hostnames explicitly listed in
  the ConfigMap are reachable; everything else 404s.
