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
| `toHost` | Tenant objects are projected onto the host; the tenant is authoritative. | Implemented |
| `fromHost` | The host mirror is bootstrap-created from the tenant, then the host is authoritative from then on: its content is reflected back into the tenant every pass. | Implemented |
| `bidirectional` | Either side may drift. When tenant and host currently agree, nothing happens; when they disagree, `conflictPolicy` decides. | Implemented |

Discovery always enumerates the tenant's own objects, in every direction — so
deleting the tenant object removes the host mirror too, regardless of which way
content flows. There is no persisted history of prior syncs: each pass compares
*current* tenant vs. *current* host state, so a real, resolved conflict looks
the same to the engine as one side having drifted alone.

## Conflict policy

`conflictPolicy` selects how a `bidirectional` disagreement resolves:

- `manual` (the safe default) — when `explain.recordDecisions` is set, a
  one-sided drift (only the tenant or only the host changed since they last
  agreed) auto-resolves in the direction of that change; only a genuine
  two-sided change — or no recorded history yet — is left alone as a `Skip`
  decision. Without `explain.recordDecisions` there's nowhere to persist that
  history, so every disagreement is treated as needing a human, exactly as if
  this distinction didn't exist.
- `tenant-wins` — the tenant's current state is always pushed to the host,
  like `toHost`, on any disagreement — a one-sided drift doesn't change this.
- `host-wins` — the host's current state is always pulled into the tenant,
  like `fromHost`, on any disagreement — same here.

## Explainability

- `explain.recordDecisions` toggles decision recording entirely. `false` means
  no decision trail at all; `true` records every sync action (create, update,
  delete, skip) both as a Kubernetes Event on the owning TenantCluster and in a
  durable `SyncDecision` object.
- `explain.retain` caps how many decisions the `SyncDecision` object keeps —
  the oldest are evicted first. Events, separately, still age out on the
  cluster's own default retention.

A `SyncDecision` is a namespaced object named after its TenantCluster (and
owned by it), e.g. `kubectl get syncdecision dev -n team-dev -o yaml`. Unlike
Events, its entries aren't subject to cluster-wide Event retention and can be
queried directly.

See the [sync engine](/docs/concepts/sync-engine/) for how these declarations are
executed.
