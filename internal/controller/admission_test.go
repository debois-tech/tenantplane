package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildRuntimeClassPolicyShape(t *testing.T) {
	u := buildRuntimeClassPolicy()
	if u.GetKind() != "ValidatingAdmissionPolicy" || u.GetAPIVersion() != "admissionregistration.k8s.io/v1" {
		t.Fatalf("GVK = %s/%s, want admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy", u.GetAPIVersion(), u.GetKind())
	}
	if u.GetName() != runtimeClassPolicyName {
		t.Fatalf("name = %q, want %q", u.GetName(), runtimeClassPolicyName)
	}
	paramKind, found, _ := unstructured.NestedMap(u.Object, "spec", "paramKind")
	if !found || paramKind["kind"] != "IsolationProfile" {
		t.Fatalf("paramKind = %v, want kind IsolationProfile", paramKind)
	}
}

func TestBuildRuntimeClassBindingScopesToTenant(t *testing.T) {
	tc := cloudTenant()
	tc.Spec.IsolationProfileRef.Name = "sandboxed"

	u := buildRuntimeClassBinding(tc)
	if u.GetKind() != "ValidatingAdmissionPolicyBinding" {
		t.Fatalf("kind = %q, want ValidatingAdmissionPolicyBinding", u.GetKind())
	}

	// The binding must carry this tenant's identifying labels, the same check
	// teardown uses to decide what it may delete.
	asNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Labels: u.GetLabels()}}
	if !ownedByTenant(asNamespace, tc) {
		t.Fatalf("binding labels %v do not satisfy ownedByTenant for %s/%s", u.GetLabels(), tc.Namespace, tc.Name)
	}

	policyName, _, _ := unstructured.NestedString(u.Object, "spec", "policyName")
	if policyName != runtimeClassPolicyName {
		t.Fatalf("policyName = %q, want %q", policyName, runtimeClassPolicyName)
	}
	paramRefName, _, _ := unstructured.NestedString(u.Object, "spec", "paramRef", "name")
	if paramRefName != "sandboxed" {
		t.Fatalf("paramRef.name = %q, want the tenant's own IsolationProfile (sandboxed)", paramRefName)
	}
	notFoundAction, _, _ := unstructured.NestedString(u.Object, "spec", "paramRef", "parameterNotFoundAction")
	if notFoundAction != "Deny" {
		t.Fatal("a deleted IsolationProfile must fail closed (Deny), not silently admit")
	}
	nsName, _, _ := unstructured.NestedString(u.Object, "spec", "matchResources", "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name")
	if nsName != tc.Namespace {
		t.Fatalf("binding must scope to the tenant's own workload namespace, got %q", nsName)
	}
}

func TestRuntimeClassBindingNameDeterministicAndDistinct(t *testing.T) {
	a := cloudTenant()
	b := cloudTenant()
	b.Namespace = "team-prod"
	if runtimeClassBindingName(a) != runtimeClassBindingName(a) {
		t.Fatal("binding name must be deterministic")
	}
	if runtimeClassBindingName(a) == runtimeClassBindingName(b) {
		t.Fatal("bindings for different tenants must not collide")
	}
}

func TestAdmissionHardeningUnsupportedNilError(t *testing.T) {
	if admissionHardeningUnsupported(nil) {
		t.Fatal("nil error must not report unsupported")
	}
}
