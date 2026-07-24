package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// controllerServiceAccountUsername is the identity these policies constrain.
// It must match the ServiceAccount/namespace deploy/tenantplane.yaml grants
// the ClusterRole to.
const controllerServiceAccountUsername = "system:serviceaccount:tenantplane-system:tenantplane-controller"

const (
	writeScopePolicyName      = "tenantplane-controller-write-scope"
	namespaceScopePolicyName  = "tenantplane-controller-namespace-scope"
	writeScopeBindingName     = "tenantplane-controller-write-scope"
	namespaceScopeBindingName = "tenantplane-controller-namespace-scope"
)

// The controller's ClusterRole (deploy/tenantplane.yaml) is necessarily
// cluster-wide: it manages an open-ended set of tenant workload and
// control-plane namespaces it cannot enumerate in advance. Native RBAC has no
// way to restrict a ClusterRoleBinding to "only namespaces this identity
// itself labeled," so these two ValidatingAdmissionPolicies backstop it at
// the admission layer instead — the same defense-in-depth pattern as
// runtimeClassName in admission.go. Together they mean a compromised or
// buggy controller identity cannot write into, or delete, any namespace it
// does not already manage, even though its RBAC grant would otherwise allow it.
//
// Reads (get/list/watch) and CONNECT (pods/exec) are not admission-controlled
// operations in Kubernetes at all — ValidatingAdmissionPolicy only sees
// CREATE, UPDATE, and DELETE — so those two permissions cannot be narrowed
// this way. That gap is real and left for a future pass (see the comment on
// ensureKubeconfigSecret).

// buildControllerWriteScopePolicy restricts CREATE/UPDATE/DELETE on the
// namespaced resources the controller manages to namespaces it has actually
// labeled as its own (see applyNamespacePodSecurityLabels and
// buildControlPlaneNamespace, both of which stamp labelManagedBy).
func buildControllerWriteScopePolicy() *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":   writeScopePolicyName,
			"labels": map[string]interface{}{labelManagedBy: "tenantplane"},
		},
		"spec": map[string]interface{}{
			"failurePolicy": "Fail",
			"matchConstraints": map[string]interface{}{
				"resourceRules": []interface{}{
					map[string]interface{}{
						"apiGroups":   []interface{}{""},
						"apiVersions": []interface{}{"v1"},
						"operations":  []interface{}{"CREATE", "UPDATE", "DELETE"},
						"resources":   []interface{}{"secrets", "configmaps", "services", "resourcequotas", "limitranges", "persistentvolumeclaims", "pods"},
					},
					map[string]interface{}{
						"apiGroups":   []interface{}{"apps"},
						"apiVersions": []interface{}{"v1"},
						"operations":  []interface{}{"CREATE", "UPDATE", "DELETE"},
						"resources":   []interface{}{"statefulsets", "deployments"},
					},
					map[string]interface{}{
						"apiGroups":   []interface{}{"networking.k8s.io"},
						"apiVersions": []interface{}{"v1"},
						"operations":  []interface{}{"CREATE", "UPDATE", "DELETE"},
						"resources":   []interface{}{"networkpolicies"},
					},
				},
			},
			"validations": []interface{}{
				map[string]interface{}{
					"expression": `request.userInfo.username != "` + controllerServiceAccountUsername + `"`,
					"message":    "the tenantplane controller may only write to namespaces it manages (labeled app.kubernetes.io/managed-by=tenantplane)",
				},
			},
		},
	}}
	u.SetGroupVersionKind(vapGVK)
	return u
}

// buildControllerWriteScopeBinding scopes the policy above to exactly the
// namespaces NOT labeled as tenantplane-managed: the policy's validation only
// denies the controller's own identity, so this binding only needs to fire
// where that denial should actually apply. Namespaces the controller does
// manage never hit this binding at all, so its normal reconciliation is
// unaffected.
func buildControllerWriteScopeBinding() *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":   writeScopeBindingName,
			"labels": map[string]interface{}{labelManagedBy: "tenantplane"},
		},
		"spec": map[string]interface{}{
			"policyName": writeScopePolicyName,
			"matchResources": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{
					"matchExpressions": []interface{}{
						map[string]interface{}{
							"key":      labelManagedBy,
							"operator": "NotIn",
							"values":   []interface{}{"tenantplane"},
						},
					},
				},
			},
			"validationActions": []interface{}{"Deny"},
		},
	}}
	u.SetGroupVersionKind(vapBindingGVK)
	return u
}

// buildNamespaceDeleteScopePolicy restricts the controller to deleting only
// namespaces it labeled as its own. Namespace is cluster-scoped, so unlike the
// policy above there is no containing namespace to select by: oldObject here
// *is* the namespace being deleted, and its own labels are checked directly.
func buildNamespaceDeleteScopePolicy() *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":   namespaceScopePolicyName,
			"labels": map[string]interface{}{labelManagedBy: "tenantplane"},
		},
		"spec": map[string]interface{}{
			"failurePolicy": "Fail",
			"matchConstraints": map[string]interface{}{
				"resourceRules": []interface{}{
					map[string]interface{}{
						"apiGroups":   []interface{}{""},
						"apiVersions": []interface{}{"v1"},
						"operations":  []interface{}{"DELETE"},
						"resources":   []interface{}{"namespaces"},
					},
				},
			},
			"validations": []interface{}{
				map[string]interface{}{
					// has() alone only confirms the labels map itself exists;
					// bracket-indexing a key that map doesn't contain still
					// throws "no such key" (the same category of mistake
					// admission.go's runtimeClassName check hit and fixed —
					// see its comment). The `in` operator is the correct,
					// absence-safe way to test for one specific key.
					"expression": `request.userInfo.username != "` + controllerServiceAccountUsername + `" || ` +
						`(has(oldObject.metadata.labels) && "` + labelManagedBy + `" in oldObject.metadata.labels && ` +
						`oldObject.metadata.labels["` + labelManagedBy + `"] == "tenantplane")`,
					"message": "the tenantplane controller may only delete namespaces it manages (labeled app.kubernetes.io/managed-by=tenantplane)",
				},
			},
		},
	}}
	u.SetGroupVersionKind(vapGVK)
	return u
}

func buildNamespaceDeleteScopeBinding() *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":   namespaceScopeBindingName,
			"labels": map[string]interface{}{labelManagedBy: "tenantplane"},
		},
		"spec": map[string]interface{}{
			"policyName":        namespaceScopePolicyName,
			"validationActions": []interface{}{"Deny"},
		},
	}}
	u.SetGroupVersionKind(vapBindingGVK)
	return u
}

// ensureControllerScopePolicies creates or converges the two global,
// cluster-wide policies above. Unlike the per-tenant runtimeClass binding,
// these are not tied to any single TenantCluster, so this is idempotently
// called on every reconcile of any tenant rather than tracked per-object.
// ok is false when the cluster does not support ValidatingAdmissionPolicy at
// all — same honesty gate as ensureRuntimeClassPolicy.
func (r *TenantClusterReconciler) ensureControllerScopePolicies(ctx context.Context) (ok bool, err error) {
	objs := []*unstructured.Unstructured{
		buildControllerWriteScopePolicy(),
		buildControllerWriteScopeBinding(),
		buildNamespaceDeleteScopePolicy(),
		buildNamespaceDeleteScopeBinding(),
	}

	for _, desired := range objs {
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(desired.GroupVersionKind())
		getErr := r.Get(ctx, types.NamespacedName{Name: desired.GetName()}, existing)
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
			continue
		}
		if getErr != nil {
			return false, getErr
		}
		existing.Object["spec"] = desired.Object["spec"]
		existing.SetLabels(desired.GetLabels())
		if err := r.Update(ctx, existing); err != nil {
			return false, err
		}
	}
	return true, nil
}
