package talos_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/configmanager/talos"
)

func TestTalosImage(t *testing.T) {
	t.Parallel()

	image := talos.DefaultTalosImage

	// Should not be empty
	if image == "" {
		t.Error("DefaultTalosImage is empty")
	}

	// Should be a valid image reference with registry, repo, and tag
	if !strings.Contains(image, "/") {
		t.Errorf("DefaultTalosImage should contain registry/repo path, got: %s", image)
	}

	if !strings.Contains(image, ":") {
		t.Errorf("DefaultTalosImage should contain a tag, got: %s", image)
	}

	// Should be the siderolabs/talos image
	if !strings.Contains(image, "siderolabs/talos") {
		t.Errorf("DefaultTalosImage should be siderolabs/talos image, got: %s", image)
	}
}

func TestDefaultTalosImage_NotEmpty(t *testing.T) {
	t.Parallel()

	// Verify the image is set (proves Dockerfile embedding works)
	if talos.DefaultTalosImage == "" {
		t.Error("DefaultTalosImage was not set - Dockerfile may not be embedded")
	}
}
