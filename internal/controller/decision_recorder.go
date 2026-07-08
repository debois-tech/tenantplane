package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	"github.com/debois-tech/tenantplane/internal/syncer"
)

// eventDecisionRecorder surfaces each sync decision as a Kubernetes Event on the
// owning TenantCluster, so `kubectl describe tenantcluster` answers "why does
// this host object exist?" without a separate CRD. Deletes are recorded as
// Warning events because they remove host state; everything else is Normal.
type eventDecisionRecorder struct {
	recorder record.EventRecorder
	object   runtime.Object
}

func (e *eventDecisionRecorder) Record(_ context.Context, d syncer.Decision) {
	if e.recorder == nil {
		return
	}
	eventType := corev1.EventTypeNormal
	if d.Action == syncer.ActionDelete {
		eventType = corev1.EventTypeWarning
	}
	e.recorder.Eventf(e.object, eventType, "Sync"+string(d.Action),
		"%s %s/%s -> host %s/%s: %s",
		d.Kind, d.Ref.VirtualNamespace, d.Ref.Name, d.Host.Namespace, d.Host.Name, d.Reason)
}
