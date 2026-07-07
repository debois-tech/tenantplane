---
title: "Concepts"
description: "The three resources and the sync engine that make up tenantplane's model."
weight: 10
---

tenantplane has a deliberately small surface: three custom resources and one
sync engine. Understand these four things and you understand the whole system.

- **[TenantCluster](/docs/concepts/tenantcluster/)** — the lifecycle of one
  virtual tenant.
- **[IsolationProfile](/docs/concepts/isolationprofile/)** — the security and
  fairness boundary around a tenant.
- **[SyncPolicy](/docs/concepts/syncpolicy/)** — which resources cross the
  virtual-to-host boundary and how.
- **[Sync engine](/docs/concepts/sync-engine/)** — how those decisions are
  applied and kept from drifting.
