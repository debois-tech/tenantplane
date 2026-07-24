package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTenantClusterRejectsUnsupportedKubernetesVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"render", "tenantcluster", "--kubernetes-version", "v1.35.0", "dev"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for an unsupported kubernetesVersion")
	}
	if !strings.Contains(err.Error(), "v1.35.0") {
		t.Fatalf("error should name the rejected version: %v", err)
	}
}

func TestRenderTenantClusterAcceptsSupportedKubernetesVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"render", "tenantcluster", "--kubernetes-version", "v1.30", "dev"}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "kubernetesVersion: v1.30\n") {
		t.Fatalf("output missing the requested version:\n%s", stdout.String())
	}
}
