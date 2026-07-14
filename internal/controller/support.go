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
// both), so they are not flagged; runtimeClassName and apiFairness have no
// enforcement behind them yet.
func setIsolationCondition(tc *v1alpha1.TenantCluster, profile *v1alpha1.IsolationProfile) {
	if profile == nil {
		return
	}
	controls := profile.Spec.Controls

	var advisory []string
	if controls.RuntimeClassName != "" {
		advisory = append(advisory, "runtimeClassName")
	}
	if controls.APIFairness != "" {
		advisory = append(advisory, "apiFairness")
	}

	var notes []string
	if len(advisory) > 0 {
		notes = append(notes, fmt.Sprintf("declared but not yet enforced through admission: %s", strings.Join(advisory, ", ")))
	}
	if controls.PodSecurity == "restricted" {
		notes = append(notes, `podSecurity "restricted" is enforced at "baseline" (audit and warn stay "restricted") until control planes move to a dedicated namespace`)
	}

	if len(notes) > 0 {
		setCondition(tc, "IsolationEnforced", corev1.ConditionFalse, "PartiallyEnforced", strings.Join(notes, "; "))
		return
	}
	setCondition(tc, "IsolationEnforced", corev1.ConditionTrue, "Enforced", "")
}

// setSyncSupportCondition reports which parts of the referenced SyncPolicy the
// engine actually honors today.
func setSyncSupportCondition(tc *v1alpha1.TenantCluster, policy *v1alpha1.SyncPolicy) {
	if policy == nil {
		return
	}

	var skipped []string
	for _, res := range policy.Spec.Resources {
		if syncer.Direction(res.Direction) != syncer.DirectionToHost {
			skipped = append(skipped, fmt.Sprintf("%s %s (%s)", res.APIVersion, res.Kind, res.Direction))
		}
	}

	var notes []string
	if len(skipped) > 0 {
		notes = append(notes, fmt.Sprintf(`only "toHost" sync is implemented; these declared resources are not synced: %s`, strings.Join(skipped, ", ")))
	}
	if policy.Spec.ConflictPolicy != "" && policy.Spec.ConflictPolicy != "manual" {
		notes = append(notes, fmt.Sprintf("conflictPolicy %q is not yet honored: tenant state overwrites managed host objects and unmanaged collisions are skipped", policy.Spec.ConflictPolicy))
	}
	if policy.Spec.DriftDetection.Enabled && policy.Spec.DriftDetection.Interval != "" {
		notes = append(notes, "driftDetection.interval is not yet honored; sync runs on the controller's fixed resync cadence")
	}
	if policy.Spec.Explain.Retain > 0 {
		notes = append(notes, "explain.retain is not yet honored; decisions are Kubernetes Events with cluster-default retention")
	}

	if len(notes) > 0 {
		setCondition(tc, "SyncSupported", corev1.ConditionFalse, "PartiallyImplemented", strings.Join(notes, "; "))
		return
	}
	setCondition(tc, "SyncSupported", corev1.ConditionTrue, "Implemented", "")
}
