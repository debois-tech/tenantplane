# tenantplane

## Architecture

![tenantplane Architecture](resources/architecture%20diagram.png)

**tenantplane** is an open source virtual Kubernetes tenant platform built for modern platform engineering teams that need secure, transparent, and scalable multi-tenancy.

It provides a lightweight and inspectable control plane for managing virtual Kubernetes tenants while keeping synchronization, isolation, and operations predictable and explainable.

## Vision

As Kubernetes adoption grows, platform teams need multi-tenancy that is easy to operate, secure by design, and flexible enough to support different workloads and isolation requirements.

tenantplane aims to provide:

- Deterministic resource synchronization with dry-run planning and explainable output
- Open implementations for day-2 platform operations
- Configurable isolation profiles covering both control-plane and data-plane security
- Flexible migration paths between shared, dedicated, and private tenant environments
- High-density ephemeral tenant clusters for CI/CD, preview environments, and internal developer platforms
- A lightweight architecture that is easy to understand, audit, and extend

---

## Current Status

tenantplane is currently in its early development stage.

The repository includes:

- Custom Resource Definitions (CRDs)
  - `TenantCluster`
  - `IsolationProfile`
  - `SyncPolicy`
- A lightweight CLI for generating example resources
- Commands for explaining tenant-to-host resource synchronization
- Pure Go packages for sync planning and isolation profile management
- Initial deployment manifests
- Example configurations

The next milestone is implementing the Kubernetes controller that reconciles these resources into fully functional tenant control planes.

---

## Quick Start

### Build the CLI

```bash
go build ./cmd/tenantplane
```

### Generate a TenantCluster manifest

```bash
tenantplane render tenantcluster dev \
  --namespace team-dev \
  --mode shared
```

### Explain resource synchronization

```bash
tenantplane explain-sync \
  --tenant dev \
  --tenant-namespace team-dev \
  --virtual-namespace default \
  --kind Pod \
  --name nginx
```

---

## Core Resources

### TenantCluster

Represents a virtual Kubernetes tenant that can operate in shared, dedicated, or private modes.

### IsolationProfile

Defines security, networking, and resource isolation boundaries for tenant workloads.

### SyncPolicy

Defines how Kubernetes resources are synchronized between tenant environments and the host cluster with deterministic behavior.

---

## Design Principles

- Every synchronization decision should be explainable.
- Every isolation boundary should be explicit.
- Platform operations should remain transparent.
- Enterprise-grade day-2 capabilities should be open and extensible.
- Tenant environments should evolve as isolation requirements change.
- The host cluster should always remain understandable and observable.

---

## Roadmap

Planned milestones include:

- Kubernetes reconciliation controller
- Declarative synchronization engine
- Bidirectional resource synchronization
- Isolation profile enforcement
- Dry-run execution and change previews
- OpenTelemetry integration
- Prometheus metrics
- Multi-cluster tenant management
- GitOps workflows
- High-density ephemeral tenant provisioning
- Tenant lifecycle management
- Upgrade and migration workflows

---

## Why tenantplane?

tenantplane is designed for teams that value transparency, predictability, and operational simplicity.

Platform engineers should always be able to answer:

- Why was a resource synchronized?
- Why was a resource rejected?
- Which policy made this decision?
- What changes will occur before they are applied?
- How can a tenant safely migrate between isolation models?

---

## Contributing

Contributions are welcome!

Whether you're interested in Kubernetes, controllers, networking, security, observability, documentation, or testing, we'd love your help. Feel free to open an issue or submit a pull request.

---

## License

Licensed under the Apache License 2.0. See the `LICENSE` file for details.
