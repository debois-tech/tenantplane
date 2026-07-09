---
title: "Deploy on Google GKE"
description: "Run tenantplane on Google Kubernetes Engine."
weight: 26
---

This guide deploys tenantplane on a GKE Standard cluster with Persistent Disk
storage and optional exposure through a Google Cloud load balancer.

{{< diagram src="/img/gke-architecture.svg" alt="tenantplane on Google GKE: Artifact Registry image pull, controller reconciling a tenant namespace, Persistent Disk CSI storage, and optional Cloud Load Balancer exposure" >}}

## Prerequisites

- `gcloud` CLI authenticated, plus `kubectl` and `docker`
- A GKE **Standard** cluster with network policy enforcement. Dataplane V2 has
  enforcement built in:

```bash
gcloud container clusters create tenantplane \
  --zone us-central1-a --num-nodes 2 --machine-type e2-standard-2 \
  --enable-dataplane-v2
gcloud container clusters get-credentials tenantplane --zone us-central1-a
```

(On older clusters, `--enable-network-policy` enables Calico instead. GKE
Autopilot is not yet validated for tenantplane.)

## 1. Storage

GKE ships CSI-backed StorageClasses out of the box. `standard-rwo`
(pd-balanced) is the default and works well for control planes:

```bash
kubectl get storageclass
```

## 2. Push the controller image to Artifact Registry

```bash
gcloud artifacts repositories create tenantplane \
  --repository-format=docker --location=us-central1
gcloud auth configure-docker us-central1-docker.pkg.dev

make docker-push IMG=us-central1-docker.pkg.dev/<PROJECT_ID>/tenantplane/manager:dev
```

## 3. Install tenantplane

```bash
kubectl apply -f config/crd
# Point the Deployment at your Artifact Registry image, then:
make deploy
kubectl -n tenantplane-system rollout status deploy/tenantplane-controller
```

## 4. Create a tenant with Persistent Disk storage

```yaml
spec:
  controlPlane:
    storage:
      className: standard-rwo
      size: 2Gi
```

```bash
kubectl apply -f config/samples/isolationprofile_restricted.yaml
kubectl apply -f config/samples/syncpolicy_default.yaml
kubectl apply -f config/samples/tenantcluster_cloud.yaml
kubectl -n team-dev get tenantcluster cloud-dev -w
```

## 5. Optional: expose the tenant API via a load balancer

For an internal (VPC-only) load balancer:

```yaml
spec:
  controlPlane:
    expose:
      loadBalancer: true
      annotations:
        networking.gke.io/load-balancer-type: "Internal"
```

Omit the annotation for an external passthrough load balancer. Read the
provisioned address from status:

```bash
kubectl -n team-dev get tenantcluster cloud-dev \
  -o jsonpath='{.status.externalEndpoint}{"\n"}'
```

Add that IP to `spec.controlPlane.extraTLSSANs` so the tenant API certificate
covers it (the control-plane pod restarts to pick up the new SAN), then point
your kubeconfig's `server:` at the external endpoint.

## Notes

- Persistent Disk is `ReadWriteOnce`, matching the single-replica control plane
  this milestone supports.
- Pod Security: tenantplane enforces `baseline` on tenant namespaces (with
  `restricted` audit/warn) so the k3s control-plane pod is admitted — see the
  [IsolationProfile docs](/docs/concepts/isolationprofile/).
