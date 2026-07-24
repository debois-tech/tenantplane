---
title: "Sync engine"
description: "How tenant objects become deterministic host objects — and how they're traced back."
weight: 14
---

The sync engine is the runtime that executes a [SyncPolicy](/docs/concepts/syncpolicy/).
Where the *planner* decides where a tenant object should land, the *engine*
actually applies and garbage-collects the host objects — recording an
explainable decision for every action.

## Deterministic naming

Every host object is named from the tenant object's identity:

```
<resource name>-x-<virtual namespace>-x-<tenant cluster>
```

For example, a Pod `nginx` in the tenant's `default` namespace on tenant `dev`
becomes `nginx-x-default-x-dev`. If that would exceed the 63-character DNS label
limit, the name is truncated and suffixed with a stable hash so it stays unique
and within bounds.

This determinism means the same tenant object **always** maps to the same host
object — no bookkeeping table required.

## Reverse mapping

Determinism gives you the forward direction; labels and annotations give you the
reverse. Every host object carries:

| Metadata | Key | Purpose |
| --- | --- | --- |
| Label | `app.kubernetes.io/managed-by: tenantplane` | Marks the object as tenantplane-owned. |
| Label | `tenantplane.io/tenant` | Selectable tenant name. |
| Label | `tenantplane.io/virtual-namespace` | Selectable virtual namespace. |
| Label | `tenantplane.io/kind` | Selectable kind. |
| Annotation | `tenantplane.io/virtual-namespace` | Verbatim original namespace. |
| Annotation | `tenantplane.io/virtual-name` | Verbatim original name (survives name hashing). |

Because the name can be hashed, the original identity is preserved verbatim in
annotations — so a reverse lookup is always exact, even for long names.

## A convergence pass

For each `toHost` resource kind, one pass does:

1. **List** tenant objects of that kind, skipping tenant system namespaces
   (`kube-system`, `kube-public`, `kube-node-lease`).
2. **Transform** each into its host object: deep-copy, rename/re-namespace,
   merge reverse-mapping metadata, and strip server-populated and host-owned
   fields (`resourceVersion`, `uid`, `ownerReferences`, `status`, …).
3. **Apply** it — create if absent, otherwise update in place. Before
   overwriting, the engine checks the existing object's provenance: if it belongs
   to a *different* tenant source (a name collision) or to no tenantplane source
   at all (a foreign object occupying the name), it refuses to clobber and records
   a `Skip` instead.
4. **Garbage-collect** — delete host objects tenantplane manages for this tenant
   and kind whose tenant source no longer exists. Objects that aren't
   tenantplane-managed are never touched.

Individual object errors are aggregated, so one bad object doesn't abandon the
rest of the pass.

## fromHost and bidirectional

Discovery always enumerates the tenant's own objects — even for `fromHost` and
`bidirectional` — so deleting the tenant object removes the host mirror too, in
every direction. Where they differ is what happens once both sides exist:

- **`fromHost`**: a missing host mirror is bootstrap-created from the tenant
  (there is nothing on the host yet to prefer), exactly like `toHost`'s first
  create. Once it exists, every later pass pulls the host's current content
  into the tenant instead of pushing the tenant's content out — the host is
  authoritative from then on.
- **`bidirectional`**: same bootstrap-create, but afterward the engine compares
  tenant and host on every pass. If they already agree, nothing happens. If
  they disagree, [`conflictPolicy`](/docs/concepts/syncpolicy/#conflict-policy)
  decides which way it resolves.

Pulling host content into the tenant only ever replaces the object's *content*
(everything but `metadata`/`status`) — the tenant's own labels, annotations, and
resourceVersion are left alone, since the tenant object is something the tenant
still manages, not something tenantplane owns outright the way a host mirror is.

### Convergence history

When `explain.recordDecisions` is set, a `SyncDecision` object also tracks —
per host object, in `status.lastConverged` — the tenant and host
resourceVersions the last time a `bidirectional` pair was confirmed to agree.
resourceVersion is an opaque, monotonically-changing "has anyone written this
since I last looked" marker, which is exactly what's needed here without
hashing content.

This is what lets `conflictPolicy: manual` tell a one-sided drift from a
genuine conflict: if only one side's resourceVersion has moved since the
recorded baseline, that side's change propagates automatically — it isn't
really a conflict, just an ordinary update the other side hasn't caught up to
yet. Only when both sides have moved (or no baseline is recorded yet) does
`manual` actually leave both sides untouched. `tenant-wins`/`host-wins` are
unaffected by this — they're already fully decisive, so they enforce their
declared winner on any disagreement regardless of history.

Without `explain.recordDecisions` there's no `SyncDecision` object to persist
this in, so `bidirectional` falls back to comparing only current tenant vs.
current host state on every pass, exactly as if this history didn't exist.

### Name collisions

The host name is deterministic but not perfectly injective: names over the
63-character DNS limit are truncated and suffixed with a hash, so two very long
names could in principle map to the same host name. tenantplane does not pretend
this can't happen — it makes it *safe*. The apply step above never overwrites an
object that reverse-maps to a different source, turning a would-be silent
data-loss into a visible `Skip` decision you can act on.

## Decisions

Every action produces a `Decision` with an action (`Create`/`Update`/`Delete`/
`Skip`), the tenant reference, the host target, and a human-readable reason.
Today these are emitted as Kubernetes Events on the TenantCluster (deletes as
`Warning`, everything else `Normal`), so `kubectl describe tenantcluster` answers
"why does this host object exist?".

```bash
kubectl describe tenantcluster dev | grep -A2 Events
```
