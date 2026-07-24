package syncplan

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Reverse-mapping metadata. Labels are selectable, so they carry the pieces an
// operator filters host objects by (tenant, virtual namespace, kind). Label
// values are bounded to 63 characters and a restricted charset, so the original
// virtual name and namespace — which may be longer or, once the host name is
// hashed, unrecoverable from the name alone — are preserved verbatim in
// annotations. Together they answer "why does this host object exist?".
const (
	LabelManagedBy        = "app.kubernetes.io/managed-by"
	LabelTenant           = "tenantplane.io/tenant"
	LabelVirtualNamespace = "tenantplane.io/virtual-namespace"
	LabelKind             = "tenantplane.io/kind"

	AnnotationVirtualNamespace = "tenantplane.io/virtual-namespace"
	AnnotationVirtualName      = "tenantplane.io/virtual-name"

	ManagedByValue = "tenantplane"
)

// hostManagedMetadataFields are server-populated or host-owned metadata keys
// that must never be copied from the tenant object onto the host object: they
// either belong to the tenant control plane's view or would be rejected/ignored
// on create. ownerReferences are dropped because tenant-side owners have no
// meaning on the host; the host object is owned via tenantplane's own labels.
var hostManagedMetadataFields = []string{
	"resourceVersion",
	"uid",
	"generation",
	"creationTimestamp",
	"deletionTimestamp",
	"deletionGracePeriodSeconds",
	"managedFields",
	"ownerReferences",
	"selfLink",
	"finalizers",
}

// HostLabels returns the selectable reverse-mapping labels stamped on every host
// object tenantplane materializes for ref. Kind is lowercased to match the
// convention used across tenantplane (see explain-sync output).
func HostLabels(ref ResourceRef) map[string]string {
	return map[string]string{
		LabelManagedBy:        ManagedByValue,
		LabelTenant:           SanitizeName(ref.TenantCluster),
		LabelVirtualNamespace: SanitizeName(ref.VirtualNamespace),
		LabelKind:             SanitizeName(ref.Kind),
	}
}

// HostAnnotations returns the verbatim virtual name/namespace preserved on the
// host object so the original identity survives even when the host name is
// hashed for length. See ReverseLookup for the inverse.
func HostAnnotations(ref ResourceRef) map[string]string {
	return map[string]string{
		AnnotationVirtualNamespace: ref.VirtualNamespace,
		AnnotationVirtualName:      ref.Name,
	}
}

// BuildHostObject transforms a tenant object into the host object tenantplane
// should apply for it. tenant is the object as seen inside the virtual cluster;
// the returned object is a deep copy with a deterministic host name/namespace,
// reverse-mapping labels/annotations merged in, and all server-populated or
// host-owned metadata and any status stripped. The input is never mutated.
//
// ref supplies the tenant/virtual-namespace/kind coordinates; ref.Name and
// ref.Kind must agree with the object so the reverse mapping stays truthful.
func BuildHostObject(ref ResourceRef, tenant *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if tenant == nil {
		return nil, fmt.Errorf("tenant object is required")
	}
	plan, err := Explain(ref)
	if err != nil {
		return nil, err
	}

	host := tenant.DeepCopy()
	for _, field := range hostManagedMetadataFields {
		unstructured.RemoveNestedField(host.Object, "metadata", field)
	}
	unstructured.RemoveNestedField(host.Object, "status")
	stripHostAllocatedSpecFields(ref.Kind, host)

	host.SetNamespace(plan.Host.Namespace)
	host.SetName(plan.Host.Name)
	host.SetLabels(mergeMap(host.GetLabels(), HostLabels(ref)))
	host.SetAnnotations(mergeMap(host.GetAnnotations(), HostAnnotations(ref)))

	return host, nil
}

// serviceAllocatedSpecFields are Service spec fields the API server allocates
// per cluster. A tenant control plane hands out addresses from its own service
// CIDR (e.g. k3s's 10.43.0.0/16), which the host would reject outright — so
// they are stripped before create and re-adopted from the live host object
// before update (clusterIP is immutable once allocated).
var serviceAllocatedSpecFields = []string{
	"clusterIP",
	"clusterIPs",
	"healthCheckNodePort",
	"ipFamilies",
	"ipFamilyPolicy",
}

func stripHostAllocatedSpecFields(kind string, host *unstructured.Unstructured) {
	if kind != "Service" {
		return
	}
	for _, field := range serviceAllocatedSpecFields {
		unstructured.RemoveNestedField(host.Object, "spec", field)
	}
	// nodePorts are likewise host-allocated; missing values are (re)assigned by
	// the host on write, which is the safe behavior for a projected Service.
	if ports, found, _ := unstructured.NestedSlice(host.Object, "spec", "ports"); found {
		for i := range ports {
			if port, ok := ports[i].(map[string]interface{}); ok {
				delete(port, "nodePort")
			}
		}
		_ = unstructured.SetNestedSlice(host.Object, ports, "spec", "ports")
	}
}

// AdoptHostAllocatedFields copies fields the host — not the tenant — owns
// from the live host object onto the desired object before an update, so the
// write does not omit or contradict something only the host allocates or that
// Kubernetes forbids changing post-create. For Service that is a handful of
// allocator fields (clusterIP is immutable once assigned). For Pod it is
// almost the entire spec: see adoptPodImmutableFields.
func AdoptHostAllocatedFields(existing, desired *unstructured.Unstructured) {
	if existing == nil || desired == nil {
		return
	}
	switch existing.GetKind() {
	case "Service":
		adoptServiceAllocatedFields(existing, desired)
	case "Pod":
		adoptPodImmutableFields(existing, desired)
	}
}

func adoptServiceAllocatedFields(existing, desired *unstructured.Unstructured) {
	for _, field := range serviceAllocatedSpecFields {
		value, found, _ := unstructured.NestedFieldCopy(existing.Object, "spec", field)
		if !found {
			continue
		}
		_ = unstructured.SetNestedField(desired.Object, value, "spec", field)
	}
}

// adoptPodImmutableFields keeps a re-synced Pod's update valid by starting
// from the live object's spec (Kubernetes rejects a Pod update touching
// almost anything else once it is scheduled — nodeName above all) and
// re-applying only the handful of fields Kubernetes does allow changing
// after create: container/init-container images by name, activeDeadlineSeconds,
// and tolerations (as pure additions — Kubernetes never allows removing one).
// Without this, every sync pass after the first successful Create would fail
// outright the moment the Pod was scheduled to a node.
func adoptPodImmutableFields(existing, desired *unstructured.Unstructured) {
	existingSpec, found, _ := unstructured.NestedMap(existing.Object, "spec")
	if !found {
		return
	}
	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")

	if containers, found, _ := unstructured.NestedSlice(desiredSpec, "containers"); found {
		if ec, found, _ := unstructured.NestedSlice(existingSpec, "containers"); found {
			_ = unstructured.SetNestedSlice(existingSpec, overlayContainerImages(ec, containers), "containers")
		}
	}
	if initContainers, found, _ := unstructured.NestedSlice(desiredSpec, "initContainers"); found {
		if ec, found, _ := unstructured.NestedSlice(existingSpec, "initContainers"); found {
			_ = unstructured.SetNestedSlice(existingSpec, overlayContainerImages(ec, initContainers), "initContainers")
		}
	}
	if v, found, _ := unstructured.NestedInt64(desiredSpec, "activeDeadlineSeconds"); found {
		_ = unstructured.SetNestedField(existingSpec, v, "activeDeadlineSeconds")
	}
	if tolerations, found, _ := unstructured.NestedSlice(desiredSpec, "tolerations"); found {
		existingTolerations, _, _ := unstructured.NestedSlice(existingSpec, "tolerations")
		_ = unstructured.SetNestedSlice(existingSpec, mergeTolerations(existingTolerations, tolerations), "tolerations")
	}

	_ = unstructured.SetNestedMap(desired.Object, existingSpec, "spec")
}

// overlayContainerImages returns existing's containers with each one's image
// replaced by the same-named container's image from desired, if present. The
// container set itself is never added to or removed from: Kubernetes does not
// allow that on a running Pod either, only image changes within it.
func overlayContainerImages(existing, desired []interface{}) []interface{} {
	desiredImages := make(map[string]interface{}, len(desired))
	for _, c := range desired {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		if image, ok := m["image"]; ok {
			desiredImages[name] = image
		}
	}

	out := make([]interface{}, len(existing))
	for i, c := range existing {
		m, ok := c.(map[string]interface{})
		if !ok {
			out[i] = c
			continue
		}
		name, _ := m["name"].(string)
		if image, ok := desiredImages[name]; ok {
			m["image"] = image
		}
		out[i] = m
	}
	return out
}

// mergeTolerations returns existing's tolerations plus any from desired not
// already present, since Kubernetes allows only adding tolerations to a
// running Pod, never removing or replacing one.
func mergeTolerations(existing, desired []interface{}) []interface{} {
	out := append([]interface{}{}, existing...)
	for _, d := range desired {
		dup := false
		for _, e := range existing {
			if reflect.DeepEqual(d, e) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, d)
		}
	}
	return out
}

// ReverseLookup recovers the tenant-side identity of a host object from the
// labels and annotations BuildHostObject stamped on it. ok is false when the
// object is not a tenantplane-managed host object (its managed-by label is
// missing or foreign), so callers can distinguish "not ours" from a partial
// match.
func ReverseLookup(host *unstructured.Unstructured) (ref ResourceRef, ok bool) {
	if host == nil {
		return ResourceRef{}, false
	}
	labels := host.GetLabels()
	if labels[LabelManagedBy] != ManagedByValue {
		return ResourceRef{}, false
	}
	annotations := host.GetAnnotations()

	return ResourceRef{
		TenantCluster:    labels[LabelTenant],
		TenantNamespace:  host.GetNamespace(),
		VirtualNamespace: annotations[AnnotationVirtualNamespace],
		Kind:             host.GetKind(),
		Name:             annotations[AnnotationVirtualName],
	}, true
}

// contentFrom returns obj's non-metadata content: every top-level field
// except apiVersion, kind, metadata, and status. This is deliberately generic
// across Kinds instead of hand-listing field names per Kind: a Pod's content
// is "spec"; a ConfigMap's is "data"/"binaryData"; a Secret's is
// "data"/"stringData"/"type" — Kubernetes objects have no other top-level
// categories to worry about missing.
func contentFrom(obj *unstructured.Unstructured) map[string]interface{} {
	out := make(map[string]interface{}, len(obj.Object))
	for k, v := range obj.Object {
		switch k {
		case "apiVersion", "kind", "metadata", "status":
			continue
		}
		out[k] = v
	}
	return out
}

// ContentEqual reports whether a and b already agree on everything but
// metadata/status, once each side's own host-allocated fields (e.g. Service
// clusterIP — the host and the tenant's own virtual cluster each allocate
// their own, from different ranges, so these can never legitimately match)
// are excluded from the comparison. Used by fromHost/bidirectional sync to
// decide whether there is anything to reconcile at all before consulting
// conflictPolicy.
func ContentEqual(a, b *unstructured.Unstructured) bool {
	ac, bc := a.DeepCopy(), b.DeepCopy()
	stripHostAllocatedSpecFields(a.GetKind(), ac)
	stripHostAllocatedSpecFields(b.GetKind(), bc)
	return reflect.DeepEqual(contentFrom(ac), contentFrom(bc))
}

// MergeHostContentIntoTenant returns a copy of tenantObj with its content
// (everything but metadata/status) replaced by hostObj's, after stripping
// fields the host's own cluster allocates that the tenant's own (differently
// addressed) virtual cluster could never accept verbatim. The tenant's own
// metadata — labels, annotations, name, namespace, resourceVersion — is left
// untouched: fromHost/bidirectional reflect an object's content back into
// something the tenant still owns, they do not replace its identity the way
// BuildHostObject does for a host object tenantplane owns outright.
func MergeHostContentIntoTenant(hostObj, tenantObj *unstructured.Unstructured) *unstructured.Unstructured {
	merged := tenantObj.DeepCopy()
	hostCopy := hostObj.DeepCopy()
	stripHostAllocatedSpecFields(hostObj.GetKind(), hostCopy)

	for k := range contentFrom(merged) {
		delete(merged.Object, k)
	}
	for k, v := range contentFrom(hostCopy) {
		merged.Object[k] = v
	}
	return merged
}

// mergeMap returns base with every key from overlay set, allocating a new map
// when base is nil so callers never share tenantplane's constant maps.
func mergeMap(base, overlay map[string]string) map[string]string {
	if base == nil {
		base = make(map[string]string, len(overlay))
	}
	for k, v := range overlay {
		base[k] = v
	}
	return base
}
