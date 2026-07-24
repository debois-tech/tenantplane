package syncplan

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func devRef() ResourceRef {
	return ResourceRef{
		TenantCluster:    "dev",
		TenantNamespace:  "team-dev",
		VirtualNamespace: "default",
		Kind:             "ConfigMap",
		Name:             "app-config",
	}
}

func TestBuildHostObjectNamingAndMetadata(t *testing.T) {
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "app-config",
			"namespace":       "default",
			"resourceVersion": "12345",
			"uid":             "abc-123",
			"labels":          map[string]interface{}{"team": "payments"},
			"ownerReferences": []interface{}{map[string]interface{}{"name": "owner"}},
		},
		"data":   map[string]interface{}{"key": "value"},
		"status": map[string]interface{}{"phase": "Active"},
	}}

	host, err := BuildHostObject(devRef(), tenant)
	if err != nil {
		t.Fatalf("BuildHostObject() error = %v", err)
	}

	if got, want := host.GetName(), "app-config-x-default-x-dev"; got != want {
		t.Fatalf("host name = %q, want %q", got, want)
	}
	if got, want := host.GetNamespace(), "team-dev"; got != want {
		t.Fatalf("host namespace = %q, want %q", got, want)
	}

	// Server-populated and host-owned metadata must be stripped.
	md := host.Object["metadata"].(map[string]interface{})
	for _, field := range []string{"resourceVersion", "uid", "ownerReferences"} {
		if _, ok := md[field]; ok {
			t.Fatalf("metadata.%s should have been stripped", field)
		}
	}
	if _, ok := host.Object["status"]; ok {
		t.Fatal("status should have been stripped")
	}

	// Spec-ish data is preserved.
	data := host.Object["data"].(map[string]interface{})
	if data["key"] != "value" {
		t.Fatalf("data not preserved: %v", data)
	}

	// Reverse-mapping labels are merged with existing labels, not replacing them.
	labels := host.GetLabels()
	if labels["team"] != "payments" {
		t.Fatalf("pre-existing label dropped: %v", labels)
	}
	if labels[LabelManagedBy] != ManagedByValue {
		t.Fatalf("managed-by label = %q, want %q", labels[LabelManagedBy], ManagedByValue)
	}
	if labels[LabelKind] != "configmap" {
		t.Fatalf("kind label = %q, want configmap", labels[LabelKind])
	}
}

func TestBuildHostObjectDoesNotMutateInput(t *testing.T) {
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "app-config",
			"namespace": "default",
		},
	}}

	if _, err := BuildHostObject(devRef(), tenant); err != nil {
		t.Fatalf("BuildHostObject() error = %v", err)
	}
	if got := tenant.GetName(); got != "app-config" {
		t.Fatalf("input name mutated to %q", got)
	}
	if got := tenant.GetNamespace(); got != "default" {
		t.Fatalf("input namespace mutated to %q", got)
	}
}

func TestBuildHostObjectRejectsNil(t *testing.T) {
	if _, err := BuildHostObject(devRef(), nil); err == nil {
		t.Fatal("BuildHostObject(nil) should error")
	}
}

func TestReverseLookupRoundTrips(t *testing.T) {
	ref := devRef()
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      ref.Name,
			"namespace": ref.VirtualNamespace,
		},
	}}

	host, err := BuildHostObject(ref, tenant)
	if err != nil {
		t.Fatalf("BuildHostObject() error = %v", err)
	}

	got, ok := ReverseLookup(host)
	if !ok {
		t.Fatal("ReverseLookup() ok = false, want true")
	}
	if got != ref {
		t.Fatalf("ReverseLookup() = %+v, want %+v", got, ref)
	}
}

func TestReverseLookupRejectsForeignObject(t *testing.T) {
	foreign := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name":   "not-ours",
			"labels": map[string]interface{}{"app.kubernetes.io/managed-by": "helm"},
		},
	}}
	if _, ok := ReverseLookup(foreign); ok {
		t.Fatal("ReverseLookup() ok = true for foreign object, want false")
	}
	if _, ok := ReverseLookup(nil); ok {
		t.Fatal("ReverseLookup(nil) ok = true, want false")
	}
}

// A long virtual name forces the host name to be hashed and therefore
// unrecoverable from the name alone; the annotation must still carry the
// original so ReverseLookup stays exact.
func TestReverseLookupSurvivesHashedName(t *testing.T) {
	ref := devRef()
	ref.Name = "this-name-is-longer-than-most-people-should-ever-use-for-a-configmap"

	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      ref.Name,
			"namespace": ref.VirtualNamespace,
		},
	}}

	host, err := BuildHostObject(ref, tenant)
	if err != nil {
		t.Fatalf("BuildHostObject() error = %v", err)
	}
	if host.GetName() == ref.Name {
		t.Fatal("expected host name to be transformed/hashed for a long name")
	}

	got, ok := ReverseLookup(host)
	if !ok || got.Name != ref.Name {
		t.Fatalf("ReverseLookup() name = %q (ok=%v), want %q", got.Name, ok, ref.Name)
	}
}

func TestBuildHostObjectStripsServiceAllocatedFields(t *testing.T) {
	ref := devRef()
	ref.Kind = "Service"
	ref.Name = "kubernetes"

	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]interface{}{"name": "kubernetes", "namespace": "default"},
		"spec": map[string]interface{}{
			"clusterIP":      "10.43.0.1",
			"clusterIPs":     []interface{}{"10.43.0.1"},
			"ipFamilies":     []interface{}{"IPv4"},
			"ipFamilyPolicy": "SingleStack",
			"ports": []interface{}{
				map[string]interface{}{"port": int64(443), "nodePort": int64(31234)},
			},
		},
	}}

	host, err := BuildHostObject(ref, tenant)
	if err != nil {
		t.Fatalf("BuildHostObject() error = %v", err)
	}

	for _, field := range serviceAllocatedSpecFields {
		if _, found, _ := unstructured.NestedFieldNoCopy(host.Object, "spec", field); found {
			t.Fatalf("spec.%s must be stripped: the tenant's allocator assigned it, not the host's", field)
		}
	}
	ports, _, _ := unstructured.NestedSlice(host.Object, "spec", "ports")
	if _, has := ports[0].(map[string]interface{})["nodePort"]; has {
		t.Fatal("spec.ports[].nodePort must be stripped")
	}
	if port := ports[0].(map[string]interface{})["port"]; port != int64(443) {
		t.Fatalf("spec.ports[].port = %v, want 443 (only allocator fields are stripped)", port)
	}
}

func TestAdoptHostAllocatedFields(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"spec": map[string]interface{}{
			"clusterIP":  "10.96.5.7",
			"clusterIPs": []interface{}{"10.96.5.7"},
		},
	}}
	desired := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"spec":       map[string]interface{}{"selector": map[string]interface{}{"app": "web"}},
	}}

	AdoptHostAllocatedFields(existing, desired)

	if got, _, _ := unstructured.NestedString(desired.Object, "spec", "clusterIP"); got != "10.96.5.7" {
		t.Fatalf("clusterIP = %q, want the host-allocated 10.96.5.7 (immutable on update)", got)
	}
	if sel, _, _ := unstructured.NestedMap(desired.Object, "spec", "selector"); sel["app"] != "web" {
		t.Fatal("adoption must not clobber tenant-owned spec fields")
	}

	// Non-Services are untouched.
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"spec": map[string]interface{}{"clusterIP": "10.96.9.9"},
	}}
	out := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap", "spec": map[string]interface{}{},
	}}
	AdoptHostAllocatedFields(cm, out)
	if _, found, _ := unstructured.NestedFieldNoCopy(out.Object, "spec", "clusterIP"); found {
		t.Fatal("AdoptHostAllocatedFields must only act on Services and Pods")
	}
}

func TestAdoptHostAllocatedFieldsPodPreservesImmutableSpecAndUpdatesImage(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"spec": map[string]interface{}{
			"nodeName":           "tenantplane-dev-control-plane",
			"serviceAccountName": "default",
			"containers": []interface{}{
				map[string]interface{}{"name": "web", "image": "nginx:1.24"},
			},
			"tolerations": []interface{}{
				map[string]interface{}{"key": "node.kubernetes.io/not-ready", "operator": "Exists"},
			},
		},
	}}
	// desired is what a fresh sync pass built from the tenant's (never
	// scheduled, so nodeName-less) pod: an image bump plus a new toleration.
	desired := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{"name": "web", "image": "nginx:1.25"},
			},
			"tolerations": []interface{}{
				map[string]interface{}{"key": "node.kubernetes.io/not-ready", "operator": "Exists"},
				map[string]interface{}{"key": "extra", "operator": "Exists"},
			},
			"runtimeClassName": "kata-qemu",
		},
	}}

	AdoptHostAllocatedFields(existing, desired)

	if got, _, _ := unstructured.NestedString(desired.Object, "spec", "nodeName"); got != "tenantplane-dev-control-plane" {
		t.Fatalf("nodeName = %q, want the scheduled node adopted from the live object (Kubernetes forbids clearing it)", got)
	}
	if got, _, _ := unstructured.NestedString(desired.Object, "spec", "serviceAccountName"); got != "default" {
		t.Fatal("serviceAccountName must be adopted from the live object too")
	}
	containers, _, _ := unstructured.NestedSlice(desired.Object, "spec", "containers")
	if img, _, _ := unstructured.NestedString(containers[0].(map[string]interface{}), "image"); img != "nginx:1.25" {
		t.Fatalf("container image = %q, want the tenant's updated image to still take effect", img)
	}
	tolerations, _, _ := unstructured.NestedSlice(desired.Object, "spec", "tolerations")
	if len(tolerations) != 2 {
		t.Fatalf("tolerations = %v, want the existing one plus the new addition (2 total)", tolerations)
	}
}

func TestContentEqualIgnoresMetadataAndHostAllocatedFields(t *testing.T) {
	a := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]interface{}{"name": "web", "labels": map[string]interface{}{"a": "1"}},
		"spec": map[string]interface{}{
			"selector":  map[string]interface{}{"app": "web"},
			"clusterIP": "10.96.1.1",
		},
	}}
	b := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]interface{}{"name": "web-x-default-x-dev", "labels": map[string]interface{}{"b": "2"}},
		"spec": map[string]interface{}{
			"selector":  map[string]interface{}{"app": "web"},
			"clusterIP": "10.96.9.9", // host-allocated: different values must not count as a diff
		},
	}}
	if !ContentEqual(a, b) {
		t.Fatal("ContentEqual must ignore metadata and host-allocated spec fields")
	}

	b2 := b.DeepCopy()
	_ = unstructured.SetNestedField(b2.Object, "db", "spec", "selector", "app")
	if ContentEqual(a, b2) {
		t.Fatal("ContentEqual must still catch a real content difference")
	}
}

func TestMergeHostContentIntoTenantPreservesTenantMetadata(t *testing.T) {
	host := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "shared-x-default-x-dev", "namespace": "team-dev",
			"labels": map[string]interface{}{"app.kubernetes.io/managed-by": "tenantplane"},
		},
		"data": map[string]interface{}{"key": "from-host"},
	}}
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "shared", "namespace": "default", "resourceVersion": "42",
			"labels": map[string]interface{}{"owner": "tenant-team"},
		},
		"data": map[string]interface{}{"key": "stale-tenant-value"},
	}}

	merged := MergeHostContentIntoTenant(host, tenant)

	if merged.GetName() != "shared" || merged.GetNamespace() != "default" {
		t.Fatalf("identity = %s/%s, want the tenant's own name/namespace preserved", merged.GetNamespace(), merged.GetName())
	}
	if merged.GetResourceVersion() != "42" {
		t.Fatal("resourceVersion must be preserved so the update is a proper optimistic-concurrency check")
	}
	if merged.GetLabels()["owner"] != "tenant-team" {
		t.Fatal("the tenant's own labels must be preserved, not replaced with the host object's")
	}
	data, _, _ := unstructured.NestedMap(merged.Object, "data")
	if data["key"] != "from-host" {
		t.Fatalf("data = %v, want the host's content reflected in", data)
	}
}
