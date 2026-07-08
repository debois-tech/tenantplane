// Package syncer materializes objects from a tenant (virtual) control plane onto
// the host cluster and keeps the two views from drifting. It is the runtime
// counterpart to internal/syncplan: syncplan decides *where* a tenant object
// lands and how to map it back; syncer actually applies and garbage-collects
// those host objects, recording a decision for every action so an operator can
// always ask "why does this host object exist?".
package syncer

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/debois-tech/tenantplane/internal/syncplan"
)

// Direction controls which way a resource kind is synced across the
// virtual-to-host boundary. Only DirectionToHost is implemented in this
// milestone; the others are accepted so a SyncPolicy can declare intent without
// the engine silently doing the wrong thing.
type Direction string

const (
	DirectionToHost        Direction = "toHost"
	DirectionFromHost      Direction = "fromHost"
	DirectionBidirectional Direction = "bidirectional"
)

// Resource names a Kubernetes kind to sync and the direction it flows.
type Resource struct {
	GVK       schema.GroupVersionKind
	Direction Direction
}

// Action is the outcome the engine chose for a single object.
type Action string

const (
	ActionCreate Action = "Create"
	ActionUpdate Action = "Update"
	ActionDelete Action = "Delete"
	ActionSkip   Action = "Skip"
)

// Decision is an explainable record of one sync action. It is what makes the
// sync subsystem inspectable: every Create/Update/Delete/Skip the engine
// performs produces exactly one Decision.
type Decision struct {
	Action Action
	Kind   string
	Ref    syncplan.ResourceRef
	Host   syncplan.HostTarget
	Reason string
}

// DecisionRecorder receives every decision the engine makes. Implementations
// may emit Events, append to a status list, or drop them; the engine never
// depends on the recorder for correctness. A nil recorder is safe.
type DecisionRecorder interface {
	Record(ctx context.Context, d Decision)
}

// defaultSkipNamespaces are virtual-cluster system namespaces whose objects are
// infrastructure of the tenant control plane itself (CoreDNS, leases, RBAC
// bootstrap) and must not be projected onto the host.
var defaultSkipNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// Engine syncs one tenant's resources. It holds a client for the tenant
// (virtual) control plane and one for the host cluster; every host object it
// writes lands in HostNamespace, named deterministically by syncplan so the
// same virtual object always maps to the same host object.
type Engine struct {
	Tenant        string
	HostNamespace string
	VirtualClient client.Client
	HostClient    client.Client
	Recorder      DecisionRecorder

	// SkipNamespaces overrides defaultSkipNamespaces when non-nil.
	SkipNamespaces map[string]bool
}

// SyncToHost performs one convergence pass for res: it projects every eligible
// virtual object of the kind onto the host, then deletes host objects the
// engine previously created whose virtual source is gone. It returns the
// decisions made, most useful for tests and status. Errors from individual
// objects are aggregated so one bad object does not abandon the rest of the
// pass.
func (e *Engine) SyncToHost(ctx context.Context, res Resource) ([]Decision, error) {
	if res.Direction != DirectionToHost {
		return nil, fmt.Errorf("syncer: direction %q is not implemented (only %q)", res.Direction, DirectionToHost)
	}

	virtual := &unstructured.UnstructuredList{}
	virtual.SetGroupVersionKind(listGVK(res.GVK))
	if err := e.VirtualClient.List(ctx, virtual); err != nil {
		return nil, fmt.Errorf("list virtual %s: %w", res.GVK.Kind, err)
	}

	var decisions []Decision
	var errs []error
	desiredHostNames := make(map[string]bool)

	for i := range virtual.Items {
		obj := &virtual.Items[i]
		if e.skip(obj.GetNamespace()) {
			continue
		}

		ref := e.refFor(res.GVK.Kind, obj)
		host, err := syncplan.BuildHostObject(ref, obj)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: build host object: %w", obj.GetNamespace(), obj.GetName(), err))
			continue
		}
		host.SetGroupVersionKind(res.GVK)
		desiredHostNames[host.GetName()] = true

		d, err := e.applyHost(ctx, ref, host)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: apply host object: %w", obj.GetNamespace(), obj.GetName(), err))
			continue
		}
		e.record(ctx, d)
		decisions = append(decisions, d)
	}

	gc, err := e.collectOrphans(ctx, res.GVK, desiredHostNames)
	if err != nil {
		errs = append(errs, err)
	}
	for _, d := range gc {
		e.record(ctx, d)
		decisions = append(decisions, d)
	}

	return decisions, aggregate(errs)
}

// applyHost creates or updates one host object and returns the decision. It
// preserves the existing resourceVersion on update so the write is a
// conflict-checked replacement of the fields tenantplane owns.
//
// Before overwriting an existing object it verifies provenance: the host name is
// deterministic but not injective (long names are hash-truncated), so two
// distinct tenant objects could in principle target the same host name, and an
// unrelated object could already occupy it. In either case applyHost refuses to
// clobber and records an explainable Skip rather than silently destroying data.
func (e *Engine) applyHost(ctx context.Context, ref syncplan.ResourceRef, host *unstructured.Unstructured) (Decision, error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(host.GroupVersionKind())
	err := e.HostClient.Get(ctx, client.ObjectKeyFromObject(host), existing)

	target := syncplan.HostTarget{Namespace: host.GetNamespace(), Name: host.GetName()}

	if apierrors.IsNotFound(err) {
		if err := e.HostClient.Create(ctx, host); err != nil {
			return Decision{}, err
		}
		return Decision{Action: ActionCreate, Kind: ref.Kind, Ref: ref, Host: target,
			Reason: "virtual object has no host counterpart yet"}, nil
	}
	if err != nil {
		return Decision{}, err
	}

	existingRef, managed := syncplan.ReverseLookup(existing)
	if !managed {
		return Decision{Action: ActionSkip, Kind: ref.Kind, Ref: ref, Host: target,
			Reason: "host name is already occupied by an object tenantplane does not manage"}, nil
	}
	if !sameSource(existingRef, ref) {
		return Decision{Action: ActionSkip, Kind: ref.Kind, Ref: ref, Host: target,
			Reason: fmt.Sprintf("host name collides with a different tenant object (%s/%s)", existingRef.VirtualNamespace, existingRef.Name)}, nil
	}

	host.SetResourceVersion(existing.GetResourceVersion())
	if err := e.HostClient.Update(ctx, host); err != nil {
		return Decision{}, err
	}
	return Decision{Action: ActionUpdate, Kind: ref.Kind, Ref: ref, Host: target,
		Reason: "reconciled host object to match virtual source"}, nil
}

// sameSource reports whether an existing host object's reverse-mapped identity
// refers to the same tenant object we are about to sync.
func sameSource(a, b syncplan.ResourceRef) bool {
	return a.TenantCluster == b.TenantCluster &&
		a.VirtualNamespace == b.VirtualNamespace &&
		a.Name == b.Name
}

// collectOrphans deletes host objects tenantplane manages for this tenant and
// kind whose virtual source no longer exists (their host name is absent from
// desired). This is what keeps a delete in the tenant from leaving a ghost on
// the host.
func (e *Engine) collectOrphans(ctx context.Context, gvk schema.GroupVersionKind, desired map[string]bool) ([]Decision, error) {
	hostList := &unstructured.UnstructuredList{}
	hostList.SetGroupVersionKind(listGVK(gvk))
	if err := e.HostClient.List(ctx, hostList,
		client.InNamespace(e.HostNamespace),
		client.MatchingLabels{
			syncplan.LabelManagedBy: syncplan.ManagedByValue,
			syncplan.LabelTenant:    syncplan.SanitizeName(e.Tenant),
			syncplan.LabelKind:      syncplan.SanitizeName(gvk.Kind),
		},
	); err != nil {
		return nil, fmt.Errorf("list host %s for GC: %w", gvk.Kind, err)
	}

	var decisions []Decision
	var errs []error
	for i := range hostList.Items {
		obj := &hostList.Items[i]
		if desired[obj.GetName()] {
			continue
		}
		ref, _ := syncplan.ReverseLookup(obj)
		if err := e.HostClient.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete orphan %s: %w", obj.GetName(), err))
			continue
		}
		decisions = append(decisions, Decision{
			Action: ActionDelete, Kind: gvk.Kind, Ref: ref,
			Host:   syncplan.HostTarget{Namespace: obj.GetNamespace(), Name: obj.GetName()},
			Reason: "virtual source no longer exists",
		})
	}
	return decisions, aggregate(errs)
}

func (e *Engine) refFor(kind string, obj *unstructured.Unstructured) syncplan.ResourceRef {
	return syncplan.ResourceRef{
		TenantCluster:    e.Tenant,
		TenantNamespace:  e.HostNamespace,
		VirtualNamespace: obj.GetNamespace(),
		Kind:             kind,
		Name:             obj.GetName(),
	}
}

func (e *Engine) skip(namespace string) bool {
	if e.SkipNamespaces != nil {
		return e.SkipNamespaces[namespace]
	}
	return defaultSkipNamespaces[namespace]
}

func (e *Engine) record(ctx context.Context, d Decision) {
	if e.Recorder != nil {
		e.Recorder.Record(ctx, d)
	}
}

// listGVK returns the List kind for an item GVK (Pod -> PodList), which is what
// the client needs to populate an UnstructuredList.
func listGVK(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	list := gvk
	list.Kind = gvk.Kind + "List"
	return list
}

func aggregate(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		msg := fmt.Sprintf("%d sync errors", len(errs))
		for _, err := range errs {
			msg += "\n  - " + err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
}
