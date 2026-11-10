package image_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// exportOperands records the image operands each node's export command named.
type exportOperands struct {
	mu     sync.Mutex
	byNode map[string][]string
}

func (operands *exportOperands) record(node string, refs []string) {
	operands.mu.Lock()
	defer operands.mu.Unlock()

	operands.byNode[node] = append(operands.byNode[node], refs...)
}

func (operands *exportOperands) of(node string) []string {
	operands.mu.Lock()
	defer operands.mu.Unlock()

	return slices.Clone(operands.byNode[node])
}

// setupOperandNodeArchive makes node hold held, and answers its export with an
// archive of exactly the references the export command named, so an export that
// asks a node for more than it should is visible in the archive.
func setupOperandNodeArchive(
	ctx context.Context,
	t *testing.T,
	client *docker.MockAPIClient,
	node string,
	held []string,
	operands *exportOperands,
) {
	t.Helper()

	optionalNodeExec(ctx, client, node, "list",
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		strings.Join(held, "\n")+"\n")
	optionalNodeExec(ctx, client, node, "platform", []string{"uname", "-m"}, "x86_64\n")
	optionalNodeExec(ctx, client, node, "help",
		[]string{ctrCommand, "--namespace=k8s.io", "images", "export", "--help"},
		"OPTIONS:\n   --local  Use client-side export\n   --help, -h  Show help\n")

	exportPrefix := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export", "--platform", "linux/amd64",
		"--local", "--skip-manifest-json", kindExporterTarPath,
	}

	client.EXPECT().
		ContainerExecCreate(ctx, node, mock.MatchedBy(func(options container.ExecOptions) bool {
			return len(options.Cmd) > len(exportPrefix) &&
				slices.Equal(options.Cmd[:len(exportPrefix)], exportPrefix)
		})).
		RunAndReturn(func(
			_ context.Context, _ string, options container.ExecOptions,
		) (container.ExecCreateResponse, error) {
			operands.record(node, options.Cmd[len(exportPrefix):])

			return container.ExecCreateResponse{ID: node + "-export"}, nil
		}).
		Maybe()
	client.EXPECT().ContainerExecAttach(ctx, node+"-export", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Maybe()
	client.EXPECT().ContainerExecInspect(ctx, node+"-export").
		Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()

	client.EXPECT().CopyFromContainer(ctx, node, kindExporterTarPath).
		RunAndReturn(func(context.Context, string, string) (io.ReadCloser, container.PathStat, error) {
			archive := createExportTar(t, namedNodeArchive(t, operands.of(node)...))

			return io.NopCloser(bytes.NewReader(archive)), container.PathStat{}, nil
		}).
		Maybe()
	optionalNodeExec(detachedCleanupContext(), client, node, "cleanup",
		[]string{"rm", "-f", kindExporterTarPath}, "")
}

// A selected export that asks each node for a proper subset of what it holds
// must name only the requested images, both in the archive and in the
// operands sent to each node, so exporting every image a node holds cannot pass.
func TestExportSelectsAProperSubsetOfEachNodesImages(t *testing.T) {
	t.Parallel()

	const (
		wantedOnControlPlane = "docker.io/library/fixture-a:v1"
		unwantedOnControl    = "docker.io/library/fixture-c:v1"
		wantedOnWorker       = "docker.io/library/fixture-b:v1"
		unwantedOnWorker     = "docker.io/library/fixture-d:v1"
		workerNodeName       = "my-cluster-worker"
	)

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	operands := &exportOperands{byNode: make(map[string][]string)}

	client.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(kindMultiNodeSummaries()[:2], nil)

	setupOperandNodeArchive(ctx, t, client, kindExporterNodeName,
		[]string{wantedOnControlPlane, unwantedOnControl}, operands)
	setupOperandNodeArchive(ctx, t, client, workerNodeName,
		[]string{wantedOnWorker, unwantedOnWorker}, operands)

	client.EXPECT().ContainerExecCreate(ctx, mock.Anything, mock.Anything).
		Return(container.ExecCreateResponse{}, fmt.Errorf("fixture image unavailable on this node: %w", os.ErrNotExist)).
		Maybe()

	output := filepath.Join(t.TempDir(), "images.tar")

	err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ExportOptions{
			OutputPath: output,
			Images:     []string{wantedOnControlPlane, wantedOnWorker},
		})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantedOnControlPlane, wantedOnWorker},
		archiveNamedReferences(t, output))
	assertImageArchiveReadable(t, output, 2)
	require.Equal(t, []string{wantedOnControlPlane}, operands.of(kindExporterNodeName))
	require.Equal(t, []string{wantedOnWorker}, operands.of(workerNodeName))
}
