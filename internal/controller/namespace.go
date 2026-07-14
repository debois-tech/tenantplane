package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/syncplan"
)

const (
	// teardownFinalizer holds TenantCluster deletion until the control-plane
	// namespace — which cannot carry an owner reference to a namespaced
	// object — has been removed.
	teardownFinalizer = "tenantplane.io/teardown"

	labelManagedBy       = "app.kubernetes.io/managed-by"
	labelTenant          = "tenantplane.io/tenant"
	labelTenantNamespace = "tenantplane.io/tenant-namespace"
)

// controlPlaneNamespace is the dedicated namespace holding this tenant's k3s
// control plane. Keeping the control plane out of the workload namespace lets
// Pod Security enforce the profile's real level (e.g. "restricted") on tenant
// workloads: the k3s pod runs as root (like upstream) and only satisfies
// "baseline". Naming reuses the deterministic, collision-safe HostName scheme,
// so two tenants named "dev" in different namespaces cannot collide.
func controlPlaneNamespace(tc *v1alpha1.TenantCluster) string {
	return syncplan.HostName("cp", tc.Name, tc.Namespace)
}

// buildControlPlaneNamespace returns the desired control-plane Namespace. PSA
// "baseline" is enforced on it — k3s satisfies baseline — so the namespace is
// never left wide open.
func buildControlPlaneNamespace(tc *v1alpha1.TenantCluster) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: controlPlaneNamespace(tc),
			Labels: map[string]string{
				labelManagedBy:                       "tenantplane",
				labelTenant:                          tc.Name,
				labelTenantNamespace:                 tc.Namespace,
				"pod-security.kubernetes.io/enforce": "baseline",
				"pod-security.kubernetes.io/audit":   "baseline",
				"pod-security.kubernetes.io/warn":    "baseline",
			},
		},
	}
}

// ownedByTenant reports whether ns carries the labels tenantplane stamps on
// namespaces it creates for this specific tenant.
func ownedByTenant(ns *corev1.Namespace, tc *v1alpha1.TenantCluster) bool {
	return ns.Labels[labelManagedBy] == "tenantplane" &&
		ns.Labels[labelTenant] == tc.Name &&
		ns.Labels[labelTenantNamespace] == tc.Namespace
}

// ensureControlPlaneNamespace creates or converges the tenant's control-plane
// namespace. A pre-existing namespace tenantplane did not create is never
// adopted: that would hand its contents to the tenant lifecycle (including
// deletion at teardown).
func (r *TenantClusterReconciler) ensureControlPlaneNamespace(ctx context.Context, tc *v1alpha1.TenantCluster) error {
	desired := buildControlPlaneNamespace(tc)

	existing := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !ownedByTenant(existing, tc) {
		return fmt.Errorf("namespace %q already exists and is not managed by tenantplane for this tenant; refusing to adopt it", desired.Name)
	}

	changed := false
	for k, v := range desired.Labels {
		if existing.Labels[k] != v {
			if existing.Labels == nil {
				existing.Labels = map[string]string{}
			}
			existing.Labels[k] = v
			changed = true
		}
	}
	if changed {
		return r.Update(ctx, existing)
	}
	return nil
}

// teardown runs while the TenantCluster is being deleted: it removes the
// control-plane namespace (taking the StatefulSet, Service, and PVC with it)
// and only then releases the finalizer. Everything in the workload namespace
// is owner-referenced and garbage-collected by Kubernetes itself.
func (r *TenantClusterReconciler) teardown(ctx context.Context, tc *v1alpha1.TenantCluster) (bool, error) {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: controlPlaneNamespace(tc)}, ns)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !ownedByTenant(ns, tc) {
		// Never delete a namespace tenantplane did not create — release the
		// tenant and leave the foreign namespace alone.
		return true, nil
	}
	if ns.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return false, nil
}
