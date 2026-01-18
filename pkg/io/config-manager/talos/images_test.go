package talos

import (
	"strings"
	"testing"
)

func TestTalosImage(t *testing.T) {
	image := talosImage()

	// Should not be empty
	if image == "" {
		t.Error("talosImage() returned empty string")
	}

	// Should be a valid image reference with registry, repo, and tag
	if !strings.Contains(image, "/") {
		t.Errorf("talosImage() should contain registry/repo path, got: %s", image)
	}

	if !strings.Contains(image, ":") {
		t.Errorf("talosImage() should contain a tag, got: %s", image)
	}

	// Should be the siderolabs/talos image
	if !strings.Contains(image, "siderolabs/talos") {
		t.Errorf("talosImage() should be siderolabs/talos image, got: %s", image)
	}
}

func TestDockerfileEmbed(t *testing.T) {
	// Verify the Dockerfile is embedded
	if dockerfile == "" {
		t.Error("Dockerfile was not embedded")
	}

	// Should contain FROM directive
	if !strings.Contains(dockerfile, "FROM") {
		t.Error("Dockerfile should contain FROM directive")
	}
}
