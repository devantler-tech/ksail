package setup

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	registryhelpers "github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"
)

// ShouldPushOCIArtifact determines if OCI artifact push should happen for GitOps engines.
// Returns true if Flux or ArgoCD is enabled and a local registry is configured.
func ShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	// Only push for GitOps engines that consume OCI artifacts
	engine := clusterCfg.Spec.Cluster.GitOpsEngine
	if engine != v1alpha1.GitOpsEngineFlux && engine != v1alpha1.GitOpsEngineArgoCD {
		return false
	}

	// Only push if local registry is enabled
	return clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
}

// resolveClusterNameFromContext resolves the cluster name from the cluster config.
// It first attempts to parse the cluster name from Connection.Context
// (e.g., "k3d-system-test-cluster" -> "system-test-cluster").
// Falls back to the distribution's default cluster name if context is not set or parsing fails.
// The cluster name is used for constructing registry container names
// (e.g., system-test-cluster-local-registry).
func resolveClusterNameFromContext(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg == nil {
		return kindconfigmanager.DefaultClusterName
	}

	// First try to extract cluster name from the context if available
	contextName := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context)
	if contextName != "" {
		_, clusterName, err := clusterdetector.DetectDistributionFromContext(contextName)
		if err == nil && clusterName != "" {
			return clusterName
		}
	}

	// Fall back to default cluster name for the distribution
	return clusterCfg.Spec.Cluster.Distribution.DefaultClusterName()
}

func configureGitOpsResources(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
	gitOpsKubeconfig string,
) error {
	// Only show configure stage if there are GitOps resources to configure
	if !reqs.NeedsArgoCD && !reqs.NeedsFlux {
		return nil
	}

	// Resolve cluster name for registry naming
	clusterName := resolveClusterNameFromContext(clusterCfg)
	writer := cmd.OutOrStdout()

	// Show title for configure stage
	notify.WriteMessage(notify.Message{
		Type: notify.TitleType, Content: "Configuring components...", Emoji: "⚙️", Writer: writer,
	})

	// Post-install GitOps configuration
	if reqs.NeedsArgoCD {
		err := configureArgoCD(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	if reqs.NeedsFlux {
		err := configureFlux(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	// Show success message for configure stage
	notify.WriteMessage(
		notify.Message{Type: notify.SuccessType, Content: "components configured", Writer: writer},
	)

	return nil
}

func configureArgoCD(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	// Ensure OCI artifact exists before creating the ArgoCD Application,
	// otherwise ArgoCD enters a ComparisonError loop that can saturate etcd.
	_, err := factories.callEnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact for ArgoCD: %w", err)
	}

	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: argoCDResourcesActivity, Writer: writer},
	)

	err = factories.EnsureArgoCDResources(ctx, kubeconfig, clusterCfg, clusterName)
	if err != nil {
		return fmt.Errorf("failed to configure Argo CD resources: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
		Writer:  writer,
	})

	return nil
}

func configureFlux(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: fluxResourcesActivity, Writer: writer},
	)

	// For VCluster, resolve the registry container's Docker IP since pods inside
	// VCluster use CoreDNS which cannot resolve Docker container names.
	registryHost, resolveErr := resolveRegistryHost(ctx, clusterCfg, clusterName)
	if resolveErr != nil {
		return fmt.Errorf("resolve registry host for flux: %w", resolveErr)
	}

	// Step 1: Setup FluxInstance CR (does not wait for readiness)
	err := factories.SetupFluxInstance(ctx, kubeconfig, clusterCfg, clusterName, registryHost)
	if err != nil {
		return fmt.Errorf("failed to setup FluxInstance: %w", err)
	}

	// Step 2: Check if OCI artifact exists and push if needed
	artifactPushed, err := factories.callEnsureOCIArtifact(
		ctx,
		cmd,
		clusterCfg,
		clusterName,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact: %w", err)
	}

	// Step 3: Wait for FluxInstance to be ready (only if artifact was pushed/exists)
	if artifactPushed {
		notify.WriteMessage(
			notify.Message{
				Type:    notify.ActivityType,
				Content: "waiting for flux to be ready",
				Writer:  writer,
			},
		)

		err = factories.WaitForFluxReady(ctx, kubeconfig)
		if err != nil {
			return fmt.Errorf("failed waiting for Flux to be ready: %w", err)
		}
	}

	return nil
}

// callEnsureOCIArtifact calls EnsureOCIArtifact if set on the factory, or falls
// back to the default ensureOCIArtifact implementation. This eliminates the
// repeated nil-guard pattern in configureArgoCD and configureFlux.
func (f *InstallerFactories) callEnsureOCIArtifact(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	if f.EnsureOCIArtifact != nil {
		return f.EnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	}

	return ensureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
}

// ensureOCIArtifact checks if an OCI artifact exists and pushes one if needed.
// Returns true if an artifact exists or was pushed, false if no artifact needed.
func ensureOCIArtifact(
	ctx context.Context,
	_ *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Only check/push for local registries
	if !clusterCfg.Spec.Cluster.LocalRegistry.Enabled() {
		return false, nil
	}

	// Resolve registry info
	registryInfo, err := registryhelpers.ResolveRegistry(
		ctx,
		registryhelpers.ResolveRegistryOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   clusterName,
		},
	)
	if err != nil {
		return false, fmt.Errorf("resolve registry: %w", err)
	}

	// Build the artifact reference details
	artifactOpts := buildArtifactExistsOptions(registryInfo, clusterCfg)

	// Check if artifact already exists
	verifier := oci.NewRegistryVerifier()

	exists, err := verifier.ArtifactExists(ctx, artifactOpts)
	if err != nil {
		// Log warning but continue - we'll try to push
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "checking for existing artifact",
			Writer:  writer,
		})
	}

	if exists {
		// Artifact already exists, no need to push
		return true, nil
	}

	return pushInitialOCIArtifact(ctx, clusterCfg, clusterName, writer)
}

// pushInitialOCIArtifact pushes an initial OCI artifact when none exists.
func pushInitialOCIArtifact(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Artifact doesn't exist, push an empty one
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing initial oci artifact",
		Writer:  writer,
	})

	result, err := registryhelpers.PushOCIArtifact(ctx, registryhelpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   clusterName,
		SourceDir:     "", // Use default from config
		Ref:           "", // Use default tag
		Validate:      clusterCfg.Spec.Workload.ValidateOnPush,
	})
	if err != nil {
		return false, fmt.Errorf("push oci artifact: %w", err)
	}

	if result.Empty {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "pushed empty kustomization (source directory not found)",
			Writer:  writer,
		})
	}

	return result.Pushed, nil
}

// buildArtifactExistsOptions creates options for checking artifact existence.
func buildArtifactExistsOptions(
	registryInfo *registryhelpers.Info,
	clusterCfg *v1alpha1.Cluster,
) oci.ArtifactExistsOptions {
	return oci.ArtifactExistsOptions{
		RegistryEndpoint: resolveRegistryEndpoint(registryInfo),
		Repository:       resolveRepository(registryInfo, clusterCfg),
		Tag:              resolveArtifactTag(clusterCfg.Spec.Workload.Tag, registryInfo.Tag),
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
		Insecure:         !clusterCfg.Spec.Cluster.LocalRegistry.IsExternal(),
	}
}

func resolveRegistryEndpoint(info *registryhelpers.Info) string {
	if info.Port > 0 {
		return net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))
	}

	return info.Host
}

func resolveRepository(info *registryhelpers.Info, cfg *v1alpha1.Cluster) string {
	if info.Repository != "" {
		return info.Repository
	}

	sourceDir := cfg.Spec.Workload.SourceDirectory
	if sourceDir == "" {
		return v1alpha1.DefaultSourceDirectory
	}

	return sourceDir
}

// resolveArtifactTag determines the OCI artifact tag using priority-based
// resolution: workload tag > registry-embedded tag > default. It is shared by
// the OCI-artifact existence check (post-CNI GitOps configuration) and the
// ArgoCD ensure-options builder (install_gitops.go) so the tag priority is
// single-sourced. Pass an empty registryTag when no registry-embedded tag
// applies (e.g. non-external local registries).
func resolveArtifactTag(workloadTag, registryTag string) string {
	if workloadTag != "" {
		return workloadTag
	}

	if registryTag != "" {
		return registryTag
	}

	return registry.DefaultLocalArtifactTag
}
