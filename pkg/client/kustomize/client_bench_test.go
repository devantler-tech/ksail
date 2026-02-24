package kustomize_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
)

// Package-level constants for medium kustomization benchmark YAML content.
const (
	mediumNamespaceYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	mediumDeploymentYAML = `apiVersion: apps/v1
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

	mediumServiceYAML = `apiVersion: v1
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

	mediumConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-namespace
data:
  config.yaml: |
    setting1: value1
    setting2: value2
`

	mediumKustomizationYAML = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - deployment.yaml
  - service.yaml
  - configmap.yaml
`
)

// Package-level constants for labels kustomization benchmark YAML content.
const (
	labelsDeploymentYAML = `apiVersion: apps/v1
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

	labelsServiceYAML = `apiVersion: v1
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

	labelsKustomizationYAML = `apiVersion: kustomize.config.k8s.io/v1beta1
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
)

// setupMediumKustomizationBench creates a temporary directory with a medium
// kustomization (Namespace, Deployment, Service, ConfigMap).
func setupMediumKustomizationBench(b *testing.B) string {
	b.Helper()

	tmpDir := b.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "namespace.yaml"), []byte(mediumNamespaceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write namespace: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(tmpDir, "deployment.yaml"),
		[]byte(mediumDeploymentYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write deployment: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(mediumServiceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write service: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(mediumConfigMapYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write configmap: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(mediumKustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	return tmpDir
}

// setupWithLabelsBench creates a temporary directory with a kustomization
// that applies labels to all resources.
func setupWithLabelsBench(b *testing.B) string {
	b.Helper()

	tmpDir := b.TempDir()

	err := os.WriteFile(
		filepath.Join(tmpDir, "deployment.yaml"),
		[]byte(labelsDeploymentYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write deployment: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(labelsServiceYAML), 0o600)
	if err != nil {
		b.Fatalf("failed to write service: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(labelsKustomizationYAML),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	return tmpDir
}

// largeConfigMapYAML returns a ConfigMap YAML for the large benchmark,
// using resourceIdx to generate a unique name (a-j).
func largeConfigMapYAML(resourceIdx int) string {
	letter := "abcdefghij"[resourceIdx : resourceIdx+1]

	return `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-` + letter + `
  namespace: default
data:
  key: value
`
}

// largeServiceYAML returns a Service YAML for the large benchmark,
// using resourceIdx to generate a unique name (a-j).
func largeServiceYAML(resourceIdx int) string {
	letter := "abcdefghij"[resourceIdx : resourceIdx+1]

	return `apiVersion: v1
kind: Service
metadata:
  name: service-` + letter + `
  namespace: default
spec:
  selector:
    app: test-` + letter + `
  ports:
  - port: 80
    targetPort: 8080
`
}

// buildLargeKustomizationYAML builds the kustomization.yaml content for the
// large benchmark listing all 20 resources (10 ConfigMaps + 10 Services).
func buildLargeKustomizationYAML() string {
	const kustomizationHeader = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n"

	var builder strings.Builder

	builder.Grow(len(kustomizationHeader) + 40*10) // 40 chars per resource pair × 10 iterations
	builder.WriteString(kustomizationHeader)

	for resourceIdx := range 10 {
		letter := "abcdefghij"[resourceIdx : resourceIdx+1]

		builder.WriteString("  - configmap-")
		builder.WriteString(letter)
		builder.WriteString(".yaml\n")
		builder.WriteString("  - service-")
		builder.WriteString(letter)
		builder.WriteString(".yaml\n")
	}

	return builder.String()
}

// setupLargeKustomizationBench creates a temporary directory with a large
// kustomization containing 20 resources (10 ConfigMaps + 10 Services).
func setupLargeKustomizationBench(b *testing.B) string {
	b.Helper()

	tmpDir := b.TempDir()

	for resourceIdx := range 10 {
		letter := "abcdefghij"[resourceIdx : resourceIdx+1]

		err := os.WriteFile(
			filepath.Join(tmpDir, "configmap-"+letter+".yaml"),
			[]byte(largeConfigMapYAML(resourceIdx)),
			0o600,
		)
		if err != nil {
			b.Fatalf("failed to write configmap: %v", err)
		}
	}

	for resourceIdx := range 10 {
		letter := "abcdefghij"[resourceIdx : resourceIdx+1]

		err := os.WriteFile(
			filepath.Join(tmpDir, "service-"+letter+".yaml"),
			[]byte(largeServiceYAML(resourceIdx)),
			0o600,
		)
		if err != nil {
			b.Fatalf("failed to write service: %v", err)
		}
	}

	err := os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(buildLargeKustomizationYAML()),
		0o600,
	)
	if err != nil {
		b.Fatalf("failed to write kustomization: %v", err)
	}

	return tmpDir
}

// BenchmarkBuild_SmallKustomization benchmarks building a small kustomization
// with a single resource (ConfigMap). This represents the minimal overhead of
// the kustomize build process.
func BenchmarkBuild_SmallKustomization(b *testing.B) {
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

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(simpleConfigMapKustomization),
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

// BenchmarkBuild_MediumKustomization benchmarks building a medium-sized
// kustomization with multiple resources representing a typical application
// deployment (Namespace, Deployment, Service, ConfigMap).
func BenchmarkBuild_MediumKustomization(b *testing.B) {
	tmpDir := setupMediumKustomizationBench(b)
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
	tmpDir := setupWithLabelsBench(b)
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
	tmpDir := setupLargeKustomizationBench(b)
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
