---
title: "Applying isolation"
description: "Choose an isolation level and verify the boundary it creates."
weight: 22
---

Isolation in tenantplane is explicit: an [IsolationProfile](/docs/concepts/isolationprofile/)
compiles into concrete host objects you can inspect. This guide shows how to pick
a level and verify what it produced.

## Choose a level

Render a profile at the level you want and apply it:

```bash
tenantplane render isolationprofile restricted --level restricted | kubectl apply -f -
```

Available levels: `baseline`, `restricted` (default), and `sandboxed`. See the
[concept page](/docs/concepts/isolationprofile/) for the full comparison.

## Reference it from a tenant

```yaml
spec:
  isolationProfileRef:
    name: restricted
```

## Verify the boundary

After the tenant reconciles, inspect the objects the profile produced in the
tenant's namespace:

```bash
# Default-deny network policy (restricted / sandboxed)
kubectl -n team-dev get networkpolicy tenantplane-default-deny -o yaml

# Resource quota, sized from the TenantCluster's resources
kubectl -n team-dev get resourcequota tenantplane-quota

# Per-container defaults
kubectl -n team-dev get limitrange tenantplane-defaults

# Pod Security Admission labels
kubectl get namespace team-dev -o jsonpath='{.metadata.labels}' | tr ',' '\n' | grep pod-security
```

## How the control plane stays reachable

A default-deny NetworkPolicy would normally cut off the tenant's own API server.
tenantplane labels its control-plane pods with
`tenantplane.io/isolation-exempt: "true"`, and the generated policy excludes
exactly those pods — so isolation applies to workloads without breaking the
control plane.

## Drift correction

Isolation objects are reconciled on every pass. If someone edits the
NetworkPolicy or deletes the LimitRange, the next reconcile restores it to match
the profile.
