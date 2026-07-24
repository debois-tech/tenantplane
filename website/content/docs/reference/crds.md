---
title: "CRD reference"
description: "Field-level reference for the tenantplane custom resources."
weight: 31
---

All three resources live in the `tenantplane.io/v1alpha1` API group. The
authoritative definitions are the CRDs in `config/crd`; this page summarizes the
fields.

## TenantCluster

| Field | Type | Notes |
| --- | --- | --- |
| `spec.mode` | string | `shared` \| `dedicated` \| `private`. Only `shared` implemented. |
| `spec.kubernetesVersion` | string | Requested tenant version (not yet image-selecting). |
| `spec.isolationProfileRef.name` | string | Referenced IsolationProfile. |
| `spec.syncPolicyRef.name` | string | Referenced SyncPolicy. |
| `spec.controlPlane.replicas` | int | Control-plane replicas (single supported). |
| `spec.controlPlane.datastore.type` | string | `sqlite` only. |
| `spec.controlPlane.storage.className` | string | StorageClass for the control-plane volume (e.g. `gp3` on EKS, `managed-csi` on AKS, `standard-rwo` on GKE). Empty = cluster default. |
| `spec.controlPlane.storage.size` | string | Control-plane volume size (default `1Gi`). |
| `spec.controlPlane.expose.loadBalancer` | bool | Publish the tenant API server via a LoadBalancer Service. |
| `spec.controlPlane.expose.annotations` | map | Annotations for the LoadBalancer Service (cloud-specific behavior). |
| `spec.controlPlane.extraTLSSANs` | []string | Extra TLS SANs for the tenant API certificate (e.g. the LB address). |
| `spec.networking.podCIDR` / `serviceCIDR` | string | Optional tenant CIDRs. |
| `spec.networking.egressPolicy` | string | Egress posture, e.g. `deny-by-default`. |
| `spec.migration.allowModeChange` | bool | Whether mode migration is permitted. |
| `spec.resources.cpu` / `memory` | string | Control-plane and quota sizing. |
| `status.phase` | string | `Pending`/`Provisioning`/`Ready`/`Degraded`. |
| `status.endpoint` | string | In-cluster tenant API server address once Ready. |
| `status.externalEndpoint` | string | Load-balancer address once `expose.loadBalancer` has provisioned. |
| `status.conditions[]` | list | `Ready`, `Synced`, `ModeSupported`. |

## IsolationProfile

| Field | Type | Notes |
| --- | --- | --- |
| `spec.level` | string | `baseline` \| `restricted` \| `sandboxed`. |
| `spec.controls.podSecurity` | string | PSA level applied to the namespace. |
| `spec.controls.defaultDenyNetworkPolicy` | bool | Create a default-deny NetworkPolicy. |
| `spec.controls.requireResourceRequests` | bool | Create ResourceQuota + LimitRange. |
| `spec.controls.runtimeClassName` | string | Optional runtime class for tenant pods. |
| `spec.controls.blockHostPathVolumes` | bool | Policy intent (enforcement expanding). |
| `spec.controls.blockPrivilegedContainers` | bool | Policy intent (enforcement expanding). |
| `spec.controls.apiFairness` | string | API fairness posture. |

## SyncPolicy

| Field | Type | Notes |
| --- | --- | --- |
| `spec.conflictPolicy` | string | `manual` \| `tenant-wins` \| `host-wins`. |
| `spec.driftDetection.enabled` | bool | Whether drift detection is on. |
| `spec.driftDetection.interval` | string | Drift check cadence, e.g. `30s`. |
| `spec.explain.recordDecisions` | bool | Record a decision per sync action. |
| `spec.explain.retain` | int | How many decisions to retain. |
| `spec.resources[].apiVersion` | string | e.g. `v1`. |
| `spec.resources[].kind` | string | e.g. `Pod`, `ConfigMap`. |
| `spec.resources[].direction` | string | `toHost` \| `fromHost` \| `bidirectional` — all implemented. |

## Reverse-mapping metadata

Host objects created by the sync engine carry these labels and annotations:

| Key | Kind | Meaning |
| --- | --- | --- |
| `app.kubernetes.io/managed-by=tenantplane` | label | Ownership marker. |
| `tenantplane.io/tenant` | label | Tenant name. |
| `tenantplane.io/virtual-namespace` | label | Virtual namespace (sanitized). |
| `tenantplane.io/kind` | label | Resource kind. |
| `tenantplane.io/virtual-namespace` | annotation | Verbatim virtual namespace. |
| `tenantplane.io/virtual-name` | annotation | Verbatim virtual name. |
