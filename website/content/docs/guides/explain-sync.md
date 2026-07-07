---
title: "Predicting sync with explain-sync"
description: "Ask where a tenant resource will land on the host — before applying anything."
weight: 23
---

Because host placement is [deterministic](/docs/concepts/sync-engine/), you can
predict exactly where a tenant object will land without touching a cluster. The
`explain-sync` command does this offline.

## Usage

```bash
tenantplane explain-sync \
  --tenant dev \
  --tenant-namespace team-dev \
  --virtual-namespace default \
  --kind Pod \
  --name nginx
```

Output:

```yaml
tenantResource:
  tenantCluster: dev
  virtualNamespace: default
  kind: Pod
  name: nginx
hostResource:
  namespace: team-dev
  name: nginx-x-default-x-dev
labels:
  app.kubernetes.io/managed-by: tenantplane
  tenantplane.io/tenant: dev
  tenantplane.io/virtual-namespace: default
  tenantplane.io/kind: pod
annotations:
  tenantplane.io/virtual-namespace: default
  tenantplane.io/virtual-name: nginx
reason:
  tenantplane uses a stable name made from resource, virtual namespace, and
  tenant cluster; labels preserve the reverse mapping.
```

The `labels` and `annotations` blocks are exactly what the sync engine will stamp
on the host object — the command shares the same constants the engine uses, so
the prediction never drifts from reality.

## Flags

| Flag | Required | Default | Description |
| --- | --- | --- | --- |
| `--tenant` | yes | — | TenantCluster name. |
| `--tenant-namespace` | yes | — | Host namespace of the tenant. |
| `--name` | yes | — | Resource name. |
| `--virtual-namespace` | no | `default` | Namespace as seen inside the tenant. |
| `--kind` | no | `Pod` | Resource kind. |

## Why it's useful

- Confirm naming before an audit or a migration.
- Find the host object for a given tenant object (and vice versa) by hand.
- Sanity-check that a long name will be hashed the way you expect.
