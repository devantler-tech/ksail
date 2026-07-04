package clusterprovisioner

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// buildHostClusterClients builds Kubernetes clients for the host cluster
// from the Kubernetes provider options.
func buildHostClusterClients(
	opts v1alpha1.OptionsKubernetes,
) (kubernetes.Interface, *rest.Config, dynamic.Interface, error) {
	kubeconfig := resolveKubernetesOption(opts.Kubeconfig, opts.KubeconfigEnvVar)

	// When no kubeconfig is configured, prefer the in-cluster service account if we are
	// running inside a pod (e.g. the KSail operator). rest.InClusterConfig returns an error
	// when not in a pod, so CLI behavior outside a cluster is unchanged.
	if kubeconfig == "" {
		inClusterConfig, inClusterErr := rest.InClusterConfig()
		if inClusterErr == nil {
			return clientsFromRESTConfig(inClusterConfig)
		}

		kubeconfig = k8s.DefaultKubeconfigPath()
	}

	// Canonicalize the path (expand ~, resolve symlinks)
	kubeconfig, err := fsutil.ExpandHomePath(kubeconfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	kubeconfig, err = fsutil.EvalCanonicalPath(kubeconfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	context := resolveKubernetesOption(opts.Context, opts.ContextEnvVar)

	restConfig, err := k8s.BuildRESTConfig(kubeconfig, context)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build host REST config: %w", err)
	}

	return clientsFromRESTConfig(restConfig)
}

// hostClientQPS and hostClientBurst raise the host-cluster REST client throughput above
// client-go's restrictive defaults (5 QPS / 10 burst). The nested-provider provisioners
// poll the host cluster heavily — k3k Cluster status, vCluster kubeconfig secrets, pods —
// across several nested clusters, and the default rate limiter deadlines under load
// ("client rate limiter Wait returned an error: context deadline exceeded").
const (
	hostClientQPS   = 50
	hostClientBurst = 100
)

// clientsFromRESTConfig builds the typed and dynamic clients used by the Kubernetes-provider
// provisioners from an already-resolved REST config. It raises the client rate-limiter
// throughput (unless already configured) so readiness polling survives host-cluster load.
func clientsFromRESTConfig(
	restConfig *rest.Config,
) (kubernetes.Interface, *rest.Config, dynamic.Interface, error) {
	if restConfig.QPS == 0 {
		restConfig.QPS = hostClientQPS
	}

	if restConfig.Burst == 0 {
		restConfig.Burst = hostClientBurst
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create host clientset: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create host dynamic client: %w", err)
	}

	return clientset, restConfig, dynClient, nil
}

// resolveKubernetesOption returns the value from the environment variable
// if set, otherwise returns the direct value.
func resolveKubernetesOption(directValue, envVar string) string {
	if envVar != "" {
		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
	}

	return directValue
}

// buildKubernetesInfra creates the host-cluster clients and Kubernetes provider in one call.
// This consolidates the repeated buildHostClusterClients + NewProvider sequence.
func buildKubernetesInfra(
	opts v1alpha1.OptionsKubernetes,
) (kubernetes.Interface, *rest.Config, dynamic.Interface, *kubernetesprovider.Provider, error) {
	hostClient, restConfig, dynClient, err := buildHostClusterClients(opts)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("build host cluster clients: %w", err)
	}

	k8sProvider, err := kubernetesprovider.NewProvider(hostClient, opts)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("create Kubernetes provider: %w", err)
	}

	return hostClient, restConfig, dynClient, k8sProvider, nil
}

// buildDinDProvisionerConfig assembles the host-cluster wiring shared by the DinD-based nested
// provisioners (Kind, KWOK) from the resolved cluster, options, clients and provider. Both
// distributions embed the result in their provisioner config, so building it here keeps the two
// factories from drifting.
func buildDinDProvisionerConfig(
	cluster *v1alpha1.Cluster,
	opts v1alpha1.OptionsKubernetes,
	hostClient kubernetes.Interface,
	restConfig *rest.Config,
	dynClient dynamic.Interface,
	k8sProvider *kubernetesprovider.Provider,
	clusterName string,
) kubernetesprovider.DinDProvisionerConfig {
	return kubernetesprovider.DinDProvisionerConfig{
		KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
		HostClientset:    hostClient,
		K8sProvider:      k8sProvider,
		DynamicClient:    dynClient,
		RestConfig:       restConfig,
		ClusterName:      clusterName,
		Distribution:     string(cluster.Spec.Cluster.Distribution),
		GatewayClassName: opts.GatewayClassName,
		Persistence:      opts.Persistence,
	}
}
