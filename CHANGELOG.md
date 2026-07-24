# Changelog

All notable changes to tenantplane are documented here. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/); dates are UTC.

## 0.2.0-dev — unreleased

A full pass closing the P0 (correctness/security) and P1 (sync completeness)
gaps against the original roadmap. Every `SyncPolicy` setting is now honored
end to end, and the controller's own blast radius is narrowed. Still
pre-1.0: `dedicated`/`private` isolation modes, HA control planes, and a
persisted decision history remain unimplemented (see `docs/roadmap`).

### Added

- `SyncDecision`: a new namespaced CRD giving every tenant a durable,
  queryable record of its sync decisions, bounded by `explain.retain`
  — Events alone weren't queryable and aged out on cluster-default retention.
- `fromHost` and `bidirectional` sync directions, with `conflictPolicy`
  (`manual`, `tenant-wins`, `host-wins`) honored for real.
- `kubernetesVersion` now selects an actual k3s image (`v1.28`-`v1.33`);
  unsupported versions are rejected at admission instead of silently
  defaulted.
- `driftDetection.interval` sets the actual sync/reconcile cadence.
- Two `ValidatingAdmissionPolicy` objects narrowing the controller's own
  (unavoidably cluster-wide) RBAC grant to only the namespaces it manages —
  defense-in-depth alongside a trimmed `ClusterRole`.
- A friendly startup banner on manager boot.

### Fixed

- The sync engine's Pod update path failed outright on every sync pass after
  the first, once a synced Pod was actually scheduled to a node (Kubernetes
  forbids changing most Pod spec fields post-schedule). Re-syncing now
  adopts the live object's immutable fields and only re-applies what
  Kubernetes actually allows changing.

### Changed

- `SyncSupported` now reads `True` for a well-formed `SyncPolicy` — every
  field it declares is implemented, none silently ignored.

## 0.1.0-dev

Initial `shared`-mode tenant control planes on k3s, `toHost` sync with
orphan garbage collection, isolation enforcement (NetworkPolicy,
ResourceQuota, LimitRange, Pod Security Admission), and the CLI's offline
`render`/`explain-sync` commands.
