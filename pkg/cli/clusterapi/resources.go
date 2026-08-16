package clusterapi

import (
	"context"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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

// loadKubeconfig reads the user's kubeconfig once, best-effort and offline. List calls it a single
// time and shares the parsed config with both clusterEndpoints and unmanagedClusters, so the file is
// read and YAML-parsed once per List (not once per helper) and both helpers observe the SAME snapshot
// — closing the TOCTOU window where two independent reads could otherwise mix contexts from a
// kubeconfig that changed between them. An unreadable kubeconfig yields nil, which both helpers treat
// as "no contexts".
func (s *Service) loadKubeconfig() *clientcmdapi.Config {
	// Best-effort load with nil-on-error semantics, shared with the CLI's cluster list via
	// clusterdiscovery.LoadKubeconfig so both surfaces read a kubeconfig identically.
	return clusterdiscovery.LoadKubeconfig(s.kubeconfigPath())
}

// endpointForContext resolves the API server URL for a kubeconfig context, or "" when the context or
// its cluster is absent. Shared by clusterEndpoints and unmanagedClusters so the two-step
// context -> cluster -> server lookup and its nil checks live in one place.
func endpointForContext(config *clientcmdapi.Config, contextName string) string {
	kubeContext, contextExists := config.Contexts[contextName]
	if !contextExists {
		return ""
	}

	cluster, clusterExists := config.Clusters[kubeContext.Cluster]
	if !clusterExists {
		return ""
	}

	return cluster.Server
}

// clusterEndpoints maps every cluster name detectable from the kubeconfig's contexts to its API
// server URL, so List can report a real endpoint for local/discovered clusters (the operator surface
// observes it during reconciliation instead). Best-effort and offline (no cluster round-trips): a nil
// config (unreadable kubeconfig) yields no endpoints. The first context detected for a name wins,
// matching contextForCluster.
func clusterEndpoints(config *clientcmdapi.Config) map[string]string {
	if config == nil {
		return nil
	}

	endpoints := make(map[string]string, len(config.Contexts))

	for contextName := range config.Contexts {
		_, name, detectErr := clusterdetector.DetectDistributionFromContext(contextName)
		if detectErr != nil {
			continue
		}

		server := endpointForContext(config, contextName)
		if server == "" {
			continue
		}

		if _, exists := endpoints[name]; !exists {
			endpoints[name] = server
		}
	}

	return endpoints
}

// unmanagedClusters synthesizes a Cluster for every kubeconfig context that does NOT correspond to a
// cluster ksail already lists (discovered via an infrastructure provider or tracked as an in-flight
// job — the keys of managed), flagged unmanaged via the ksail.io/unmanaged annotation. It lets ksail
// see clusters that exist in the user's kubeconfig but were not provisioned by ksail (a managed
// EKS/GKE/AKS cluster, a kubeadm cluster, a colleague's cluster) so read/operate surfaces can work
// against them within limits while ksail-only operations are refused. Best-effort and offline, exactly
// like clusterEndpoints: no cluster round-trips, and a nil config (unreadable kubeconfig) yields none.
// Contexts are emitted in sorted order so the list is stable.
func unmanagedClusters(
	config *clientcmdapi.Config,
	managed map[string]listEntry,
) []v1alpha1.Cluster {
	// The enumerate-and-dedup skeleton (sorted context names, minus any whose raw or ksail-detected
	// name is in the managed set) is shared with the CLI's cluster list via
	// clusterdiscovery.UnmanagedContextNames, so both surfaces stay aligned and cannot drift.
	contextNames := clusterdiscovery.UnmanagedContextNames(config, func(name string) bool {
		_, ok := managed[name]

		return ok
	})

	items := make([]v1alpha1.Cluster, 0, len(contextNames))
	for _, contextName := range contextNames {
		endpoint := endpointForContext(config, contextName)
		items = append(items, newUnmanagedCluster(contextName, endpoint))
	}

	return items
}

// newUnmanagedCluster builds the Cluster ksail surfaces for a kubeconfig context it does not manage.
// It is keyed by the context name, carries the ksail.io/unmanaged=true annotation (the stable marker
// every surface reads), and reports a Ready=False/reason=Unmanaged condition — reusing the same
// "Ready" condition the web UI already renders for stopped clusters, so an unmanaged cluster is never
// shown green/normal even before a surface adds a dedicated badge. Distribution and provider are left
// empty: without a ksail spec they are unknown.
func newUnmanagedCluster(contextName, endpoint string) v1alpha1.Cluster {
	cluster := v1alpha1.Cluster{}
	cluster.Name = contextName
	cluster.Namespace = localNamespace
	cluster.Annotations = map[string]string{v1alpha1.UnmanagedAnnotation: "true"}
	cluster.Status.Endpoint = endpoint
	cluster.Status.Conditions = []metav1.Condition{unmanagedCondition()}

	return cluster
}

// unmanagedCondition is the Ready=False condition attached to an unmanaged (kubeconfig-only) cluster.
func unmanagedCondition() metav1.Condition {
	return metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionFalse,
		Reason:  "Unmanaged",
		Message: "Cluster is present in the kubeconfig but not managed by ksail",
	}
}
