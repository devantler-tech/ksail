package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Each node holds a different real image archive. Returning just the preferred
// node's valid archive must not satisfy either cluster-wide export mode.
func TestExportCombinesImagesHeldByDifferentNodes(t *testing.T) {
	t.Parallel()

	for _, selected := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "all images", true: "selected images"}[selected],
			func(t *testing.T) {
				t.Parallel()

				ctx := context.Background()
				client := docker.NewMockAPIClient(t)
				refs := []string{"docker.io/library/fixture-a:v1", "docker.io/library/fixture-b:v1"}

				client.EXPECT().
					ContainerList(ctx, mock.Anything).
					Return(kindMultiNodeSummaries()[:2], nil)

				for index, node := range []string{kindExporterNodeName, "my-cluster-worker"} {
					setupNodeArchive(ctx, t, client, node, refs[index])
				}

				client.EXPECT().ContainerExecCreate(ctx, mock.Anything, mock.Anything).
					Return(container.ExecCreateResponse{}, fmt.Errorf("fixture image unavailable on this node: %w", os.ErrNotExist)).
					Maybe()

				output := filepath.Join(t.TempDir(), "images.tar")

				options := image.ExportOptions{OutputPath: output}
				if selected {
					options.Images = append(slices.Clone(refs), refs[0], "fixture-a:v1")
				}

				err := image.NewExporter(client).
					Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
						v1alpha1.ProviderDocker, options)
				require.NoError(t, err)
				require.ElementsMatch(t, refs, archiveNamedReferences(t, output))
				assertImageArchiveReadable(t, output, len(refs))
			},
		)
	}
}

// A failed inventory cannot establish the union, even when another node has
// valid images. Refusal must preserve an existing destination archive.
func TestExportAllRefusesUnreadableNodeInventory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	client.EXPECT().ContainerList(ctx, mock.Anything).Return(kindMultiNodeSummaries()[:2], nil)
	setupNodeArchive(ctx, t, client, kindExporterNodeName, "docker.io/library/fixture-a:v1")
	client.EXPECT().ContainerExecCreate(ctx, "my-cluster-worker", mock.Anything).
		Return(container.ExecCreateResponse{}, fmt.Errorf("fixture inventory unavailable: %w", os.ErrPermission)).
		Maybe()

	output := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(output, []byte("existing export"), 0o600))
	err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ExportOptions{OutputPath: output})
	require.ErrorContains(t, err, "fixture inventory unavailable")

	root, rootErr := os.OpenRoot(filepath.Dir(output))
	require.NoError(t, rootErr)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	contents, readErr := root.ReadFile("images.tar")
	require.NoError(t, readErr)
	require.Equal(t, "existing export", string(contents))
}

// Docker is the external boundary; selection, export collection and archive
// processing remain real. Only refs actually held by this node can be exported.
func setupNodeArchive(
	ctx context.Context,
	t *testing.T,
	client *docker.MockAPIClient,
	node string,
	refs ...string,
) {
	t.Helper()
	setupNodeArchiveContents(ctx, t, client, node, refs, namedNodeArchive(t, refs...))
}

func setupNodeArchiveContents(
	ctx context.Context,
	t *testing.T,
	client *docker.MockAPIClient,
	node string,
	refs []string,
	archive []byte,
) {
	t.Helper()
	optionalNodeExec(
		ctx,
		client,
		node,
		"list",
		[]string{
			ctrCommand,
			"--namespace=k8s.io",
			"images",
			"list",
			"-q",
		},
		strings.Join(refs, "\n")+"\n",
	)
	optionalNodeExec(ctx, client, node, "platform", []string{"uname", "-m"}, "x86_64\n")
	optionalNodeExec(ctx, client, node, "help",
		[]string{ctrCommand, "--namespace=k8s.io", "images", "export", "--help"},
		"OPTIONS:\n   --local  Use client-side export\n   --help, -h  Show help\n")
	optionalNodeExport(ctx, client, node, refs)

	contents := createExportTar(t, archive)

	client.EXPECT().CopyFromContainer(ctx, node, kindExporterTarPath).
		RunAndReturn(func(context.Context, string, string) (io.ReadCloser, container.PathStat, error) {
			return io.NopCloser(bytes.NewReader(contents)), container.PathStat{}, nil
		}).
		Maybe()
	optionalNodeExec(ctx, client, node, "cleanup", []string{"rm", "-f", kindExporterTarPath}, "")
}

func optionalNodeExport(
	ctx context.Context,
	client *docker.MockAPIClient,
	node string,
	refs []string,
) {
	exportPrefix := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export", "--platform", "linux/amd64",
		"--local", kindExporterTarPath,
	}

	client.EXPECT().
		ContainerExecCreate(ctx, node, mock.MatchedBy(func(options container.ExecOptions) bool {
			if len(options.Cmd) <= len(exportPrefix) ||
				!slices.Equal(options.Cmd[:len(exportPrefix)], exportPrefix) {
				return false
			}

			for _, ref := range options.Cmd[len(exportPrefix):] {
				if !slices.Contains(refs, ref) {
					return false
				}
			}

			return true
		})).
		Return(container.ExecCreateResponse{ID: node + "-export"}, nil).
		Maybe()
	client.EXPECT().ContainerExecAttach(ctx, node+"-export", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Maybe()
	client.EXPECT().ContainerExecInspect(ctx, node+"-export").
		Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()
}

// Each protocol operation has a separate stream. Optional calls let the old
// one-node exporter reach the real archive assertion without mock-count noise.
func optionalNodeExec(
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
		Return(container.ExecCreateResponse{ID: execID}, nil).
		Maybe()
	client.EXPECT().ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse(stdout, ""), nil).Maybe()
	client.EXPECT().ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()
}

// Build input with a separate OCI-layout implementation and an actual tar
// layer, so a successful export cannot be a non-image placeholder byte stream.
func namedNodeArchive(t *testing.T, refs ...string) []byte {
	t.Helper()
	root := t.TempDir()
	store, err := layout.Write(root, empty.Index)
	require.NoError(t, err)

	for _, ref := range refs {
		appendNamedFixtureImage(t, store, ref)
	}

	return tarFixtureDirectory(t, root)
}

func appendNamedFixtureImage(t *testing.T, store layout.Path, ref string) {
	t.Helper()
	img := fixtureImage(t, ref)
	require.NoError(t, store.AppendImage(img, layout.WithAnnotations(map[string]string{
		"io.containerd.image.name": ref, "org.opencontainers.image.ref.name": "v1",
	})))
}

func fixtureImage(t *testing.T, payload string) v1.Image {
	t.Helper()

	var layer bytes.Buffer

	layerWriter := tar.NewWriter(&layer)
	require.NoError(
		t,
		layerWriter.WriteHeader(
			&tar.Header{Name: "payload.txt", Mode: 0o644, Size: int64(len(payload))},
		),
	)
	_, err := layerWriter.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, layerWriter.Close())

	img, err := mutate.AppendLayers(
		empty.Image,
		static.NewLayer(layer.Bytes(), types.OCIUncompressedLayer),
	)
	require.NoError(t, err)

	return img
}

func TestExportResolvesContainerdImportAliases(t *testing.T) {
	t.Parallel()

	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "selected"}[selected], func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			client.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(kindMultiNodeSummaries()[:2], nil)

			refs := make([]string, 0, 2)
			rawRefs := make([]string, 0, 2)

			for index, node := range []string{kindExporterNodeName, "my-cluster-worker"} {
				ref, archive := importAliasFixture(t, fmt.Sprintf("imported image %d", index))
				setupNodeArchiveContents(
					ctx,
					t,
					client,
					node,
					[]string{ref},
					archive,
				)
				refs = append(refs, image.NormalizeImageRef(ref))
				rawRefs = append(rawRefs, ref)
			}

			client.EXPECT().ContainerExecCreate(ctx, mock.Anything, mock.Anything).
				Return(container.ExecCreateResponse{}, fmt.Errorf("unknown raw fixture operand: %w", os.ErrNotExist)).
				Maybe()

			output := filepath.Join(t.TempDir(), "images.tar")

			options := image.ExportOptions{OutputPath: output}
			if selected {
				options.Images = slices.Clone(refs)
			}

			err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker, options)
			require.NoError(t, err)

			assertNodeRawExports(ctx, t, client, rawRefs)

			actual := archiveNamedReferences(t, output)
			for index, ref := range actual {
				actual[index] = image.NormalizeImageRef(ref)
			}

			require.ElementsMatch(t, refs, actual)
			assertImageArchiveReadable(t, output, len(refs))
		})
	}
}

func assertNodeRawExports(
	ctx context.Context,
	t *testing.T,
	client *docker.MockAPIClient,
	refs []string,
) {
	t.Helper()
	require.Len(t, refs, 2)

	for index, node := range []string{kindExporterNodeName, "my-cluster-worker"} {
		client.AssertCalled(
			t,
			"ContainerExecCreate",
			ctx,
			node,
			mock.MatchedBy(func(options container.ExecOptions) bool {
				return slices.Equal(options.Cmd, buildKindCtrExportCommand(refs[index]))
			}),
		)
	}
}

func importAliasFixture(t *testing.T, payload string) (string, []byte) {
	t.Helper()
	img := fixtureImage(t, payload)
	digest, err := img.Digest()
	require.NoError(t, err)

	ref := "import-2026-08-26@" + digest.String()
	store, err := layout.Write(t.TempDir(), empty.Index)
	require.NoError(t, err)
	require.NoError(t, store.AppendImage(img, layout.WithAnnotations(map[string]string{
		"io.containerd.image.name": ref, "org.opencontainers.image.ref.name": "v1",
	})))

	return ref, tarFixtureDirectory(t, string(store))
}

func TestExportImportAliasRefusesWrongRepositoryAndDigest(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"different-repository", "different-digest"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			client.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(kindMultiNodeSummaries()[:2], nil)

			img := fixtureImage(t, "available import")
			digest, err := img.Digest()
			require.NoError(t, err)

			raw := "import-2026-08-26@" + digest.String()
			for _, node := range []string{kindExporterNodeName, "my-cluster-worker"} {
				optionalNodeExec(
					ctx,
					client,
					node,
					"list",
					[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
					raw+"\n",
				)
			}

			requested := "docker.io/library/another-import@" + digest.String()

			if name == "different-digest" {
				other, otherErr := fixtureImage(t, "unavailable import").Digest()
				require.NoError(t, otherErr)

				requested = "docker.io/library/import-2026-08-26@" + other.String()
			}

			output := filepath.Join(t.TempDir(), "images.tar")
			require.NoError(t, os.WriteFile(output, []byte("existing export"), 0o600))
			err = image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker, image.ExportOptions{OutputPath: output, Images: []string{requested}})
			require.ErrorContains(t, err, "unavailable references")

			root, rootErr := os.OpenRoot(filepath.Dir(output))
			require.NoError(t, rootErr)
			t.Cleanup(func() { require.NoError(t, root.Close()) })

			contents, readErr := root.ReadFile("images.tar")
			require.NoError(t, readErr)
			require.Equal(t, "existing export", string(contents))
		})
	}
}

func tarFixtureDirectory(t *testing.T, root string) []byte {
	t.Helper()

	var output bytes.Buffer

	writer := tar.NewWriter(&output)
	err := fs.WalkDir(
		os.DirFS(root),
		".",
		func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				return nil
			}

			contents, readErr := fs.ReadFile(os.DirFS(root), name)
			if readErr != nil {
				return fmt.Errorf("read fixture entry: %w", readErr)
			}

			headerErr := writer.WriteHeader(
				&tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))},
			)
			if headerErr != nil {
				return fmt.Errorf("write fixture header: %w", headerErr)
			}

			_, writeErr := writer.Write(contents)
			if writeErr != nil {
				return fmt.Errorf("write fixture content: %w", writeErr)
			}

			return nil
		},
	)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return output.Bytes()
}

// Read named top-level references independently from the exporter and its
// future merge helper, rather than calculating expected results with that code.
func archiveNamedReferences(t *testing.T, path string) []string {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	file, err := root.Open(filepath.Base(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	reader := tar.NewReader(file)
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			t.Fatal("export has no OCI index")
		}

		require.NoError(t, readErr)

		if header.Name != "index.json" {
			continue
		}

		var index struct {
			Manifests []struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"manifests"`
		}
		require.NoError(t, json.NewDecoder(reader).Decode(&index))

		refs := make([]string, 0, len(index.Manifests))
		for _, manifest := range index.Manifests {
			refs = append(refs, manifest.Annotations["io.containerd.image.name"])
		}

		return refs
	}
}
