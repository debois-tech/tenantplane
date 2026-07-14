package syncplan

import "testing"

func TestExplain(t *testing.T) {
	plan, err := Explain(ResourceRef{
		TenantCluster:    "dev",
		TenantNamespace:  "team-dev",
		VirtualNamespace: "default",
		Kind:             "Pod",
		Name:             "nginx",
	})
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if plan.Host.Namespace != "team-dev" {
		t.Fatalf("Host.Namespace = %q, want team-dev", plan.Host.Namespace)
	}
	if plan.Host.Name != "nginx-x-default-x-dev" {
		t.Fatalf("Host.Name = %q, want nginx-x-default-x-dev", plan.Host.Name)
	}
}

func TestHostNameIsBounded(t *testing.T) {
	name := HostName("this-name-is-longer-than-most-people-should-use-in-a-test", "namespace", "tenant")
	if len(name) > maxDNSLabelLength {
		t.Fatalf("HostName length = %d, want <= %d", len(name), maxDNSLabelLength)
	}
}

func TestSanitizeName(t *testing.T) {
	got := SanitizeName("Team_A/Dev")
	if got != "team-a-dev" {
		t.Fatalf("SanitizeName() = %q, want team-a-dev", got)
	}
}
