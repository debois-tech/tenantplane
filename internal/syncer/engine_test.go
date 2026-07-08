package syncer

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/debois-tech/tenantplane/internal/syncplan"
)

var configMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return s
}

func newEngine(t *testing.T, virtual, host client.Client) (*Engine, *captureRecorder) {
	rec := &captureRecorder{}
	return &Engine{
		Tenant:        "dev",
		HostNamespace: "team-dev",
		VirtualClient: virtual,
		HostClient:    host,
		Recorder:      rec,
	}, rec
}

type captureRecorder struct{ decisions []Decision }

func (c *captureRecorder) Record(_ context.Context, d Decision) { c.decisions = append(c.decisions, d) }

func virtualConfigMap(namespace, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Data:       data,
	}
}

func getHostConfigMap(t *testing.T, host client.Client, name string) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := host.Get(context.Background(), client.ObjectKey{Namespace: "team-dev", Name: name}, cm); err != nil {
		t.Fatalf("get host configmap %q: %v", name, err)
	}
	return cm
}

func TestSyncToHostCreatesAndSkipsSystemNamespaces(t *testing.T) {
	scheme := testScheme(t)
	virtual := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		virtualConfigMap("default", "app-config", map[string]string{"key": "value"}),
		virtualConfigMap("kube-system", "coredns", map[string]string{"Corefile": "..."}),
	).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).Build()

	e, rec := newEngine(t, virtual, host)
	decisions, err := e.SyncToHost(context.Background(), Resource{GVK: configMapGVK, Direction: DirectionToHost})
	if err != nil {
		t.Fatalf("SyncToHost() error = %v", err)
	}

	if len(decisions) != 1 || decisions[0].Action != ActionCreate {
		t.Fatalf("expected 1 create decision, got %+v", decisions)
	}
	if len(rec.decisions) != len(decisions) {
		t.Fatalf("recorder saw %d decisions, engine returned %d", len(rec.decisions), len(decisions))
	}

	cm := getHostConfigMap(t, host, "app-config-x-default-x-dev")
	if cm.Data["key"] != "value" {
		t.Fatalf("host data not synced: %v", cm.Data)
	}
	if cm.Labels[syncplan.LabelManagedBy] != syncplan.ManagedByValue {
		t.Fatalf("managed-by label missing: %v", cm.Labels)
	}

	// The kube-system configmap must not have been projected.
	list := &corev1.ConfigMapList{}
	if err := host.List(context.Background(), list); err != nil {
		t.Fatalf("list host: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected exactly 1 host configmap, got %d", len(list.Items))
	}
}

func TestSyncToHostUpdatesExisting(t *testing.T) {
	scheme := testScheme(t)
	virtual := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		virtualConfigMap("default", "app-config", map[string]string{"key": "v2"}),
	).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).Build()

	e, _ := newEngine(t, virtual, host)
	res := Resource{GVK: configMapGVK, Direction: DirectionToHost}

	if _, err := e.SyncToHost(context.Background(), res); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	// Change the virtual source and sync again.
	src := &corev1.ConfigMap{}
	if err := virtual.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "app-config"}, src); err != nil {
		t.Fatalf("get virtual: %v", err)
	}
	src.Data["key"] = "v3"
	if err := virtual.Update(context.Background(), src); err != nil {
		t.Fatalf("update virtual: %v", err)
	}

	decisions, err := e.SyncToHost(context.Background(), res)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Action != ActionUpdate {
		t.Fatalf("expected 1 update decision, got %+v", decisions)
	}
	if cm := getHostConfigMap(t, host, "app-config-x-default-x-dev"); cm.Data["key"] != "v3" {
		t.Fatalf("host not updated: %v", cm.Data)
	}
}

func TestSyncToHostGarbageCollectsOrphans(t *testing.T) {
	scheme := testScheme(t)
	virtual := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		virtualConfigMap("default", "keep", map[string]string{"k": "v"}),
		virtualConfigMap("default", "remove-me", map[string]string{"k": "v"}),
	).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).Build()

	e, _ := newEngine(t, virtual, host)
	res := Resource{GVK: configMapGVK, Direction: DirectionToHost}
	if _, err := e.SyncToHost(context.Background(), res); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Delete one virtual source; next pass must GC its host counterpart.
	orphanSource := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "remove-me"}}
	if err := virtual.Delete(context.Background(), orphanSource); err != nil {
		t.Fatalf("delete virtual: %v", err)
	}

	decisions, err := e.SyncToHost(context.Background(), res)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	var deletes int
	for _, d := range decisions {
		if d.Action == ActionDelete {
			deletes++
			if d.Ref.Name != "remove-me" {
				t.Fatalf("delete decision lost reverse mapping: %+v", d.Ref)
			}
		}
	}
	if deletes != 1 {
		t.Fatalf("expected exactly 1 delete, got %d in %+v", deletes, decisions)
	}

	list := &corev1.ConfigMapList{}
	if err := host.List(context.Background(), list); err != nil {
		t.Fatalf("list host: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "keep-x-default-x-dev" {
		t.Fatalf("expected only the kept object, got %+v", names(list))
	}
}

func TestSyncToHostRejectsUnimplementedDirection(t *testing.T) {
	e, _ := newEngine(t, nil, nil)
	if _, err := e.SyncToHost(context.Background(), Resource{GVK: configMapGVK, Direction: DirectionBidirectional}); err == nil {
		t.Fatal("expected error for unimplemented direction")
	}
}

// GC must not touch host objects that are not tenantplane-managed, even if they
// share the namespace and kind.
func TestSyncToHostLeavesForeignObjectsAlone(t *testing.T) {
	scheme := testScheme(t)
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-dev",
			Name:      "someone-elses",
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "helm"},
		},
	}
	virtual := fake.NewClientBuilder().WithScheme(scheme).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()

	e, _ := newEngine(t, virtual, host)
	if _, err := e.SyncToHost(context.Background(), Resource{GVK: configMapGVK, Direction: DirectionToHost}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := host.Get(context.Background(), client.ObjectKey{Namespace: "team-dev", Name: "someone-elses"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("foreign object should be untouched, got: %v", err)
	}
}

// A non-tenantplane object already sitting at the deterministic host name must
// not be overwritten — the engine skips it with an explainable decision.
func TestSyncToHostSkipsForeignObjectAtTargetName(t *testing.T) {
	scheme := testScheme(t)
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-dev",
			Name:      "app-config-x-default-x-dev",
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "helm"},
		},
		Data: map[string]string{"owner": "helm"},
	}
	virtual := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		virtualConfigMap("default", "app-config", map[string]string{"key": "value"}),
	).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()

	e, _ := newEngine(t, virtual, host)
	decisions, err := e.SyncToHost(context.Background(), Resource{GVK: configMapGVK, Direction: DirectionToHost})
	if err != nil {
		t.Fatalf("SyncToHost() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].Action != ActionSkip {
		t.Fatalf("expected 1 skip decision, got %+v", decisions)
	}
	cm := getHostConfigMap(t, host, "app-config-x-default-x-dev")
	if cm.Data["owner"] != "helm" || cm.Data["key"] == "value" {
		t.Fatalf("foreign object was clobbered: %v", cm.Data)
	}
}

// When the deterministic host name is already held by a tenantplane object that
// maps back to a *different* tenant source (a name collision, e.g. from hash
// truncation), the engine skips rather than silently overwriting it.
func TestSyncToHostSkipsCollisionWithDifferentSource(t *testing.T) {
	scheme := testScheme(t)
	occupant := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-dev",
			Name:      "app-config-x-default-x-dev",
			Labels: map[string]string{
				syncplan.LabelManagedBy:        syncplan.ManagedByValue,
				syncplan.LabelTenant:           "dev",
				syncplan.LabelVirtualNamespace: "default",
				syncplan.LabelKind:             "configmap",
			},
			Annotations: map[string]string{
				syncplan.AnnotationVirtualNamespace: "default",
				syncplan.AnnotationVirtualName:      "some-other-name",
			},
		},
		Data: map[string]string{"from": "other-source"},
	}
	virtual := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		virtualConfigMap("default", "app-config", map[string]string{"key": "value"}),
	).Build()
	host := fake.NewClientBuilder().WithScheme(scheme).WithObjects(occupant).Build()

	e, _ := newEngine(t, virtual, host)
	decisions, err := e.SyncToHost(context.Background(), Resource{GVK: configMapGVK, Direction: DirectionToHost})
	if err != nil {
		t.Fatalf("SyncToHost() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].Action != ActionSkip {
		t.Fatalf("expected 1 skip decision, got %+v", decisions)
	}
	if cm := getHostConfigMap(t, host, "app-config-x-default-x-dev"); cm.Data["from"] != "other-source" {
		t.Fatalf("collided object was clobbered: %v", cm.Data)
	}
}

func names(list *corev1.ConfigMapList) []string {
	var out []string
	for i := range list.Items {
		out = append(out, list.Items[i].Name)
	}
	return out
}
