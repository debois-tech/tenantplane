---
title: "Why tenantplane"
description: "What tenantplane is, how it thinks about multi-tenancy, and where it draws the line."
weight: 4
---

tenantplane is a **transparency-first multi-tenancy control plane** for
Kubernetes. Its job is not to hide multi-tenancy behind an abstraction — it is to
make tenancy **explainable and auditable**. Every host object traces back to the
tenant object that caused it, every isolation boundary is an explicit resource,
and every synchronization decision is recorded.

## The thesis

Most tenancy tooling optimizes for *convenience*: give a team something that
looks like a cluster and hide the machinery. tenantplane optimizes for a
different thing — **the ability to answer questions about your platform.**

- *Why does this host object exist?* → `explain-sync`, reverse-mapping labels,
  and a decision Event for the action that created it.
- *Why was a resource rejected?* → the SyncPolicy decision that skipped it.
- *What isolation applies to this tenant, and what does it enforce?* → the
  IsolationProfile it references, compiled into concrete host objects.
- *What will change before it is applied?* → a deterministic mapping you can
  predict offline.

If that set of questions is central to how you run your platform — regulated
estates, internal platforms with audit requirements, anywhere "trust me" is not
an acceptable answer — tenantplane is built for you.

## What makes it a different product

Running a lightweight Kubernetes control plane inside a host namespace is an
industry pattern — popularized by projects like [vcluster](https://www.vcluster.com)
and used across the ecosystem. tenantplane treats that pattern as an
*implementation detail*. The product is the layer above it:

- **Explainability is the core, not a feature.** Sync decisions are first-class,
  recorded outcomes — not a debug log you grep after the fact. The system is
  designed so you can always reconstruct *why*.
- **Isolation is an explicit, composable API.** `IsolationProfile` is a
  declarative security boundary — network policy, quota, limits, pod security,
  runtime class — with named levels, not implicit defaults you discover at
  runtime.
- **Determinism you can audit offline.** `explain-sync` predicts exactly where a
  tenant object lands, from the same code the controller runs, before anything
  touches a cluster. It even refuses to overwrite a host object that belongs to
  a different tenant source, rather than silently colliding.
- **A deliberately small, inspectable surface.** Three resources and a mapping
  you can reason about by hand — no hidden coordination.

## What tenantplane is not

- It is **not** a drop-in replacement for an existing virtual-cluster tool, and
  it does not try to match one feature for feature.
- It is **not** trying to make tenancy invisible. The opposite: it makes tenancy
  legible.
- It is **early**. The differentiators above are the design center; several are
  still being built out — see the [roadmap](/docs/roadmap/).

## Where it's headed

The differentiation deepens along the explainability and auditability axis:
durable, queryable decision records beyond Events; richer isolation enforcement
wired through to admission; and OpenTelemetry traces for every sync decision. The
goal is a tenancy control plane you could put in front of an auditor.
