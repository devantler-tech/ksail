package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errListReferences = errors.New("fixture image listing failed")

const importedImageDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// writeIndexOnlyArchive writes an OCI image layout archive whose index lists one single-platform
// image, and returns its path and the digest of its index.json.
func writeIndexOnlyArchive(t *testing.T) (string, string) {
	t.Helper()

	index := []byte(`{"schemaVersion":2,"manifests":[{` +
		`"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"digest":"` + importedImageDigest + `","size":10,` +
		`"annotations":{"io.containerd.image.name":"docker.io/library/app:1"}}]}`)

	var buffer bytes.Buffer

	writer := tar.NewWriter(&buffer)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name: "index.json", Mode: 0o644, Size: int64(len(index)), Typeflag: tar.TypeReg,
	}))
	_, err := writer.Write(index)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	path := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(path, buffer.Bytes(), 0o600))

	sum := sha256.Sum256(index)

	return path, "sha256:" + hex.EncodeToString(sum[:])
}

type referenceCheckCase struct {
	name string
	// before and after are the node's image listings around the import; listErr fails the
	// listing before the import instead.
	before  string
	after   string
	listErr error
	wantErr []string
	// wantCopied is whether the archive reaches the node.
	wantCopied bool
}

func referenceCheckCases(rootDigest string) []referenceCheckCase {
	return []referenceCheckCase{
		{
			name:   "named images and their digest references are accepted",
			before: "registry.k8s.io/pause:3.10\n",
			after: "registry.k8s.io/pause:3.10\ndocker.io/library/app:1\n" +
				"import-2026-11-12@" + importedImageDigest + "\n" + importedImageDigest + "\n",
			wantCopied: true,
		},
		{
			name:   "a reference to the archive's own index fails and names it",
			before: "registry.k8s.io/pause:3.10\n",
			after: "registry.k8s.io/pause:3.10\ndocker.io/library/app:1\n" +
				"import-2026-11-12@" + rootDigest + "\n",
			wantErr: []string{
				image.ErrImportStoredUnknownReference.Error(),
				"import-2026-11-12@" + rootDigest,
				"the archive's own index " + rootDigest,
				kindExporterNodeName,
			},
			wantCopied: true,
		},
		{
			name:   "a digest reference to other content fails",
			before: "",
			after: "docker.io/library/app:1\n" +
				"import-2026-11-12@sha256:2222222222222222222222222222222222222222222222222222222222222222\n",
			wantErr: []string{
				image.ErrImportStoredUnknownReference.Error(),
				"content outside the archive index " + rootDigest,
			},
			wantCopied: true,
		},
		{
			name:   "bare image IDs recorded by the runtime are not judged",
			before: "",
			after: "docker.io/library/app:1\n" +
				"sha256:3333333333333333333333333333333333333333333333333333333333333333\n",
			wantCopied: true,
		},
		{
			name:       "a reference that existed before the import is not attributed to it",
			before:     "old@" + rootDigest + "\n",
			after:      "old@" + rootDigest + "\ndocker.io/library/app:1\n",
			wantCopied: true,
		},
		{
			name:       "an unreadable listing refuses before anything is copied",
			listErr:    errListReferences,
			wantErr:    []string{"check image references before import"},
			wantCopied: false,
		},
	}
}

// An import of an OCI archive may add only the archive's images. Any other new digest reference
// fails the import with the reference, the node and the archive index it came from, so a later
// export failure on that reference is attributable.
func TestImportRefusesReferencesOutsideTheArchive(t *testing.T) {
	t.Parallel()

	inputPath, rootDigest := writeIndexOnlyArchive(t)

	for _, testCase := range referenceCheckCases(rootDigest) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockClient := docker.NewMockAPIClient(t)
			setupKindNodeListMock(ctx, mockClient)

			var copied atomic.Bool

			observeReferenceCheckNode(t, mockClient, testCase, &copied)

			err := image.NewImporter(mockClient).Import(ctx, "my-cluster",
				v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker,
				image.ImportOptions{InputPath: inputPath})

			if len(testCase.wantErr) == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)

				for _, want := range testCase.wantErr {
					assert.Contains(t, err.Error(), want)
				}
			}

			assert.Equal(t, testCase.wantCopied, copied.Load())
		})
	}
}

// An input that is not an OCI image layout keeps the plain import, without listing references.
func TestImportWithoutArchiveIndexSkipsReferenceCheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	setupKindNodeListMock(ctx, mockClient)

	copied := false
	commands := observeImportNode(t, mockClient, kindExporterNodeName,
		importModeCase{help: legacyImportHelp, wantImport: legacyImportCommand()}, &copied)

	inputPath := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(inputPath, []byte("fake tar content"), 0o600))

	err := image.NewImporter(mockClient).Import(ctx, "my-cluster",
		v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath})
	require.NoError(t, err)

	for _, command := range drain(commands) {
		assert.NotEqual(t, listReferencesCommand(), command)
	}
}

func listReferencesCommand() []string {
	return []string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"}
}

// observeReferenceCheckNode answers the commands of one legacy-mode import on the Kind node: the
// capability probe, the reference listings before and after, the import and the cleanup.
func observeReferenceCheckNode(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	testCase referenceCheckCase,
	copied *atomic.Bool,
) {
	t.Helper()

	node := kindExporterNodeName
	outputs := map[string]string{
		"help":        legacyImportHelp,
		"list-before": testCase.before,
		"list-after":  testCase.after,
	}

	mockClient.EXPECT().
		CopyToContainer(mock.Anything, node, "/root", mock.Anything, container.CopyToContainerOptions{}).
		RunAndReturn(func(context.Context, string, string, io.Reader, container.CopyToContainerOptions) error {
			copied.Store(true)

			return nil
		}).
		Maybe()

	mockClient.EXPECT().
		ContainerExecCreate(mock.Anything, node, mock.Anything).
		RunAndReturn(referenceCheckExecIDs(t, testCase))

	for _, execID := range []string{"help", "list-before", "list-after", "other"} {
		mockClient.EXPECT().
			ContainerExecAttach(mock.Anything, execID, container.ExecStartOptions{}).
			Return(mockDockerStreamResponse(outputs[execID], ""), nil).Maybe()
		mockClient.EXPECT().
			ContainerExecInspect(mock.Anything, execID).
			Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()
	}
}

// referenceCheckExecIDs names each command's exec so its output can be answered: the first
// reference listing is the one before the import, the second the one after.
func referenceCheckExecIDs(
	t *testing.T,
	testCase referenceCheckCase,
) func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
	t.Helper()

	var listings atomic.Int32

	return func(
		_ context.Context, _ string, options container.ExecOptions,
	) (container.ExecCreateResponse, error) {
		switch {
		case slices.Equal(options.Cmd, importHelpCommand()):
			return container.ExecCreateResponse{ID: "help"}, nil
		case slices.Equal(options.Cmd, listReferencesCommand()):
			if listings.Add(1) > 1 {
				return container.ExecCreateResponse{ID: "list-after"}, nil
			}

			if testCase.listErr != nil {
				return container.ExecCreateResponse{}, testCase.listErr
			}

			return container.ExecCreateResponse{ID: "list-before"}, nil
		case slices.Equal(options.Cmd, legacyImportCommand()),
			slices.Equal(options.Cmd, []string{"rm", "-f", kindImportPath}):
			return container.ExecCreateResponse{ID: "other"}, nil
		default:
			t.Errorf("unexpected command %v", options.Cmd)

			return container.ExecCreateResponse{}, errListReferences
		}
	}
}
