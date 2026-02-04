package image

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractImagesFromManifest(t *testing.T) {
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
		{
			name: "deduplication",
			manifest: `apiVersion: apps/v1
kind: DaemonSet
spec:
  template:
    spec:
      initContainers:
        - image: busybox:1.36
      containers:
        - image: busybox:1.36
        - image: nginx:1.25
`,
			want: []string{
				"docker.io/library/busybox:1.36",
				"docker.io/library/nginx:1.25",
			},
		},
		{
			name: "sha256 digest",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: nginx@sha256:abc123def456
`,
			want: []string{"docker.io/library/nginx@sha256:abc123def456"},
		},
		{
			name: "skip template variables",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: {{ .Values.image }}
    - image: nginx:1.25
`,
			want: []string{"docker.io/library/nginx:1.25"},
		},
		{
			name: "image with comment",
			manifest: `apiVersion: v1
kind: Pod
spec:
  containers:
    - image: nginx:1.25  # main app
`,
			want: []string{"docker.io/library/nginx:1.25"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractImagesFromManifest(tt.manifest)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeImageRef(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeImageRef(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractImagesFromMultipleManifests(t *testing.T) {
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

	got, err := ExtractImagesFromMultipleManifests(manifest1, manifest2)
	require.NoError(t, err)

	want := []string{
		"docker.io/library/nginx:1.25",
		"docker.io/library/redis:7.4",
	}
	assert.Equal(t, want, got)
}
