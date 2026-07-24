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

	"golang.org/x/time/rate"

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

// ConvergedVersions is the tenant and host resourceVersions the last time a
// bidirectional pair was confirmed to agree.
type ConvergedVersions struct {
	TenantResourceVersion string
	HostResourceVersion   string
}

// ConvergenceHistory is an in-memory, per-pass view of ConvergedVersions,
// keyed by host object name, that lets bidirectional sync tell "only one
// side changed since the last time these agreed" from a genuine two-sided
// conflict. A nil or empty history means no prior record exists for a given
// pair — every difference is then treated as needing conflictPolicy, exactly
// as if this didn't exist. Engine.History is a plain map: the caller loads it
// once before a sync pass (e.g. from a durable SyncDecision object) and,
// since map mutations are visible through the same reference, can persist
// the same map back once after — see internal/controller/sync_decisions.go.
type ConvergenceHistory map[string]ConvergedVersions

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

	// RequiredRuntimeClassName, when set, is stamped onto every synced Pod's
	// spec.runtimeClassName — overwriting whatever the tenant set. Isolation is
	// not a tenant-negotiable setting: this is how an IsolationProfile's
	// runtimeClassName (e.g. a sandboxed runtime) reaches every pod without
	// requiring tenant awareness, and it cannot be bypassed by a tenant simply
	// omitting or overriding the field. A ValidatingAdmissionPolicy binds the
	// same requirement at the host API server as a defense-in-depth backstop.
	RequiredRuntimeClassName string

	// RateLimiter caps how many host writes this tenant's sync passes may make
	// per second (IsolationProfile.apiFairness). A nil limiter is unlimited.
	// When the limit is hit, the write is not attempted; it is recorded as an
	// explainable Skip decision and retried on the next sync pass, rather than
	// blocking the shared reconcile worker.
	RateLimiter *rate.Limiter

	// History backs bidirectional conflict detection (see
	// ConvergenceHistory). Nil disables it: every bidirectional difference is
	// then treated as needing conflictPolicy, exactly as before this existed.
	History ConvergenceHistory
}

// SyncToHost performs one convergence pass for res: it projects every eligible
// virtual object of the kind onto the host, then deletes host objects the
// engine previously created whose virtual source is gone. It returns the
// decisions made, most useful for tests and status. Errors from individual
// objects are aggregated so one bad object does not abandon the rest of the
// pass. The tenant is authoritative: every pass pushes its current state to
// the host, full stop.
func (e *Engine) SyncToHost(ctx context.Context, res Resource) ([]Decision, error) {
	if res.Direction != DirectionToHost {
		return nil, fmt.Errorf("syncer: direction %q is not implemented (only %q)", res.Direction, DirectionToHost)
	}
	return e.syncPass(ctx, res, "")
}

// SyncFromHost performs one convergence pass in the opposite direction: the
// host is authoritative. The tenant object still has to exist for tenantplane
// to know a pair should exist at all (discovery always enumerates the
// tenant's own objects, exactly like SyncToHost) — but once a host mirror
// exists, this pulls its current content back into the tenant on every pass,
// rather than pushing the tenant's content out. A host mirror that does not
// exist yet is bootstrap-created from the tenant's current state (there is
// nothing on the host yet to prefer), exactly like SyncToHost's first create.
func (e *Engine) SyncFromHost(ctx context.Context, res Resource) ([]Decision, error) {
	if res.Direction != DirectionFromHost {
		return nil, fmt.Errorf("syncer: direction %q is not implemented (only %q)", res.Direction, DirectionFromHost)
	}
	return e.syncPass(ctx, res, "")
}

// SyncBidirectional performs one convergence pass where either side may have
// drifted: if tenant and host already agree, nothing happens. If they
// disagree, conflictPolicy decides — "tenant-wins" pushes to the host (like
// SyncToHost), "host-wins" pulls into the tenant (like SyncFromHost), and
// "manual" (the safe default) touches neither side and just records that
// they disagree, exactly as documented in the SyncPolicy conflictPolicy
// field. There is no persisted history of prior syncs to tell "only the
// tenant changed" from "only the host changed" from "both changed" — every
// pass simply compares current state, so "manual" will report a conflict
// even when, with more context, one side's change would have been obvious.
func (e *Engine) SyncBidirectional(ctx context.Context, res Resource, conflictPolicy string) ([]Decision, error) {
	if res.Direction != DirectionBidirectional {
		return nil, fmt.Errorf("syncer: direction %q is not implemented (only %q)", res.Direction, DirectionBidirectional)
	}
	return e.syncPass(ctx, res, conflictPolicy)
}

// syncPass is the shared driver behind all three directions: it always
// discovers pairs by enumerating the tenant's own objects (so deleting the
// tenant object removes the host mirror too, in every direction), and always
// establishes a missing host mirror by bootstrapping from the tenant. Only
// what happens when both sides already exist — reconcilePair — differs by
// direction.
func (e *Engine) syncPass(ctx context.Context, res Resource, conflictPolicy string) ([]Decision, error) {
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
		if isVirtualInfrastructure(res.GVK.Kind, obj) {
			continue
		}

		ref := e.refFor(res.GVK.Kind, obj)
		host, err := syncplan.BuildHostObject(ref, obj)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: build host object: %w", obj.GetNamespace(), obj.GetName(), err))
			continue
		}
		host.SetGroupVersionKind(res.GVK)
		if res.GVK.Kind == "Pod" && e.RequiredRuntimeClassName != "" {
			if err := unstructured.SetNestedField(host.Object, e.RequiredRuntimeClassName, "spec", "runtimeClassName"); err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: set runtimeClassName: %w", obj.GetNamespace(), obj.GetName(), err))
				continue
			}
		}
		desiredHostNames[host.GetName()] = true

		d, err := e.reconcilePair(ctx, res.Direction, conflictPolicy, ref, obj, host)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: reconcile: %w", obj.GetNamespace(), obj.GetName(), err))
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

// reconcilePair converges one already-identified virtual/host pair. host is
// the desired host object built from the tenant's current state — used
// as-is when creating (nothing on the host yet to prefer) or pushing
// (toHost, or bidirectional's tenant-wins). virtualObj is the tenant's live
// object — pulled onto (fromHost, or bidirectional's host-wins).
//
// Before overwriting an existing host object it verifies provenance: the host
// name is deterministic but not injective (long names are hash-truncated),
// so two distinct tenant objects could in principle target the same host
// name, and an unrelated object could already occupy it. In either case
// reconcilePair refuses to clobber and records an explainable Skip rather
// than silently destroying data.
func (e *Engine) reconcilePair(ctx context.Context, direction Direction, conflictPolicy string, ref syncplan.ResourceRef, virtualObj, host *unstructured.Unstructured) (Decision, error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(host.GroupVersionKind())
	err := e.HostClient.Get(ctx, client.ObjectKeyFromObject(host), existing)

	target := syncplan.HostTarget{Namespace: host.GetNamespace(), Name: host.GetName()}

	if apierrors.IsNotFound(err) {
		if d, limited := e.rateLimited(ref, target); limited {
			return d, nil
		}
		if err := e.HostClient.Create(ctx, host); err != nil {
			return Decision{}, err
		}
		if direction == DirectionBidirectional {
			e.setConverged(target.Name, virtualObj.GetResourceVersion(), host.GetResourceVersion())
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

	if direction == DirectionToHost {
		return e.pushToHost(ctx, ref, target, virtualObj, existing, host, false)
	}

	// runtimeClassName is stamped onto the host object by this engine, not by
	// the tenant (see syncPass) — it must never be compared against or pulled
	// back from the host: the tenant's own virtual cluster has no such
	// RuntimeClass to satisfy, and every pass would otherwise see a permanent,
	// unresolvable "conflict" purely from an injection tenantplane itself made.
	comparableHost := existing
	if ref.Kind == "Pod" && e.RequiredRuntimeClassName != "" {
		comparableHost = existing.DeepCopy()
		unstructured.RemoveNestedField(comparableHost.Object, "spec", "runtimeClassName")
	}

	if syncplan.ContentEqual(virtualObj, comparableHost) {
		if direction == DirectionBidirectional {
			e.setConverged(target.Name, virtualObj.GetResourceVersion(), existing.GetResourceVersion())
		}
		return Decision{Action: ActionSkip, Kind: ref.Kind, Ref: ref, Host: target,
			Reason: "tenant and host already agree"}, nil
	}

	if direction == DirectionFromHost {
		return e.pullToTenant(ctx, ref, target, virtualObj, comparableHost, "host is authoritative for this direction", false)
	}
	return e.reconcileBidirectionalConflict(ctx, conflictPolicy, ref, target, virtualObj, existing, comparableHost, host)
}

// reconcileBidirectionalConflict decides what to do once a bidirectional
// pair's content is known to currently differ.
//
// tenant-wins and host-wins are already fully decisive — the user declared an
// unconditional winner — so they enforce that winner on any difference,
// exactly as before history existed; there is nothing ambiguous left for
// history to resolve.
//
// "manual" (the default) is different: its whole point is minimizing how
// often a human needs to be bothered. If History has a record of the last
// time tenant and host agreed, and only one side's resourceVersion has moved
// since, that side's change is propagated automatically — a one-sided change
// isn't really a conflict, just an ordinary update the other side hasn't
// caught up to yet. Only when both sides have moved since the last
// known-good point — or no history exists at all — does "manual" actually
// skip and leave both sides alone.
//
// resourceVersion is an opaque, monotonically-changing marker for "has
// anyone written this object since I last looked," which is exactly what's
// needed here — but it isn't a content hash: an edit to some field outside
// what ContentEqual compares (a label, say) would also count as "changed."
// That's a rare, acceptable false positive (falling back to a skip), not a
// false negative (silently missing a real change).
func (e *Engine) reconcileBidirectionalConflict(ctx context.Context, conflictPolicy string, ref syncplan.ResourceRef, target syncplan.HostTarget, virtualObj, existing, comparableHost, host *unstructured.Unstructured) (Decision, error) {
	switch conflictPolicy {
	case "tenant-wins":
		return e.pushToHost(ctx, ref, target, virtualObj, existing, host, true)
	case "host-wins":
		return e.pullToTenant(ctx, ref, target, virtualObj, comparableHost,
			`conflictPolicy "host-wins": pulled the host's state into the tenant`, true)
	}

	if last, ok := e.History[target.Name]; ok {
		tenantChanged := virtualObj.GetResourceVersion() != last.TenantResourceVersion
		hostChanged := existing.GetResourceVersion() != last.HostResourceVersion
		switch {
		case tenantChanged && !hostChanged:
			return e.pushToHost(ctx, ref, target, virtualObj, existing, host, true)
		case hostChanged && !tenantChanged:
			return e.pullToTenant(ctx, ref, target, virtualObj, comparableHost,
				"only the host changed since tenant and host last agreed; conflictPolicy \"manual\" only holds back a genuine two-sided conflict", true)
		}
		// Both moved (or, in principle, neither — but ContentEqual already
		// ruled that out before this was called): a genuine conflict.
	}
	return Decision{Action: ActionSkip, Kind: ref.Kind, Ref: ref, Host: target,
		Reason: `tenant and host disagree; conflictPolicy is "manual" (or unset) so neither side was changed`}, nil
}

// setConverged records the current tenant/host resourceVersions as the new
// converged baseline for hostName. Deliberately not called for a "manual"
// conflict left unresolved: that would make the next pass forget there was
// ever a disagreement, instead of continuing to flag it until it's actually
// resolved.
func (e *Engine) setConverged(hostName, tenantRV, hostRV string) {
	if e.History == nil {
		e.History = ConvergenceHistory{}
	}
	e.History[hostName] = ConvergedVersions{TenantResourceVersion: tenantRV, HostResourceVersion: hostRV}
}

// pushToHost writes the tenant's current state to the host, preserving the
// host-allocated fields only the host's own cluster can assign (clusterIP and
// friends) and the existing object's resourceVersion for an optimistic
// concurrency check.
func (e *Engine) pushToHost(ctx context.Context, ref syncplan.ResourceRef, target syncplan.HostTarget, virtualObj, existingHost, host *unstructured.Unstructured, trackHistory bool) (Decision, error) {
	if d, limited := e.rateLimited(ref, target); limited {
		return d, nil
	}
	host.SetResourceVersion(existingHost.GetResourceVersion())
	syncplan.AdoptHostAllocatedFields(existingHost, host)
	if err := e.HostClient.Update(ctx, host); err != nil {
		return Decision{}, err
	}
	if trackHistory {
		e.setConverged(target.Name, virtualObj.GetResourceVersion(), host.GetResourceVersion())
	}
	return Decision{Action: ActionUpdate, Kind: ref.Kind, Ref: ref, Host: target,
		Reason: "reconciled host object to match virtual source"}, nil
}

// pullToTenant writes the host's current content into the tenant's object,
// preserving the tenant's own metadata (labels, annotations, resourceVersion):
// unlike a host object tenantplane owns outright, the tenant object is
// something the tenant still manages — only its content is reflected back.
func (e *Engine) pullToTenant(ctx context.Context, ref syncplan.ResourceRef, target syncplan.HostTarget, virtualObj, existingHost *unstructured.Unstructured, reason string, trackHistory bool) (Decision, error) {
	if d, limited := e.rateLimited(ref, target); limited {
		return d, nil
	}
	merged := syncplan.MergeHostContentIntoTenant(existingHost, virtualObj)
	if err := e.VirtualClient.Update(ctx, merged); err != nil {
		return Decision{}, err
	}
	if trackHistory {
		e.setConverged(target.Name, merged.GetResourceVersion(), existingHost.GetResourceVersion())
	}
	return Decision{Action: ActionUpdate, Kind: ref.Kind, Ref: ref, Host: target, Reason: reason}, nil
}

// rateLimited reports whether this tenant's apiFairness budget is exhausted
// for right now. When true, the caller must not perform the write; the
// returned Decision explains the deferral so throttling is visible, not
// silent, and the object is simply retried on the next sync pass.
func (e *Engine) rateLimited(ref syncplan.ResourceRef, target syncplan.HostTarget) (Decision, bool) {
	if e.RateLimiter == nil || e.RateLimiter.Allow() {
		return Decision{}, false
	}
	return Decision{Action: ActionSkip, Kind: ref.Kind, Ref: ref, Host: target,
		Reason: "apiFairness rate limit reached for this tenant; retrying next sync pass"}, true
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
		target := syncplan.HostTarget{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if d, limited := e.rateLimited(ref, target); limited {
			decisions = append(decisions, d)
			continue
		}
		if err := e.HostClient.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete orphan %s: %w", obj.GetName(), err))
			continue
		}
		decisions = append(decisions, Decision{
			Action: ActionDelete, Kind: gvk.Kind, Ref: ref, Host: target,
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

// isVirtualInfrastructure reports objects every Kubernetes cluster materializes
// as part of its own API machinery: the apiserver's Service (and Endpoints) in
// "default", and the per-namespace root-CA bundle ConfigMap. They describe the
// tenant control plane itself, not tenant workloads, so — like the system
// namespaces — they are never projected onto the host.
func isVirtualInfrastructure(kind string, obj *unstructured.Unstructured) bool {
	switch kind {
	case "Service", "Endpoints":
		return obj.GetNamespace() == "default" && obj.GetName() == "kubernetes"
	case "ConfigMap":
		return obj.GetName() == "kube-root-ca.crt"
	}
	return false
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
