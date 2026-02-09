package workload

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	imagesvc "github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/spf13/cobra"
)

const importCmdLong = `Import container images from a tar archive to the cluster's containerd di.

Images are imported to all nodes in the cluster, making them available for
pod scheduling without requiring registry pulls.

Examples:
  # Import images from images.tar (default)
  ksail workload import

  # Import images from a specific file
  ksail workload import ./backups/my-images.tar

  # Import to a specific kubeconfig context
  ksail workload import --context=kind-dev --kubeconfig=~/.kube/config`

// NewImportCmd creates the image import command.
func NewImportCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "import [<input>]",
		Short:        "Import container images to the cluster",
		Long:         importCmdLong,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	// Create config manager during command setup to register flags
	// This enables --context, --kubeconfig, and other standard flags
	cfgManager := createImageConfigManager(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runImportCommand(cmd, args, cfgManager)
	}

	return cmd
}

func runImportCommand(
	cmd *cobra.Command,
	args []string,
	cfgManager *configmanager.ConfigManager,
) error {
	ctx, err := initImageCommandContext(cmd, cfgManager)
	if err != nil {
		return err
	}

	inputPath := "images.tar"
	if len(args) > 0 {
		inputPath = args[0]
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📥",
		Content: "Import Container Images...",
		Writer:  cmd.OutOrStdout(),
	})

	err = ctx.detectClusterInfo()
	if err != nil {
		return err
	}

	return executeImport(cmd, ctx, inputPath)
}

func executeImport(
	cmd *cobra.Command,
	ctx *imageCommandContext,
	inputPath string,
) error {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	importer := imagesvc.NewImporter(dockerClient)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "importing images to cluster %s",
		Args:    []any{ctx.ClusterInfo.ClusterName},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = importer.Import(
		cmd.Context(),
		ctx.ClusterInfo.ClusterName,
		ctx.ClusterInfo.Distribution,
		ctx.ClusterInfo.Provider,
		imagesvc.ImportOptions{
			InputPath: inputPath,
		},
	)
	if err != nil {
		return fmt.Errorf("import images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images imported from %s",
		Args:    []any{inputPath},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
