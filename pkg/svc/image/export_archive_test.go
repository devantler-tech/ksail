package image_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/mock"
)

var errStopAfterCleanupCreate = errors.New("stop after exec create")

// Two exports on the same node must never share an archive, so every
// generated name is distinct.
func TestDefaultExportArchiveNamesAreDistinct(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^ksail-images-export-[0-9a-f]{16}\.tar$`)
	seen := make(map[string]struct{})

	for range 64 {
		name := image.DefaultExportArchiveNameForTest()
		if !pattern.MatchString(name) {
			t.Fatalf("archive name %q does not match %s", name, pattern)
		}

		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("archive name %q was generated twice", name)
		}

		seen[name] = struct{}{}
	}
}

// A cancelled export must still remove its archive from the node, so the
// removal runs on a live context even after the caller's context is done.
func TestRemoveNodeArchiveRunsAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const archive = "/root/ksail-images-export-0123456789abcdef.tar"

	client := docker.NewMockAPIClient(t)
	client.EXPECT().
		ContainerExecCreate(
			mock.MatchedBy(func(cleanupCtx context.Context) bool { return cleanupCtx.Err() == nil }),
			kindExporterNodeName,
			mock.MatchedBy(func(opts container.ExecOptions) bool {
				return slices.Equal(opts.Cmd, []string{"rm", "-f", archive})
			}),
		).
		Return(container.ExecCreateResponse{ID: "exec-cleanup"}, nil).Once()
	client.EXPECT().
		ContainerExecAttach(mock.Anything, "exec-cleanup", mock.Anything).
		Return(types.HijackedResponse{}, errStopAfterCleanupCreate).Once()

	image.RemoveNodeArchiveForTest(image.NewExporter(client), ctx, kindExporterNodeName, archive)
}
