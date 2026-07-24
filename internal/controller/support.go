package controller

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/syncer"
)

// setSupportConditions records an honest condition for every spec setting that
// is accepted by the API but not (or only partially) implemented. New objects
// carrying unsupported settings are rejected at admission by CRD validation;
// objects stored before that validation existed still reconcile, so their
// status must state exactly what is and is not real.
func setSupportConditions(tc *v1alpha1.TenantCluster, profile *v1alpha1.IsolationProfile, policy *v1alpha1.SyncPolicy) {
	setModeCondition(tc)
	setIsolationCondition(tc, profile)
	setSyncSupportCondition(tc, policy)
}

// setAdmissionHardeningCondition reports whether the host cluster supports the
// ValidatingAdmissionPolicy defense-in-depth backstop for runtimeClassName.
// This is deliberately a separate condition from IsolationEnforced: the
// declared control is enforced either way (the sync engine injects
// runtimeClassName on every synced pod), this only reports whether the extra
// admission-layer safety net is also active on this cluster.
func setAdmissionHardeningCondition(tc *v1alpha1.TenantCluster, supported bool) {
	if supported {
		setCondition(tc, "AdmissionHardening", corev1.ConditionTrue, "ValidatingAdmissionPolicyActive", "")
		return
	}
	setCondition(tc, "AdmissionHardening", corev1.ConditionFalse, "ValidatingAdmissionPolicyUnavailable",
		"runtimeClassName is still enforced by the sync engine, but the admission-layer backstop is unavailable: this cluster does not serve admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy (requires Kubernetes 1.30+)")
}

// setControllerScopeCondition reports whether the admission-layer backstop
// that confines the controller's own writes and namespace deletions to
// namespaces it manages (see rbac_scope.go) is active on this cluster. Like
// AdmissionHardening, this is defense-in-depth only: the controller's RBAC
// grant is unaffected either way, this just reports whether the extra
// admission-layer narrowing is also active.
func setControllerScopeCondition(tc *v1alpha1.TenantCluster, supported bool) {
	if supported {
		setCondition(tc, "ControllerRBACScoped", corev1.ConditionTrue, "ValidatingAdmissionPolicyActive", "")
		return
	}
	setCondition(tc, "ControllerRBACScoped", corev1.ConditionFalse, "ValidatingAdmissionPolicyUnavailable",
		"the controller's ClusterRole is unavoidably cluster-wide, but the admission-layer policy narrowing it to namespaces it manages is unavailable: this cluster does not serve admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy (requires Kubernetes 1.30+)")
}

func setModeCondition(tc *v1alpha1.TenantCluster) {
	if tc.Spec.Mode != v1alpha1.TenantModeShared {
		setCondition(tc, "ModeSupported", corev1.ConditionFalse, "NotImplemented",
			fmt.Sprintf("mode %q is accepted but only %q is implemented; reconciling as shared", tc.Spec.Mode, v1alpha1.TenantModeShared))
		return
	}
	setCondition(tc, "ModeSupported", corev1.ConditionTrue, "Shared", "")
}

// setIsolationCondition reports which declared isolation controls carry real
// enforcement. blockPrivilegedContainers and blockHostPathVolumes are enforced
// through the Pod Security Admission enforce label (the "baseline" level blocks
// both); runtimeClassName is enforced by the sync engine stamping it onto every
// synced pod (see AdmissionHardening for the separate admission-layer
// backstop); apiFairness is enforced as a per-tenant rate limit on sync writes.
// There are currently no isolation controls left unenforced, but the function
// stays generic so a future control can be flagged the same way.
func setIsolationCondition(tc *v1alpha1.TenantCluster, profile *v1alpha1.IsolationProfile) {
	if profile == nil {
		return
	}

	var advisory []string
	if len(advisory) > 0 {
		setCondition(tc, "IsolationEnforced", corev1.ConditionFalse, "PartiallyEnforced",
			fmt.Sprintf("declared but not yet enforced through admission: %s", strings.Join(advisory, ", ")))
		return
	}
	setCondition(tc, "IsolationEnforced", corev1.ConditionTrue, "Enforced", "")
}

// setSyncSupportCondition reports which parts of the referenced SyncPolicy the
// engine actually honors today. All three directions (toHost, fromHost,
// bidirectional), all three conflictPolicy values, and explain.recordDecisions
// / explain.retain (see recordSyncDecisions) are implemented; what remains
// unimplemented is driftDetection.interval (see below), plus any declared
// direction/kind combination the CRD's enum would already reject before this
// ever runs (kept here only as a defensive, unreachable-in-practice check).
func setSyncSupportCondition(tc *v1alpha1.TenantCluster, policy *v1alpha1.SyncPolicy) {
	if policy == nil {
		return
	}

	var skipped []string
	for _, res := range policy.Spec.Resources {
		switch syncer.Direction(res.Direction) {
		case syncer.DirectionToHost, syncer.DirectionFromHost, syncer.DirectionBidirectional:
		default:
			skipped = append(skipped, fmt.Sprintf("%s %s (%s)", res.APIVersion, res.Kind, res.Direction))
		}
	}

	var notes []string
	if len(skipped) > 0 {
		notes = append(notes, fmt.Sprintf(`unrecognized sync direction; these declared resources are not synced: %s`, strings.Join(skipped, ", ")))
	}
	if policy.Spec.DriftDetection.Enabled && policy.Spec.DriftDetection.Interval != "" {
		notes = append(notes, "driftDetection.interval is not yet honored; sync runs on the controller's fixed resync cadence")
	}

	if len(notes) > 0 {
		setCondition(tc, "SyncSupported", corev1.ConditionFalse, "PartiallyImplemented", strings.Join(notes, "; "))
		return
	}
	setCondition(tc, "SyncSupported", corev1.ConditionTrue, "Implemented", "")
}
