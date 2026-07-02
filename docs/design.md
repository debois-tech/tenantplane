# tenantplane design

tenantplane separates the user-facing tenant API from the host-cluster implementation details.

## Core resources

`TenantCluster` describes the lifecycle of one tenant Kubernetes environment.

`IsolationProfile` describes the security and fairness controls applied around a tenant. It is intentionally data-plane aware: network policy, pod security, runtime class, resource requests, and API fairness belong in the same policy conversation.

`SyncPolicy` describes which resources cross the virtual-to-host boundary and how conflicts are handled.

## First architecture target

The first implementation target is shared-node tenancy for internal developer platforms and CI:

1. A tenant control plane runs in a host namespace.
2. The sync planner maps tenant resources into deterministic host resources.
3. Host resources receive labels that preserve reverse lookup.
4. Isolation profiles apply default-deny networking, pod security, quotas, and scheduling constraints.
5. Every sync action is recorded so an operator can ask why a host object exists.

## Why deterministic sync first

Virtual clusters become difficult to operate when the host view and tenant view drift. tenantplane treats sync as a visible subsystem:

- `explain-sync` predicts where a tenant object will land.
- sync decisions are designed to be stored as events or status records.
- conflict policy is explicit, with `manual` as the safe default.
- labels preserve reverse mapping from host resources to tenant resources.

## Isolation modes

`shared` runs tenant workloads on host nodes and applies software isolation controls.

`dedicated` runs tenant workloads on a selected node pool but still uses host infrastructure services.

`private` gives the tenant separate worker nodes, CNI, and CSI. The design goal is to support migration from shared to dedicated to private without recreating the tenant API state.

## Next implementation steps

1. Add controller-runtime and generated Kubernetes API types.
2. Reconcile `TenantCluster` into a control-plane StatefulSet and service.
3. Reconcile `IsolationProfile` into NetworkPolicy, ResourceQuota, LimitRange, and Pod Security labels.
4. Implement the first sync loop for Pods, Services, ConfigMaps, and Secrets.
5. Store sync decisions in a `SyncDecision` status or event stream.

