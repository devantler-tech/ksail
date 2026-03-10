package image_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
)

// smallManifest is a minimal single-Pod manifest with 2 containers.
const smallManifest = `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: default
spec:
  initContainers:
    - name: init
      image: busybox:1.36
  containers:
    - name: app
      image: nginx:1.25
    - name: sidecar
      image: fluent/fluentd:v1.16
`

// mediumManifest simulates a typical Deployment + Service + ConfigMap bundle.
const mediumManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      initContainers:
        - name: init-db
          image: busybox:1.36
        - name: migrate
          image: ghcr.io/myorg/migrate:v2.1.0
      containers:
        - name: web
          image: ghcr.io/myorg/web:v1.5.0
        - name: cache
          image: redis:7.4
        - name: proxy
          image: envoyproxy/envoy:v1.29.0
---
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
data:
  key: value
`

// largeManifest simulates a realistic Helm-rendered output with many resources and images.
var largeManifest = strings.Repeat(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: agent
  namespace: monitoring
spec:
  template:
    spec:
      initContainers:
        - name: init
          image: busybox:1.36
      containers:
        - name: agent
          image: quay.io/prometheus/node-exporter:v1.8.0
        - name: config-reloader
          image: ghcr.io/prometheus-operator/prometheus-config-reloader:v0.73.0
---
`, 20)

// BenchmarkExtractImagesFromManifest measures extraction across manifest sizes.
// The pre-compiled package-level imagePattern eliminates per-call regex compilation
// (~7500 ns + 93 allocs), making extraction dominated by I/O scanning and normalization.
func BenchmarkExtractImagesFromManifest(b *testing.B) {
	b.Run("Small/3images", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = image.ExtractImagesFromManifest(smallManifest)
		}
	})

	b.Run("Medium/5images", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = image.ExtractImagesFromManifest(mediumManifest)
		}
	})

	b.Run("Large/40images", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = image.ExtractImagesFromManifest(largeManifest)
		}
	})
}

// BenchmarkExtractImagesFromMultipleManifests measures multi-manifest deduplication.
func BenchmarkExtractImagesFromMultipleManifests(b *testing.B) {
	b.Run("TwoManifests", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			_, _ = image.ExtractImagesFromMultipleManifests(smallManifest, mediumManifest)
		}
	})

	b.Run("TenManifests", func(b *testing.B) {
		manifests := make([]string, 10)
		for i := range manifests {
			manifests[i] = smallManifest
		}

		b.ResetTimer()
		b.ReportAllocs()

		for b.Loop() {
			_, _ = image.ExtractImagesFromMultipleManifests(manifests...)
		}
	})
}

// BenchmarkNormalizeImageRef measures normalization cost for common image reference formats.
func BenchmarkNormalizeImageRef(b *testing.B) {
	cases := []struct {
		name string
		ref  string
	}{
		{"Simple", "nginx"},
		{"WithTag", "nginx:1.25"},
		{"DockerHubNamespaced", "bitnami/nginx:1.25"},
		{"GHCR", "ghcr.io/fluxcd/source-controller:v1.5.0"},
		{"RegistryK8s", "registry.k8s.io/metrics-server/metrics-server:v0.7.2"},
		{"Localhost", "localhost:5000/myimage:v1"},
		{"Digest", "nginx@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = image.NormalizeImageRef(tc.ref)
			}
		})
	}
}
