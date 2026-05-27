package k3d_test

import (
	"strings"
	"testing"

	k3d "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
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

func TestDefaultK3sVersion(t *testing.T) {
	t.Parallel()

	version := k3d.DefaultK3sVersion()

	// Should be non-empty and the tag portion of DefaultK3sImage (no repo, no digest).
	if version == "" {
		t.Fatal("DefaultK3sVersion is empty")
	}

	if strings.ContainsAny(version, "/@") {
		t.Errorf("DefaultK3sVersion should not contain repo or digest, got: %s", version)
	}

	if !strings.HasPrefix(version, "v") || !strings.Contains(version, "k3s") {
		t.Errorf(
			"DefaultK3sVersion should be a k3s version tag like v1.36.1-k3s1, got: %s",
			version,
		)
	}

	if !strings.Contains(k3d.DefaultK3sImage, ":"+version+"@") {
		t.Errorf(
			"DefaultK3sVersion %q should be the tag of DefaultK3sImage %q",
			version,
			k3d.DefaultK3sImage,
		)
	}
}
