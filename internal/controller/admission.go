package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/syncplan"
)

// The runtimeClassName enforcement objects are built as unstructured.Unstructured
// rather than typed k8s.io/api/admissionregistration/v1 structs: this repo's
// pinned k8s.io/api version predates the Go types for the GA (v1)
// ValidatingAdmissionPolicy API, and adding them purely to model two objects
// isn't worth a dependency bump. The controller-runtime client already handles
// arbitrary GVKs generically (the sync engine leans on the same capability).
var (
	vapGVK        = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicy"}
	vapBindingGVK = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingAdmissionPolicyBinding"}

	isolationProfileParamKind = map[string]interface{}{
		"apiVersion": "tenantplane.io/v1alpha1",
		"kind":       "IsolationProfile",
	}
)

const runtimeClassPolicyName = "tenantplane-runtimeclass"

// buildRuntimeClassPolicy is the single, shared ValidatingAdmissionPolicy that
// backstops runtime-class enforcement at the host API server: a defense-in-depth
// check alongside the sync engine's own injection (RequiredRuntimeClassName),
// which is what actually makes enforcement transparent to tenants. It is
// parameterized per binding by an IsolationProfile, so one policy definition
// serves every tenant.
func buildRuntimeClassPolicy() *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":   runtimeClassPolicyName,
			"labels": map[string]interface{}{labelManagedBy: "tenantplane"},
		},
		"spec": map[string]interface{}{
			"failurePolicy": "Fail",
			"paramKind":     isolationProfileParamKind,
			"matchConstraints": map[string]interface{}{
				"resourceRules": []interface{}{
					map[string]interface{}{
						"apiGroups":   []interface{}{""},
						"apiVersions": []interface{}{"v1"},
						"operations":  []interface{}{"CREATE"},
						"resources":   []interface{}{"pods"},
					},
				},
			},
			"validations": []interface{}{
				map[string]interface{}{
					// spec.runtimeClassName is an optional string: when a pod omits
					// it outright (not just sets it to ""), CEL has no key to read,
					// and a bare `object.spec.runtimeClassName` access errors rather
					// than returning "". has() guards that so an omitted field is
					// cleanly rejected by our own message instead of a raw CEL error
					// (both deny, since failurePolicy is Fail — this only changes
					// *how* — but a clean, intentional message is worth getting right).
					"expression": `params.spec.controls.runtimeClassName == "" || ` +
						`(has(object.spec.runtimeClassName) && object.spec.runtimeClassName == params.spec.controls.runtimeClassName)`,
					"message": "this namespace's IsolationProfile requires a specific runtimeClassName; " +
						"the pod's spec.runtimeClassName is missing or does not match it",
				},
			},
		},
	}}
	u.SetGroupVersionKind(vapGVK)
	return u
}

// runtimeClassBindingName is deterministic and collision-safe per tenant, reusing
// the same hash-truncation scheme as host object names.
func runtimeClassBindingName(tc *v1alpha1.TenantCluster) string {
	return syncplan.HostName("runtimeclass", tc.Namespace, tc.Name)
}

// buildRuntimeClassBinding scopes the shared policy to one tenant's workload
// namespace, parameterized by that tenant's own IsolationProfile.
// parameterNotFoundAction "Deny" fails closed if the profile is ever deleted out
// from under a running tenant, rather than silently admitting unconstrained pods.
func buildRuntimeClassBinding(tc *v1alpha1.TenantCluster) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": runtimeClassBindingName(tc),
			"labels": map[string]interface{}{
				labelManagedBy:       "tenantplane",
				labelTenant:          tc.Name,
				labelTenantNamespace: tc.Namespace,
			},
		},
		"spec": map[string]interface{}{
			"policyName": runtimeClassPolicyName,
			"paramRef": map[string]interface{}{
				"name":                    tc.Spec.IsolationProfileRef.Name,
				"parameterNotFoundAction": "Deny",
			},
			"matchResources": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"kubernetes.io/metadata.name": tc.Namespace,
					},
				},
			},
			"validationActions": []interface{}{"Deny"},
		},
	}}
	u.SetGroupVersionKind(vapBindingGVK)
	return u
}

// admissionHardeningUnsupported reports whether err indicates the cluster does
// not serve admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy at all
// (it GAed in Kubernetes 1.30; older clusters, or ones with it disabled,
// surface this as a NoKindMatchError from the RESTMapper before any HTTP
// request is even made). This is deliberately narrower than "not found": a
// plain 404 means the type IS recognized and the specific object just doesn't
// exist yet — the normal, expected first-create case — and must still fall
// through to Create.
func admissionHardeningUnsupported(err error) bool {
	return meta.IsNoMatchError(err)
}

// ensureRuntimeClassPolicy creates or updates the shared ValidatingAdmissionPolicy.
// ok is false when the cluster does not support the API at all — a supported
// cluster but a missing object is a real error, not this case.
func (r *TenantClusterReconciler) ensureRuntimeClassPolicy(ctx context.Context) (ok bool, err error) {
	desired := buildRuntimeClassPolicy()

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vapGVK)
	getErr := r.Get(ctx, types.NamespacedName{Name: runtimeClassPolicyName}, existing)
	if admissionHardeningUnsupported(getErr) {
		return false, nil
	}
	if apierrors.IsNotFound(getErr) {
		if err := r.Create(ctx, desired); err != nil {
			if admissionHardeningUnsupported(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	if getErr != nil {
		return false, getErr
	}

	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(desired.GetLabels())
	if err := r.Update(ctx, existing); err != nil {
		return false, err
	}
	return true, nil
}

// reconcileRuntimeClassBinding converges this tenant's ValidatingAdmissionPolicyBinding.
// Called only after ensureRuntimeClassPolicy has confirmed the API is supported.
func (r *TenantClusterReconciler) reconcileRuntimeClassBinding(ctx context.Context, tc *v1alpha1.TenantCluster) error {
	desired := buildRuntimeClassBinding(tc)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vapBindingGVK)
	err := r.Get(ctx, types.NamespacedName{Name: desired.GetName()}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(desired.GetLabels())
	return r.Update(ctx, existing)
}

// deleteRuntimeClassBinding removes this tenant's binding at teardown. A missing
// API or object is not an error at this point — there is nothing left to clean up.
func (r *TenantClusterReconciler) deleteRuntimeClassBinding(ctx context.Context, tc *v1alpha1.TenantCluster) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vapBindingGVK)
	existing.SetName(runtimeClassBindingName(tc))
	err := r.Delete(ctx, existing)
	if err != nil && !apierrors.IsNotFound(err) && !admissionHardeningUnsupported(err) {
		return err
	}
	return nil
}
