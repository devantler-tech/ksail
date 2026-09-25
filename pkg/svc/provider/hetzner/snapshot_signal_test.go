//go:build unix

package hetzner_test

import (
	"context"
	"os"
	"syscall"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A signal during a build must cancel it and delete its temporary resources, instead of
// ending the process with the server still running. The signal really is sent to this
// process: without a handler in place it ends the test binary.
//
//nolint:paralleltest // Signals are process-wide, so a parallel build would receive them too.
func TestSnapshotManager_EnsureTalosSnapshot_SignalDeletesBuildResources(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			uploader := newStuckUploader(t, func() {
				assert.NoError(t, syscall.Kill(os.Getpid(), signal))
			})
			api := &buildResourcesAPI{listedBuildID: uploader.buildID}
			client := newBuildResourcesAPI(t, api)
			manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

			err := ensureTalosSnapshotWithin(t.Context(), t, manager)

			require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
			require.ErrorIs(t, err, context.Canceled)
			assert.ElementsMatch(t, []string{buildServerPath, buildSSHKeyPath}, api.deletedPaths())
		})
	}
}
