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
3. **Apply** it — create if absent, otherwise update in place.
4. **Garbage-collect** — delete host objects tenantplane manages for this tenant
   and kind whose tenant source no longer exists. Objects that aren't
   tenantplane-managed are never touched.

Individual object errors are aggregated, so one bad object doesn't abandon the
rest of the pass.

## Decisions

Every action produces a `Decision` with an action (`Create`/`Update`/`Delete`/
`Skip`), the tenant reference, the host target, and a human-readable reason.
Today these are emitted as Kubernetes Events on the TenantCluster (deletes as
`Warning`, everything else `Normal`), so `kubectl describe tenantcluster` answers
"why does this host object exist?".

```bash
kubectl describe tenantcluster dev | grep -A2 Events
```
