package image_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const multiPlatformImageName = "docker.io/library/multi:v1"

// multiPlatformArchive writes an image archive whose index advertises linux/amd64 and
// linux/arm64 for one named image. It omits the manifests of uncarried architectures
// and can leave a manifest present with a missing layer for one architecture.
func multiPlatformArchive(t *testing.T, missingLayerFor string, carried ...string) string {
	t.Helper()

	root := t.TempDir()
	store, err := layout.Write(root, empty.Index)
	require.NoError(t, err)

	index := v1.ImageIndex(empty.Index)

	var omittedBlobs []v1.Hash

	for _, architecture := range []string{"amd64", "arm64"} {
		img := fixtureImage(t, "multi-"+architecture)

		if !slices.Contains(carried, architecture) {
			digest, err := img.Digest()
			require.NoError(t, err)

			omittedBlobs = append(omittedBlobs, digest)
		} else if architecture == missingLayerFor {
			manifest, err := img.Manifest()
			require.NoError(t, err)
			require.NotEmpty(t, manifest.Layers)

			omittedBlobs = append(omittedBlobs, manifest.Layers[0].Digest)
		}

		index = mutate.AppendManifests(index, mutate.IndexAddendum{
			Add:      img,
			Platform: &v1.Platform{OS: "linux", Architecture: architecture},
		})
	}

	require.NoError(t, store.AppendIndex(index, layout.WithAnnotations(map[string]string{
		"io.containerd.image.name": multiPlatformImageName,
	})))

	for _, digest := range omittedBlobs {
		require.NoError(t, os.Remove(filepath.Join(root, "blobs", digest.Algorithm, digest.Hex)))
	}

	path := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(path, tarFixtureDirectory(t, root), 0o600))

	return path
}

// An export keeps the whole index but carries only its own platform. The export notice must
// name that image and the platform it carries, and stay quiet when every platform is carried.
func TestArchivePartialPlatformImages(t *testing.T) {
	t.Parallel()

	names, carried, err := image.ArchivePartialPlatformImagesForTest(
		multiPlatformArchive(t, "", "amd64"),
	)
	require.NoError(t, err)
	require.Equal(t, []string{multiPlatformImageName}, names)
	require.Equal(t, []string{"linux/amd64"}, carried)

	names, _, err = image.ArchivePartialPlatformImagesForTest(
		multiPlatformArchive(t, "", "amd64", "arm64"),
	)
	require.NoError(t, err)
	require.Empty(t, names)

	names, carried, err = image.ArchivePartialPlatformImagesForTest(
		multiPlatformArchive(t, "arm64", "amd64", "arm64"),
	)
	require.NoError(t, err)
	require.Equal(t, []string{multiPlatformImageName}, names)
	require.Equal(t, []string{"linux/amd64"}, carried)

	// A file that is not an OCI image layout archive has no multi-platform images to report.
	plain := filepath.Join(t.TempDir(), "plain.tar")
	require.NoError(t, os.WriteFile(plain, []byte("fake tar content"), 0o600))

	names, _, err = image.ArchivePartialPlatformImagesForTest(plain)
	require.NoError(t, err)
	require.Empty(t, names)
}

// An archive exported on amd64 nodes and imported on an arm64 node is refused before anything is
// copied, naming the image, instead of failing later inside ctr.
func TestImportRefusesANodePlatformTheArchiveDoesNotCarry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)

	client.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)
	optionalNodeExec(ctx, client, "my-cluster-control-plane", "platform",
		[]string{"uname", "-m"}, "aarch64\n")

	err := image.NewImporter(client).Import(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ImportOptions{InputPath: multiPlatformArchive(t, "", "amd64")})
	require.ErrorIs(t, err, image.ErrArchiveMissingPlatform)
	require.ErrorContains(t, err, multiPlatformImageName)
	require.ErrorContains(t, err, "linux/arm64")
}

// The same archive still imports on a node of the platform it carries. Its index is only partly
// present, so a local import is pinned to exactly that platform: the default matcher would also
// walk compatible platforms whose manifests the archive omits.
func TestImportAcceptsTheCarriedNodePlatform(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	node := "my-cluster-control-plane"

	client.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/" + node},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)
	repeatedNodeExec(ctx, client, node, "platform", []string{"uname", "-m"}, "x86_64\n")
	optionalNodeExec(ctx, client, node, "help", importHelpCommand(), localImportHelp)
	client.EXPECT().
		CopyToContainer(ctx, node, "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(nil).Once()
	optionalNodeExec(ctx, client, node, "import",
		[]string{
			ctrCommand, "--namespace=k8s.io", "images", "import", "--digests",
			"--local", "--platform", "linux/amd64", "/root/ksail-images-import.tar",
		}, "")
	optionalNodeExec(detachedCleanupContext(), client, node, "cleanup",
		[]string{"rm", "-f", "/root/ksail-images-import.tar"}, "")

	err := image.NewImporter(client).Import(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ImportOptions{InputPath: multiPlatformArchive(t, "", "amd64")})
	require.NoError(t, err)
}

// repeatedNodeExec answers command on node every time it runs, with a fresh output stream per
// run, for probes such as platform detection that an import may repeat.
func repeatedNodeExec(
	ctx context.Context,
	client *docker.MockAPIClient,
	node, operation string,
	command []string,
	stdout string,
) {
	execID := node + "-" + operation
	client.EXPECT().
		ContainerExecCreate(ctx, node, mock.MatchedBy(func(options container.ExecOptions) bool {
			return slices.Equal(options.Cmd, command)
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil)
	client.EXPECT().ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		RunAndReturn(func(context.Context, string, container.ExecStartOptions) (dockertypes.HijackedResponse, error) {
			return mockDockerStreamResponse(stdout, ""), nil
		})
	client.EXPECT().ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil)
}
