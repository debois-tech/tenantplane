package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"golang.org/x/time/rate"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/isolation"
	"github.com/debois-tech/tenantplane/internal/syncer"
)

// apiFairness rate limits: apiFairness is enforced as a cap on how many host
// writes this tenant's sync passes may make per second, protecting the shared
// host API server from one noisy tenant. "tenant-strict" (the sandboxed level)
// gets a tighter budget than "tenant"; an unset value is unlimited.
const (
	fairnessRateTenant        = 20
	fairnessBurstTenant       = 40
	fairnessRateTenantStrict  = 5
	fairnessBurstTenantStrict = 10
)

// rateLimiterForFairness builds the per-tenant sync rate limiter for an
// apiFairness setting, or nil (unlimited) when unset or unrecognized.
func rateLimiterForFairness(apiFairness string) *rate.Limiter {
	switch apiFairness {
	case "tenant":
		return rate.NewLimiter(rate.Limit(fairnessRateTenant), fairnessBurstTenant)
	case "tenant-strict":
		return rate.NewLimiter(rate.Limit(fairnessRateTenantStrict), fairnessBurstTenantStrict)
	default:
		return nil
	}
}

// runSync performs one sync convergence pass for tc using the kubeconfig secret
// the reconciler already extracted from the control-plane pod. It is best-effort
// relative to the tenant's readiness: a control plane that is Ready per its
// StatefulSet may still be seconds away from serving its API, so a failure here
// is surfaced as a condition but does not fail the whole reconcile.
func (r *TenantClusterReconciler) runSync(ctx context.Context, tc *v1alpha1.TenantCluster, policy *v1alpha1.SyncPolicy, profile isolation.Profile, kubeconfigSecret string) error {
	resources := syncResourcesFromPolicy(policy)
	if len(resources) == 0 {
		return nil
	}

	virtualClient, err := r.virtualClientFor(ctx, tc, kubeconfigSecret)
	if err != nil {
		return fmt.Errorf("connect to tenant control plane: %w", err)
	}

	engine := &syncer.Engine{
		Tenant:                   tc.Name,
		HostNamespace:            tc.Namespace,
		VirtualClient:            virtualClient,
		HostClient:               r.Client,
		Recorder:                 &eventDecisionRecorder{recorder: r.Recorder, object: tc},
		RequiredRuntimeClassName: profile.RuntimeClassName,
		RateLimiter:              rateLimiterForFairness(profile.APIFairness),
	}

	logger := log.FromContext(ctx)
	var firstErr error
	for _, res := range resources {
		var decisions []syncer.Decision
		var err error
		switch res.Direction {
		case syncer.DirectionToHost:
			decisions, err = engine.SyncToHost(ctx, res)
		case syncer.DirectionFromHost:
			decisions, err = engine.SyncFromHost(ctx, res)
		case syncer.DirectionBidirectional:
			decisions, err = engine.SyncBidirectional(ctx, res, policy.Spec.ConflictPolicy)
		default:
			// Accepted in the SyncPolicy but not a real direction; skip
			// without pretending it happened.
			continue
		}
		if err != nil {
			logger.Error(err, "sync pass failed", "kind", res.GVK.Kind, "direction", res.Direction)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		logger.V(1).Info("sync pass complete", "kind", res.GVK.Kind, "direction", res.Direction, "decisions", len(decisions))
	}
	return firstErr
}

// virtualClientFor builds a controller-runtime client against the tenant control
// plane from the kubeconfig stored in the named host Secret.
func (r *TenantClusterReconciler) virtualClientFor(ctx context.Context, tc *v1alpha1.TenantCluster, secretName string) (client.Client, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: tc.Namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("kubeconfig secret %q not found yet", secretName)
		}
		return nil, err
	}
	raw, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig secret %q has no \"kubeconfig\" key", secretName)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	return client.New(restConfig, client.Options{Scheme: clientgoscheme.Scheme})
}

// syncResourcesFromPolicy translates the SyncPolicy's declared resources into the
// engine's Resource list. Entries with an unparseable apiVersion are skipped;
// the SyncPolicy schema does not yet validate them, so a bad entry should not
// abort the whole sync.
func syncResourcesFromPolicy(policy *v1alpha1.SyncPolicy) []syncer.Resource {
	if policy == nil {
		return nil
	}
	out := make([]syncer.Resource, 0, len(policy.Spec.Resources))
	for _, res := range policy.Spec.Resources {
		gv, err := schema.ParseGroupVersion(res.APIVersion)
		if err != nil {
			continue
		}
		out = append(out, syncer.Resource{
			GVK:       gv.WithKind(res.Kind),
			Direction: syncer.Direction(res.Direction),
		})
	}
	return out
}
