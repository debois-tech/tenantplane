package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
)

func cloudTenant() *v1alpha1.TenantCluster {
	tc := &v1alpha1.TenantCluster{}
	tc.Name = "dev"
	tc.Namespace = "team-dev"
	tc.Spec.Mode = v1alpha1.TenantModeShared
	tc.Spec.ControlPlane.Replicas = 1
	tc.Spec.ControlPlane.Datastore.Type = "sqlite"
	return tc
}

func TestBuildStatefulSetDefaultStorage(t *testing.T) {
	sts, err := buildStatefulSet(cloudTenant())
	if err != nil {
		t.Fatalf("buildStatefulSet() error = %v", err)
	}
	pvc := sts.Spec.VolumeClaimTemplates[0]
	if pvc.Spec.StorageClassName != nil {
		t.Fatalf("StorageClassName = %v, want nil (cluster default)", *pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "1Gi" {
		t.Fatalf("storage request = %s, want 1Gi", got.String())
	}
}

func TestBuildStatefulSetCloudStorage(t *testing.T) {
	tc := cloudTenant()
	tc.Spec.ControlPlane.Storage = v1alpha1.StorageSpec{ClassName: "gp3", Size: "5Gi"}

	sts, err := buildStatefulSet(tc)
	if err != nil {
		t.Fatalf("buildStatefulSet() error = %v", err)
	}
	pvc := sts.Spec.VolumeClaimTemplates[0]
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "gp3" {
		t.Fatalf("StorageClassName = %v, want gp3", pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "5Gi" {
		t.Fatalf("storage request = %s, want 5Gi", got.String())
	}
}

func TestBuildStatefulSetRejectsBadStorageSize(t *testing.T) {
	tc := cloudTenant()
	tc.Spec.ControlPlane.Storage.Size = "lots"
	if _, err := buildStatefulSet(tc); err == nil {
		t.Fatal("expected error for invalid storage size")
	}
}

func TestBuildStatefulSetExtraTLSSANs(t *testing.T) {
	tc := cloudTenant()
	tc.Spec.ControlPlane.ExtraTLSSANs = []string{"tenants.example.internal", "10.0.12.34"}

	sts, err := buildStatefulSet(tc)
	if err != nil {
		t.Fatalf("buildStatefulSet() error = %v", err)
	}
	args := strings.Join(sts.Spec.Template.Spec.Containers[0].Args, " ")
	for _, san := range tc.Spec.ControlPlane.ExtraTLSSANs {
		if !strings.Contains(args, "--tls-san="+san) {
			t.Fatalf("args missing --tls-san=%s: %s", san, args)
		}
	}
}

func TestBuildExternalService(t *testing.T) {
	tc := cloudTenant()
	if svc := buildExternalService(tc); svc != nil {
		t.Fatalf("expected nil Service when exposure is off, got %+v", svc)
	}

	tc.Spec.ControlPlane.Expose = v1alpha1.ExposeSpec{
		LoadBalancer: true,
		Annotations:  map[string]string{"service.beta.kubernetes.io/azure-load-balancer-internal": "true"},
	}
	svc := buildExternalService(tc)
	if svc == nil {
		t.Fatal("expected Service when exposure is on")
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Fatalf("Service type = %s, want LoadBalancer", svc.Spec.Type)
	}
	if svc.Name != "dev-control-plane-external" {
		t.Fatalf("Service name = %s, want dev-control-plane-external", svc.Name)
	}
	if svc.Annotations["service.beta.kubernetes.io/azure-load-balancer-internal"] != "true" {
		t.Fatalf("annotations not propagated: %v", svc.Annotations)
	}
	if svc.Spec.Ports[0].Port != apiPort {
		t.Fatalf("port = %d, want %d", svc.Spec.Ports[0].Port, apiPort)
	}
}
