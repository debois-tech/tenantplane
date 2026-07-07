---
title: "Roadmap"
description: "What's built, what's next, and where tenantplane is headed."
weight: 40
---

tenantplane is in early development. This roadmap reflects the direction; it is
not a commitment to dates or ordering.

## Built today

- Custom resources: `TenantCluster`, `IsolationProfile`, `SyncPolicy`.
- Controller that reconciles a shared-mode k3s control plane (StatefulSet +
  headless Service).
- Isolation enforcement: default-deny NetworkPolicy, ResourceQuota, LimitRange,
  and Pod Security Admission labels.
- Tenant kubeconfig extraction into a host Secret.
- Deterministic host-ward sync (`toHost`) with orphan garbage collection.
- Sync decisions recorded as Kubernetes Events.
- CLI: resource rendering and offline `explain-sync`.

## Next

- **Bidirectional sync** — `fromHost` and `bidirectional` directions with the
  declared conflict policy honored.
- **SyncDecision records** — a durable, queryable decision stream beyond Events.
- **Expanded isolation enforcement** — runtime class, host-path, and privileged
  container controls wired through to admission.
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
[GitHub](https://github.com/tenantplane/tenantplane).
