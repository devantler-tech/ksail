package helm_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

// BenchmarkChartSpec measures the performance of ChartSpec struct initialization.
func BenchmarkChartSpec(b *testing.B) {
	b.Run("Basic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &helm.ChartSpec{
				ReleaseName: "test-release",
				ChartName:   "test-chart",
				Namespace:   "default",
			}
		}
	})

	b.Run("WithAllFields", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &helm.ChartSpec{
				ReleaseName:     "my-release",
				ChartName:       "my-chart",
				Namespace:       "my-namespace",
				Version:         "1.0.0",
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         5 * time.Minute,
				Silent:          true,
				UpgradeCRDs:     true,
				ValuesYaml:      "key: value\nreplicas: 3",
				ValueFiles:      []string{"values.yaml", "override.yaml"},
				SetValues: map[string]string{
					"replicas":        "3",
					"image.tag":       "latest",
					"resources.cpu":   "100m",
					"resources.mem":   "256Mi",
					"ingress.enabled": "true",
				},
				SetJSONVals: map[string]string{
					"config": `{"debug": true, "level": "info"}`,
				},
			}
		}
	})
}

// BenchmarkRepositoryEntry measures the performance of RepositoryEntry struct initialization.
func BenchmarkRepositoryEntry(b *testing.B) {
	b.Run("Basic", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &helm.RepositoryEntry{
				Name: "test-repo",
				URL:  "https://charts.example.com",
			}
		}
	})

	b.Run("WithAuth", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &helm.RepositoryEntry{
				Name:                  "secure-repo",
				URL:                   "https://charts.secure.com",
				Username:              "user",
				Password:              "pass",
				CertFile:              "/path/to/cert",
				KeyFile:               "/path/to/key",
				CaFile:                "/path/to/ca",
				InsecureSkipTLSverify: true,
			}
		}
	})
}

// BenchmarkReleaseInfo measures the performance of ReleaseInfo struct initialization.
func BenchmarkReleaseInfo(b *testing.B) {
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = &helm.ReleaseInfo{
			Name:       "my-release",
			Namespace:  "default",
			Revision:   1,
			Status:     "deployed",
			Chart:      "my-chart-1.0.0",
			AppVersion: "1.0.0",
			Updated:    now,
			Notes:      "Installation successful",
		}
	}
}

// BenchmarkMockClient measures the performance of mock client operations.
func BenchmarkMockClient(b *testing.B) {
	ctx := context.Background()

	b.Run("AddRepository", func(b *testing.B) {
		client := helm.NewMockInterface(b)
		entry := &helm.RepositoryEntry{
			Name: "test-repo",
			URL:  "https://charts.example.com",
		}

		client.EXPECT().
			AddRepository(ctx, entry, time.Minute).
			Return(nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = client.AddRepository(ctx, entry, time.Minute)
		}
	})

	b.Run("InstallOrUpgradeChart", func(b *testing.B) {
		client := helm.NewMockInterface(b)
		spec := &helm.ChartSpec{
			ReleaseName: "test-release",
			ChartName:   "test-chart",
			Namespace:   "default",
		}

		expectedRelease := &helm.ReleaseInfo{
			Name:       "test-release",
			Namespace:  "default",
			Revision:   1,
			Status:     "deployed",
			Chart:      "test-chart-1.0.0",
			AppVersion: "1.0.0",
		}

		client.EXPECT().
			InstallOrUpgradeChart(ctx, spec).
			Return(expectedRelease, nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = client.InstallOrUpgradeChart(ctx, spec)
		}
	})

	b.Run("ReleaseExists", func(b *testing.B) {
		client := helm.NewMockInterface(b)

		client.EXPECT().
			ReleaseExists(ctx, "test-release", "default").
			Return(true, nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = client.ReleaseExists(ctx, "test-release", "default")
		}
	})

	b.Run("UninstallRelease", func(b *testing.B) {
		client := helm.NewMockInterface(b)

		client.EXPECT().
			UninstallRelease(ctx, "test-release", "default").
			Return(nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = client.UninstallRelease(ctx, "test-release", "default")
		}
	})

	b.Run("TemplateChart", func(b *testing.B) {
		client := helm.NewMockInterface(b)
		spec := &helm.ChartSpec{
			ReleaseName: "test-release",
			ChartName:   "test-chart",
			Namespace:   "default",
		}

		expectedOutput := `apiVersion: v1
kind: Service
metadata:
  name: test-service
spec:
  ports:
  - port: 80
  selector:
    app: test`

		client.EXPECT().
			TemplateChart(ctx, spec).
			Return(expectedOutput, nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = client.TemplateChart(ctx, spec)
		}
	})

	b.Run("InstallChart", func(b *testing.B) {
		client := helm.NewMockInterface(b)
		spec := &helm.ChartSpec{
			ReleaseName: "test-release",
			ChartName:   "test-chart",
			Namespace:   "default",
		}

		expectedRelease := &helm.ReleaseInfo{
			Name:       "test-release",
			Namespace:  "default",
			Revision:   1,
			Status:     "deployed",
			Chart:      "test-chart-1.0.0",
			AppVersion: "1.0.0",
		}

		client.EXPECT().
			InstallChart(ctx, spec).
			Return(expectedRelease, nil).
			Times(b.N)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = client.InstallChart(ctx, spec)
		}
	})
}

// BenchmarkChartSpecWithLargeValues tests performance with large value configurations.
func BenchmarkChartSpecWithLargeValues(b *testing.B) {
	// Simulate a large values.yaml structure
	largeYAML := `
global:
  imageRegistry: docker.io
  imagePullSecrets: []
  storageClass: ""

replicas: 3

image:
  registry: docker.io
  repository: bitnami/nginx
  tag: 1.23.0
  pullPolicy: IfNotPresent
  pullSecrets: []

service:
  type: ClusterIP
  port: 80
  targetPort: http
  nodePorts:
    http: ""
  clusterIP: ""
  externalTrafficPolicy: Cluster
  loadBalancerIP: ""
  loadBalancerSourceRanges: []

ingress:
  enabled: false
  pathType: ImplementationSpecific
  hostname: nginx.local
  path: /
  annotations: {}
  tls: false
  extraHosts: []
  extraPaths: []
  extraTls: []

resources:
  limits:
    cpu: 100m
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 10
  targetCPU: 80
  targetMemory: 80

persistence:
  enabled: false
  storageClass: ""
  accessModes:
    - ReadWriteOnce
  size: 8Gi
`

	// Create large set of override values
	largeSetValues := make(map[string]string)
	for i := 0; i < 50; i++ {
		largeSetValues["config.key"+strconv.Itoa(i)] = "value"
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = &helm.ChartSpec{
			ReleaseName:     "large-release",
			ChartName:       "bitnami/nginx",
			Namespace:       "production",
			Version:         "13.2.0",
			CreateNamespace: true,
			Atomic:          true,
			Wait:            true,
			WaitForJobs:     true,
			Timeout:         10 * time.Minute,
			ValuesYaml:      largeYAML,
			ValueFiles:      []string{"values.yaml", "values-prod.yaml", "values-override.yaml"},
			SetValues:       largeSetValues,
		}
	}
}
