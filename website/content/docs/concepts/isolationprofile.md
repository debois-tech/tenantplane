---
title: "IsolationProfile"
description: "The composable security and fairness boundary applied around a tenant."
weight: 12
---

An `IsolationProfile` defines the security, networking, and resource isolation
boundary for a tenant. It is intentionally **data-plane aware**: network policy,
pod security, runtime class, resource requests, and API fairness all belong in
the same policy conversation.

## Example

```yaml
apiVersion: tenantplane.io/v1alpha1
kind: IsolationProfile
metadata:
  name: restricted
spec:
  level: restricted
  controls:
    podSecurity: restricted
    defaultDenyNetworkPolicy: true
    requireResourceRequests: true
    runtimeClassName: ""
    blockHostPathVolumes: true
    blockPrivilegedContainers: true
    apiFairness: tenant
```

## Built-in levels

tenantplane ships three levels. The CLI can render each with
`tenantplane render isolationprofile NAME --level <level>`.

| Level | Pod Security | Default-deny network | Runtime class | Intended use |
| --- | --- | --- | --- | --- |
| `baseline` | baseline | off | — | Trusted internal workloads |
| `restricted` | restricted | on | — | Default for most tenants |
| `sandboxed` | restricted | on | `kata-qemu` | Untrusted or hostile workloads |

## What the controls become

When a TenantCluster references a profile, the controller compiles its controls
into concrete namespace-scoped objects:

| Control | Host object |
| --- | --- |
| `defaultDenyNetworkPolicy` | A `NetworkPolicy` denying all ingress/egress, exempting tenantplane's own control-plane pods. |
| `requireResourceRequests` | A `ResourceQuota` (capped by the TenantCluster's `resources`) and a `LimitRange` with per-container defaults. |
| `podSecurity` | Pod Security Admission `enforce`/`audit`/`warn` labels on the namespace. |
| `runtimeClassName`, `blockHostPathVolumes`, `blockPrivilegedContainers`, `apiFairness` | Captured in the profile model; enforcement is being expanded — see the [Roadmap](/docs/roadmap/). |

Isolation objects are reconciled idempotently on every pass, so drift is
corrected automatically.
