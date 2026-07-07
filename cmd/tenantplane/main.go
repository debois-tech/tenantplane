package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tenantplane/tenantplane/internal/isolation"
	"github.com/tenantplane/tenantplane/internal/syncplan"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tenantplane:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "tenantplane %s\n", version)
		return nil
	case "render":
		return render(args[1:], stdout, stderr)
	case "explain-sync":
		return explainSync(args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func render(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("render requires one of: tenantcluster, isolationprofile, syncpolicy")
	}

	switch args[0] {
	case "tenantcluster":
		return renderTenantCluster(args[1:], stdout)
	case "isolationprofile":
		return renderIsolationProfile(args[1:], stdout)
	case "syncpolicy":
		return renderSyncPolicy(args[1:], stdout)
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderTenantCluster(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render tenantcluster", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	namespace := fs.String("namespace", "tenantplane-system", "host namespace for the tenant control plane")
	mode := fs.String("mode", "shared", "tenant mode: shared, dedicated, or private")
	isolationProfile := fs.String("isolation-profile", "restricted", "IsolationProfile name")
	syncPolicy := fs.String("sync-policy", "default", "SyncPolicy name")
	kubernetesVersion := fs.String("kubernetes-version", "v1.35.0", "tenant Kubernetes version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("render tenantcluster requires exactly one name")
	}

	name := fs.Arg(0)
	if err := requireDNSName(name, "tenant cluster name"); err != nil {
		return err
	}
	if err := requireOneOf(*mode, "mode", "shared", "dedicated", "private"); err != nil {
		return err
	}

	fmt.Fprintf(stdout, `apiVersion: tenantplane.io/v1alpha1
kind: TenantCluster
metadata:
  name: %s
  namespace: %s
spec:
  mode: %s
  kubernetesVersion: %s
  isolationProfileRef:
    name: %s
  syncPolicyRef:
    name: %s
  controlPlane:
    replicas: 1
    datastore:
      type: sqlite
  networking:
    egressPolicy: deny-by-default
  migration:
    allowModeChange: true
`, name, *namespace, *mode, *kubernetesVersion, *isolationProfile, *syncPolicy)
	return nil
}

func renderIsolationProfile(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render isolationprofile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	level := fs.String("level", "restricted", "profile level: baseline, restricted, or sandboxed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("render isolationprofile requires exactly one name")
	}
	name := fs.Arg(0)
	if err := requireDNSName(name, "isolation profile name"); err != nil {
		return err
	}

	profile, err := isolation.ProfileForLevel(*level)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, `apiVersion: tenantplane.io/v1alpha1
kind: IsolationProfile
metadata:
  name: %s
spec:
  level: %s
  controls:
    podSecurity: %s
    defaultDenyNetworkPolicy: %t
    requireResourceRequests: %t
    runtimeClassName: %s
    blockHostPathVolumes: %t
    blockPrivilegedContainers: %t
    apiFairness: %s
`, name, profile.Level, profile.PodSecurity, profile.DefaultDenyNetworkPolicy, profile.RequireResourceRequests, yamlString(profile.RuntimeClassName), profile.BlockHostPathVolumes, profile.BlockPrivilegedContainers, profile.APIFairness)
	return nil
}

func renderSyncPolicy(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render syncpolicy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	conflictPolicy := fs.String("conflict-policy", "manual", "conflict policy: manual, tenant-wins, or host-wins")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("render syncpolicy requires exactly one name")
	}
	name := fs.Arg(0)
	if err := requireDNSName(name, "sync policy name"); err != nil {
		return err
	}
	if err := requireOneOf(*conflictPolicy, "conflict-policy", "manual", "tenant-wins", "host-wins"); err != nil {
		return err
	}

	fmt.Fprintf(stdout, `apiVersion: tenantplane.io/v1alpha1
kind: SyncPolicy
metadata:
  name: %s
spec:
  conflictPolicy: %s
  driftDetection:
    enabled: true
    interval: 30s
  explain:
    recordDecisions: true
    retain: 1000
  resources:
    - apiVersion: v1
      kind: Pod
      direction: toHost
    - apiVersion: v1
      kind: Service
      direction: bidirectional
    - apiVersion: v1
      kind: ConfigMap
      direction: bidirectional
    - apiVersion: v1
      kind: Secret
      direction: bidirectional
`, name, *conflictPolicy)
	return nil
}

func explainSync(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("explain-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tenant := fs.String("tenant", "", "TenantCluster name")
	tenantNamespace := fs.String("tenant-namespace", "", "host namespace of the tenant")
	virtualNamespace := fs.String("virtual-namespace", "default", "namespace as seen by the tenant")
	kind := fs.String("kind", "Pod", "resource kind")
	name := fs.String("name", "", "resource name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" || *tenantNamespace == "" || *name == "" {
		return errors.New("explain-sync requires --tenant, --tenant-namespace, and --name")
	}

	plan, err := syncplan.Explain(syncplan.ResourceRef{
		TenantCluster:    *tenant,
		TenantNamespace:  *tenantNamespace,
		VirtualNamespace: *virtualNamespace,
		Kind:             *kind,
		Name:             *name,
	})
	if err != nil {
		return err
	}

	labels := syncplan.HostLabels(plan.Input)
	annotations := syncplan.HostAnnotations(plan.Input)

	fmt.Fprintf(stdout, `tenantResource:
  tenantCluster: %s
  virtualNamespace: %s
  kind: %s
  name: %s
hostResource:
  namespace: %s
  name: %s
labels:
  %s: %s
  %s: %s
  %s: %s
  %s: %s
annotations:
  %s: %s
  %s: %s
reason:
  %s
`,
		plan.Input.TenantCluster, plan.Input.VirtualNamespace, plan.Input.Kind, plan.Input.Name,
		plan.Host.Namespace, plan.Host.Name,
		syncplan.LabelManagedBy, labels[syncplan.LabelManagedBy],
		syncplan.LabelTenant, labels[syncplan.LabelTenant],
		syncplan.LabelVirtualNamespace, labels[syncplan.LabelVirtualNamespace],
		syncplan.LabelKind, labels[syncplan.LabelKind],
		syncplan.AnnotationVirtualNamespace, annotations[syncplan.AnnotationVirtualNamespace],
		syncplan.AnnotationVirtualName, annotations[syncplan.AnnotationVirtualName],
		plan.Reason)
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `tenantplane creates inspectable virtual Kubernetes tenant clusters.

Usage:
  tenantplane version
  tenantplane render tenantcluster NAME [--namespace ns] [--mode shared|dedicated|private]
  tenantplane render isolationprofile NAME [--level baseline|restricted|sandboxed]
  tenantplane render syncpolicy NAME [--conflict-policy manual|tenant-wins|host-wins]
  tenantplane explain-sync --tenant NAME --tenant-namespace NS --name RESOURCE_NAME [--kind Pod]`)
}

func requireDNSName(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	if syncplan.SanitizeName(value) != value {
		return fmt.Errorf("%s %q must already be a DNS-safe name", label, value)
	}
	return nil
}

func requireOneOf(value, label string, allowed ...string) error {
	for _, item := range allowed {
		if value == item {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of: %s", label, strings.Join(allowed, ", "))
}

func yamlString(value string) string {
	if value == "" {
		return "\"\""
	}
	return value
}

