//go:build !windows

package environment_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/require"
)

// TestCloneEnvironmentConfig_SourceIsNonRegularFile pins the documented contract
// that the source must be a regular file: a FIFO (or any other non-regular node)
// that exists and is not a directory must still be rejected, so that a clone never
// flows a blocking node into cloneFile -> fsutil.ReadFileSafe.
func TestCloneEnvironmentConfig_SourceIsNonRegularFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	fifo := filepath.Join(repoRoot, "ksail.prod.yaml")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	rewrites := environment.DeriveConfigRewrites("prod", "staging", "", "")

	_, _, err := environment.CloneEnvironmentConfig(
		repoRoot, "ksail.prod.yaml", rewrites, false)
	require.ErrorIs(t, err, environment.ErrSourceConfigMissing)
}
