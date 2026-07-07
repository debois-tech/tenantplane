---
title: "SyncPolicy"
description: "Which resources cross the virtual-to-host boundary, and how conflicts resolve."
weight: 13
---

A `SyncPolicy` describes which resources cross the virtual-to-host boundary, the
direction they flow, and how conflicts are handled. It is what turns a tenant
control plane into a *working* virtual cluster whose workloads actually run on
host nodes.

## Example

```yaml
apiVersion: tenantplane.io/v1alpha1
kind: SyncPolicy
metadata:
  name: default
spec:
  conflictPolicy: manual
  driftDetection:
    enabled: true
    interval: 30s
  explain:
    recordDecisions: true
    retain: 1000
  resources:
    - apiVersion: v1
      kind: Pod
      direction: toHost
    - apiVersion: v1
      kind: Service
      direction: bidirectional
    - apiVersion: v1
      kind: ConfigMap
      direction: bidirectional
    - apiVersion: v1
      kind: Secret
      direction: bidirectional
```

## Directions

| Direction | Meaning | Status |
| --- | --- | --- |
| `toHost` | Tenant objects are projected onto the host. | Implemented |
| `fromHost` | Host objects are reflected back into the tenant. | Planned |
| `bidirectional` | Synced both ways with conflict resolution. | Planned |

Entries whose direction is not yet implemented are **accepted but skipped** — the
engine never pretends to have synced something it can't.

## Conflict policy

`conflictPolicy` selects how bidirectional conflicts resolve. `manual` is the
safe default: the engine records the conflict rather than guessing. `tenant-wins`
and `host-wins` will be honored as bidirectional sync lands.

## Explainability

- `explain.recordDecisions` toggles decision recording.
- Every sync action (create, update, delete, skip) produces one decision,
  surfaced today as a Kubernetes Event on the owning TenantCluster.

See the [sync engine](/docs/concepts/sync-engine/) for how these declarations are
executed.
