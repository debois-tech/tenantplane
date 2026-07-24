---
title: "Roadmap"
description: "What's built, what's next, and where tenantplane is headed."
weight: 40
---

tenantplane is in early development. This roadmap reflects the direction; it is
not a commitment to dates or ordering.

## Built today

- Custom resources: `TenantCluster`, `IsolationProfile`, `SyncPolicy`,
  `SyncDecision`.
- Controller that reconciles a shared-mode k3s control plane (StatefulSet +
  headless Service) in a dedicated control-plane namespace, separate from
  tenant workloads, so Pod Security is enforced at the profile's real
  declared level (including `restricted`).
- Isolation enforcement: default-deny NetworkPolicy, ResourceQuota, LimitRange,
  Pod Security Admission labels, and runtimeClassName/apiFairness enforced by
  both the sync engine and a ValidatingAdmissionPolicy backstop (Kubernetes
  1.30+).
- Tenant kubeconfig extraction into a host Secret.
- Full sync engine: `toHost`, `fromHost`, and `bidirectional` directions, all
  with orphan garbage collection; `bidirectional` honors `conflictPolicy`
  (`manual`, `tenant-wins`, `host-wins`).
- Sync decisions recorded as Kubernetes Events and, when
  `explain.recordDecisions` is set, in a durable, queryable `SyncDecision`
  object per tenant (capped by `explain.retain`).
- Controller RBAC narrowed to the namespaces it actually manages, with the
  same ValidatingAdmissionPolicy backstop pattern hardening it further.
- CLI: resource rendering and offline `explain-sync`.
- Managed Kubernetes support (EKS, AKS, GKE): storage class selection,
  LoadBalancer exposure with cloud annotations, extra TLS SANs.

## Next

- **Honor `driftDetection.interval`** — sync currently runs on the
  controller's fixed resync cadence regardless of what a SyncPolicy declares.
  A persisted decision history (now that `SyncDecision` exists) would also let
  conflict detection compare against the last synced state instead of only
  current tenant vs. current host state.
- **Kubernetes version selection** — map `kubernetesVersion` to a k3s image.
- **Multi-replica / HA control planes** and non-SQLite datastores.

## Later

- OpenTelemetry tracing and Prometheus metrics.
- `dedicated` and `private` isolation modes.
- Migration workflows across isolation models without recreating tenant state.
- GitOps workflows and high-density ephemeral tenant provisioning.
- Tenant lifecycle: upgrades and safe teardown.

## Get involved

Contributions are welcome across Kubernetes controllers, networking, security,
observability, docs, and testing. Open an issue or a pull request on
[GitHub](https://github.com/debois-tech/tenantplane).
