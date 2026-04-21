package setup

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdgitops "github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// InstallArgoCDSilent installs ArgoCD silently for parallel execution.
func InstallArgoCDSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.ArgoCD,
		ErrArgoCDInstallerFactoryNil, "argocd",
	)
}

// InstallFluxSilent installs Flux silently for parallel execution.
func InstallFluxSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, _, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	timeout := max(installer.GetInstallTimeout(clusterCfg), installer.FluxInstallTimeout)
	fluxInst := factories.Flux(helmClient, timeout)

	installErr := fluxInst.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("failed to install flux controllers: %w", installErr)
	}

	return nil
}

// EnsureArgoCDResources configures default Argo CD resources post-install.
func EnsureArgoCDResources(
	ctx context.Context,
	kubeconfigPath string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	if clusterCfg == nil {
		return ErrClusterConfigNil
	}

	installTimeout := installer.GetInstallTimeout(clusterCfg)

	err := argocdinstaller.EnsureDefaultResources(ctx, kubeconfigPath, installTimeout)
	if err != nil {
		return fmt.Errorf("ensure argocd default resources: %w", err)
	}

	err = argocdinstaller.EnsureSopsAgeSecret(ctx, kubeconfigPath, clusterCfg)
	if err != nil {
		return fmt.Errorf("ensure argocd sops-age secret: %w", err)
	}

	mgr, err := argocdgitops.NewManagerFromKubeconfig(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd manager: %w", err)
	}

	// For VCluster, resolve the registry container's Docker IP since pods inside
	// VCluster use CoreDNS which cannot resolve Docker container names.
	registryHost, resolveErr := resolveRegistryHost(ctx, clusterCfg, clusterName)
	if resolveErr != nil {
		return fmt.Errorf("resolve registry host for argocd: %w", resolveErr)
	}

	// Build repository URL and credentials based on registry configuration
	opts := buildArgoCDEnsureOptions(clusterCfg, clusterName, registryHost)

	err = mgr.Ensure(ctx, opts)
	if err != nil {
		return fmt.Errorf("ensure argocd resources: %w", err)
	}

	return nil
}

// buildArgoCDEnsureOptions constructs ArgoCD ensure options based on registry config.
// registryHost overrides the default registry hostname for the OCI repository URL.
// Pass an empty string to use the default Docker container name.
func buildArgoCDEnsureOptions(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	registryHost string,
) argocdgitops.EnsureOptions {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry

	// Resolve tag: workload tag > registry-embedded tag > default.
	tag := clusterCfg.Spec.Workload.Tag
	if tag == "" && localRegistry.IsExternal() {
		parsed := localRegistry.Parse()
		if parsed.Tag != "" {
			tag = parsed.Tag
		}
	}

	if tag == "" {
		tag = registry.DefaultLocalArtifactTag
	}

	opts := argocdgitops.EnsureOptions{
		SourcePath:      ".",
		ApplicationName: "ksail",
		TargetRevision:  tag,
	}

	if localRegistry.IsExternal() {
		applyExternalRegistryOptions(&opts, localRegistry)
	} else {
		applyLocalRegistryOptions(&opts, clusterCfg, clusterName, registryHost)
	}

	return opts
}

// applyExternalRegistryOptions configures options for external OCI registries.
func applyExternalRegistryOptions(
	opts *argocdgitops.EnsureOptions,
	localRegistry v1alpha1.LocalRegistry,
) {
	parsed := localRegistry.Parse()
	opts.RepositoryURL = fmt.Sprintf("oci://%s/%s", parsed.Host, parsed.Path)
	username, password := localRegistry.ResolveCredentials()
	opts.Username = username
	opts.Password = password
	opts.Insecure = false
}

// applyLocalRegistryOptions configures options for local in-cluster registries.
// registryHostOverride replaces the default Docker container name when non-empty.
// This is needed for VCluster where pods cannot resolve Docker container names.
func applyLocalRegistryOptions(
	opts *argocdgitops.EnsureOptions,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	registryHostOverride string,
) {
	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = v1alpha1.DefaultSourceDirectory
	}

	repoName := registry.SanitizeRepoName(sourceDir)

	registryHost := strings.TrimSpace(registryHostOverride)
	if registryHost == "" {
		registryHost = registry.BuildLocalRegistryName(clusterName)
	}

	hostPort := net.JoinHostPort(
		registryHost,
		strconv.Itoa(dockerclient.DefaultRegistryPort),
	)
	opts.RepositoryURL = fmt.Sprintf("oci://%s/%s", hostPort, repoName)
	opts.Insecure = true
}
