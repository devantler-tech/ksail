package operator

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// vclusterConnectionFor derives the in-hub vcluster connection coordinates for a Cluster. The
// coordinates (namespace/secret/endpoint) come from the vcluster provisioner package, the single
// source of truth for the naming contract, so a naming change there can never silently desync the
// operator's status observation or resource browsing.
func vclusterConnectionFor(cluster *v1alpha1.Cluster) vclusterprovisioner.Connection {
	return vclusterprovisioner.ConnectionFor(controller.ProvisionedName(cluster))
}

// restConfigForChild parses a child cluster's kubeconfig and points it at the given in-cluster endpoint,
// verifying the served certificate against the vcluster server name (the in-cluster Service DNS name is
// not a SAN). Shared by countNodes (status) and childClusterRESTConfig (resource browsing).
func restConfigForChild(kubeconfig []byte, endpoint string) (*rest.Config, error) {
	if len(kubeconfig) == 0 {
		return nil, errMissingKubeconfig
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	restConfig.Host = endpoint
	restConfig.ServerName = vclusterprovisioner.InClusterServerName

	return restConfig, nil
}

// childClusterRESTConfig builds a rest.Config for a Cluster's managed vcluster. It errors for a
// non-VCluster cluster (resource browsing is vcluster-only, like status observation) or one whose
// kubeconfig Secret is not published yet.
func childClusterRESTConfig(
	ctx context.Context,
	hub client.Reader,
	cluster *v1alpha1.Cluster,
) (*rest.Config, error) {
	if cluster.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return nil, fmt.Errorf(
			"%w: resource access requires a VCluster cluster, got %s",
			api.ErrNotSupported, cluster.Spec.Cluster.Distribution,
		)
	}

	conn := vclusterConnectionFor(cluster)

	var secret corev1.Secret

	key := types.NamespacedName{Namespace: conn.Namespace, Name: conn.SecretName}

	err := hub.Get(ctx, key, &secret)
	if err != nil {
		return nil, fmt.Errorf("get vcluster kubeconfig secret: %w", err)
	}

	return restConfigForChild(secret.Data[vclusterprovisioner.KubeconfigSecretKey], conn.Endpoint)
}

// childClusterDynamicClient builds a dynamic client for a Cluster's managed vcluster. It is wired into
// the operator API backend (NewCRClusterServiceWithResources) so the resource browser can read the
// child cluster.
func childClusterDynamicClient(
	ctx context.Context,
	hub client.Reader,
	cluster *v1alpha1.Cluster,
) (dynamic.Interface, error) {
	restConfig, err := childClusterRESTConfig(ctx, hub, cluster)
	if err != nil {
		return nil, err
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}

	return dyn, nil
}
