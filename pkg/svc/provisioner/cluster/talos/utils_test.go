package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

//nolint:funlen // Table-driven test with comprehensive image tag scenarios
func TestExtractTagFromImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "standard image with tag",
			image: "ghcr.io/siderolabs/talos:v1.13.0-beta.1",
			want:  "v1.13.0-beta.1",
		},
		{
			name:  "installer image with tag",
			image: "ghcr.io/siderolabs/installer:v1.13.0",
			want:  "v1.13.0",
		},
		{
			name:  "image without tag",
			image: "ghcr.io/siderolabs/talos",
			want:  "",
		},
		{
			name:  "image with digest only",
			image: "ghcr.io/siderolabs/talos@sha256:abc123",
			want:  "",
		},
		{
			name:  "image with tag and digest",
			image: "ghcr.io/siderolabs/talos:v1.13.0@sha256:abc123",
			want:  "v1.13.0",
		},
		{
			name:  "image with port in registry",
			image: "localhost:5000/siderolabs/talos:v1.13.0",
			want:  "v1.13.0",
		},
		{
			name:  "image with port in registry and no tag",
			image: "localhost:5000/siderolabs/talos",
			want:  "",
		},
		{
			name:  "simple image with tag",
			image: "talos:latest",
			want:  "latest",
		},
		{
			name:  "empty string",
			image: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.ExtractTagFromImageForTest(tt.image)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstallerImageFromTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		want string
	}{
		{
			name: "release version",
			tag:  "v1.13.0",
			want: "ghcr.io/siderolabs/installer:v1.13.0",
		},
		{
			name: "beta version",
			tag:  "v1.13.0-beta.1",
			want: "ghcr.io/siderolabs/installer:v1.13.0-beta.1",
		},
		{
			name: "alpha version",
			tag:  "v1.13.0-alpha.0",
			want: "ghcr.io/siderolabs/installer:v1.13.0-alpha.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.InstallerImageFromTagForTest(tt.tag)
			assert.Equal(t, tt.want, got)
		})
	}
}
