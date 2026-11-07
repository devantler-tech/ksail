package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestExportAllDeduplicatesReferencesAcrossNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	client.EXPECT().ContainerList(ctx, mock.Anything).Return(kindMultiNodeSummaries()[:2], nil)

	refs := []string{
		"docker.io/library/fixture-a:v1",
		"docker.io/library/shared:v1",
		"docker.io/library/fixture-b:v1",
	}
	setupNodeArchive(ctx, t, client, kindExporterNodeName, refs[0], refs[1])
	setupNodeArchive(ctx, t, client, "my-cluster-worker", refs[1], refs[2])
	output := filepath.Join(t.TempDir(), "images.tar")
	err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ExportOptions{OutputPath: output})
	require.NoError(t, err)
	require.ElementsMatch(t, refs, archiveNamedReferences(t, output))
	assertImageArchiveReadable(t, output, len(refs))

	// The shared reference is assigned once, so each node exports only its own operands.
	require.Equal(t, map[string][]string{
		kindExporterNodeName: {refs[0], refs[1]},
		"my-cluster-worker":  {refs[2]},
	}, exportOperandsByNode(client))
}

// exportOperandsByNode reads the image operands of every ctr export call the exporter made.
func exportOperandsByNode(client *docker.MockAPIClient) map[string][]string {
	const prefix = 9 // ctr --namespace=k8s.io images export --platform <p> --local --skip-manifest-json <path>

	operands := map[string][]string{}

	for _, call := range client.Calls {
		if call.Method != "ContainerExecCreate" {
			continue
		}

		options, _ := call.Arguments.Get(2).(container.ExecOptions)
		if len(options.Cmd) > prefix && options.Cmd[3] == "export" {
			node, _ := call.Arguments.Get(1).(string)
			operands[node] = append(operands[node], options.Cmd[prefix:]...)
		}
	}

	return operands
}

func TestExportRefusesIncompleteArchivesWithoutReplacingDestination(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"invalid archive", "missing reference", "missing layer"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			client.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(kindMultiNodeSummaries()[:2], nil)

			first := "docker.io/library/fixture-a:v1"
			second := "docker.io/library/fixture-b:v1"

			setupNodeArchive(ctx, t, client, kindExporterNodeName, first)
			archive := failedNodeArchive(t, failure, second)
			setupNodeArchiveContents(ctx, t, client, "my-cluster-worker", []string{second}, archive)
			directory := t.TempDir()
			root, err := os.OpenRoot(directory)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, root.Close()) })
			require.NoError(t, root.WriteFile("images.tar", []byte("existing export"), 0o600))

			err = image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker, image.ExportOptions{OutputPath: filepath.Join(directory, "images.tar")})
			require.Error(t, err)
			data, err := root.ReadFile("images.tar")
			require.NoError(t, err)
			require.Equal(t, "existing export", string(data))

			entries, err := os.ReadDir(directory)
			require.NoError(t, err)
			require.Len(t, entries, 1, "private staging content must be removed after refusal")
		})
	}
}

func failedNodeArchive(t *testing.T, failure, ref string) []byte {
	t.Helper()

	switch failure {
	case "invalid archive":
		return []byte("not an image archive")
	case "missing reference":
		return namedNodeArchive(t, "docker.io/library/other:v1")
	case "missing layer":
		return withoutFixtureLayer(t, namedNodeArchive(t, ref))
	default:
		t.Fatalf("unknown fixture %s", failure)

		return nil
	}
}

func withoutFixtureLayer(t *testing.T, data []byte) []byte {
	t.Helper()

	input := tar.NewReader(bytes.NewReader(data))

	var output bytes.Buffer

	writer := tar.NewWriter(&output)
	removed := false

	for {
		header, err := input.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)
		body, err := io.ReadAll(input)
		require.NoError(t, err)
		// The fixture layer is the only blob containing an uncompressed tar header.
		if bytes.Contains(body, []byte("payload.txt")) {
			removed = true

			continue
		}

		require.NoError(t, writer.WriteHeader(header))
		_, err = writer.Write(body)
		require.NoError(t, err)
	}

	require.True(t, removed, "negative control must remove the actual image layer")
	require.NoError(t, writer.Close())

	return output.Bytes()
}

// Read the resulting archive through go-containerregistry, independently from
// the containerd SDK used to merge it, including every config and layer body.
func assertImageArchiveReadable(t *testing.T, path string, count int) {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	file, err := root.Open(filepath.Base(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	directory := t.TempDir()
	unpackFixtureArchive(t, tar.NewReader(file), directory)
	index, err := layout.Path(directory).ImageIndex()
	require.NoError(t, err)
	manifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, count)
	assertFixtureIndexContent(t, index)
}

func assertFixtureIndexContent(t *testing.T, index v1.ImageIndex) {
	t.Helper()

	manifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.NotEmpty(t, manifest.Manifests)

	for _, descriptor := range manifest.Manifests {
		if descriptor.MediaType.IsIndex() {
			nested, indexErr := index.ImageIndex(descriptor.Digest)
			require.NoError(t, indexErr)
			assertFixtureIndexContent(t, nested)

			continue
		}

		img, imageErr := index.Image(descriptor.Digest)
		require.NoError(t, imageErr)
		_, imageErr = img.ConfigFile()
		require.NoError(t, imageErr)
		layers, imageErr := img.Layers()
		require.NoError(t, imageErr)
		require.NotEmpty(t, layers)

		for _, layer := range layers {
			reader, layerErr := layer.Uncompressed()
			require.NoError(t, layerErr)
			body, layerErr := io.ReadAll(reader)
			require.NoError(t, layerErr)
			require.NoError(t, reader.Close())
			require.Contains(t, string(body), "payload.txt")
		}
	}
}

func unpackFixtureArchive(t *testing.T, reader *tar.Reader, directory string) {
	t.Helper()

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			return
		}

		require.NoError(t, readErr)
		require.True(t, filepath.IsLocal(header.Name))

		if header.Typeflag == tar.TypeDir {
			require.NoError(t, root.MkdirAll(header.Name, 0o700))

			continue
		}

		require.Equal(t, byte(tar.TypeReg), header.Typeflag)
		require.NoError(t, root.MkdirAll(filepath.Dir(header.Name), 0o700))
		file, openErr := root.OpenFile(header.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require.NoError(t, openErr)

		_, copyErr := io.Copy(file, reader)
		require.NoError(t, copyErr)
		require.NoError(t, file.Close())
	}
}
