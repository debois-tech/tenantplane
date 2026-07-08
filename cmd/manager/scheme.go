package main

import (
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/debois-tech/tenantplane/internal/api/v1alpha1"
)

func runtimeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}

func metricsServerOptions(bindAddress string) metricsserver.Options {
	return metricsserver.Options{BindAddress: bindAddress}
}
