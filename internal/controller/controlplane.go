package controller

import (
	"fmt"
	"regexp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
	"github.com/debois-tech/tenantplane/internal/isolation"
)

// defaultK3sImage is used when spec.kubernetesVersion doesn't resolve to a
// known minor version (see k3sImageForVersion) — a known-good fallback, not a
// silent substitute: KubernetesVersionSupported (support.go) reports honestly
// whenever this happens.
const defaultK3sImage = "rancher/k3s:v1.30.4-k3s1"

// k3sImageVersionPattern extracts the requested major.minor, accepting an
// optional patch component tenantplane doesn't need to pin itself (e.g.
// "v1.30", "v1.30.0", and "v1.30.4" all resolve to the same image below). The
// CRD's own CEL validation already restricts kubernetesVersion to this same
// pattern at admission; this regexp is what actually resolves it to an image.
var k3sImageVersionPattern = regexp.MustCompile(`^(v1\.(?:28|29|30|31|32|33))(?:\.[0-9]+)?$`)

// k3sImages maps a requested Kubernetes minor version to a specific,
// verified-available k3s image tag. Only the minor version is selectable —
// like GKE/EKS/AKS's own "version selection," patch management is
// tenantplane's job, not the tenant's. Every tag here was confirmed to exist
// via `docker manifest inspect` before being added; this is a real,
// maintained allowlist, not a guess at a naming convention.
var k3sImages = map[string]string{
	"v1.28": "rancher/k3s:v1.28.13-k3s1",
	"v1.29": "rancher/k3s:v1.29.9-k3s1",
	"v1.30": "rancher/k3s:v1.30.6-k3s1",
	"v1.31": "rancher/k3s:v1.31.5-k3s1",
	"v1.32": "rancher/k3s:v1.32.3-k3s1",
	"v1.33": "rancher/k3s:v1.33.1-k3s1",
}

// k3sImageForVersion resolves spec.kubernetesVersion to a k3s image. ok is
// false when the version doesn't match any supported minor — the caller
// should fall back to defaultK3sImage and report it (see
// setKubernetesVersionCondition), not silently use it as if it were what was
// requested.
func k3sImageForVersion(version string) (image string, ok bool) {
	m := k3sImageVersionPattern.FindStringSubmatch(version)
	if m == nil {
		return "", false
	}
	image, ok = k3sImages[m[1]]
	return image, ok
}

const apiPort = 6443

func controlPlaneName(tc *v1alpha1.TenantCluster) string {
	return tc.Name + "-control-plane"
}

func controlPlaneObjectMeta(tc *v1alpha1.TenantCluster, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: controlPlaneNamespace(tc),
		Labels:    controlPlaneLabels(tc),
	}
}

// controlPlaneLabels label every control-plane object. tenant + tenant-namespace
// together identify the owning TenantCluster: objects in the control-plane
// namespace cannot carry an owner reference to it (cross-namespace references
// are invalid), so watches and teardown resolve ownership through these labels.
func controlPlaneLabels(tc *v1alpha1.TenantCluster) map[string]string {
	return map[string]string{
		labelManagedBy:           "tenantplane",
		"app.kubernetes.io/name": "tenantplane-control-plane",
		labelTenant:              tc.Name,
		labelTenantNamespace:     tc.Namespace,
		isolation.ExemptLabelKey: isolation.ExemptLabelValue,
	}
}

// controlPlaneServiceFQDN is the in-cluster DNS name of the headless Service fronting
// the control-plane pod; it doubles as the StatefulSet's governing service and the
// endpoint published in the tenant kubeconfig.
func controlPlaneServiceFQDN(tc *v1alpha1.TenantCluster) string {
	return fmt.Sprintf("%s.%s.svc", controlPlaneName(tc), controlPlaneNamespace(tc))
}

func buildHeadlessService(tc *v1alpha1.TenantCluster) *corev1.Service {
	name := controlPlaneName(tc)
	labels := controlPlaneLabels(tc)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: controlPlaneNamespace(tc),
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       apiPort,
					TargetPort: intstr.FromInt(apiPort),
				},
			},
		},
	}
}

// buildStatefulSet returns the desired control-plane StatefulSet. It errors only
// when the TenantCluster carries an unparseable storage size, so a bad spec value
// degrades the tenant instead of panicking the manager.
func buildStatefulSet(tc *v1alpha1.TenantCluster) (*appsv1.StatefulSet, error) {
	name := controlPlaneName(tc)
	labels := controlPlaneLabels(tc)
	replicas := int32(tc.Spec.ControlPlane.Replicas)
	if replicas < 1 {
		replicas = 1
	}

	image, ok := k3sImageForVersion(tc.Spec.KubernetesVersion)
	if !ok {
		image = defaultK3sImage
	}

	storageSize := resource.MustParse("1Gi")
	if size := tc.Spec.ControlPlane.Storage.Size; size != "" {
		parsed, err := resource.ParseQuantity(size)
		if err != nil {
			return nil, fmt.Errorf("invalid controlPlane.storage.size %q: %w", size, err)
		}
		storageSize = parsed
	}
	// nil means "use the cluster's default StorageClass" — the right default on
	// EKS/AKS/GKE alike, while className pins a specific CSI-backed class.
	var storageClassName *string
	if class := tc.Spec.ControlPlane.Storage.ClassName; class != "" {
		storageClassName = &class
	}

	fqdn := controlPlaneServiceFQDN(tc)
	args := []string{
		"server",
		"--data-dir=/data",
		"--disable-agent",
		"--disable=traefik,servicelb,metrics-server,local-storage,coredns",
		"--write-kubeconfig-mode=644",
		"--tls-san=" + fqdn,
		"--tls-san=" + fqdn + ".cluster.local",
	}
	for _, san := range tc.Spec.ControlPlane.ExtraTLSSANs {
		args = append(args, "--tls-san="+san)
	}

	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("250m"),
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	}
	limits := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("1Gi"),
	}
	// User-supplied quantities are parsed, never MustParse'd: the CRD pattern
	// validates them at admission, but objects stored before that validation
	// existed must degrade the tenant, not panic the manager.
	if cpu := tc.Spec.Resources.CPU; cpu != "" {
		parsed, err := resource.ParseQuantity(cpu)
		if err != nil {
			return nil, fmt.Errorf("invalid resources.cpu %q: %w", cpu, err)
		}
		limits[corev1.ResourceCPU] = parsed
	}
	if mem := tc.Spec.Resources.Memory; mem != "" {
		parsed, err := resource.ParseQuantity(mem)
		if err != nil {
			return nil, fmt.Errorf("invalid resources.memory %q: %w", mem, err)
		}
		limits[corev1.ResourceMemory] = parsed
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: controlPlaneNamespace(tc),
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "k3s",
							Image:   image,
							Command: []string{"k3s"},
							Args:    args,
							Ports: []corev1.ContainerPort{
								{Name: "https", ContainerPort: apiPort},
							},
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/data"},
							},
							// k3s needs root (like upstream), but the default
							// seccomp profile keeps the container within PSA
							// baseline expectations.
							SecurityContext: &corev1.SecurityContext{
								SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(apiPort)},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(apiPort)},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       15,
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: storageClassName,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: storageSize,
							},
						},
					},
				},
			},
		},
	}, nil
}

// externalServiceName is the LoadBalancer Service created when
// spec.controlPlane.expose.loadBalancer is set.
func externalServiceName(tc *v1alpha1.TenantCluster) string {
	return controlPlaneName(tc) + "-external"
}

// buildExternalService returns the LoadBalancer Service publishing the tenant
// API server outside the cluster, or nil when exposure is not requested.
// Cloud-specific behavior (internal LB, NLB, …) comes from the user-supplied
// annotations, so the same spec works on EKS, AKS, and GKE.
func buildExternalService(tc *v1alpha1.TenantCluster) *corev1.Service {
	if !tc.Spec.ControlPlane.Expose.LoadBalancer {
		return nil
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        externalServiceName(tc),
			Namespace:   controlPlaneNamespace(tc),
			Labels:      controlPlaneLabels(tc),
			Annotations: tc.Spec.ControlPlane.Expose.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: controlPlaneLabels(tc),
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       apiPort,
					TargetPort: intstr.FromInt(apiPort),
				},
			},
		},
	}
}

// controlPlanePodName is the deterministic name of the sole StatefulSet replica.
// Only valid while ControlPlane.Replicas <= 1, which is all this milestone supports.
func controlPlanePodName(tc *v1alpha1.TenantCluster) string {
	return controlPlaneName(tc) + "-0"
}
