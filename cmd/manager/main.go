package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/debois-tech/tenantplane/internal/controller"
)

const version = "0.1.0-dev"

// banner prints a short, human-friendly startup line to stdout — separate from
// the structured zap logging below, which stays JSON/console-formatted for log
// aggregation. Each line is only printed once the real step it describes has
// actually completed, the same way minikube's own startup banner narrates real
// progress rather than a fixed, cosmetic sequence.
func banner(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func main() {
	var metricsAddr string
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "address the metrics endpoint binds to")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address the health probe endpoint binds to")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	banner("🪐 tenantplane %s", version)

	scheme := runtimeScheme()
	banner("✅ Registered the tenantplane.io/v1alpha1 API types")

	restConfig := ctrl.GetConfigOrDie()
	banner("🔌 Connecting to the Kubernetes API server at %s", restConfig.Host)

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions(metricsAddr),
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		ctrl.Log.Error(err, "unable to build clientset")
		os.Exit(1)
	}

	reconciler := &controller.TenantClusterReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RESTConfig: restConfig,
		ClientSet:  clientSet,
		Recorder:   mgr.GetEventRecorderFor("tenantcluster-controller"),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "TenantCluster")
		os.Exit(1)
	}
	banner("🚀 TenantCluster controller registered")

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	banner("🏥 Health checks: healthz/readyz on %s", probeAddr)
	banner("📊 Metrics on %s", metricsAddr)
	banner("🎉 tenantplane is up — watching for TenantCluster, IsolationProfile, and SyncPolicy objects")

	ctrl.Log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "problem running manager")
		os.Exit(1)
	}
}
