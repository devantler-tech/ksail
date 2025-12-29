package cluster

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	"github.com/spf13/cobra"
)

// NewInitCmd creates and returns the init command.
func NewInitCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize a new project",
		Long:         "Initialize a new project in the specified directory (or current directory if none specified).",
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, InitFieldSelectors())

	// Bind init-local flags (not part of shared cluster config). Keeping this scoped
	// here avoids polluting the generic config manager with scaffolding concerns.
	bindInitLocalFlags(cmd, cfgManager)

	cmd.RunE = runtime.RunEWithRuntime(
		runtimeContainer,
		runtime.WithTimer(func(cmd *cobra.Command, _ runtime.Injector, tmr timer.Timer) error {
			deps := InitDeps{Timer: tmr}

			return HandleInitRunE(cmd, cfgManager, deps)
		}),
	)

	return cmd
}

// InitFieldSelectors returns the field selectors used by the init command.
// Kept local (rather than separate file) to keep init-specific wiring cohesive.
func InitFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	selectors = append(selectors, ksailconfigmanager.StandardSourceDirectoryFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCNIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCSIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	// Unified node count selectors for all distributions
	selectors = append(selectors, ksailconfigmanager.ControlPlanesFieldSelector())
	selectors = append(selectors, ksailconfigmanager.WorkersFieldSelector())

	return selectors
}

// bindInitLocalFlags adds and binds flags that are specific to the init command only.
// They intentionally do not belong to the shared cluster field selectors.
func bindInitLocalFlags(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("output", "o", "", "Output directory for the project")
	_ = cfgManager.Viper.BindPFlag("output", cmd.Flags().Lookup("output"))
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	cmd.Flags().StringSlice(
		"mirror-registry",
		[]string{},
		"Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io).",
	)
	_ = cfgManager.Viper.BindPFlag("mirror-registry", cmd.Flags().Lookup("mirror-registry"))
}

// InitDeps captures dependencies required for the init command.
type InitDeps struct {
	Timer timer.Timer
}

// HandleInitRunE handles the init command.
func HandleInitRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps InitDeps,
) error {
	if deps.Timer != nil {
		deps.Timer.Start()
	}

	clusterCfg, err := cfgManager.LoadConfigFromFlagsOnly()
	if err != nil {
		return fmt.Errorf("failed to resolve configuration for scaffolding: %w", err)
	}

	targetPath, err := resolveInitTargetPath(cfgManager)
	if err != nil {
		return err
	}

	force := cfgManager.Viper.GetBool("force")
	mirrorRegistries := cfgManager.Viper.GetStringSlice("mirror-registry")

	scaffolderInstance := scaffolder.NewScaffolder(
		*clusterCfg,
		cmd.OutOrStdout(),
		mirrorRegistries,
	)

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Initialize project...",
		Emoji:   "📂",
		Writer:  cmd.OutOrStdout(),
	})

	err = scaffolderInstance.Scaffold(targetPath, force)
	if err != nil {
		return fmt.Errorf("failed to scaffold project files: %w", err)
	}

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "initialized project",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func resolveInitTargetPath(cfgManager *ksailconfigmanager.ConfigManager) (string, error) {
	flagOutputPath := cfgManager.Viper.GetString("output")
	if flagOutputPath != "" {
		return flagOutputPath, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return wd, nil
}
