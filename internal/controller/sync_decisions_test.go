package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/syncer"
	"github.com/debois-tech/tenantplane/internal/syncplan"
)

func decisionScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return s
}

func testDecision(name, reason string) syncer.Decision {
	return syncer.Decision{
		Action: syncer.ActionUpdate,
		Kind:   "ConfigMap",
		Ref:    syncplan.ResourceRef{VirtualNamespace: "default", Name: name},
		Host:   syncplan.HostTarget{Namespace: "team-dev", Name: name + "-x-default-x-dev"},
		Reason: reason,
	}
}

func TestRecordSyncDecisionsCreatesObjectOnFirstCall(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SyncDecision{}).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}

	if err := r.recordSyncDecisions(context.Background(), tc, []syncer.Decision{testDecision("a", "first")}, 10, nil); err != nil {
		t.Fatalf("recordSyncDecisions: %v", err)
	}

	sd := &v1alpha1.SyncDecision{}
	if err := fc.Get(context.Background(), types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd); err != nil {
		t.Fatalf("get SyncDecision: %v", err)
	}
	if len(sd.Status.Entries) != 1 || sd.Status.Entries[0].Reason != "first" {
		t.Fatalf("entries = %+v, want one entry with reason 'first'", sd.Status.Entries)
	}
	if len(sd.OwnerReferences) != 1 || sd.OwnerReferences[0].Name != tc.Name {
		t.Fatalf("owner references = %+v, want an owner reference to the TenantCluster", sd.OwnerReferences)
	}
}

func TestRecordSyncDecisionsAppendsAcrossCalls(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SyncDecision{}).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}
	ctx := context.Background()

	if err := r.recordSyncDecisions(ctx, tc, []syncer.Decision{testDecision("a", "first")}, 10, nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := r.recordSyncDecisions(ctx, tc, []syncer.Decision{testDecision("b", "second")}, 10, nil); err != nil {
		t.Fatalf("second call: %v", err)
	}

	sd := &v1alpha1.SyncDecision{}
	if err := fc.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd); err != nil {
		t.Fatalf("get SyncDecision: %v", err)
	}
	if len(sd.Status.Entries) != 2 {
		t.Fatalf("entries = %+v, want 2 (appended across calls, not overwritten)", sd.Status.Entries)
	}
}

func TestRecordSyncDecisionsTrimsToRetain(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SyncDecision{}).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		if err := r.recordSyncDecisions(ctx, tc, []syncer.Decision{testDecision(name, name)}, 3, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	sd := &v1alpha1.SyncDecision{}
	if err := fc.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd); err != nil {
		t.Fatalf("get SyncDecision: %v", err)
	}
	if len(sd.Status.Entries) != 3 {
		t.Fatalf("entries = %d, want exactly retain (3)", len(sd.Status.Entries))
	}
	// Oldest ("a", "b") must have been evicted; the 3 most recent survive.
	var reasons []string
	for _, e := range sd.Status.Entries {
		reasons = append(reasons, e.Reason)
	}
	want := []string{"c", "d", "e"}
	for i, w := range want {
		if reasons[i] != w {
			t.Fatalf("entries = %v, want %v (oldest evicted first)", reasons, want)
		}
	}
}

func TestRecordSyncDecisionsNoOpsWhenRetainIsZero(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SyncDecision{}).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}

	if err := r.recordSyncDecisions(context.Background(), tc, []syncer.Decision{testDecision("a", "first")}, 0, nil); err != nil {
		t.Fatalf("recordSyncDecisions: %v", err)
	}

	sd := &v1alpha1.SyncDecision{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, sd)
	if err == nil {
		t.Fatal("retain=0 must not create a SyncDecision object at all")
	}
}

func TestLoadConvergenceHistoryEmptyWhenNoSyncDecisionExists(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}

	history, err := r.loadConvergenceHistory(context.Background(), tc)
	if err != nil {
		t.Fatalf("loadConvergenceHistory: %v", err)
	}
	if history == nil || len(history) != 0 {
		t.Fatalf("history = %v, want a non-nil empty map", history)
	}
}

func TestRecordSyncDecisionsPersistsAndLoadConvergenceHistoryRoundTrips(t *testing.T) {
	scheme := decisionScheme(t)
	tc := cloudTenant()
	fc := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.SyncDecision{}).WithObjects(tc).Build()
	r := &TenantClusterReconciler{Client: fc, Scheme: scheme}
	ctx := context.Background()

	history := syncer.ConvergenceHistory{
		"shared-x-default-x-dev": {TenantResourceVersion: "10", HostResourceVersion: "20"},
	}
	if err := r.recordSyncDecisions(ctx, tc, []syncer.Decision{testDecision("shared", "converged")}, 10, history); err != nil {
		t.Fatalf("recordSyncDecisions: %v", err)
	}

	loaded, err := r.loadConvergenceHistory(ctx, tc)
	if err != nil {
		t.Fatalf("loadConvergenceHistory: %v", err)
	}
	got, ok := loaded["shared-x-default-x-dev"]
	if !ok {
		t.Fatalf("loaded history = %v, missing the persisted entry", loaded)
	}
	if got.TenantResourceVersion != "10" || got.HostResourceVersion != "20" {
		t.Fatalf("loaded entry = %+v, want {10 20}", got)
	}
}
