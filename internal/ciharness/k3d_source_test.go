package ciharness_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	k3dModulePath      = "github.com/k3d-io/k3d/v5"
	k3dOriginalVersion = "v5.9.0-rc.0.0.20260602135457-2a0cb9f9a5c1"
	k3dOriginalSum     = "h1:qZ3L5kFytLaW7E1XMma9H7cbE5YgZRezw54eqRoR/Z4="
)

// The v5.9.0 tag moved after the checksum database recorded its original bytes.
// Verify the actual selected, downloaded source rather than accepting new tag
// bytes or relying on the owned CodeQL workflow's environment.
func TestK3dUsesAuthenticatedImmutableSource(t *testing.T) {
	t.Parallel()

	_, err := os.Stat(filepath.Join("..", "..", "vendor", "modules.txt"))
	require.True(t, os.IsNotExist(err), "vendored sources require their own integrity verification")

	var selected struct {
		Path    string
		Version string
		Replace *struct {
			Path    string
			Version string
		}
	}

	require.NoError(t, json.Unmarshal(k3dGoOutput(t, "list", "-m", "-json", k3dModulePath), &selected))
	require.Equal(t, k3dModulePath, selected.Path)
	require.Equal(t, "v5.9.0", selected.Version)
	require.NotNil(t, selected.Replace, "the mutable tag must select its authenticated original commit")
	require.Equal(t, k3dModulePath, selected.Replace.Path)
	require.Equal(t, k3dOriginalVersion, selected.Replace.Version)

	var downloaded struct {
		Path    string
		Version string
		Sum     string
		Error   string
	}

	data := k3dGoOutput(t, "mod", "download", "-json", k3dModulePath+"@"+selected.Replace.Version)
	require.NoError(t, json.Unmarshal(data, &downloaded))
	require.Empty(t, downloaded.Error)
	require.Equal(t, k3dModulePath, downloaded.Path)
	require.Equal(t, k3dOriginalVersion, downloaded.Version)
	require.Equal(t, k3dOriginalSum, downloaded.Sum, "the archive must retain the original authenticated source")
}

func k3dGoOutput(t *testing.T, args ...string) []byte {
	t.Helper()

	//nolint:gosec // The callers use fixed Go module inspection subcommands.
	command := exec.CommandContext(t.Context(), "go", args...)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "GOFLAGS=", "GOENV=off", "GOWORK=off", "GOEXPERIMENT=",
		"GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=sum.golang.org")

	var stderr bytes.Buffer

	command.Stderr = &stderr

	output, err := command.Output()
	require.NoError(t, err, "inspect k3d source: %s", stderr.String())

	return output
}
