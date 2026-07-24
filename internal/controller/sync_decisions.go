package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/syncer"
)

// loadConvergenceHistory reads this tenant's persisted bidirectional
// convergence history (see syncer.ConvergenceHistory) so a fresh Engine can
// tell a one-sided drift from a genuine two-sided conflict on its very first
// pass, not just ones after it. An empty (not nil) history is returned when
// no SyncDecision object exists yet — the engine treats an empty history the
// same as a nil one (no baseline for anything), but starting from a non-nil
// map means the same map can always be handed straight to recordSyncDecisions
// afterward, mutated in place by the engine or not.
func (r *TenantClusterReconciler) loadConvergenceHistory(ctx context.Context, tc *v1alpha1.TenantCluster) (syncer.ConvergenceHistory, error) {
	sd := &v1alpha1.SyncDecision{}
	err := r.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd)
	if apierrors.IsNotFound(err) {
		return syncer.ConvergenceHistory{}, nil
	}
	if err != nil {
		return nil, err
	}
	history := make(syncer.ConvergenceHistory, len(sd.Status.LastConverged))
	for k, v := range sd.Status.LastConverged {
		history[k] = syncer.ConvergedVersions{TenantResourceVersion: v.TenantResourceVersion, HostResourceVersion: v.HostResourceVersion}
	}
	return history, nil
}

// recordSyncDecisions appends decisions and persists the (possibly mutated by
// this pass) convergence history to this tenant's SyncDecision object — one
// object per tenant, same name/namespace as the TenantCluster (a valid owner
// reference here, unlike the control-plane namespace: both live in the same
// namespace). Entries are trimmed to at most retain, oldest evicted first;
// history has no such cap; it naturally only grows with the number of
// distinct bidirectional objects, not with time. A single Get-then-Update per
// sync pass, not per decision: Record() on the engine's DecisionRecorder is
// unconditional Events already have a good spot for immediate, per-object
// feedback (see eventDecisionRecorder); the durable store only needs to be
// current as of the end of a pass.
func (r *TenantClusterReconciler) recordSyncDecisions(ctx context.Context, tc *v1alpha1.TenantCluster, decisions []syncer.Decision, retain int, history syncer.ConvergenceHistory) error {
	if len(decisions) == 0 || retain <= 0 {
		return nil
	}

	sd := &v1alpha1.SyncDecision{}
	err := r.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd)
	if apierrors.IsNotFound(err) {
		sd = &v1alpha1.SyncDecision{ObjectMeta: metav1.ObjectMeta{Name: tc.Name, Namespace: tc.Namespace}}
		if err := controllerutil.SetControllerReference(tc, sd, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, sd); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	now := metav1.Now()
	for _, d := range decisions {
		sd.Status.Entries = append(sd.Status.Entries, v1alpha1.SyncDecisionEntry{
			Time:            now,
			Action:          string(d.Action),
			Kind:            d.Kind,
			TenantNamespace: d.Ref.VirtualNamespace,
			TenantName:      d.Ref.Name,
			HostNamespace:   d.Host.Namespace,
			HostName:        d.Host.Name,
			Reason:          d.Reason,
		})
	}
	if len(sd.Status.Entries) > retain {
		sd.Status.Entries = sd.Status.Entries[len(sd.Status.Entries)-retain:]
	}

	if len(history) > 0 {
		sd.Status.LastConverged = make(map[string]v1alpha1.ConvergedVersions, len(history))
		for k, v := range history {
			sd.Status.LastConverged[k] = v1alpha1.ConvergedVersions{TenantResourceVersion: v.TenantResourceVersion, HostResourceVersion: v.HostResourceVersion}
		}
	}

	return r.Status().Update(ctx, sd)
}
