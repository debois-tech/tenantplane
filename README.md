# tenantplane

tenantplane is an open source virtual Kubernetes tenant platform.

The goal is not to clone vCluster feature-for-feature. The goal is to build a small, inspectable control plane for teams that need:

- deterministic resource sync with dry-run and explain output
- open source day-2 operations from the start
- isolation profiles that include data-plane controls, not only API isolation
- migration paths between shared, dedicated, and private tenant modes
- high-density ephemeral tenant clusters for CI and internal platforms

## Current status

This repository is at the first scaffold stage. It includes:

- CRD definitions for `TenantCluster`, `IsolationProfile`, and `SyncPolicy`
- a dependency-light CLI for generating example resources and explaining sync names
- pure Go sync planning and isolation profile packages
- initial deployment manifests and examples

The next milestone is a Kubernetes controller that reconciles these CRDs into tenant control planes.

## Quick start

Build the CLI:

```sh
go build ./cmd/tenantplane
```

Generate a tenant cluster manifest:

```sh
tenantplane render tenantcluster dev --namespace team-dev --mode shared
```

Explain how a tenant resource maps to the host:

```sh
tenantplane explain-sync --tenant dev --tenant-namespace team-dev --virtual-namespace default --kind Pod --name nginx
```

## Design principles

- Every sync decision should be explainable.
- Every isolation boundary should be explicit.
- Every enterprise-critical day-2 feature should have an open implementation path.
- Tenants should be migratable as their isolation needs change.
- The host cluster should never become a mystery box.

## License

Apache-2.0

