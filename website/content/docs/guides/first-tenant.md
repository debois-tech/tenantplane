---
title: "Your first tenant"
description: "Create a shared-mode tenant, connect to it, and see resources sync to the host."
weight: 21
---

This guide assumes the controller is [installed](/docs/guides/install/). We'll
create a tenant, connect to its control plane, and watch a ConfigMap sync to the
host.

## 1. Create the namespace and supporting resources

```bash
kubectl create namespace team-dev
kubectl apply -f config/samples/isolationprofile_restricted.yaml
kubectl apply -f config/samples/syncpolicy_default.yaml
```

## 2. Create the tenant

You can use the sample, or generate a manifest with the CLI:

```bash
tenantplane render tenantcluster dev \
  --namespace team-dev \
  --mode shared \
  --isolation-profile restricted \
  --sync-policy default | kubectl apply -f -
```

## 3. Wait for Ready

```bash
kubectl -n team-dev get tenantcluster dev -w
```

When `PHASE` is `Ready`, both the `Ready` and `Synced` conditions are true:

```bash
kubectl -n team-dev get tenantcluster dev \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {end}{"\n"}'
```

## 4. Connect to the tenant

```bash
kubectl -n team-dev get secret dev-control-plane-kubeconfig \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > tenant.kubeconfig

export KUBECONFIG_TENANT=tenant.kubeconfig
kubectl --kubeconfig "$KUBECONFIG_TENANT" get namespaces
```

> The kubeconfig's server address points at the in-cluster Service FQDN, so run
> these commands from inside the cluster (or port-forward the control-plane
> Service) to reach it.

## 5. Create something that syncs

The default SyncPolicy marks ConfigMaps for sync. Create one in the tenant:

```bash
kubectl --kubeconfig "$KUBECONFIG_TENANT" -n default \
  create configmap app-config --from-literal=key=value
```

Within a reconcile cycle it appears on the host, deterministically named:

```bash
kubectl -n team-dev get configmap app-config-x-default-x-dev -o yaml
```

## 6. Ask why it exists

```bash
kubectl -n team-dev describe tenantcluster dev | sed -n '/Events/,$p'
```

You'll see a `SyncCreate` event tracing the host object back to its tenant
source. Delete the tenant ConfigMap and the host copy is garbage-collected on
the next pass.

## Clean up

```bash
kubectl -n team-dev delete tenantcluster dev
```

Owner references cascade the delete to the Service, StatefulSet, kubeconfig
Secret, and namespaced isolation objects.
