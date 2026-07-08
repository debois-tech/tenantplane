---
title: "Introduction"
description: "What tenantplane is, who it's for, and the principles behind it."
weight: 1
---

**tenantplane** is a lightweight, inspectable control plane for managing virtual
Kubernetes tenants. It gives platform engineering teams multi-tenancy where
synchronization, isolation, and day-2 operations stay predictable and
explainable.

## The problem

As Kubernetes adoption grows, platform teams need to hand out cluster-like
environments to many teams, CI pipelines, and preview deployments — without
running a full cluster for each one. Existing virtual-cluster approaches work,
but they often become hard to operate: the host view and the tenant view drift,
isolation is implicit, and nobody can answer *why* a particular host object
exists.

## The approach

tenantplane treats the two hardest parts of multi-tenancy — **synchronization**
and **isolation** — as first-class, visible subsystems.

- Each tenant runs a small [k3s](https://k3s.io) control plane inside a host
  namespace, provisioned and reconciled by the tenantplane controller.
- A [SyncPolicy](/docs/concepts/syncpolicy/) declares which resources cross the
  virtual-to-host boundary. The [sync engine](/docs/concepts/sync-engine/) maps
  every tenant object to a **deterministic** host object and records a decision
  for each action.
- An [IsolationProfile](/docs/concepts/isolationprofile/) composes NetworkPolicy,
  ResourceQuota, LimitRange, and Pod Security into one explicit boundary.

## Who it's for

- Platform teams building internal developer platforms.
- CI/CD and preview-environment systems that need high-density, ephemeral
  tenant clusters.
- Anyone who has to answer audit questions about what runs where and why.

Coming from vcluster, Kamaji, or Capsule and wondering how tenantplane differs?
See [Why tenantplane](/docs/why-tenantplane/) — it uses the per-tenant
control-plane pattern as an implementation detail and builds a distinct,
explainability-first product on top.

## Design principles

- Every synchronization decision should be explainable.
- Every isolation boundary should be explicit.
- Platform operations should remain transparent.
- The host cluster should always remain understandable and observable.
- Tenant environments should evolve as isolation requirements change.

## Project status

tenantplane is in **early development**. Today the controller provisions a
shared-mode control plane, applies isolation, extracts a tenant kubeconfig, and
runs host-ward resource sync with decisions surfaced as Kubernetes Events. See
the [Roadmap](/docs/roadmap/) for what's next, and expect pre-1.0 APIs to change.
