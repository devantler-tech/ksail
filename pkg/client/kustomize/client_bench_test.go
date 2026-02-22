package kustomize_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
)

// BenchmarkBuild_SmallKustomization benchmarks building a small kustomization
// with a single resource (ConfigMap). This represents the minimal overhead of
// the kustomize build process.
func BenchmarkBuild_SmallKustomization(b *testing.B) {
	b.Helper()
	// Setup: Create a temporary directory with a simple kustomization
	tmpDir := b.TempDir()

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
		b.Fatalf("failed to write configmap: %v", err)
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
		b.Fatalf("failed to write kustomization: %v", err)
	}

	// Create client and context once, reuse in benchmark loop
	client := kustomize.NewClient()
	ctx := context.Background()

	// Report memory allocations
	b.ReportAllocs()

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Benchmark loop
	for range b.N {
		_, err := client.Build(ctx, tmpDir)
		if err != nil {
			b.Fatalf("build failed: %v", err)
		}
	}
}

// BenchmarkBuild_MediumKustomization benchmarks building a medium-sized
// kustomization with multiple resources representing a typical application
// deployment (Namespace, Deployment, Service, ConfigMap).
func BenchmarkBuild_MediumKustomization(b *testing.B) {
	b.Helper()
	// Setup: Create a temporary directory with multiple resources
	tmpDir := b.TempDir()

	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	err := os.WriteFile(filepath.Join(tmpDir, "namespace.yaml"), []byte(namespaceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write namespace: %v", err)
	}

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: test-namespace
spec:
  replicas: 3
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
        ports:
        - containerPort: 80
`

	err = os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write deployment: %v", err)
	}

	serviceYAML := `apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: test-namespace
spec:
  selector:
    app: test
  ports:
  - port: 80
    targetPort: 8080
`

	err = os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(serviceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write service: %v", err)
	}

	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-namespace
data:
  config.yaml: |
    setting1: value1
    setting2: value2
`

	err = os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write configmap: %v", err)
	}

	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - deployment.yaml
  - service.yaml
  - configmap.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	client := kustomize.NewClient()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := client.Build(ctx, tmpDir)
		if err != nil {
			b.Fatalf("build failed: %v", err)
		}
	}
}

// BenchmarkBuild_WithLabels benchmarks building a kustomization that
// applies labels to all resources. This tests the overhead of
// kustomize transformations.
func BenchmarkBuild_WithLabels(b *testing.B) {
	b.Helper()
	// Setup
	tmpDir := b.TempDir()

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: default
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

	err := os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write deployment: %v", err)
	}

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
`

	err = os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(serviceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write service: %v", err)
	}

	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
  - service.yaml
labels:
  - pairs:
      environment: production
      team: platform
      managed-by: kustomize
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	client := kustomize.NewClient()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := client.Build(ctx, tmpDir)
		if err != nil {
			b.Fatalf("build failed: %v", err)
		}
	}
}

// BenchmarkBuild_LargeKustomization benchmarks building a large kustomization
// with many resources. This tests performance with realistic complex applications.
func BenchmarkBuild_LargeKustomization(b *testing.B) {
	b.Helper()
	// Setup: Create a kustomization with 20 resources
	tmpDir := b.TempDir()

	// Create 10 ConfigMaps
	for i := range 10 {
		configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-` + string(rune('a'+i)) + `
  namespace: default
data:
  key: value
`
		filename := "configmap-" + string(rune('a'+i)) + ".yaml"
		err := os.WriteFile(filepath.Join(tmpDir, filename), []byte(configMapYAML), 0o600)
		if err != nil {
			b.Fatalf("failed to write configmap: %v", err)
		}
	}

	// Create 10 Services
	for i := range 10 {
		serviceYAML := `apiVersion: v1
kind: Service
metadata:
  name: service-` + string(rune('a'+i)) + `
  namespace: default
spec:
  selector:
    app: test-` + string(rune('a'+i)) + `
  ports:
  - port: 80
    targetPort: 8080
`
		filename := "service-" + string(rune('a'+i)) + ".yaml"
		err := os.WriteFile(filepath.Join(tmpDir, filename), []byte(serviceYAML), 0o600)
		if err != nil {
			b.Fatalf("failed to write service: %v", err)
		}
	}

	// Build the resources list for kustomization
	const kustomizationHeader = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n"
	var kustomizationBuilder strings.Builder
	kustomizationBuilder.Grow(len(kustomizationHeader) + 40*10) // 40 chars per resource pair × 10 iterations
	kustomizationBuilder.WriteString(kustomizationHeader)
	for i := range 10 {
		kustomizationBuilder.WriteString("  - configmap-")
		kustomizationBuilder.WriteByte(byte('a' + i))
		kustomizationBuilder.WriteString(".yaml\n")
		kustomizationBuilder.WriteString("  - service-")
		kustomizationBuilder.WriteByte(byte('a' + i))
		kustomizationBuilder.WriteString(".yaml\n")
	}
	kustomizationYAML := kustomizationBuilder.String()

	err := os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	client := kustomize.NewClient()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := client.Build(ctx, tmpDir)
		if err != nil {
			b.Fatalf("build failed: %v", err)
		}
	}
}

// BenchmarkBuild_WithNamePrefix benchmarks building a kustomization that
// applies a name prefix to all resources. This tests another common
// kustomize transformation pattern.
func BenchmarkBuild_WithNamePrefix(b *testing.B) {
	b.Helper()
	// Setup
	tmpDir := b.TempDir()

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: deployment
  namespace: default
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

	err := os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write deployment: %v", err)
	}

	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namePrefix: dev-
resources:
  - deployment.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	client := kustomize.NewClient()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := client.Build(ctx, tmpDir)
		if err != nil {
			b.Fatalf("build failed: %v", err)
		}
	}
}
