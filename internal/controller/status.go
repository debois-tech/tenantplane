package controller

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/tenantplane/tenantplane/internal/api/v1alpha1"
)

const (
	PhasePending      = "Pending"
	PhaseProvisioning = "Provisioning"
	PhaseReady        = "Ready"
	PhaseDegraded     = "Degraded"
)

// setCondition upserts a condition by Type, matching corev1.ConditionStatus semantics
// ("True"/"False"/"Unknown") in the string-typed v1alpha1.Condition.
func setCondition(tc *v1alpha1.TenantCluster, conditionType string, status corev1.ConditionStatus, reason, message string) {
	for i := range tc.Status.Conditions {
		if tc.Status.Conditions[i].Type == conditionType {
			tc.Status.Conditions[i].Status = string(status)
			tc.Status.Conditions[i].Reason = reason
			tc.Status.Conditions[i].Message = message
			return
		}
	}
	tc.Status.Conditions = append(tc.Status.Conditions, v1alpha1.Condition{
		Type:    conditionType,
		Status:  string(status),
		Reason:  reason,
		Message: message,
	})
}
