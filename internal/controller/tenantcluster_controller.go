package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tenantplane/tenantplane/internal/api/v1alpha1"
)

const (
	isoProfileIndexKey = ".spec.isolationProfileRef.name"
	syncPolicyIndexKey = ".spec.syncPolicyRef.name"

	requeueWaiting = 5 * time.Second
	requeueSteady  = 60 * time.Second
)

// TenantClusterReconciler reconciles a TenantCluster into a real, running k3s-based
// virtual control plane plus the NetworkPolicy/ResourceQuota/LimitRange/PSA labels its
// referenced IsolationProfile requires.
type TenantClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RESTConfig *rest.Config
	ClientSet  kubernetes.Interface
	Recorder   record.EventRecorder
}

func (r *TenantClusterReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)

	tc := &v1alpha1.TenantCluster{}
	if err := r.Get(ctx, req.NamespacedName, tc); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	isoProfile := &v1alpha1.IsolationProfile{}
	if err := r.Get(ctx, types.NamespacedName{Name: tc.Spec.IsolationProfileRef.Name}, isoProfile); err != nil {
		return r.degrade(ctx, tc, "IsolationProfileNotFound", fmt.Sprintf("isolationProfile %q not found: %v", tc.Spec.IsolationProfileRef.Name, err))
	}

	syncPolicy := &v1alpha1.SyncPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: tc.Spec.SyncPolicyRef.Name}, syncPolicy); err != nil {
		return r.degrade(ctx, tc, "SyncPolicyNotFound", fmt.Sprintf("syncPolicy %q not found: %v", tc.Spec.SyncPolicyRef.Name, err))
	}

	if tc.Spec.Mode != v1alpha1.TenantModeShared {
		setCondition(tc, "ModeSupported", corev1.ConditionFalse, "NotImplemented",
			fmt.Sprintf("mode %q is accepted but only %q is implemented; reconciling as shared", tc.Spec.Mode, v1alpha1.TenantModeShared))
	} else {
		setCondition(tc, "ModeSupported", corev1.ConditionTrue, "Shared", "")
	}

	if tc.Spec.ControlPlane.Datastore.Type != "sqlite" {
		return r.degrade(ctx, tc, "DatastoreNotImplemented",
			fmt.Sprintf("datastore type %q is accepted but only \"sqlite\" is implemented", tc.Spec.ControlPlane.Datastore.Type))
	}

	profile := profileFromCR(isoProfile)
	if err := r.applyIsolation(ctx, tc, profile); err != nil {
		return r.degrade(ctx, tc, "IsolationApplyFailed", err.Error())
	}

	svc := buildHeadlessService(tc)
	if err := r.reconcileService(ctx, tc, svc); err != nil {
		return r.degrade(ctx, tc, "ServiceReconcileFailed", err.Error())
	}

	sts, err := r.reconcileStatefulSet(ctx, tc)
	if err != nil {
		return r.degrade(ctx, tc, "StatefulSetReconcileFailed", err.Error())
	}

	if sts.Status.ReadyReplicas < 1 {
		tc.Status.Phase = PhaseProvisioning
		setCondition(tc, "Ready", corev1.ConditionFalse, "WaitingForControlPlane", "control-plane pod is not ready yet")
		if err := r.Status().Update(ctx, tc); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: requeueWaiting}, nil
	}

	secretName := controlPlaneName(tc) + "-kubeconfig"
	if _, err := r.ensureKubeconfigSecret(ctx, tc, secretName); err != nil {
		logger.Error(err, "failed to extract kubeconfig from control-plane pod")
		return r.degrade(ctx, tc, "KubeconfigExtractFailed", err.Error())
	}

	tc.Status.Phase = PhaseReady
	tc.Status.Endpoint = fmt.Sprintf("https://%s:%d", controlPlaneServiceFQDN(tc), apiPort)
	setCondition(tc, "Ready", corev1.ConditionTrue, "ControlPlaneRunning", "")

	// The control plane is Ready per its StatefulSet, but its API server may need
	// a moment more before it serves. A sync failure here is expected during that
	// window: record it as a condition and requeue soon rather than degrading the
	// otherwise-healthy tenant.
	if err := r.runSync(ctx, tc, syncPolicy, secretName); err != nil {
		setCondition(tc, "Synced", corev1.ConditionFalse, "SyncFailed", err.Error())
		if updateErr := r.Status().Update(ctx, tc); updateErr != nil {
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{RequeueAfter: requeueWaiting}, nil
	}
	setCondition(tc, "Synced", corev1.ConditionTrue, "SyncComplete", "")
	if err := r.Status().Update(ctx, tc); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{RequeueAfter: requeueSteady}, nil
}

func (r *TenantClusterReconciler) degrade(ctx context.Context, tc *v1alpha1.TenantCluster, reason, message string) (reconcile.Result, error) {
	tc.Status.Phase = PhaseDegraded
	setCondition(tc, "Ready", corev1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, tc); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: requeueWaiting}, nil
}

func (r *TenantClusterReconciler) reconcileService(ctx context.Context, tc *v1alpha1.TenantCluster, desired *corev1.Service) error {
	if err := controllerutil.SetControllerReference(tc, desired, r.Scheme); err != nil {
		return err
	}
	existing := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Labels = desired.Labels
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	return r.Update(ctx, existing)
}

// reconcileStatefulSet only mutates the subset of fields that are safe to change after
// creation (replicas, pod template, labels); ServiceName/Selector/VolumeClaimTemplates
// are immutable on StatefulSets once created and are left untouched on update.
func (r *TenantClusterReconciler) reconcileStatefulSet(ctx context.Context, tc *v1alpha1.TenantCluster) (*appsv1.StatefulSet, error) {
	desired := buildStatefulSet(tc)
	if err := controllerutil.SetControllerReference(tc, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}
	if err != nil {
		return nil, err
	}

	existing.Labels = desired.Labels
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template = desired.Spec.Template
	if err := r.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (r *TenantClusterReconciler) ensureKubeconfigSecret(ctx context.Context, tc *v1alpha1.TenantCluster, name string) (*corev1.Secret, error) {
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, existing)
	if err == nil {
		return existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	raw, err := execInPod(ctx, r.RESTConfig, r.ClientSet, tc.Namespace, controlPlanePodName(tc), "k3s",
		[]string{"cat", "/etc/rancher/k3s/k3s.yaml"})
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig from control-plane pod: %w", err)
	}

	kubeconfig := rewriteKubeconfigServer(raw, controlPlaneServiceFQDN(tc))

	secret := &corev1.Secret{
		ObjectMeta: controlPlaneObjectMeta(tc, name),
		Data: map[string][]byte{
			"kubeconfig": []byte(kubeconfig),
		},
	}
	if err := controllerutil.SetControllerReference(tc, secret, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func (r *TenantClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.TenantCluster{}, isoProfileIndexKey, func(o client.Object) []string {
		tc := o.(*v1alpha1.TenantCluster)
		if tc.Spec.IsolationProfileRef.Name == "" {
			return nil
		}
		return []string{tc.Spec.IsolationProfileRef.Name}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.TenantCluster{}, syncPolicyIndexKey, func(o client.Object) []string {
		tc := o.(*v1alpha1.TenantCluster)
		if tc.Spec.SyncPolicyRef.Name == "" {
			return nil
		}
		return []string{tc.Spec.SyncPolicyRef.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TenantCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Watches(&v1alpha1.IsolationProfile{}, handler.EnqueueRequestsFromMapFunc(r.mapForIndex(isoProfileIndexKey))).
		Watches(&v1alpha1.SyncPolicy{}, handler.EnqueueRequestsFromMapFunc(r.mapForIndex(syncPolicyIndexKey))).
		Complete(r)
}

func (r *TenantClusterReconciler) mapForIndex(indexKey string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list v1alpha1.TenantClusterList
		if err := r.List(ctx, &list, client.MatchingFields{indexKey: obj.GetName()}); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(list.Items))
		for _, tc := range list.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}})
		}
		return reqs
	}
}
