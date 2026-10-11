package image_test

import (
	"context"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/stretchr/testify/require"
)

func TestExportSelectsSynchronousRuntimeMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		help  string
		local bool
	}{
		{"containerd-1.6", "OPTIONS:\n   --platform value  Export this platform\n   --help, -h  Show help\n", false},
		{"containerd-1.7", "OPTIONS:\n   --local  Use client-side export (default: true)\n   --help, -h  Show help\n", true},
		{"containerd-2.x", "OPTIONS:\n   --local  Use client-side export (default: false)\n   --help, -h  Show help\n", true},
		{"unrelated-option", "OPTIONS:\n   --localization value  Output language\n   --help, -h  Show help\n", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			setupKindNodeListMock(ctx, client)
			setupEmptyImageListMockForExporter(ctx, t, client, kindExporterNodeName)
			setupExecMockWithStdoutForExporter(ctx, t, client, kindExporterNodeName,
				[]string{"uname", "-m"}, "x86_64\n")
			setupExecMockWithStdoutForExporter(
				ctx,
				t,
				client,
				kindExporterNodeName,
				[]string{
					ctrCommand,
					"--namespace=k8s.io",
					"images",
					"export",
					"--help",
				},
				testCase.help,
			)

			command := []string{
				ctrCommand,
				"--namespace=k8s.io",
				"images",
				"export",
				"--platform",
				"linux/amd64",
			}
			if testCase.local {
				command = append(command, "--local")
			}

			command = append(command, kindExporterTarPath, "docker.io/library/nginx:latest")
			setupExecMockWithCmdForExporter(ctx, t, client, kindExporterNodeName, command)
			expectCopiedExportTar(ctx, t, client)
			setupExecMockWithCmdForExporter(ctx, t, client, kindExporterNodeName,
				[]string{"rm", "-f", kindExporterTarPath})

			exportRequestedImages(ctx, t, client, filepath.Join(t.TempDir(), "images.tar"),
				[]string{"nginx:latest"})
		})
	}
}

func TestExportDoesNotGuessAfterCapabilityDiscoveryFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := docker.NewMockAPIClient(t)
	setupKindNodeListMock(ctx, client)
	setupEmptyImageListMockForExporter(ctx, t, client, kindExporterNodeName)
	setupExecMockWithStdoutForExporter(ctx, t, client, kindExporterNodeName,
		[]string{"uname", "-m"}, "x86_64\n")
	setupKindExecFailWithCmdForExporter(ctx, t, client,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "export", "--help"}, "ctr unavailable")

	err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker, image.ExportOptions{
			OutputPath: filepath.Join(t.TempDir(), "images.tar"), Images: []string{"nginx:latest"},
		})
	require.ErrorContains(t, err, "failed to discover image export options")
	require.ErrorContains(t, err, "ctr unavailable")
}

func TestExportRefusesIncompleteCapabilityOutput(t *testing.T) {
	t.Parallel()

	for _, help := range []string{"", "OPTIONS:\n   --platform value  Export this platform\n"} {
		t.Run(help, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := docker.NewMockAPIClient(t)
			setupKindNodeListMock(ctx, client)
			setupEmptyImageListMockForExporter(ctx, t, client, kindExporterNodeName)
			setupExecMockWithStdoutForExporter(ctx, t, client, kindExporterNodeName,
				[]string{"uname", "-m"}, "x86_64\n")
			setupExecMockWithStdoutForExporter(ctx, t, client, kindExporterNodeName,
				[]string{ctrCommand, "--namespace=k8s.io", "images", "export", "--help"}, help)

			err := image.NewExporter(client).Export(ctx, "my-cluster", v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker, image.ExportOptions{
					OutputPath: filepath.Join(
						t.TempDir(),
						"images.tar",
					), Images: []string{"nginx:latest"},
				})
			require.ErrorContains(t, err, "incomplete help output")
		})
	}
}
