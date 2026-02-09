package workload

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	imagesvc "github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/spf13/cobra"
)

const exportCmdLong = `Export container images from the cluster's containerd runtime to a tar archive.

The exported archive can be used to:
  - Share image sets between development machines
  - Pre-load images for offline development
  - Speed up cluster recreation by avoiding registry pulls

Examples:
  # Export all images from cluster to images.tar (default)
  ksail workload export

  # Export all images to a specific file
  ksail workload export ./backups/my-images.tar

  # Export specific images from cluster
  ksail workload export --image=nginx:latest --image=redis:7

  # Export from a specific kubeconfig context
  ksail workload export --context=kind-dev --kubeconfig=~/.kube/config`

// NewExportCmd creates the image export command.
func NewExportCmd(_ *runtime.Runtime) *cobra.Command {
	var images []string

	cmd := &cobra.Command{
		Use:          "export [<output>]",
		Short:        "Export container images from the cluster",
		Long:         exportCmdLong,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	// Create config manager during command setup to register flags
	// This enables --context, --kubeconfig, and other standard flags
	cfgManager := createImageConfigManager(cmd)

	cmd.Flags().StringArrayVar(&images, "image", nil,
		"Image(s) to export (repeatable); if not specified, all images are exported")

	_ = cfgManager.Viper.BindPFlag("image", cmd.Flags().Lookup("image"))

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runExportCommand(cmd, args, cfgManager, images)
	}

	return cmd
}

func runExportCommand(
	cmd *cobra.Command,
	args []string,
	cfgManager *configmanager.ConfigManager,
	images []string,
) error {
	ctx, err := initImageCommandContext(cmd, cfgManager)
	if err != nil {
		return err
	}

	outputPath := "images.tar"
	if len(args) > 0 {
		outputPath = args[0]
	}

	if len(images) == 0 {
		images = cfgManager.Viper.GetStringSlice("image")
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📤",
		Content: "Export Container Images...",
		Writer:  cmd.OutOrStdout(),
	})

	err = ctx.detectClusterInfo()
	if err != nil {
		return err
	}

	return executeExport(cmd, ctx, images, outputPath)
}

func executeExport(
	cmd *cobra.Command,
	ctx *imageCommandContext,
	images []string,
	outputPath string,
) error {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	exporter := imagesvc.NewExporter(dockerClient)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "exporting images from cluster %s",
		Args:    []any{ctx.ClusterInfo.ClusterName},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = exporter.Export(
		cmd.Context(),
		ctx.ClusterInfo.ClusterName,
		ctx.ClusterInfo.Distribution,
		ctx.ClusterInfo.Provider,
		imagesvc.ExportOptions{
			OutputPath: outputPath,
			Images:     images,
		},
	)
	if err != nil {
		return fmt.Errorf("export images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images exported to %s",
		Args:    []any{outputPath},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
