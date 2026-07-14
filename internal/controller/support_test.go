package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
)

func condition(tc *v1alpha1.TenantCluster, condType string) *v1alpha1.Condition {
	for i := range tc.Status.Conditions {
		if tc.Status.Conditions[i].Type == condType {
			return &tc.Status.Conditions[i]
		}
	}
	return nil
}

func supportedProfile() *v1alpha1.IsolationProfile {
	p := &v1alpha1.IsolationProfile{}
	p.Name = "restricted"
	p.Spec.Level = "restricted"
	p.Spec.Controls = v1alpha1.IsolationControls{
		PodSecurity:               "baseline",
		DefaultDenyNetworkPolicy:  true,
		RequireResourceRequests:   true,
		BlockHostPathVolumes:      true,
		BlockPrivilegedContainers: true,
	}
	return p
}

func supportedPolicy() *v1alpha1.SyncPolicy {
	sp := &v1alpha1.SyncPolicy{}
	sp.Name = "default"
	sp.Spec.ConflictPolicy = "manual"
	sp.Spec.Resources = []v1alpha1.SyncedResource{
		{APIVersion: "v1", Kind: "Pod", Direction: "toHost"},
	}
	return sp
}

func TestSupportConditionsAllTrueForSupportedSpec(t *testing.T) {
	tc := cloudTenant()
	setSupportConditions(tc, supportedProfile(), supportedPolicy())

	for _, condType := range []string{"ModeSupported", "IsolationEnforced", "SyncSupported"} {
		cond := condition(tc, condType)
		if cond == nil {
			t.Fatalf("condition %s not set", condType)
		}
		if cond.Status != string(corev1.ConditionTrue) {
			t.Fatalf("%s = %s (%s: %s), want True", condType, cond.Status, cond.Reason, cond.Message)
		}
	}
}

func TestSupportConditionsFlagUnsupportedMode(t *testing.T) {
	tc := cloudTenant()
	tc.Spec.Mode = v1alpha1.TenantModeDedicated
	setSupportConditions(tc, supportedProfile(), supportedPolicy())

	cond := condition(tc, "ModeSupported")
	if cond == nil || cond.Status != string(corev1.ConditionFalse) {
		t.Fatalf("ModeSupported = %+v, want False", cond)
	}
	if !strings.Contains(cond.Message, "dedicated") {
		t.Fatalf("message should name the unsupported mode: %q", cond.Message)
	}
}

func TestSupportConditionsFlagUnenforcedIsolationControls(t *testing.T) {
	tc := cloudTenant()
	profile := supportedProfile()
	profile.Spec.Controls.RuntimeClassName = "gvisor"
	profile.Spec.Controls.APIFairness = "tenant"
	profile.Spec.Controls.PodSecurity = "restricted"
	setSupportConditions(tc, profile, supportedPolicy())

	cond := condition(tc, "IsolationEnforced")
	if cond == nil || cond.Status != string(corev1.ConditionFalse) {
		t.Fatalf("IsolationEnforced = %+v, want False", cond)
	}
	for _, want := range []string{"runtimeClassName", "apiFairness", "baseline"} {
		if !strings.Contains(cond.Message, want) {
			t.Fatalf("message missing %q: %q", want, cond.Message)
		}
	}
}

func TestSupportConditionsDoNotFlagPSAEnforcedControls(t *testing.T) {
	// blockPrivilegedContainers and blockHostPathVolumes are enforced by the
	// PSA baseline label, so declaring them must not degrade the condition.
	tc := cloudTenant()
	setSupportConditions(tc, supportedProfile(), supportedPolicy())

	cond := condition(tc, "IsolationEnforced")
	if cond == nil || cond.Status != string(corev1.ConditionTrue) {
		t.Fatalf("IsolationEnforced = %+v, want True", cond)
	}
}

func TestSupportConditionsFlagUnimplementedSyncSettings(t *testing.T) {
	tc := cloudTenant()
	policy := supportedPolicy()
	policy.Spec.ConflictPolicy = "host-wins"
	policy.Spec.DriftDetection = v1alpha1.DriftDetectionSpec{Enabled: true, Interval: "30s"}
	policy.Spec.Explain = v1alpha1.ExplainSpec{RecordDecisions: true, Retain: 1000}
	policy.Spec.Resources = append(policy.Spec.Resources,
		v1alpha1.SyncedResource{APIVersion: "v1", Kind: "Service", Direction: "bidirectional"})
	setSupportConditions(tc, supportedProfile(), policy)

	cond := condition(tc, "SyncSupported")
	if cond == nil || cond.Status != string(corev1.ConditionFalse) {
		t.Fatalf("SyncSupported = %+v, want False", cond)
	}
	for _, want := range []string{"Service (bidirectional)", "host-wins", "driftDetection.interval", "explain.retain"} {
		if !strings.Contains(cond.Message, want) {
			t.Fatalf("message missing %q: %q", want, cond.Message)
		}
	}
}
