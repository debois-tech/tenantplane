---
title: "CLI reference"
description: "The tenantplane command-line interface."
weight: 30
---

The `tenantplane` CLI generates example resources and explains sync placement. It
is a thin, dependency-free binary — useful in CI, scripts, and for learning the
API.

```bash
go build ./cmd/tenantplane
```

## Commands

### `version`

```bash
tenantplane version
```

### `render tenantcluster`

```bash
tenantplane render tenantcluster NAME \
  [--namespace ns] \
  [--mode shared|dedicated|private] \
  [--isolation-profile NAME] \
  [--sync-policy NAME] \
  [--kubernetes-version VERSION]
```

Prints a `TenantCluster` manifest to stdout.

### `render isolationprofile`

```bash
tenantplane render isolationprofile NAME [--level baseline|restricted|sandboxed]
```

Prints an `IsolationProfile` for the chosen built-in level.

### `render syncpolicy`

```bash
tenantplane render syncpolicy NAME [--conflict-policy manual|tenant-wins|host-wins]
```

Prints a `SyncPolicy` with a sensible default resource set.

### `explain-sync`

```bash
tenantplane explain-sync \
  --tenant NAME \
  --tenant-namespace NS \
  --name RESOURCE_NAME \
  [--virtual-namespace default] \
  [--kind Pod]
```

Predicts the host namespace, name, labels, and annotations a tenant resource will
receive. See the [guide](/docs/guides/explain-sync/).

## Conventions

- Names passed to `render` must already be DNS-safe; the CLI rejects names that
  would be rewritten by sanitization, so what you type is what you get.
- All commands write to stdout and exit non-zero on error, so they compose
  cleanly with `kubectl apply -f -`.
