package registry

import (
	"context"
	"errors"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerhelpers "github.com/devantler-tech/ksail/v5/pkg/cli/helpers/docker"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	registrypkg "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterEnvironment holds auto-detected cluster configuration from the running environment.
type ClusterEnvironment struct {
	RegistryPort int32
	GitOpsEngine v1alpha1.GitOpsEngine
}

// ErrNoLocalRegistry is returned when no local registry container is found.
var ErrNoLocalRegistry = errors.New(
	"no running local registry found; " +
		"create a cluster with '--local-registry' during cluster init",
)

// ErrNoGitOpsEngine is returned when no GitOps engine is detected.
var ErrNoGitOpsEngine = errors.New(
	"no GitOps engine detected in cluster; " +
		"create a cluster with '--gitops-engine Flux|ArgoCD' during cluster init",
)

// DetectClusterEnvironment auto-detects the registry port and GitOps engine
// from the running Docker containers and Kubernetes cluster.
func DetectClusterEnvironment(ctx context.Context) (*ClusterEnvironment, error) {
	env := &ClusterEnvironment{}

	// Detect local registry
	port, err := DetectLocalRegistryPort(ctx)
	if err != nil {
		return nil, err
	}

	env.RegistryPort = port

	// Detect GitOps engine from cluster
	engine, err := DetectGitOpsEngine(ctx)
	if err != nil {
		return nil, err
	}

	env.GitOpsEngine = engine

	return env, nil
}

// DetectLocalRegistryPort finds the host port of a running local-registry container.
// It checks for cluster-prefixed registries (e.g., "kind-local-registry", "k3d-default-local-registry")
// by searching for containers matching the "*-local-registry" pattern.
// Both KSail-managed registries (with labels) and K3d-managed registries are detected.
func DetectLocalRegistryPort(ctx context.Context) (int32, error) {
	resources, err := dockerhelpers.NewDockerRegistryManager()
	if err != nil {
		return 0, fmt.Errorf("create docker registry manager: %w", err)
	}

	defer resources.Close()

	registryManager := resources.RegistryManager

	// Search for any running container ending with "-local-registry" suffix
	registrySuffix := "-" + registrypkg.LocalRegistryBaseName

	registryName, err := registryManager.FindContainerBySuffix(ctx, registrySuffix)
	if err != nil {
		return 0, fmt.Errorf("find local registry: %w", err)
	}

	if registryName == "" {
		return 0, ErrNoLocalRegistry
	}

	// First, check for KSail-managed local-registry (with KSail labels)
	inUse, err := registryManager.IsRegistryInUse(ctx, registryName)
	if err != nil {
		return 0, fmt.Errorf("check registry status: %w", err)
	}

	if inUse {
		port, portErr := registryManager.GetRegistryPort(ctx, registryName)
		if portErr != nil {
			return 0, fmt.Errorf("get registry port: %w", portErr)
		}

		return int32(port), nil //nolint:gosec // port is validated by Docker API
	}

	// Second, check if it's a K3d-managed local-registry (without KSail labels).
	k3dRunning, k3dErr := registryManager.IsContainerRunning(ctx, registryName)
	if k3dErr != nil {
		return 0, fmt.Errorf("check k3d registry status: %w", k3dErr)
	}

	if k3dRunning {
		port, portErr := registryManager.GetContainerPort(
			ctx,
			registryName,
			dockerclient.DefaultRegistryPort,
		)
		if portErr != nil {
			return 0, fmt.Errorf("get k3d registry port: %w", portErr)
		}

		return int32(port), nil //nolint:gosec // port is validated by Docker API
	}

	return 0, ErrNoLocalRegistry
}

// DetectGitOpsEngine checks if Flux or ArgoCD is deployed in the cluster.
func DetectGitOpsEngine(ctx context.Context) (v1alpha1.GitOpsEngine, error) {
	clientset, err := getKubernetesClient()
	if err != nil {
		// If we can't get cluster config, we can't detect GitOps engine.
		// Return ErrNoGitOpsEngine so the user knows detection failed.
		return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
	}

	// Check for Flux (flux-system namespace)
	_, err = clientset.CoreV1().Namespaces().Get(ctx, "flux-system", metav1.GetOptions{})
	if err == nil {
		return v1alpha1.GitOpsEngineFlux, nil
	}

	// Check for ArgoCD (argocd namespace)
	_, err = clientset.CoreV1().Namespaces().Get(ctx, "argocd", metav1.GetOptions{})
	if err == nil {
		return v1alpha1.GitOpsEngineArgoCD, nil
	}

	return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
}
