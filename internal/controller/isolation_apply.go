package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/isolation"
)

func profileFromCR(cr *v1alpha1.IsolationProfile) isolation.Profile {
	c := cr.Spec.Controls
	return isolation.Profile{
		Level:                     cr.Spec.Level,
		PodSecurity:               c.PodSecurity,
		DefaultDenyNetworkPolicy:  c.DefaultDenyNetworkPolicy,
		RequireResourceRequests:   c.RequireResourceRequests,
		RuntimeClassName:          c.RuntimeClassName,
		BlockHostPathVolumes:      c.BlockHostPathVolumes,
		BlockPrivilegedContainers: c.BlockPrivilegedContainers,
		APIFairness:               c.APIFairness,
	}
}

// applyIsolation reconciles the NetworkPolicy, ResourceQuota, LimitRange, and namespace
// Pod Security labels derived from profile into namespace.
func (r *TenantClusterReconciler) applyIsolation(ctx context.Context, tc *v1alpha1.TenantCluster, profile isolation.Profile) error {
	namespace := tc.Namespace

	if desired := isolation.BuildNetworkPolicy(profile, namespace); desired != nil {
		np := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
			np.Labels = desired.Labels
			np.Spec = desired.Spec
			return controllerutil.SetControllerReference(tc, np, r.Scheme)
		}); err != nil {
			return err
		}
	}

	desiredQuota, err := isolation.BuildResourceQuota(profile, tc.Spec.Resources.CPU, tc.Spec.Resources.Memory, namespace)
	if err != nil {
		return err
	}
	if desiredQuota != nil {
		quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: desiredQuota.Name, Namespace: namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
			quota.Labels = desiredQuota.Labels
			quota.Spec = desiredQuota.Spec
			return controllerutil.SetControllerReference(tc, quota, r.Scheme)
		}); err != nil {
			return err
		}
	}

	if desiredLR := isolation.BuildLimitRange(profile, namespace); desiredLR != nil {
		lr := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: desiredLR.Name, Namespace: namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, lr, func() error {
			lr.Labels = desiredLR.Labels
			lr.Spec = desiredLR.Spec
			return controllerutil.SetControllerReference(tc, lr, r.Scheme)
		}); err != nil {
			return err
		}
	}

	return r.applyNamespacePodSecurityLabels(ctx, namespace, profile)
}

// applyNamespacePodSecurityLabels merges Pod Security Admission labels onto the
// namespace. Namespaces are cluster-scoped so they cannot carry an owner reference
// to a namespaced TenantCluster; labels are simply reconciled idempotently each pass.
//
// It also stamps labelManagedBy, even though tenantplane did not create this
// namespace (the tenant did): the controller-write-scope admission policy (see
// rbac_scope.go) uses this label to recognize every namespace it is allowed to
// write into, and this is one of them (kubeconfig Secret, NetworkPolicy,
// ResourceQuota, LimitRange, and synced tenant objects all live here).
func (r *TenantClusterReconciler) applyNamespacePodSecurityLabels(ctx context.Context, namespace string, profile isolation.Profile) error {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}
	desired := map[string]string{labelManagedBy: "tenantplane"}
	for k, v := range isolation.PodSecurityLabels(profile) {
		desired[k] = v
	}
	changed := false
	for k, v := range desired {
		if ns.Labels[k] != v {
			ns.Labels[k] = v
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return r.Update(ctx, ns)
}
