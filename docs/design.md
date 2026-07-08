# tenantplane design

tenantplane separates the user-facing tenant API from the host-cluster
implementation details. A single controller-runtime manager reconciles three
custom resources into running, isolated, synced tenants.

The rendered architecture diagrams live in `website/static/img/` (also embedded
in the README and the docs site):

- `architecture.svg` — the full system
- `control-plane.svg` — anatomy of one tenant control plane
- `sync-flow.svg` — the sync convergence pass
- `isolation-layers.svg` — what an IsolationProfile compiles into
- `tenancy-modes.svg` — shared / dedicated / private modes

## Core resources

`TenantCluster` describes the lifecycle of one tenant Kubernetes environment:
its mode, control-plane shape (replicas, datastore, storage class/size,
optional load-balancer exposure, extra TLS SANs), networking posture, and
references to the other two resources.

`IsolationProfile` describes the security and fairness controls applied around
a tenant. It is intentionally data-plane aware: network policy, pod security,
runtime class, resource requests, and API fairness belong in the same policy
conversation.

`SyncPolicy` describes which resources cross the virtual-to-host boundary,
the direction they flow, and how conflicts are handled.

## As-built architecture

For each `TenantCluster` the controller drives this loop:

1. **Resolve references.** The IsolationProfile and SyncPolicy are fetched;
   a missing reference degrades the tenant with an explanatory condition
   rather than failing silently.
2. **Apply isolation.** The profile compiles into a default-deny
   NetworkPolicy (exempting tenantplane's own control-plane pods via the
   `tenantplane.io/isolation-exempt` label), a ResourceQuota sized from the
   tenant's resource caps, a LimitRange with per-container defaults, and Pod
   Security Admission labels on the namespace. The PSA *enforce* label is
   capped at `baseline` — the k3s control-plane pod shares the namespace and
   runs as root, so `restricted` enforcement would reject it; audit/warn stay
   at the profile's level.
3. **Reconcile the control plane.** A k3s server (agent and bundled add-ons
   disabled) runs as a single-replica StatefulSet fronted by a headless
   Service. The PVC honors `spec.controlPlane.storage` (class + size), which
   is what makes the same spec work across EKS/AKS/GKE CSI drivers. Extra TLS
   SANs from the spec are passed to k3s.
4. **Expose (optional).** When `spec.controlPlane.expose.loadBalancer` is
   set, an additional LoadBalancer Service is reconciled with user-supplied,
   cloud-specific annotations; the provisioned address is published as
   `status.externalEndpoint`.
5. **Extract the kubeconfig.** Once the control-plane pod is ready, the
   controller execs into it, reads the k3s kubeconfig, rewrites the server
   address to the in-cluster Service FQDN, and stores it in a Secret.
6. **Sync.** The sync engine connects to the tenant with that kubeconfig and
   runs a convergence pass per `toHost` resource kind (see below). Success or
   failure is reported through the `Synced` condition.

All created objects carry owner references back to the TenantCluster, so
deleting the tenant garbage-collects everything namespaced it created.

## The sync engine

Sync is a visible subsystem, not a black box:

- **Deterministic mapping.** A tenant object maps to the host name
  `<resource>-x-<virtual-namespace>-x-<tenant>`, hash-truncated to fit a DNS
  label. The same input always yields the same host object — no bookkeeping
  table.
- **Reverse mapping.** Every host object carries selectable labels (tenant,
  virtual namespace, kind, managed-by) plus annotations preserving the
  verbatim virtual name/namespace, so identity survives name hashing.
- **Convergence pass.** List tenant objects (skipping tenant system
  namespaces) → transform (deep copy, rename, merge reverse-mapping metadata,
  strip server-populated/host-owned fields) → apply (create or
  conflict-checked update) → garbage-collect managed host objects whose
  source is gone. Foreign objects are never touched.
- **Decisions.** Every action produces one explainable Decision, emitted as a
  Kubernetes Event on the TenantCluster. `tenantplane explain-sync` predicts
  the same mapping offline from the shared constants, so prediction and
  runtime cannot drift.

## Isolation modes

`shared` (implemented) runs tenant workloads on host nodes with software
isolation. `dedicated` and `private` (planned) move workloads to dedicated
node pools and fully separate data planes respectively. The design goal is
migration between modes without recreating tenant API state.

## Deliberate limitations (current milestone)

- Single-replica control planes, SQLite datastore only.
- `toHost` sync only; `fromHost`/`bidirectional` are accepted but skipped.
- `kubernetesVersion` is accepted but not yet mapped to a k3s image.
- Control plane and tenant workloads share a namespace (hence the PSA cap).

## Next steps

1. Bidirectional sync honoring the declared conflict policy.
2. Durable SyncDecision records beyond Events.
3. Dedicated control-plane namespaces, lifting the PSA enforce cap.
4. Kubernetes-version-to-image mapping.
5. OpenTelemetry traces and Prometheus metrics for sync decisions.
