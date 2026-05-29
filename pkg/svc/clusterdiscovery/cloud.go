package clusterdiscovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	"k8s.io/client-go/kubernetes"
)

// listHetzner lists Talos clusters on Hetzner Cloud. With no token configured it skips silently.
func (d *Discoverer) listHetzner(ctx context.Context) ([]Cluster, error) {
	lister := d.Hetzner
	if lister == nil {
		token := d.resolver().Value(credentials.HetznerToken)
		if token == "" {
			return nil, nil
		}

		lister = hetzner.NewProviderFromToken(token)
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Hetzner: %w", err)
	}

	return clustersWithDistribution(
		names,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderHetzner,
	), nil
}

// listOmni lists Talos clusters managed by Sidero Omni. Without both an endpoint and a service
// account key it skips silently.
func (d *Discoverer) listOmni(ctx context.Context) ([]Cluster, error) {
	lister := d.Omni
	if lister == nil {
		endpoint := d.resolver().Value(credentials.OmniEndpoint)
		serviceAccountKey := d.resolver().Value(credentials.OmniServiceAccountKey)

		if endpoint == "" || serviceAccountKey == "" {
			return nil, nil
		}

		client, err := omniclient.New(endpoint, omniclient.WithServiceAccount(serviceAccountKey))
		if err != nil {
			return nil, fmt.Errorf("create Omni client: %w", err)
		}

		lister = omni.NewProvider(client)
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Omni: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionTalos, v1alpha1.ProviderOmni), nil
}

// listAWS lists EKS clusters. It skips silently unless AWS appears configured and the eksctl binary
// is on PATH, so the common no-AWS case costs nothing and never emits a warning.
func (d *Discoverer) listAWS(ctx context.Context) ([]Cluster, error) {
	lister := d.AWS
	if lister == nil {
		if !d.awsConfigured() || !d.eksctlAvailable() {
			return nil, nil
		}

		client := eksctlclient.NewClient()

		provider, err := awsprovider.NewProvider(client, d.resolver().Value(credentials.AWSRegion))
		if err != nil {
			return nil, fmt.Errorf("create AWS provider: %w", err)
		}

		lister = provider
	}

	names, err := lister.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("query EKS: %w", err)
	}

	return clustersWithDistribution(names, v1alpha1.DistributionEKS, v1alpha1.ProviderAWS), nil
}

// listKubernetes lists clusters nested inside a host Kubernetes cluster. With no reachable host
// kubeconfig it skips silently.
func (d *Discoverer) listKubernetes(ctx context.Context) ([]Cluster, error) {
	lister := d.Kubernetes
	if lister == nil {
		provider, err := kubernetesProviderFromConfig()
		if err != nil {
			return nil, err
		}

		if provider == nil {
			return nil, nil
		}

		lister = provider
	}

	infos, err := lister.ListAllClustersWithDistribution(ctx)
	if err != nil {
		return nil, fmt.Errorf("query Kubernetes host: %w", err)
	}

	return kubernetesClusters(infos), nil
}

// kubernetesClusters converts the provider's ClusterInfo to discovered clusters, mapping the
// detected distribution string to its enum (falling back to Vanilla on an unknown value).
func kubernetesClusters(infos []kubernetesprovider.ClusterInfo) []Cluster {
	clusters := make([]Cluster, 0, len(infos))

	for _, info := range infos {
		var distribution v1alpha1.Distribution

		err := distribution.Set(info.Distribution)
		if err != nil {
			distribution = v1alpha1.DistributionVanilla
		}

		clusters = append(clusters, Cluster{
			Name:         info.Name,
			Distribution: distribution,
			Provider:     v1alpha1.ProviderKubernetes,
		})
	}

	return clusters
}

// awsConfigured reports whether AWS credentials appear set up: static keys or a named profile in
// the environment, or a shared credentials/config file under ~/.aws. Region alone does not count.
func (d *Discoverer) awsConfigured() bool {
	resolver := d.resolver()
	if resolver.Value(credentials.AWSAccessKeyID) != "" ||
		resolver.Value(credentials.AWSProfile) != "" {
		return true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	for _, name := range []string{"credentials", "config"} {
		_, statErr := os.Stat(filepath.Join(home, ".aws", name))
		if statErr == nil {
			return true
		}
	}

	return false
}

// eksctlAvailable reports whether the eksctl binary (which the AWS provider shells out to) is on
// PATH. AWS discovery is skipped when it is not, to avoid emitting a warning on every poll.
func (d *Discoverer) eksctlAvailable() bool {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	_, err := lookPath("eksctl")

	return err == nil
}

// kubernetesProviderFromConfig builds a host Kubernetes provider from KSAIL_HOST_KUBECONFIG /
// KSAIL_HOST_CONTEXT (or the default kubeconfig). It returns (nil, nil) when no usable host
// kubeconfig is present so the caller skips Kubernetes discovery silently. Mirrors the resolution
// `ksail cluster list` used before this logic was centralized here.
func kubernetesProviderFromConfig() (*kubernetesprovider.Provider, error) {
	kubeconfigPath := os.Getenv("KSAIL_HOST_KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = k8s.DefaultKubeconfigPath()
	}

	expandedPath, err := fsutil.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	// Skip silently when the kubeconfig file does not exist.
	//nolint:gosec // path is the user's kubeconfig, canonicalized below before use.
	_, statErr := os.Stat(expandedPath)
	if os.IsNotExist(statErr) {
		return nil, nil //nolint:nilnil // "no host kubeconfig" is a skip, not an error.
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	restConfig, err := k8s.BuildRESTConfig(canonicalPath, os.Getenv("KSAIL_HOST_CONTEXT"))
	if err != nil {
		// Present but invalid/unreachable host cluster: skip silently.
		return nil, nil //nolint:nilnil,nilerr // intentionally swallow: host cluster unavailable.
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	provider, err := kubernetesprovider.NewProvider(clientset, v1alpha1.OptionsKubernetes{})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes provider: %w", err)
	}

	return provider, nil
}
