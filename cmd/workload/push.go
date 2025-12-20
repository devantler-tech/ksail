package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/pkg/client/oci"
	cmdhelpers "github.com/devantler-tech/ksail/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/pkg/ui/notify"
	"github.com/devantler-tech/ksail/pkg/ui/timer"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the workload push command.
//
//nolint:funlen // Cobra command RunE functions typically combine setup, validation, and execution
func NewPushCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "push",
		Short:        "Package and push an OCI artifact to the local registry",
		Long:         "Build and push local workloads as an OCI artifact to the local registry.",
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		tmr := timer.New()
		tmr.Start()

		fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
		cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

		outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

		clusterCfg, err := cfgManager.LoadConfig(outputTimer)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		localRegistryEnabled := clusterCfg.Spec.LocalRegistry == v1alpha1.LocalRegistryEnabled
		gitOpsEngineConfigured := clusterCfg.Spec.GitOpsEngine != v1alpha1.GitOpsEngineNone

		if !localRegistryEnabled || !gitOpsEngineConfigured {
			return errLocalRegistryRequired
		}

		sourceDir := clusterCfg.Spec.SourceDirectory
		if strings.TrimSpace(sourceDir) == "" {
			sourceDir = v1alpha1.DefaultSourceDirectory
		}

		repoName := sourceDir
		artifactVersion := registry.DefaultLocalArtifactTag

		registryPort := clusterCfg.Spec.Options.LocalRegistry.HostPort
		if registryPort == 0 {
			registryPort = v1alpha1.DefaultLocalRegistryPort
		}

		builder := oci.NewWorkloadArtifactBuilder()

		cmd.Println()
		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Emoji:   "📦",
			Content: "Build and Push OCI Artifact...",
			Writer:  cmd.OutOrStdout(),
		})

		tmr.NewStage()

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "building oci artifact",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "pushing oci artifact",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		_, err = builder.Build(cmd.Context(), oci.BuildOptions{
			Name:             repoName,
			SourcePath:       sourceDir,
			RegistryEndpoint: fmt.Sprintf("localhost:%d", registryPort),
			Repository:       repoName,
			Version:          artifactVersion,
			GitOpsEngine:     clusterCfg.Spec.GitOpsEngine,
		})
		if err != nil {
			return fmt.Errorf("build and push oci artifact: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "oci artifact pushed",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	}

	return cmd
}
