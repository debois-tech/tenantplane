package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
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

	// Deletion: the control-plane namespace cannot be owner-referenced by the
	// (namespaced) TenantCluster, so a finalizer removes it explicitly.
	if !tc.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(tc, teardownFinalizer) {
			return reconcile.Result{}, nil
		}
		done, err := r.teardown(ctx, tc)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !done {
			return reconcile.Result{RequeueAfter: requeueWaiting}, nil
		}
		controllerutil.RemoveFinalizer(tc, teardownFinalizer)
		return reconcile.Result{}, r.Update(ctx, tc)
	}
	if !controllerutil.ContainsFinalizer(tc, teardownFinalizer) {
		controllerutil.AddFinalizer(tc, teardownFinalizer)
		if err := r.Update(ctx, tc); err != nil {
			return reconcile.Result{}, err
		}
	}

	isoProfile := &v1alpha1.IsolationProfile{}
	if err := r.Get(ctx, types.NamespacedName{Name: tc.Spec.IsolationProfileRef.Name}, isoProfile); err != nil {
		return r.degrade(ctx, tc, "IsolationProfileNotFound", fmt.Sprintf("isolationProfile %q not found: %v", tc.Spec.IsolationProfileRef.Name, err))
	}

	syncPolicy := &v1alpha1.SyncPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: tc.Spec.SyncPolicyRef.Name}, syncPolicy); err != nil {
		return r.degrade(ctx, tc, "SyncPolicyNotFound", fmt.Sprintf("syncPolicy %q not found: %v", tc.Spec.SyncPolicyRef.Name, err))
	}

	setSupportConditions(tc, isoProfile, syncPolicy)

	if tc.Spec.ControlPlane.Datastore.Type != "sqlite" {
		return r.degrade(ctx, tc, "DatastoreNotImplemented",
			fmt.Sprintf("datastore type %q is accepted but only \"sqlite\" is implemented", tc.Spec.ControlPlane.Datastore.Type))
	}

	profile := profileFromCR(isoProfile)
	if err := r.applyIsolation(ctx, tc, profile); err != nil {
		return r.degrade(ctx, tc, "IsolationApplyFailed", err.Error())
	}

	// Admission-layer backstop for runtimeClassName: defense-in-depth alongside
	// the sync engine's own injection (see runSync), which is what actually
	// enforces it — this only hardens against a pod reaching the host some
	// other way. Absent on clusters without ValidatingAdmissionPolicy v1
	// (pre-1.30); that gap is reported honestly rather than failing the tenant.
	admissionSupported, err := r.ensureRuntimeClassPolicy(ctx)
	if err != nil {
		return r.degrade(ctx, tc, "AdmissionPolicyFailed", err.Error())
	}
	if admissionSupported {
		if err := r.reconcileRuntimeClassBinding(ctx, tc); err != nil {
			return r.degrade(ctx, tc, "AdmissionBindingFailed", err.Error())
		}
	}
	setAdmissionHardeningCondition(tc, admissionSupported)

	// Admission-layer backstop narrowing the controller's own (unavoidably
	// cluster-wide) RBAC grant to only the namespaces it manages. Global and
	// not tenant-specific, but ensured here — cheaply idempotent — since
	// every reconcile already has a live client at hand.
	scopeSupported, err := r.ensureControllerScopePolicies(ctx)
	if err != nil {
		return r.degrade(ctx, tc, "ControllerScopePolicyFailed", err.Error())
	}
	setControllerScopeCondition(tc, scopeSupported)

	if err := r.ensureControlPlaneNamespace(ctx, tc); err != nil {
		return r.degrade(ctx, tc, "ControlPlaneNamespaceFailed", err.Error())
	}
	if err := r.cleanupLegacyControlPlane(ctx, tc); err != nil {
		return r.degrade(ctx, tc, "LegacyCleanupFailed", err.Error())
	}

	svc := buildHeadlessService(tc)
	if err := r.reconcileService(ctx, tc, svc); err != nil {
		return r.degrade(ctx, tc, "ServiceReconcileFailed", err.Error())
	}

	if err := r.reconcileExternalService(ctx, tc); err != nil {
		return r.degrade(ctx, tc, "ExternalServiceReconcileFailed", err.Error())
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
	externalEndpoint, err := r.externalEndpoint(ctx, tc)
	if err != nil {
		return reconcile.Result{}, err
	}
	tc.Status.ExternalEndpoint = externalEndpoint
	setCondition(tc, "Ready", corev1.ConditionTrue, "ControlPlaneRunning", "")

	// The control plane is Ready per its StatefulSet, but its API server may need
	// a moment more before it serves. A sync failure here is expected during that
	// window: record it as a condition and requeue soon rather than degrading the
	// otherwise-healthy tenant.
	if err := r.runSync(ctx, tc, syncPolicy, profile, secretName); err != nil {
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

// reconcileService converges the headless Service in the control-plane
// namespace. Control-plane objects carry identifying labels instead of owner
// references (cross-namespace owner references are invalid); their lifecycle
// ends with the control-plane namespace at teardown.
func (r *TenantClusterReconciler) reconcileService(ctx context.Context, tc *v1alpha1.TenantCluster, desired *corev1.Service) error {
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

// reconcileExternalService creates, updates, or removes the optional
// LoadBalancer Service publishing the tenant API server, tracking
// spec.controlPlane.expose across all cloud providers.
func (r *TenantClusterReconciler) reconcileExternalService(ctx context.Context, tc *v1alpha1.TenantCluster) error {
	desired := buildExternalService(tc)

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: externalServiceName(tc), Namespace: controlPlaneNamespace(tc)}, existing)

	if desired == nil {
		// Exposure disabled: remove a previously created Service if present.
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.Delete(ctx, existing)
	}

	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec.Type = desired.Spec.Type
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	return r.Update(ctx, existing)
}

// externalEndpoint returns the https address of the LoadBalancer once the cloud
// provider has provisioned it, or "" while provisioning or when exposure is off.
func (r *TenantClusterReconciler) externalEndpoint(ctx context.Context, tc *v1alpha1.TenantCluster) (string, error) {
	if !tc.Spec.ControlPlane.Expose.LoadBalancer {
		return "", nil
	}
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: externalServiceName(tc), Namespace: controlPlaneNamespace(tc)}, svc); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		host := ing.Hostname
		if host == "" {
			host = ing.IP
		}
		if host != "" {
			return fmt.Sprintf("https://%s:%d", host, apiPort), nil
		}
	}
	return "", nil
}

// reconcileStatefulSet only mutates the subset of fields that are safe to change after
// creation (replicas, pod template, labels); ServiceName/Selector/VolumeClaimTemplates
// are immutable on StatefulSets once created and are left untouched on update.
func (r *TenantClusterReconciler) reconcileStatefulSet(ctx context.Context, tc *v1alpha1.TenantCluster) (*appsv1.StatefulSet, error) {
	desired, err := buildStatefulSet(tc)
	if err != nil {
		return nil, err
	}
	// No owner reference: the StatefulSet lives in the control-plane namespace,
	// and cross-namespace owner references are invalid. Teardown removes the
	// whole namespace instead.

	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
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

// ensureKubeconfigSecret keeps the tenant kubeconfig in the tenant's own
// (workload) namespace — that is where users look for it, and an owner
// reference is valid there. The kubeconfig itself is read from the k3s pod in
// the control-plane namespace; if an existing Secret points at a stale server
// address (e.g. from before control planes moved to their own namespace), it
// is regenerated in place.
//
// This Secret holds a real k3s admin kubeconfig — full cluster-admin of the
// tenant's own virtual cluster, which is the intended access level (a tenant
// is meant to administer its own control plane). It carries no expiry and is
// never rotated. Access control is entirely delegated to whatever RBAC the
// host cluster operator has configured for reading Secrets in this namespace;
// tenantplane does not add a second authorization layer on top of that.
func (r *TenantClusterReconciler) ensureKubeconfigSecret(ctx context.Context, tc *v1alpha1.TenantCluster, name string) (*corev1.Secret, error) {
	fqdn := controlPlaneServiceFQDN(tc)

	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, existing)
	if err == nil && strings.Contains(string(existing.Data["kubeconfig"]), fqdn) {
		return existing, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	raw, execErr := execInPod(ctx, r.RESTConfig, r.ClientSet, controlPlaneNamespace(tc), controlPlanePodName(tc), "k3s",
		[]string{"cat", "/etc/rancher/k3s/k3s.yaml"})
	if execErr != nil {
		return nil, fmt.Errorf("read kubeconfig from control-plane pod: %w", execErr)
	}

	kubeconfig := rewriteKubeconfigServer(raw, fqdn)

	if err == nil {
		// Stale server address: refresh the existing Secret in place.
		existing.Data = map[string][]byte{"kubeconfig": []byte(kubeconfig)}
		if err := r.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tc.Namespace,
			Labels:    controlPlaneLabels(tc),
		},
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

// cleanupLegacyControlPlane removes control-plane objects created by earlier
// versions of tenantplane directly in the workload namespace, now that control
// planes live in their own namespace. Only objects carrying tenantplane's
// control-plane labels for this tenant are touched.
func (r *TenantClusterReconciler) cleanupLegacyControlPlane(ctx context.Context, tc *v1alpha1.TenantCluster) error {
	if tc.Namespace == controlPlaneNamespace(tc) {
		return nil
	}

	isLegacy := func(labels map[string]string) bool {
		return labels[labelManagedBy] == "tenantplane" &&
			labels["app.kubernetes.io/name"] == "tenantplane-control-plane" &&
			labels[labelTenant] == tc.Name
	}

	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: controlPlaneName(tc), Namespace: tc.Namespace}, sts); err == nil {
		if isLegacy(sts.Labels) {
			if err := r.Delete(ctx, sts); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	for _, name := range []string{controlPlaneName(tc), externalServiceName(tc)} {
		svc := &corev1.Service{}
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, svc); err == nil {
			if isLegacy(svc.Labels) {
				if err := r.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}

	// The StatefulSet's volumeClaimTemplate PVC survives StatefulSet deletion.
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: "data-" + controlPlanePodName(tc), Namespace: tc.Namespace}, pvc); err == nil {
		if err := r.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
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
		Owns(&corev1.Secret{}).
		// Control-plane StatefulSets/Services live in a separate namespace and
		// cannot be owner-referenced by the TenantCluster; map them back to
		// their tenant through the identifying labels instead.
		Watches(&appsv1.StatefulSet{}, handler.EnqueueRequestsFromMapFunc(mapControlPlaneObject)).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(mapControlPlaneObject)).
		Watches(&v1alpha1.IsolationProfile{}, handler.EnqueueRequestsFromMapFunc(r.mapForIndex(isoProfileIndexKey))).
		Watches(&v1alpha1.SyncPolicy{}, handler.EnqueueRequestsFromMapFunc(r.mapForIndex(syncPolicyIndexKey))).
		Complete(r)
}

// mapControlPlaneObject resolves a control-plane object back to its owning
// TenantCluster through the tenant / tenant-namespace labels.
func mapControlPlaneObject(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels[labelManagedBy] != "tenantplane" {
		return nil
	}
	tenant := labels[labelTenant]
	tenantNamespace := labels[labelTenantNamespace]
	if tenant == "" || tenantNamespace == "" {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: tenant, Namespace: tenantNamespace}},
	}
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
