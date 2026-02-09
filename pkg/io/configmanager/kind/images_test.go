package kind_test

import (
	"strings"
	"testing"

	kind "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/kind"
)

func TestKindNodeImage(t *testing.T) {
	t.Parallel()

	image := kind.DefaultKindNodeImage

	// Should not be empty
	if image == "" {
		t.Error("DefaultKindNodeImage is empty")
	}

	// Should be a valid image reference with repo and tag
	if !strings.Contains(image, "/") {
		t.Errorf("DefaultKindNodeImage should contain repo path, got: %s", image)
	}

	if !strings.Contains(image, ":") {
		t.Errorf("DefaultKindNodeImage should contain a tag, got: %s", image)
	}

	// Should be the kindest/node image
	if !strings.Contains(image, "kindest/node") {
		t.Errorf("DefaultKindNodeImage should be kindest/node image, got: %s", image)
	}
}
