package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusteridentity"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

// identityMarkerTimeout bounds the marker write, including the wait for a new API server to
// accept connections, so metadata can never hold up a create or update for long.
const identityMarkerTimeout = 60 * time.Second

// identityMarkerClientFactory connects to the cluster the marker is written into.
type identityMarkerClientFactory func(kubeconfigPath, kubeContext string) (kubernetes.Interface, error)

func newIdentityMarkerClient(kubeconfigPath, kubeContext string) (kubernetes.Interface, error) {
	client, err := k8s.NewClientset(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}

	return client, nil
}

// writeClusterIdentityMarker records in the cluster that KSail provisioned it (ksail#7191).
// The marker is metadata, not infrastructure, so a failure is reported as a warning and never
// fails the create or update that called it.
func writeClusterIdentityMarker(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) {
	reportIdentityMarker(cmd, clusterName, clusterCfg, currentIdentityMarkerClient())
}

// currentIdentityMarkerClient returns the client factory for the marker, allowing test override.
func currentIdentityMarkerClient() identityMarkerClientFactory {
	identityMarkerClientMu.RLock()
	defer identityMarkerClientMu.RUnlock()

	if identityMarkerClientOverride != nil {
		return identityMarkerClientOverride
	}

	return newIdentityMarkerClient
}

func reportIdentityMarker(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	newClient identityMarkerClientFactory,
) {
	err := writeIdentityMarker(cmd.Context(), clusterName, clusterCfg, newClient)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(), "failed to write the cluster identity marker: %v", err)
	}
}

func writeIdentityMarker(
	ctx context.Context,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	newClient identityMarkerClientFactory,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	client, err := newClient(kubeconfigPath, identityMarkerContext(clusterCfg, clusterName))
	if err != nil {
		return fmt.Errorf("connect to the cluster: %w", err)
	}

	provider := clusterCfg.Spec.Cluster.Provider
	if provider == "" {
		provider = v1alpha1.DefaultProviderForDistribution(clusterCfg.Spec.Cluster.Distribution)
	}

	ctx, cancel := context.WithTimeout(ctx, identityMarkerTimeout)
	defer cancel()

	err = clusteridentity.Write(ctx, client, clusteridentity.Marker{
		ClusterName:  clusterName,
		Distribution: string(clusterCfg.Spec.Cluster.Distribution),
		Provider:     string(provider),
		KSailVersion: buildmeta.Version,
	})
	if err != nil {
		return fmt.Errorf("write %s/%s: %w", clusteridentity.Namespace, clusteridentity.Name, err)
	}

	return nil
}

// identityMarkerContext is the context of the cluster KSail just created or updated: the
// configured one, else the context the provisioner writes. The distribution default alone is
// wrong for nested clusters (a vCluster on the Kubernetes provider is "vcluster-<name>", not
// "vcluster-docker_<name>"), so it goes through resolveCreatedContextName like create does.
func identityMarkerContext(clusterCfg *v1alpha1.Cluster, clusterName string) string {
	if configured := strings.TrimSpace(
		clusterCfg.Spec.Cluster.Connection.Context,
	); configured != "" {
		return configured
	}

	provider := clusterCfg.Spec.Cluster.Provider
	if provider == "" {
		provider = v1alpha1.DefaultProviderForDistribution(clusterCfg.Spec.Cluster.Distribution)
	}

	return resolveCreatedContextName(clusterCfg.Spec.Cluster.Distribution, provider, clusterName)
}
