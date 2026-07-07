package isolation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBuildNetworkPolicy(t *testing.T) {
	baseline, _ := ProfileForLevel("baseline")
	if got := BuildNetworkPolicy(baseline, "team-dev"); got != nil {
		t.Fatalf("expected nil NetworkPolicy for baseline profile, got %+v", got)
	}

	restricted, _ := ProfileForLevel("restricted")
	np := BuildNetworkPolicy(restricted, "team-dev")
	if np == nil {
		t.Fatal("expected NetworkPolicy for restricted profile, got nil")
	}
	if np.Namespace != "team-dev" {
		t.Fatalf("Namespace = %q, want %q", np.Namespace, "team-dev")
	}
	if len(np.Spec.PodSelector.MatchExpressions) != 1 {
		t.Fatalf("expected exactly one selector expression, got %d", len(np.Spec.PodSelector.MatchExpressions))
	}
	expr := np.Spec.PodSelector.MatchExpressions[0]
	if expr.Key != ExemptLabelKey || expr.Values[0] != ExemptLabelValue {
		t.Fatalf("expected exemption on %s=%s, got %+v", ExemptLabelKey, ExemptLabelValue, expr)
	}
}

func TestBuildResourceQuota(t *testing.T) {
	restricted, _ := ProfileForLevel("restricted")

	quota, err := BuildResourceQuota(restricted, "2", "4Gi", "team-dev")
	if err != nil {
		t.Fatalf("BuildResourceQuota() error = %v", err)
	}
	if quota == nil {
		t.Fatal("expected ResourceQuota, got nil")
	}
	if got := quota.Spec.Hard[corev1.ResourceRequestsCPU]; got.String() != "2" {
		t.Fatalf("requests.cpu quota = %s, want 2", got.String())
	}
	if got := quota.Spec.Hard[corev1.ResourceRequestsMemory]; got.String() != "4Gi" {
		t.Fatalf("requests.memory quota = %s, want 4Gi", got.String())
	}

	quota, err = BuildResourceQuota(restricted, "", "", "team-dev")
	if err != nil {
		t.Fatalf("BuildResourceQuota() error = %v", err)
	}
	if quota != nil {
		t.Fatalf("expected nil ResourceQuota with no amounts given, got %+v", quota)
	}

	baseline, _ := ProfileForLevel("baseline")
	baseline.RequireResourceRequests = false
	quota, err = BuildResourceQuota(baseline, "1", "1Gi", "team-dev")
	if err != nil {
		t.Fatalf("BuildResourceQuota() error = %v", err)
	}
	if quota != nil {
		t.Fatalf("expected nil ResourceQuota when profile does not require requests, got %+v", quota)
	}

	if _, err := BuildResourceQuota(restricted, "not-a-quantity", "", "team-dev"); err == nil {
		t.Fatal("expected error for invalid cpu quantity")
	}
}

func TestBuildLimitRange(t *testing.T) {
	sandboxed, _ := ProfileForLevel("sandboxed")
	lr := BuildLimitRange(sandboxed, "team-dev")
	if lr == nil {
		t.Fatal("expected LimitRange for sandboxed profile, got nil")
	}
	if lr.Spec.Limits[0].Default.Cpu().String() != "250m" {
		t.Fatalf("sandboxed default cpu limit = %s, want 250m", lr.Spec.Limits[0].Default.Cpu().String())
	}

	restricted, _ := ProfileForLevel("restricted")
	lr = BuildLimitRange(restricted, "team-dev")
	if lr.Spec.Limits[0].Default.Cpu().String() != "500m" {
		t.Fatalf("restricted default cpu limit = %s, want 500m", lr.Spec.Limits[0].Default.Cpu().String())
	}
}

func TestPodSecurityLabels(t *testing.T) {
	restricted, _ := ProfileForLevel("restricted")
	labels := PodSecurityLabels(restricted)
	want := "restricted"
	for _, key := range []string{
		"pod-security.kubernetes.io/enforce",
		"pod-security.kubernetes.io/audit",
		"pod-security.kubernetes.io/warn",
	} {
		if labels[key] != want {
			t.Fatalf("labels[%q] = %q, want %q", key, labels[key], want)
		}
	}
}
