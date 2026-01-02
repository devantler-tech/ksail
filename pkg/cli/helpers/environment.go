package helpers

import (
	"context"
	"errors"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	registrypkg "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterEnvironment holds auto-detected cluster configuration from the running environment.
type ClusterEnvironment struct {
	RegistryPort int32
	GitOpsEngine v1alpha1.GitOpsEngine
}

// ErrNoLocalRegistry is returned when no local registry container is found.
var ErrNoLocalRegistry = errors.New(
	"no running local registry found; " +
		"create a cluster with '--local-registry Enabled' during cluster init",
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

// DetectLocalRegistryPort finds the host port of the running local-registry container.
func DetectLocalRegistryPort(ctx context.Context) (int32, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return 0, fmt.Errorf("create docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	registryManager, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		return 0, fmt.Errorf("create registry manager: %w", err)
	}

	// Check if local-registry container exists and is running
	inUse, err := registryManager.IsRegistryInUse(ctx, registrypkg.LocalRegistryContainerName)
	if err != nil {
		return 0, fmt.Errorf("check registry status: %w", err)
	}

	if !inUse {
		return 0, ErrNoLocalRegistry
	}

	port, err := registryManager.GetRegistryPort(ctx, registrypkg.LocalRegistryContainerName)
	if err != nil {
		return 0, fmt.Errorf("get registry port: %w", err)
	}

	return int32(port), nil //nolint:gosec // port is validated by Docker API
}

// DetectGitOpsEngine checks if Flux or ArgoCD is deployed in the cluster.
func DetectGitOpsEngine(ctx context.Context) (v1alpha1.GitOpsEngine, error) {
	// Build kubeconfig from default location
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		// If we can't get cluster config, we can't detect GitOps engine.
		// Return ErrNoGitOpsEngine so the user knows detection failed.
		return v1alpha1.GitOpsEngineNone, ErrNoGitOpsEngine
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("create kubernetes client: %w", err)
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
