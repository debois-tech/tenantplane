---
title: "Quickstart"
description: "Install the CRDs and controller on a kind cluster and bring up your first tenant."
weight: 2
---

This quickstart brings up a shared-mode tenant on a local
[kind](https://kind.sigs.k8s.io) cluster. It should take about ten minutes.

## Prerequisites

- Go 1.22+
- Docker
- `kubectl`
- `kind`

## 1. Create a cluster

```bash
kind create cluster --name tenantplane-dev
```

## 2. Install the CRDs

```bash
kubectl apply -f config/crd
```

This registers the three tenantplane resource kinds: `TenantCluster`,
`IsolationProfile`, and `SyncPolicy`.

## 3. Build and load the controller image

```bash
make kind-load          # builds tenantplane/manager:dev and loads it into kind
```

## 4. Deploy the controller

```bash
make deploy             # applies deploy/tenantplane.yaml
kubectl -n tenantplane-system rollout status deploy/tenantplane-controller
```

## 5. Apply the sample resources

The repository ships ready-to-use samples:

```bash
kubectl apply -f config/samples/isolationprofile_restricted.yaml
kubectl apply -f config/samples/syncpolicy_default.yaml
kubectl apply -f config/samples/tenantcluster_dev.yaml
```

## 6. Watch the tenant come up

```bash
kubectl get tenantcluster dev -w
```

The `PHASE` column moves from `Provisioning` to `Ready` once the control-plane
pod is serving. Behind the scenes the controller has:

1. Applied the isolation boundary (NetworkPolicy, ResourceQuota, LimitRange, and
   Pod Security labels) to the namespace.
2. Created a headless Service and a StatefulSet running k3s.
3. Extracted the tenant kubeconfig into a Secret.
4. Started syncing resources declared in the SyncPolicy onto the host.

## 7. Use the tenant

```bash
kubectl -n <tenant-namespace> get secret dev-control-plane-kubeconfig \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > tenant.kubeconfig

kubectl --kubeconfig tenant.kubeconfig get ns
```

Anything you create in the tenant that the SyncPolicy marks `toHost` is
materialized onto the host cluster with a deterministic name. Ask why:

```bash
kubectl describe tenantcluster dev        # see the sync decision events
```

## Next steps

- Understand the [sync engine](/docs/concepts/sync-engine/).
- Explore [isolation profiles](/docs/guides/isolation/).
- Predict host placement with [`explain-sync`](/docs/guides/explain-sync/).
