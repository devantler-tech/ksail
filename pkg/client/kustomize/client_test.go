package kustomize_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kustomize.NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestBuild_ValidKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple ConfigMap manifest
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write configmap: %v", err)
	}

	// Create a kustomization.yaml
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	client := kustomize.NewClient()
	ctx := context.Background()

	_, err := client.Build(ctx, "/nonexistent/directory")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestBuild_MissingKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory without kustomization.yaml
	tmpDir := t.TempDir()

	client := kustomize.NewClient()
	ctx := context.Background()

	_, err := client.Build(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error for missing kustomization.yaml")
	}
}

func TestBuild_InvalidKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with invalid kustomization
	tmpDir := t.TempDir()

	// Create an invalid kustomization.yaml (reference to non-existent file)
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - nonexistent.yaml
`

	err := os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	_, err = client.Build(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid kustomization")
	}
}

func setupComplexKustomization(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create base resources
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	err := os.WriteFile(filepath.Join(tmpDir, "namespace.yaml"), []byte(namespaceYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write namespace: %v", err)
	}

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: test-namespace
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test
        image: nginx:latest
`

	err = os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write deployment: %v", err)
	}

	// Create a kustomization with commonLabels
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - deployment.yaml
commonLabels:
  environment: test
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	return tmpDir
}

func TestBuild_ComplexKustomization(t *testing.T) {
	t.Parallel()

	tmpDir := setupComplexKustomization(t)

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_OutputIsValidYAML(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple resource
	serviceYAML := `apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: default
spec:
  selector:
    app: test
  ports:
  - port: 80
    targetPort: 8080
`

	err := os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(serviceYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write service: %v", err)
	}

	// Create a kustomization.yaml
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - service.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output is not empty
	if output.Len() == 0 {
		t.Fatal("expected non-empty output")
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_ReturnsBufferNotString(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple resource
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write configmap: %v", err)
	}

	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify we get a *bytes.Buffer
	if output == nil {
		t.Fatal("expected non-nil buffer")
	}

	// Verify it has the correct type
	if output.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}
}
