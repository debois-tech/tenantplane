---
title: "Architecture"
description: "How the controller, control planes, isolation, and sync engine fit together."
weight: 3
---

tenantplane separates the **user-facing tenant API** from the **host-cluster
implementation details**. Everything is driven by three custom resources
reconciled by a single controller running on the host cluster.

## Components

### The controller

A controller-runtime manager watches `TenantCluster` resources (and re-reconciles
when a referenced `IsolationProfile` or `SyncPolicy` changes). For each
TenantCluster it drives the whole lifecycle: isolation, control plane, kubeconfig
extraction, and sync.

### Tenant control plane

Each tenant gets a small [k3s](https://k3s.io) control plane running as a
single-replica StatefulSet in a host namespace, fronted by a headless Service.
The controller runs k3s with the agent and bundled add-ons disabled so the pod
is purely an API server + datastore. The datastore is SQLite for the current
milestone.

### Isolation

The referenced IsolationProfile is compiled into concrete host objects — a
default-deny NetworkPolicy, a ResourceQuota, a LimitRange, and Pod Security
Admission labels on the namespace. tenantplane's own control-plane pods carry an
exemption label so the default-deny policy never cuts them off.

### Sync engine

Once the control plane is Ready, the controller connects to it with the extracted
kubeconfig and runs the [sync engine](/docs/concepts/sync-engine/). For each
resource kind the SyncPolicy marks `toHost`, the engine lists tenant objects,
maps each to a deterministic host object, applies it, and garbage-collects host
objects whose tenant source is gone.

## Request flow

```
        apply TenantCluster / IsolationProfile / SyncPolicy
                              │
                              ▼
                   ┌────────────────────┐
                   │  tenantplane        │  (controller-runtime manager)
                   │  controller         │
                   └─────────┬──────────┘
             ┌───────────────┼─────────────────────────────┐
             ▼               ▼                             ▼
      NetworkPolicy    StatefulSet (k3s)              Sync engine
      ResourceQuota    + headless Service      (virtual client ⇄ host client)
      LimitRange       + kubeconfig Secret             │
      PSA labels                                       ▼
                                              deterministic host objects
                                              + decision Events
```

## Determinism and reverse mapping

Every host object tenantplane creates is named from `<resource>-x-<virtual
namespace>-x-<tenant>` (hashed when that would exceed a DNS label) and carries
reverse-mapping labels and annotations. That means:

- the same tenant object always maps to the same host object, and
- any host object can be traced back to the tenant object that caused it.

This is what makes [`explain-sync`](/docs/guides/explain-sync/) able to predict
placement before anything is applied.

## Isolation modes

| Mode | Workloads run on | Status |
| --- | --- | --- |
| `shared` | Host nodes with software isolation controls | Implemented |
| `dedicated` | A selected node pool, shared infra services | Planned |
| `private` | Separate worker nodes, CNI, and CSI | Planned |

The design goal is migration from shared → dedicated → private **without
recreating the tenant API state**.
