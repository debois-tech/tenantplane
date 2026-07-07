---
title: "Install"
description: "Install the CRDs and controller onto a Kubernetes cluster."
weight: 20
---

This guide installs tenantplane's CRDs and controller. For an end-to-end local
walkthrough, see the [Quickstart](/docs/quickstart/).

## Requirements

- A Kubernetes cluster (kind, k3s, or any conformant distribution).
- `kubectl` configured against it.
- Docker and Go 1.22+ to build the controller image (until published images are
  available).

## Install the CRDs

```bash
kubectl apply -f config/crd
```

This registers `TenantCluster`, `IsolationProfile`, and `SyncPolicy`.

## Build and load the controller

On kind:

```bash
make kind-load          # docker build + kind load docker-image
```

On another cluster, build and push to a registry your nodes can pull from:

```bash
make manager-image IMG=registry.example.com/tenantplane/manager:dev
docker push registry.example.com/tenantplane/manager:dev
```

## Deploy the controller

```bash
make deploy
kubectl -n tenantplane-system rollout status deploy/tenantplane-controller
```

The manifest in `deploy/tenantplane.yaml` creates the namespace, a
ServiceAccount, the RBAC the controller needs (including `pods/exec` for
kubeconfig extraction and permission over the synced kinds), and the Deployment
with health probes.

## Verify

```bash
kubectl -n tenantplane-system get pods
kubectl api-resources | grep tenantplane.io
```

Next: bring up a tenant in the [first tenant guide](/docs/guides/first-tenant/).
