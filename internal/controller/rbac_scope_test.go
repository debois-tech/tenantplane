package controller

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildControllerWriteScopePolicyShape(t *testing.T) {
	u := buildControllerWriteScopePolicy()
	if u.GetKind() != "ValidatingAdmissionPolicy" || u.GetAPIVersion() != "admissionregistration.k8s.io/v1" {
		t.Fatalf("GVK = %s/%s, want admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy", u.GetAPIVersion(), u.GetKind())
	}
	if u.GetName() != writeScopePolicyName {
		t.Fatalf("name = %q, want %q", u.GetName(), writeScopePolicyName)
	}

	validations, found, _ := unstructured.NestedSlice(u.Object, "spec", "validations")
	if !found || len(validations) != 1 {
		t.Fatalf("validations = %v, want exactly one", validations)
	}
	expr, _, _ := unstructured.NestedString(validations[0].(map[string]interface{}), "expression")
	if !strings.Contains(expr, controllerServiceAccountUsername) {
		t.Fatalf("expression %q does not reference the controller's own identity", expr)
	}
}

func TestBuildControllerWriteScopeBindingExcludesManagedNamespaces(t *testing.T) {
	u := buildControllerWriteScopeBinding()
	if u.GetKind() != "ValidatingAdmissionPolicyBinding" {
		t.Fatalf("kind = %q, want ValidatingAdmissionPolicyBinding", u.GetKind())
	}
	policyName, _, _ := unstructured.NestedString(u.Object, "spec", "policyName")
	if policyName != writeScopePolicyName {
		t.Fatalf("policyName = %q, want %q", policyName, writeScopePolicyName)
	}

	exprs, found, _ := unstructured.NestedSlice(u.Object, "spec", "matchResources", "namespaceSelector", "matchExpressions")
	if !found || len(exprs) != 1 {
		t.Fatalf("namespaceSelector.matchExpressions = %v, want exactly one", exprs)
	}
	me := exprs[0].(map[string]interface{})
	if me["key"] != labelManagedBy {
		t.Fatalf("matchExpression key = %v, want %q", me["key"], labelManagedBy)
	}
	if me["operator"] != "NotIn" {
		t.Fatalf("matchExpression operator = %v, want NotIn (the binding must fire OUTSIDE managed namespaces, not inside them)", me["operator"])
	}
	values, _ := me["values"].([]interface{})
	if len(values) != 1 || values[0] != "tenantplane" {
		t.Fatalf("matchExpression values = %v, want [tenantplane]", values)
	}
}

func TestBuildNamespaceDeleteScopePolicyChecksOldObjectLabels(t *testing.T) {
	u := buildNamespaceDeleteScopePolicy()
	if u.GetKind() != "ValidatingAdmissionPolicy" {
		t.Fatalf("kind = %q, want ValidatingAdmissionPolicy", u.GetKind())
	}

	rules, found, _ := unstructured.NestedSlice(u.Object, "spec", "matchConstraints", "resourceRules")
	if !found || len(rules) != 1 {
		t.Fatalf("resourceRules = %v, want exactly one", rules)
	}
	rule := rules[0].(map[string]interface{})
	ops, _ := rule["operations"].([]interface{})
	if len(ops) != 1 || ops[0] != "DELETE" {
		t.Fatalf("operations = %v, want [DELETE] only (must not affect CREATE/UPDATE of namespaces)", ops)
	}
	resources, _ := rule["resources"].([]interface{})
	if len(resources) != 1 || resources[0] != "namespaces" {
		t.Fatalf("resources = %v, want [namespaces]", resources)
	}

	validations, _, _ := unstructured.NestedSlice(u.Object, "spec", "validations")
	expr, _, _ := unstructured.NestedString(validations[0].(map[string]interface{}), "expression")
	if !strings.Contains(expr, "oldObject.metadata.labels") {
		t.Fatalf("expression %q must check oldObject's own labels (Namespace is cluster-scoped, there is no containing namespace to select by)", expr)
	}
	if !strings.Contains(expr, controllerServiceAccountUsername) {
		t.Fatalf("expression %q does not reference the controller's own identity", expr)
	}
	// A namespace with no labels at all (e.g. kube-node-lease) must not make
	// this expression error out at evaluation time: has() alone guards only
	// the labels map's existence, not this specific key inside it — bracket
	// indexing an absent key still throws "no such key" in CEL. The `in`
	// operator is the fix; assert it's actually used, not just has().
	if !strings.Contains(expr, `" in oldObject.metadata.labels`) {
		t.Fatalf("expression %q must use the `in` operator to test for the label key (has() alone is not enough — see admission.go's runtimeClassName comment for the same class of bug)", expr)
	}
}

func TestBuildNamespaceDeleteScopeBindingHasNoNamespaceSelector(t *testing.T) {
	// Namespace is cluster-scoped: a namespaceSelector on the binding would be
	// meaningless (and silently ignored) here, unlike the write-scope binding.
	u := buildNamespaceDeleteScopeBinding()
	policyName, _, _ := unstructured.NestedString(u.Object, "spec", "policyName")
	if policyName != namespaceScopePolicyName {
		t.Fatalf("policyName = %q, want %q", policyName, namespaceScopePolicyName)
	}
	if _, found, _ := unstructured.NestedMap(u.Object, "spec", "matchResources", "namespaceSelector"); found {
		t.Fatal("namespaceScopeBinding must not set a namespaceSelector")
	}
}
