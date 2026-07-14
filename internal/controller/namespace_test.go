package controller

import (
	"strings"
	"testing"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
)

func TestControlPlaneNamespaceDeterministicAndDistinct(t *testing.T) {
	a := cloudTenant() // dev / team-dev
	if got, again := controlPlaneNamespace(a), controlPlaneNamespace(a); got != again {
		t.Fatalf("namespace name not deterministic: %q vs %q", got, again)
	}

	b := cloudTenant()
	b.Namespace = "team-prod"
	if controlPlaneNamespace(a) == controlPlaneNamespace(b) {
		t.Fatal("same tenant name in different namespaces must get distinct control-plane namespaces")
	}
	if controlPlaneNamespace(a) == a.Namespace {
		t.Fatal("control-plane namespace must differ from the workload namespace")
	}
}

func TestControlPlaneNamespaceFitsDNSLabel(t *testing.T) {
	tc := cloudTenant()
	tc.Name = strings.Repeat("very-long-tenant-name", 3)
	tc.Namespace = strings.Repeat("very-long-team-namespace", 3)
	got := controlPlaneNamespace(tc)
	if len(got) > 63 {
		t.Fatalf("namespace %q is %d chars, must be <= 63", got, len(got))
	}
}

func TestBuildControlPlaneNamespaceLabels(t *testing.T) {
	tc := cloudTenant()
	ns := buildControlPlaneNamespace(tc)
	if !ownedByTenant(ns, tc) {
		t.Fatalf("built namespace must satisfy its own ownership check; labels = %v", ns.Labels)
	}
	if got := ns.Labels["pod-security.kubernetes.io/enforce"]; got != "baseline" {
		t.Fatalf("control-plane namespace enforce = %q, want baseline (k3s runs as root)", got)
	}

	other := cloudTenant()
	other.Namespace = "team-prod"
	if ownedByTenant(ns, other) {
		t.Fatal("ownership check must reject a namespace belonging to a different tenant")
	}
}

func TestMapControlPlaneObjectResolvesTenant(t *testing.T) {
	tc := &v1alpha1.TenantCluster{}
	tc.Name = "dev"
	tc.Namespace = "team-dev"

	sts := buildControlPlaneNamespace(tc) // any labeled object works for the mapper
	reqs := mapControlPlaneObject(nil, sts)
	if len(reqs) != 1 || reqs[0].Name != "dev" || reqs[0].Namespace != "team-dev" {
		t.Fatalf("mapControlPlaneObject = %+v, want [team-dev/dev]", reqs)
	}
}
