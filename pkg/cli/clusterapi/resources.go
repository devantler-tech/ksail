package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Ensure the local backend exposes the read-only resource browser and the safe write actions via the
// shared adapter (the Service supplies only ResourceClient; api.ResourceAdapter provides the six
// ResourceService/ResourceWriter methods).
var (
	_ api.ResourceService        = (*Service)(nil)
	_ api.ResourceWriter         = (*Service)(nil)
	_ api.ResourceClientProvider = (*Service)(nil)
)

// ResourceClient resolves a dynamic client for the named local cluster — the one per-backend
// difference behind the shared workload-browser surface (the embedded api.ResourceAdapter provides
// the list/get/scale/restart/reconcile/delete methods). It delegates to the injectable newDynamicClient
// builder so tests can substitute a fake client.
func (s *Service) ResourceClient(
	ctx context.Context,
	_, name string,
) (dynamic.Interface, error) {
	return s.newDynamicClient(ctx, name)
}

// dynamicClientFunc builds a dynamic client for the named local cluster. Injectable so tests can
// substitute a fake client instead of resolving a real kubeconfig context.
type dynamicClientFunc func(ctx context.Context, clusterName string) (dynamic.Interface, error)

// restConfigForClusterFunc resolves a *rest.Config for the named local cluster. It is the single
// injectable seam every default client builder (dynamic, apply, log, exec) derives from, so tests can
// point all four at one fake kubeconfig and production honours s.kubeconfigPath consistently.
type restConfigForClusterFunc func(clusterName string) (*rest.Config, error)

// defaultRESTConfigForCluster resolves the cluster's kubeconfig context by name (using the same
// distribution-context patterns the detector uses: kind-<name>, k3d-<name>, admin@<name>, …) and
// builds a *rest.Config against it, reading the kubeconfig path from s.kubeconfigPath so a test can
// redirect every derived client by injecting one temp kubeconfig.
func (s *Service) defaultRESTConfigForCluster(clusterName string) (*rest.Config, error) {
	kubeconfigPath := s.kubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, clusterName)
	if err != nil {
		return nil, err
	}

	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return nil, fmt.Errorf("build rest config for %q: %w", clusterName, err)
	}

	return restConfig, nil
}

// defaultDynamicClient builds a dynamic client for a local cluster from the single restConfigForCluster
// seam (rest.Config + dynamic.NewForConfig — identical to the former k8s.NewDynamicClient path).
func (s *Service) defaultDynamicClient(
	_ context.Context,
	clusterName string,
) (dynamic.Interface, error) {
	restConfig, err := s.restConfigForCluster(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client for %q: %w", clusterName, err)
	}

	return client, nil
}

// contextForCluster finds the kubeconfig context whose detected cluster name matches clusterName.
// Returns an ErrNotFound-wrapped error (→ 404) when no context matches.
func contextForCluster(kubeconfigPath, clusterName string) (string, error) {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}

	for contextName := range config.Contexts {
		_, name, detectErr := clusterdetector.DetectDistributionFromContext(contextName)
		if detectErr == nil && name == clusterName {
			return contextName, nil
		}
	}

	return "", fmt.Errorf("%w: no kubeconfig context for cluster %q", api.ErrNotFound, clusterName)
}

// clusterEndpoints maps every cluster name detectable from the kubeconfig's contexts to its API
// server URL, so List can report a real endpoint for local/discovered clusters (the operator surface
// observes it during reconciliation instead). Best-effort and offline (one file read, no cluster
// round-trips): an unreadable kubeconfig yields no endpoints. The first context detected for a name
// wins, matching contextForCluster.
func (s *Service) clusterEndpoints() map[string]string {
	config, err := clientcmd.LoadFromFile(s.kubeconfigPath())
	if err != nil {
		return nil
	}

	endpoints := make(map[string]string, len(config.Contexts))

	for contextName, kubeContext := range config.Contexts {
		_, name, detectErr := clusterdetector.DetectDistributionFromContext(contextName)
		if detectErr != nil {
			continue
		}

		cluster, ok := config.Clusters[kubeContext.Cluster]
		if !ok || cluster.Server == "" {
			continue
		}

		if _, exists := endpoints[name]; !exists {
			endpoints[name] = cluster.Server
		}
	}

	return endpoints
}
