package releasebuildprofile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/releasebuildprofile"
	"github.com/stretchr/testify/require"
)

func TestInspectOutputsRejectsMissingOrNonExecutableTargets(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	_, err := releasebuildprofile.InspectOutputs(directory)
	require.Error(t, err)

	const permissions = 0o600

	err = os.WriteFile(
		filepath.Join(directory, "ksail"),
		[]byte("uninspected executable"),
		permissions,
	)
	require.NoError(t, err)
	_, err = releasebuildprofile.InspectOutputs(directory)
	require.Error(t, err)
}
