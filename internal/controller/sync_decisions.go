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

// recordSyncDecisions appends decisions to this tenant's SyncDecision object —
// one object per tenant, same name/namespace as the TenantCluster (a valid
// owner reference here, unlike the control-plane namespace: both live in the
// same namespace) — trimming to at most retain entries, oldest evicted first.
// A single Get-then-Update per sync pass, not per decision: Record() on the
// engine's DecisionRecorder is unconditional Events already have a good spot
// for immediate, per-object feedback (see eventDecisionRecorder); the durable
// store only needs to be current as of the end of a pass.
func (r *TenantClusterReconciler) recordSyncDecisions(ctx context.Context, tc *v1alpha1.TenantCluster, decisions []syncer.Decision, retain int) error {
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

	return r.Status().Update(ctx, sd)
}
