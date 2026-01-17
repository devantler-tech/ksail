// Package image provides CLI commands for exporting and importing container images.
package image

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	imagesvc "github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const exportCmdLong = `Export container images from the cluster's containerd runtime to a tar archive.

The exported archive can be used to:
  - Share image sets between development machines
  - Pre-load images for offline development
  - Speed up cluster recreation by avoiding registry pulls

Examples:
  # Export all images from cluster to images.tar (default)
  ksail workload image export

  # Export all images to a specific file
  ksail workload image export ./backups/my-images.tar

  # Export specific images from cluster
  ksail workload image export --image=nginx:latest --image=redis:7`

// NewImageCmd creates and returns the image command group namespace.
func NewImageCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "Manage container images in the cluster",
		Long: "Export and import container images directly to/from the cluster's containerd runtime. " +
			"This enables offline development, sharing image sets, and faster cluster recreation.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(NewExportCmd(runtimeContainer))
	cmd.AddCommand(NewImportCmd(runtimeContainer))

	return cmd
}

// NewExportCmd creates the image export command.
func NewExportCmd(_ *runtime.Runtime) *cobra.Command {
	var images []string

	viperInstance := viper.New()
	viperInstance.SetEnvPrefix(configmanager.EnvPrefix)
	viperInstance.AutomaticEnv()

	cmd := &cobra.Command{
		Use:          "export [<output>]",
		Short:        "Export container images from the cluster",
		Long:         exportCmdLong,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runExportCommand(cmd, args, viperInstance, images)
	}

	cmd.Flags().StringArrayVar(&images, "image", nil,
		"Image(s) to export (repeatable); if not specified, all images are exported")

	_ = viperInstance.BindPFlag("image", cmd.Flags().Lookup("image"))

	return cmd
}

func runExportCommand(
	cmd *cobra.Command,
	args []string,
	viperInstance *viper.Viper,
	images []string,
) error {
	outputPath := "images.tar"
	if len(args) > 0 {
		outputPath = args[0]
	}

	if len(images) == 0 {
		images = viperInstance.GetStringSlice("image")
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📤",
		Content: "Export Container Images...",
		Writer:  cmd.OutOrStdout(),
	})

	return runClusterExport(cmd, images, outputPath)
}

// runClusterExport exports images from the cluster's containerd runtime.
func runClusterExport(
	cmd *cobra.Command,
	images []string,
	outputPath string,
) error {
	ctx, err := initImageCommandContext(cmd)
	if err != nil {
		return err
	}

	err = ctx.detectClusterInfo()
	if err != nil {
		return err
	}

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
