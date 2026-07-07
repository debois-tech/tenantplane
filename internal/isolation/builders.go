package isolation

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExemptLabelKey marks pods (such as tenantplane's own control-plane pods)
// that isolation NetworkPolicies must not restrict.
const (
	ExemptLabelKey   = "tenantplane.io/isolation-exempt"
	ExemptLabelValue = "true"
)

const (
	networkPolicyName = "tenantplane-default-deny"
	resourceQuotaName = "tenantplane-quota"
	limitRangeName    = "tenantplane-defaults"
)

// BuildNetworkPolicy returns the default-deny NetworkPolicy for namespace, or
// nil if the profile does not require one. Pods labeled with ExemptLabelKey
// are left unselected so tenantplane's own control-plane pods keep working.
func BuildNetworkPolicy(profile Profile, namespace string) *networkingv1.NetworkPolicy {
	if !profile.DefaultDenyNetworkPolicy {
		return nil
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName,
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      ExemptLabelKey,
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{ExemptLabelValue},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}

// BuildResourceQuota returns a namespace-wide ResourceQuota capping aggregate
// cpu/memory to the given amounts, or nil if the profile does not require
// resource requests or no amounts were given.
func BuildResourceQuota(profile Profile, cpu, memory, namespace string) (*corev1.ResourceQuota, error) {
	if !profile.RequireResourceRequests {
		return nil, nil
	}

	hard := corev1.ResourceList{}
	if cpu != "" {
		q, err := resource.ParseQuantity(cpu)
		if err != nil {
			return nil, err
		}
		hard[corev1.ResourceRequestsCPU] = q
		hard[corev1.ResourceLimitsCPU] = q
	}
	if memory != "" {
		q, err := resource.ParseQuantity(memory)
		if err != nil {
			return nil, err
		}
		hard[corev1.ResourceRequestsMemory] = q
		hard[corev1.ResourceLimitsMemory] = q
	}
	if len(hard) == 0 {
		return nil, nil
	}

	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceQuotaName,
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{Hard: hard},
	}, nil
}

// BuildLimitRange returns per-container default request/limit guardrails for
// namespace, or nil if the profile does not require resource requests.
func BuildLimitRange(profile Profile, namespace string) *corev1.LimitRange {
	if !profile.RequireResourceRequests {
		return nil
	}

	defaultLimit := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("500m"),
		corev1.ResourceMemory: resource.MustParse("512Mi"),
	}
	if profile.Level == "sandboxed" {
		defaultLimit[corev1.ResourceCPU] = resource.MustParse("250m")
		defaultLimit[corev1.ResourceMemory] = resource.MustParse("256Mi")
	}

	return &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      limitRangeName,
			Namespace: namespace,
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Default: defaultLimit,
				},
			},
		},
	}
}

// PodSecurityLabels returns the Pod Security Admission labels namespace-scoped
// enforcement uses for this profile's level.
func PodSecurityLabels(profile Profile) map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/enforce": profile.PodSecurity,
		"pod-security.kubernetes.io/audit":   profile.PodSecurity,
		"pod-security.kubernetes.io/warn":    profile.PodSecurity,
	}
}
