package image_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImagesFromManifest_EmptyAndSingle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			name:     "empty manifest",
			manifest: "",
			want:     nil,
		},
		{
			name: "single container",
			manifest: `apiVersion: v1
kind: Pod
metadata:
  name: test
spec:
  containers:
    - name: app
      image: nginx:1.25
`,
			want: []string{"docker.io/library/nginx:1.25"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := image.ExtractImagesFromManifest(testCase.manifest)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestExtractImagesFromManifest_MultipleContainers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			name: "multiple containers",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - name: app
      image: nginx:1.25
    - name: sidecar
      image: fluent/fluentd:v1.16
`,
			want: []string{
				"docker.io/library/nginx:1.25",
				"docker.io/fluent/fluentd:v1.16",
			},
		},
		{
			name: "init containers",
			manifest: `apiVersion: v1
kind: Pod
spec:
  initContainers:
    - name: init
      image: busybox:latest
  containers:
    - name: app
      image: nginx
`,
			want: []string{
				"docker.io/library/busybox:latest",
				"docker.io/library/nginx:latest",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := image.ExtractImagesFromManifest(testCase.manifest)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestExtractImagesFromManifest_FullyQualified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			name: "fully qualified images",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: ghcr.io/fluxcd/source-controller:v1.5.0
    - image: quay.io/jetstack/cert-manager-controller:v1.17.1
    - image: registry.k8s.io/metrics-server/metrics-server:v0.7.2
`,
			want: []string{
				"ghcr.io/fluxcd/source-controller:v1.5.0",
				"quay.io/jetstack/cert-manager-controller:v1.17.1",
				"registry.k8s.io/metrics-server/metrics-server:v0.7.2",
			},
		},
		{
			name: "quoted images",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: "nginx:1.25"
    - image: 'redis:7.4'
`,
			want: []string{
				"docker.io/library/nginx:1.25",
				"docker.io/library/redis:7.4",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := image.ExtractImagesFromManifest(testCase.manifest)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestExtractImagesFromManifest_Deduplication(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: apps/v1
kind: DaemonSet
spec:
  template:
    spec:
      initContainers:
        - image: busybox:1.36
      containers:
        - image: busybox:1.36
        - image: nginx:1.25
`
	want := []string{
		"docker.io/library/busybox:1.36",
		"docker.io/library/nginx:1.25",
	}

	got, err := image.ExtractImagesFromManifest(manifest)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestExtractImagesFromManifest_Sha256Digest(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: nginx@sha256:abc123def456
`
	want := []string{"docker.io/library/nginx@sha256:abc123def456"}

	got, err := image.ExtractImagesFromManifest(manifest)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestExtractImagesFromManifest_TemplateVariables(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: {{ .Values.image }}
    - image: nginx:1.25
`
	want := []string{"docker.io/library/nginx:1.25"}

	got, err := image.ExtractImagesFromManifest(manifest)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestExtractImagesFromManifest_WithComment(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: nginx:1.25  # main app
`
	want := []string{"docker.io/library/nginx:1.25"}

	got, err := image.ExtractImagesFromManifest(manifest)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNormalizeImageRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"nginx", "docker.io/library/nginx:latest"},
		{"nginx:1.25", "docker.io/library/nginx:1.25"},
		{"redis", "docker.io/library/redis:latest"},
		{"bitnami/nginx", "docker.io/bitnami/nginx:latest"},
		{"fluent/fluentd:v1.16", "docker.io/fluent/fluentd:v1.16"},
		{"ghcr.io/fluxcd/source-controller:v1.5.0", "ghcr.io/fluxcd/source-controller:v1.5.0"},
		{"quay.io/jetstack/cert-manager:v1.17", "quay.io/jetstack/cert-manager:v1.17"},
		{"registry.k8s.io/metrics-server:v0.7.2", "registry.k8s.io/metrics-server:v0.7.2"},
		{"docker.io/library/nginx", "docker.io/library/nginx:latest"},
		{"localhost/myimage:v1", "localhost/myimage:v1"},
		{"localhost:5000/myimage", "localhost:5000/myimage:latest"},
		// sha256 digests get normalized but don't get :latest added
		{"nginx@sha256:abc123", "docker.io/library/nginx@sha256:abc123"},
		{"ghcr.io/org/image@sha256:def456", "ghcr.io/org/image@sha256:def456"},
	}

	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			got := image.NormalizeImageRef(testCase.input)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestExtractImagesFromMultipleManifests(t *testing.T) {
	t.Parallel()

	manifest1 := `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: nginx:1.25
`
	manifest2 := `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: redis:7.4
    - image: nginx:1.25
`

	got, err := image.ExtractImagesFromMultipleManifests(manifest1, manifest2)
	require.NoError(t, err)

	want := []string{
		"docker.io/library/nginx:1.25",
		"docker.io/library/redis:7.4",
	}
	assert.Equal(t, want, got)
}
