package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/require"
)

// The existing SDK entry point retains fill-only identity semantics, while the
// new CLI target API deliberately replaces a source cluster's name.
func TestCloneBaseSyncedSDKPreservesDeclaredIdentity(t *testing.T) {
	t.Parallel()

	const content = "metadata:\n  name: manually-chosen\nspec:\n  cluster:\n" +
		"    connection:\n      context: custom-context\n"

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.yaml"), []byte(content), 0o600))
	path, wrote, err := environment.CloneBaseSyncedConfigTo(
		root,
		"ksail.yaml",
		"ksail.prod.yaml",
		nil,
		environment.Identity{Name: "prod", Context: "kind-prod"},
		false,
	)
	require.NoError(t, err)
	require.True(t, wrote)
	require.Equal(t, "ksail.prod.yaml", path)
	require.Equal(t, content, readClone(t, root, path))
	require.Equal(t, content, readClone(t, root, "ksail.yaml"))
}

func TestCloneBaseSyncedSDKFillsMissingIdentityWithoutOverwritingDestination(t *testing.T) {
	t.Parallel()

	const content = "metadata: {}\nspec: {}\n"

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.yaml"), []byte(content), 0o600))

	identity := environment.Identity{Name: "prod", Context: "kind-prod"}
	path, wrote, err := environment.CloneBaseSyncedConfigTo(
		root,
		"ksail.yaml",
		"ksail.prod.yaml",
		nil,
		identity,
		false,
	)
	require.NoError(t, err)
	require.True(t, wrote)
	cloned := readClone(t, root, path)
	require.Contains(t, cloned, "name: prod")
	require.Contains(t, cloned, "context: kind-prod")

	_, wrote, err = environment.CloneBaseSyncedConfigTo(root, "ksail.yaml", path, nil,
		environment.Identity{Name: "other", Context: "other-context"}, false)
	require.NoError(t, err)
	require.False(t, wrote)
	require.Equal(t, cloned, readClone(t, root, path))
	require.Equal(t, content, readClone(t, root, "ksail.yaml"))
}
