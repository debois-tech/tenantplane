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
}

type DatastoreSpec struct {
	Type string `json:"type"`
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
	Phase      string      `json:"phase,omitempty"`
	Endpoint   string      `json:"endpoint,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
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
	APIFairness              string `json:"apiFairness"`
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

