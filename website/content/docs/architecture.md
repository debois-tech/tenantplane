---
title: "Architecture"
description: "How the controller, control planes, isolation, and sync engine fit together."
weight: 3
---

tenantplane separates the **user-facing tenant API** from the **host-cluster
implementation details**. Everything is driven by three custom resources
reconciled by a single controller running on the host cluster.

<img src="/img/architecture.svg" alt="tenantplane architecture" style="width:100%;height:auto;margin:1.5rem 0;" />

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
milestone. The control-plane volume honors `spec.controlPlane.storage`
(StorageClass + size), so the same spec works against the EBS, Azure Disk, and
Persistent Disk CSI drivers; `spec.controlPlane.expose.loadBalancer` optionally
publishes the tenant API through a cloud load balancer, with the provisioned
address reported as `status.externalEndpoint`.

<img src="/img/control-plane.svg" alt="Anatomy of a tenant control plane: StatefulSet, k3s pod, headless Service, and kubeconfig Secret" style="width:100%;height:auto;margin:1rem 0;" />

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

## The sync convergence pass

Once the control plane is Ready, each `toHost` resource kind runs the same four
deterministic steps, recording a decision at every step:

<img src="/img/sync-flow.svg" alt="tenantplane sync convergence pass" style="width:100%;height:auto;margin:1rem 0;" />

## Determinism and reverse mapping

Every host object tenantplane creates is named from `<resource>-x-<virtual
namespace>-x-<tenant>` (hashed when that would exceed a DNS label) and carries
reverse-mapping labels and annotations. That means:

- the same tenant object always maps to the same host object, and
- any host object can be traced back to the tenant object that caused it.

This is what makes [`explain-sync`](/docs/guides/explain-sync/) able to predict
placement before anything is applied.

## Isolation modes

<img src="/img/tenancy-modes.svg" alt="tenantplane isolation modes: shared, dedicated, and private, with migration paths between them" style="width:100%;height:auto;margin:1rem 0;" />

| Mode | Workloads run on | Status |
| --- | --- | --- |
| `shared` | Host nodes with software isolation controls | Implemented |
| `dedicated` | A selected node pool, shared infra services | Planned |
| `private` | Separate worker nodes, CNI, and CSI | Planned |

The design goal is migration from shared → dedicated → private **without
recreating the tenant API state**.
