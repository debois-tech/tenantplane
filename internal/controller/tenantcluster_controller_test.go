package controller

import (
	"testing"
	"time"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
)

func TestSyncRequeueIntervalDefaultsToSteady(t *testing.T) {
	for name, policy := range map[string]*v1alpha1.SyncPolicy{
		"nil policy":                 nil,
		"drift disabled":             {Spec: v1alpha1.SyncPolicySpec{DriftDetection: v1alpha1.DriftDetectionSpec{Enabled: false, Interval: "10s"}}},
		"drift enabled, no interval": {Spec: v1alpha1.SyncPolicySpec{DriftDetection: v1alpha1.DriftDetectionSpec{Enabled: true, Interval: ""}}},
		"unparseable interval":       {Spec: v1alpha1.SyncPolicySpec{DriftDetection: v1alpha1.DriftDetectionSpec{Enabled: true, Interval: "not-a-duration"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := syncRequeueInterval(policy); got != requeueSteady {
				t.Fatalf("syncRequeueInterval() = %v, want the default requeueSteady (%v)", got, requeueSteady)
			}
		})
	}
}

func TestSyncRequeueIntervalHonorsDriftDetection(t *testing.T) {
	policy := &v1alpha1.SyncPolicy{Spec: v1alpha1.SyncPolicySpec{
		DriftDetection: v1alpha1.DriftDetectionSpec{Enabled: true, Interval: "5m"},
	}}
	if got, want := syncRequeueInterval(policy), 5*time.Minute; got != want {
		t.Fatalf("syncRequeueInterval() = %v, want %v", got, want)
	}
}

func TestSyncRequeueIntervalFloorsTooSmallValues(t *testing.T) {
	// driftDetection.interval requests a check cadence, not permission to
	// hot-loop the reconciler.
	policy := &v1alpha1.SyncPolicy{Spec: v1alpha1.SyncPolicySpec{
		DriftDetection: v1alpha1.DriftDetectionSpec{Enabled: true, Interval: "1ms"},
	}}
	if got := syncRequeueInterval(policy); got != requeueWaiting {
		t.Fatalf("syncRequeueInterval() = %v, want floored to requeueWaiting (%v)", got, requeueWaiting)
	}
}
