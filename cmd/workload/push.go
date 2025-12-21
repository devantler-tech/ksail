package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the workload push command.
//
//nolint:funlen // Cobra command RunE functions typically combine setup, validation, and execution
func NewPushCmd(_ *runtime.Runtime) *cobra.Command {
	var validate bool

	cmd := &cobra.Command{
		Use:          "push",
		Short:        "Package and push an OCI artifact to the local registry",
		Long:         "Build and push local workloads as an OCI artifact to the local registry.",
		SilenceUsage: true,
	}

	cmd.Flags().BoolVar(&validate, "validate", false, "Validate workloads before pushing (overrides config)")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx, err := initCommandContext(cmd)
		if err != nil {
			return err
		}

		clusterCfg := ctx.ClusterCfg
		outputTimer := ctx.OutputTimer
		tmr := ctx.Timer

		localRegistryEnabled := clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled
		gitOpsEngineConfigured := clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineNone

		if !localRegistryEnabled || !gitOpsEngineConfigured {
			return errLocalRegistryRequired
		}

		sourceDir := clusterCfg.Spec.Workload.SourceDirectory
		if strings.TrimSpace(sourceDir) == "" {
			sourceDir = v1alpha1.DefaultSourceDirectory
		}

		// Determine if validation should run: flag takes precedence over config
		shouldValidate := validate || clusterCfg.Spec.Workload.ValidateOnPush

		// Run validation if enabled
		if shouldValidate {
			cmd.Println()
			notify.WriteMessage(notify.Message{
				Type:    notify.TitleType,
				Emoji:   "✅",
				Content: "Validating Workloads...",
				Writer:  cmd.OutOrStdout(),
			})

			tmr.NewStage()

			notify.WriteMessage(notify.Message{
				Type:    notify.ActivityType,
				Content: "validating manifests",
				Timer:   outputTimer,
				Writer:  cmd.OutOrStdout(),
			})

			err = runValidateCmd(
				cmd.Context(),
				cmd,
				[]string{sourceDir},
				true,  // skipSecrets
				true,  // strict
				true,  // ignoreMissingSchemas
				false, // verbose
			)
			if err != nil {
				return fmt.Errorf("validate workloads: %w", err)
			}

			notify.WriteMessage(notify.Message{
				Type:    notify.SuccessType,
				Content: "validation passed",
				Timer:   outputTimer,
				Writer:  cmd.OutOrStdout(),
			})
		}

		repoName := sourceDir
		artifactVersion := registry.DefaultLocalArtifactTag

		registryPort := clusterCfg.Spec.Cluster.Options.LocalRegistry.HostPort
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
			GitOpsEngine:     clusterCfg.Spec.Cluster.GitOpsEngine,
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
