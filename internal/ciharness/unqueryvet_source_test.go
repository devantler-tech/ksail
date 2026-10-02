package ciharness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Managed analysis received unauthenticated v1.4.0 bytes before its autobuild
// failed. Inspect the actual selected release and archive, retaining normal
// checksum verification and rejecting local or vendored substitutions.
func TestUnqueryvetUsesAuthenticatedRelease(t *testing.T) {
	t.Parallel()

	const (
		module   = "github.com/MirrexOne/unqueryvet"
		version  = "v1.5.4"
		checksum = "h1:38QOxShO7JmMWT+eCdDMbcUgGCOeJphVkzzRgyLJgsQ="
	)

	_, err := os.Stat(filepath.Join("..", "..", "vendor", "modules.txt"))
	require.True(t, os.IsNotExist(err), "vendored sources require their own integrity verification")

	var selected struct {
		Path    string          `json:"Path"`    //nolint:tagliatelle // Go command output contract.
		Version string          `json:"Version"` //nolint:tagliatelle // Go command output contract.
		Replace json.RawMessage `json:"Replace"` //nolint:tagliatelle // Go command output contract.
	}

	data := authenticatedSourceGoOutput(t, "list", "-m", "-json", module)
	require.NoError(t, json.Unmarshal(data, &selected))
	require.Equal(t, module, selected.Path)
	require.Equal(
		t,
		version,
		selected.Version,
		"the failed tag must select the authenticated newer release",
	)
	require.Empty(t, selected.Replace, "the released linter must not be replaced")

	var downloaded struct {
		Sum   string `json:"Sum"`   //nolint:tagliatelle // Go command output contract.
		Error string `json:"Error"` //nolint:tagliatelle // Go command output contract.
	}

	data = authenticatedSourceGoOutput(t, "mod", "download", "-json", module+"@"+selected.Version)
	require.NoError(t, json.Unmarshal(data, &downloaded))
	require.Empty(t, downloaded.Error)
	require.Equal(
		t,
		checksum,
		downloaded.Sum,
		"the selected release must retain its SumDB-authenticated archive",
	)
}
