---
title: "TenantCluster"
description: "The resource that describes the lifecycle of one virtual Kubernetes tenant."
weight: 11
---

A `TenantCluster` represents a single virtual Kubernetes tenant. It references an
IsolationProfile and a SyncPolicy, and it describes how the tenant's control
plane should run.

## Example

```yaml
apiVersion: tenantplane.io/v1alpha1
kind: TenantCluster
metadata:
  name: dev
  namespace: team-dev
spec:
  mode: shared
  kubernetesVersion: v1.30.4
  isolationProfileRef:
    name: restricted
  syncPolicyRef:
    name: default
  controlPlane:
    replicas: 1
    datastore:
      type: sqlite
  networking:
    egressPolicy: deny-by-default
  migration:
    allowModeChange: true
  resources:
    cpu: "2"
    memory: 2Gi
```

## Key fields

| Field | Description |
| --- | --- |
| `mode` | `shared`, `dedicated`, or `private`. Only `shared` is implemented; other modes are accepted and reconciled as shared with a condition explaining so. |
| `kubernetesVersion` | Requested tenant Kubernetes version. Accepted today but not yet wired to k3s image selection. |
| `isolationProfileRef.name` | The IsolationProfile applied around this tenant. |
| `syncPolicyRef.name` | The SyncPolicy governing resource sync. |
| `controlPlane.replicas` | Control-plane replicas. The current milestone supports a single replica. |
| `controlPlane.datastore.type` | Datastore backend. Only `sqlite` is implemented. |
| `resources.cpu` / `resources.memory` | Caps applied to the control plane and, when the profile requires it, to the tenant's ResourceQuota. |

## Status

The controller reports progress on `.status`:

- **`phase`** — `Pending`, `Provisioning`, `Ready`, or `Degraded`.
- **`endpoint`** — the in-cluster HTTPS address of the tenant API server, once Ready.
- **`conditions`** — including `Ready`, `Synced`, and `ModeSupported`.

```bash
kubectl get tenantcluster dev -o jsonpath='{.status.phase}{"\n"}'
kubectl describe tenantcluster dev
```

## Lifecycle

{{< diagram src="/img/control-plane.svg" alt="What the controller builds for a TenantCluster: StatefulSet, k3s pod, headless Service, and kubeconfig Secret" >}}

When you apply a TenantCluster, the controller:

1. Resolves the referenced IsolationProfile and SyncPolicy (degrading if either
   is missing).
2. Applies the isolation boundary to the namespace.
3. Reconciles the headless Service and the k3s StatefulSet.
4. Waits for the control-plane pod to become ready, then extracts its kubeconfig
   into a Secret.
5. Runs a sync pass and sets the `Synced` condition.

Deleting the TenantCluster garbage-collects the objects it owns (Service,
StatefulSet, Secret, and namespaced isolation objects) via owner references.
