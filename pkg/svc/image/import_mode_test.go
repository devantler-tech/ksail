package image_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errImportDiscovery = errors.New("fixture import capability discovery failed")

type importModeCase struct {
	name    string
	help    string
	failure error
	local   bool
	wantErr string
}

func TestImportChoosesNodeSupportedModeAndRefusesUnknownCapabilities(t *testing.T) {
	t.Parallel()

	for _, testCase := range []importModeCase{
		{name: "modern", help: "OPTIONS:\n --local  Import locally\n --help, -h  Show help\n", local: true},
		{name: "bare-help", help: "OPTIONS:\n --local  Import locally\n --help  Show help\n", local: true},
		{name: "legacy", help: "OPTIONS:\n --digests  Create digest refs\n --help, -h  Show help\n"},
		{name: "unrelated", help: "OPTIONS:\n --localization value  Language\n --help, -h  Show help\n"},
		{name: "failed", failure: errImportDiscovery, wantErr: "fixture import capability discovery failed"},
		{name: "empty", wantErr: "incomplete help output"},
		{name: "truncated", help: "OPTIONS:\n --local  Import locally\n", wantErr: "incomplete help output"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			setupKindNodeListMock(ctx, client)
			commands := observeImportModeNode(ctx, t, client, kindExporterNodeName, testCase)
			path := filepath.Join(t.TempDir(), "images.tar")
			require.NoError(
				t,
				os.WriteFile(path, []byte("archive copied at the Docker boundary"), 0o600),
			)

			err := image.NewImporter(client).Import(ctx, "my-cluster", v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker, image.ImportOptions{InputPath: path})
			if testCase.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.wantErr)
			}

			assertImportModeCommands(t, commands, testCase)
		})
	}
}

func TestImportChoosesModeIndependentlyForMixedVersionNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	client.EXPECT().ContainerList(ctx, mock.Anything).Return(kindMultiNodeSummaries()[:2], nil)

	cases := []importModeCase{
		{help: "OPTIONS:\n --digests  Create refs\n --help, -h  Show help\n"},
		{help: "OPTIONS:\n --local  Import locally\n --help, -h  Show help\n", local: true},
	}
	nodes := []string{kindExporterNodeName, "my-cluster-worker"}

	observed := make([]chan []string, len(nodes))
	for index, node := range nodes {
		observed[index] = observeImportModeNode(ctx, t, client, node, cases[index])
	}

	path := filepath.Join(t.TempDir(), "mixed.tar")
	require.NoError(t, os.WriteFile(path, []byte("mixed-version boundary fixture"), 0o600))
	err := image.NewImporter(client).Import(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ImportOptions{InputPath: path})
	require.NoError(t, err)

	for index, commands := range observed {
		assertImportModeCommands(t, commands, cases[index])
	}
}

func assertImportModeCommands(t *testing.T, commands chan []string, testCase importModeCase) {
	t.Helper()

	want := [][]string{importHelpCommand()}

	if testCase.wantErr == "" {
		command := []string{ctrCommand, "--namespace=k8s.io", "images", "import", "--digests"}
		if testCase.local {
			command = append(command, "--local")
		}

		want = append(want, append(command, "/root/ksail-images-import.tar"))
	}

	want = append(want, []string{"rm", "-f", "/root/ksail-images-import.tar"})

	close(commands)

	var observed [][]string
	for command := range commands {
		observed = append(observed, command)
	}

	require.Equal(t, want, observed)
}

func importHelpCommand() []string {
	return []string{ctrCommand, "--namespace=k8s.io", "images", "import", "--help"}
}

func observeImportModeNode(
	ctx context.Context,
	t *testing.T,
	client *docker.MockAPIClient,
	node string,
	testCase importModeCase,
) chan []string {
	t.Helper()

	commands := make(chan []string, 8)

	client.EXPECT().
		CopyToContainer(ctx, node, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Once()
	client.EXPECT().ContainerExecCreate(ctx, node, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			commands <- slices.Clone(options.Cmd)

			execID := "import-" + node
			if slices.Equal(options.Cmd, importHelpCommand()) {
				execID = "help-" + node

				if testCase.failure != nil {
					return container.ExecCreateResponse{}, testCase.failure
				}
			} else if slices.Equal(options.Cmd, []string{"rm", "-f", "/root/ksail-images-import.tar"}) {
				execID = "cleanup-" + node
			}

			return container.ExecCreateResponse{ID: execID}, nil
		})

	for _, phase := range []string{"help-", "import-", "cleanup-"} {
		stdout := ""
		if phase == "help-" {
			stdout = testCase.help
		}

		client.EXPECT().ContainerExecAttach(ctx, phase+node, container.ExecStartOptions{}).
			Return(mockDockerStreamResponse(stdout, ""), nil).Maybe()
		client.EXPECT().ContainerExecInspect(ctx, phase+node).
			Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()
	}

	return commands
}
