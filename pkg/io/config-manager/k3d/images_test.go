package k3d_test

import (
	"strings"
	"testing"

	k3d "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
)

func TestK3sImage(t *testing.T) {
	t.Parallel()

	image := k3d.DefaultK3sImage

	// Should not be empty
	if image == "" {
		t.Error("DefaultK3sImage is empty")
	}

	// Should be a valid image reference with repo and tag
	if !strings.Contains(image, "/") {
		t.Errorf("DefaultK3sImage should contain repo path, got: %s", image)
	}

	if !strings.Contains(image, ":") {
		t.Errorf("DefaultK3sImage should contain a tag, got: %s", image)
	}

	// Should be the rancher/k3s image
	if !strings.Contains(image, "rancher/k3s") {
		t.Errorf("DefaultK3sImage should be rancher/k3s image, got: %s", image)
	}
}

func TestDefaultK3sImage_NotEmpty(t *testing.T) {
	t.Parallel()

	// Verify the image is set (proves Dockerfile embedding works)
	if k3d.DefaultK3sImage == "" {
		t.Error("DefaultK3sImage was not set - Dockerfile may not be embedded")
	}
}
