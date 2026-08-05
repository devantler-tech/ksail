package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// corruptOCITar returns bytes that parse as a tar archive for at least one entry and then
// break, which is what ctr export silently produces when the containerd content store is
// incomplete. A wholly unparseable blob would not do: ValidateExportedTar deliberately
// skips validation when the very first header fails, so the truncation must come after a
// good entry.
func corruptOCITar(t *testing.T) []byte {
	t.Helper()

	payload := []byte("oci-layout")

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	err := tarWriter.WriteHeader(&tar.Header{
		Name:     "oci-layout",
		Mode:     0o644,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tarWriter.Write(payload)
	require.NoError(t, err)

	// Flush completes the entry without writing the end-of-archive marker, so the
	// following bytes are read as the start of a second header.
	err = tarWriter.Flush()
	require.NoError(t, err)

	// A partial header block: enough to start reading, too short to finish.
	buf.Write(bytes.Repeat([]byte{'A'}, 200))

	return buf.Bytes()
}

// validOCITar returns a well-formed archive that passes integrity validation.
func validOCITar(t *testing.T) []byte {
	t.Helper()

	payload := []byte("oci-layout")

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	err := tarWriter.WriteHeader(&tar.Header{
		Name:     "oci-layout",
		Mode:     0o644,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tarWriter.Write(payload)
	require.NoError(t, err)

	require.NoError(t, tarWriter.Close())

	return buf.Bytes()
}

// wrapInDockerCopyTar wraps content the way CopyFromContainer delivers it: a tar stream
// whose single regular entry is the file being copied.
func wrapInDockerCopyTar(t *testing.T, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	err := tarWriter.WriteHeader(&tar.Header{
		Name:     "ksail-images-export.tar",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	require.NoError(t, tarWriter.Close())

	return buf.Bytes()
}

// allowAnyExec makes every ContainerExec* call succeed with empty output, any number of
// times. The retry path issues a variable number of repair commands (images rm, pull,
// content fetch, re-export, cleanup); this test pins the observable outcome and the
// export-command count rather than an exact transcript of every exec.
func allowAnyExec(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ContainerExecCreate(ctx, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{ID: "exec-any"}, nil
		})

	mockClient.EXPECT().
		ContainerExecAttach(ctx, mock.Anything, container.ExecStartOptions{}).
		RunAndReturn(func(_ context.Context, _ string, _ container.ExecStartOptions) (dockertypes.HijackedResponse, error) {
			return mockDockerStreamResponse("", ""), nil
		})

	mockClient.EXPECT().
		ContainerExecInspect(ctx, mock.Anything).
		Return(container.ExecInspect{ExitCode: 0}, nil)
}

func setupSingleNodeList(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)
}

// TestExportRepairsAndRetriesAfterIntegrityFailure pins the fix: ctr export exits zero but
// writes a truncated archive, so the only signal is the integrity validation. That must
// reach the same content-refresh repair the non-zero-exit path already uses, and the
// re-export must succeed.
func TestExportRepairsAndRetriesAfterIntegrityFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	outputPath := filepath.Join(t.TempDir(), "images.tar")

	setupSingleNodeList(ctx, mockClient)
	allowAnyExec(ctx, mockClient)

	// First copy delivers the silently-truncated archive, second the repaired one.
	corrupt := wrapInDockerCopyTar(t, corruptOCITar(t))
	repaired := wrapInDockerCopyTar(t, validOCITar(t))

	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(corrupt)), container.PathStat{}, nil).Once()

	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(repaired)), container.PathStat{}, nil).Once()

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.NoError(t, err, "a repairable content-store truncation must not fail the export")

	// Both copies must have been consumed — that is what proves the retry ran rather than
	// the first archive somehow passing validation.
	mockClient.AssertExpectations(t)
}

// TestExportFailsWhenIntegrityStillBrokenAfterRepair pins the terminal case: the repair is
// attempted once and, when the content store does not recover, the command fails rather
// than looping.
func TestExportFailsWhenIntegrityStillBrokenAfterRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	outputPath := filepath.Join(t.TempDir(), "images.tar")

	setupSingleNodeList(ctx, mockClient)
	allowAnyExec(ctx, mockClient)

	corrupt := wrapInDockerCopyTar(t, corruptOCITar(t))

	// Exactly twice: the initial copy and the single post-repair retry. A third call would
	// fail the mock, which is the assertion that the retry does not loop.
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		RunAndReturn(func(_ context.Context, _ string, _ string) (io.ReadCloser, container.PathStat, error) {
			return io.NopCloser(bytes.NewReader(corrupt)), container.PathStat{}, nil
		}).
		Twice()

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "blob integrity check failed")
	assert.Contains(
		t,
		err.Error(),
		"persisted after content repair",
		"the error must say the repair was attempted, so the next reader is not sent hunting for a cause already ruled out",
	)

	mockClient.AssertExpectations(t)
}
