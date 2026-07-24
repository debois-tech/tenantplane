package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type TenantCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantClusterSpec   `json:"spec,omitempty"`
	Status TenantClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TenantClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TenantCluster `json:"items"`
}

// +kubebuilder:object:root=true
type IsolationProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IsolationProfileSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
type IsolationProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []IsolationProfile `json:"items"`
}

// +kubebuilder:object:root=true
type SyncPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SyncPolicySpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
type SyncPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SyncPolicy `json:"items"`
}

// SyncDecision is the durable, queryable record of a TenantCluster's sync
// decisions — one object per tenant (same name/namespace as the
// TenantCluster, owned by it), holding a bounded, most-recent-first window
// of entries. It exists because Kubernetes Events (the other place a
// decision is surfaced) have cluster-default retention and are not
// individually queryable by tenant/kind/action the way a real resource is.
// Only created when the owning SyncPolicy sets explain.recordDecisions.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type SyncDecision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status SyncDecisionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SyncDecisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SyncDecision `json:"items"`
}

type SyncDecisionStatus struct {
	// Entries holds at most the owning SyncPolicy's explain.retain most
	// recent decisions, oldest first.
	Entries []SyncDecisionEntry `json:"entries,omitempty"`

	// LastConverged tracks, per host object name, the tenant and host
	// resourceVersions the last time a bidirectional pair was confirmed to
	// agree. It lets the sync engine tell "only one side changed since then"
	// from a genuine two-sided conflict, instead of comparing only current
	// tenant vs. current host state. Only populated for bidirectional
	// resources, and only while explain.retain > 0 (see recordSyncDecisions).
	LastConverged map[string]ConvergedVersions `json:"lastConverged,omitempty"`
}

type ConvergedVersions struct {
	TenantResourceVersion string `json:"tenantResourceVersion"`
	HostResourceVersion   string `json:"hostResourceVersion"`
}

type SyncDecisionEntry struct {
	Time            metav1.Time `json:"time"`
	Action          string      `json:"action"`
	Kind            string      `json:"kind"`
	TenantNamespace string      `json:"tenantNamespace"`
	TenantName      string      `json:"tenantName"`
	HostNamespace   string      `json:"hostNamespace"`
	HostName        string      `json:"hostName"`
	Reason          string      `json:"reason,omitempty"`
}

type TenantMode string

const (
	TenantModeShared    TenantMode = "shared"
	TenantModeDedicated TenantMode = "dedicated"
	TenantModePrivate   TenantMode = "private"
)

type LocalObjectReference struct {
	Name string `json:"name"`
}

type TenantClusterSpec struct {
	Mode                TenantMode             `json:"mode"`
	KubernetesVersion   string                 `json:"kubernetesVersion"`
	IsolationProfileRef LocalObjectReference   `json:"isolationProfileRef"`
	SyncPolicyRef       LocalObjectReference   `json:"syncPolicyRef"`
	ControlPlane        ControlPlaneSpec       `json:"controlPlane"`
	Networking          TenantNetworkingSpec   `json:"networking"`
	Migration           TenantMigrationSpec    `json:"migration"`
	Resources           TenantResourceRequests `json:"resources,omitempty"`
}

type ControlPlaneSpec struct {
	Replicas  int           `json:"replicas"`
	Datastore DatastoreSpec `json:"datastore"`
	// Storage configures the control plane's PersistentVolumeClaim. On managed
	// Kubernetes (EKS/AKS/GKE) the class selects the cloud CSI driver; when
	// empty, the cluster's default StorageClass is used.
	Storage StorageSpec `json:"storage,omitempty"`
	// Expose optionally publishes the tenant API server outside the cluster.
	Expose ExposeSpec `json:"expose,omitempty"`
	// ExtraTLSSANs are additional subject alternative names for the tenant API
	// server certificate, e.g. an external DNS name or load-balancer address.
	ExtraTLSSANs []string `json:"extraTLSSANs,omitempty"`
}

type DatastoreSpec struct {
	Type string `json:"type"`
}

type StorageSpec struct {
	ClassName string `json:"className,omitempty"`
	Size      string `json:"size,omitempty"`
}

type ExposeSpec struct {
	// LoadBalancer creates an additional Service of type LoadBalancer in front
	// of the tenant API server. Cloud-specific behavior (internal vs external,
	// NLB vs classic, …) is selected via Annotations.
	LoadBalancer bool `json:"loadBalancer,omitempty"`
	// Annotations are set on the LoadBalancer Service, e.g.
	// service.beta.kubernetes.io/aws-load-balancer-type: external.
	Annotations map[string]string `json:"annotations,omitempty"`
}

type TenantNetworkingSpec struct {
	PodCIDR      string `json:"podCIDR,omitempty"`
	ServiceCIDR  string `json:"serviceCIDR,omitempty"`
	EgressPolicy string `json:"egressPolicy"`
}

type TenantMigrationSpec struct {
	AllowModeChange bool `json:"allowModeChange"`
}

type TenantResourceRequests struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type TenantClusterStatus struct {
	Phase    string `json:"phase,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	// ExternalEndpoint is the load-balancer address of the tenant API server,
	// populated once spec.controlPlane.expose.loadBalancer has provisioned.
	ExternalEndpoint string      `json:"externalEndpoint,omitempty"`
	Conditions       []Condition `json:"conditions,omitempty"`
}

type IsolationProfileSpec struct {
	Level    string            `json:"level"`
	Controls IsolationControls `json:"controls"`
}

type IsolationControls struct {
	PodSecurity               string `json:"podSecurity"`
	DefaultDenyNetworkPolicy  bool   `json:"defaultDenyNetworkPolicy"`
	RequireResourceRequests   bool   `json:"requireResourceRequests"`
	RuntimeClassName          string `json:"runtimeClassName,omitempty"`
	BlockHostPathVolumes      bool   `json:"blockHostPathVolumes"`
	BlockPrivilegedContainers bool   `json:"blockPrivilegedContainers"`
	APIFairness               string `json:"apiFairness"`
}

type SyncPolicySpec struct {
	ConflictPolicy string             `json:"conflictPolicy"`
	DriftDetection DriftDetectionSpec `json:"driftDetection"`
	Explain        ExplainSpec        `json:"explain"`
	Resources      []SyncedResource   `json:"resources"`
}

type DriftDetectionSpec struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
}

type ExplainSpec struct {
	RecordDecisions bool `json:"recordDecisions"`
	Retain          int  `json:"retain"`
}

type SyncedResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Direction  string `json:"direction"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}
