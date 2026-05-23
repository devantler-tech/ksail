package operator

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// vclusterNamespacePrefix and vclusterSecretPrefix mirror the names the vcluster provisioner
	// gives a cluster's namespace ("vcluster-<name>") and kubeconfig Secret ("vc-<name>").
	vclusterNamespacePrefix = "vcluster-"
	vclusterSecretPrefix    = "vc-"
	// vclusterKubeconfigKey is the Secret data key holding the kubeconfig YAML.
	vclusterKubeconfigKey = "config"
	// vclusterServerName is the TLS server name on the vcluster API server certificate. The
	// in-cluster Service DNS name is not a SAN, so the served certificate is verified against this
	// name (with the kubeconfig's CA) while connecting to the Service address.
	vclusterServerName = "kubernetes"
	// vclusterAPIPort is the port the vcluster API Service exposes in the hub.
	vclusterAPIPort = 443
)

// errMissingKubeconfig is returned when the vcluster kubeconfig Secret has no kubeconfig data yet.
var errMissingKubeconfig = errors.New("vcluster kubeconfig secret has no config data")

// ObserveVClusterStatus reports the endpoint, kubeconfig Secret, and node readiness of the vcluster
// provisioned for a Cluster. It reads the vcluster kubeconfig Secret from the hub, derives the
// stable in-cluster API endpoint, and (when reachable) connects to count Ready nodes. It satisfies
// controller.StatusObserver and is best-effort: a not-yet-ready cluster yields partial results.
func ObserveVClusterStatus(
	ctx context.Context,
	hub client.Reader,
	cluster *v1alpha1.Cluster,
) (controller.ObservedStatus, error) {
	// Endpoint/node observation here is vcluster-specific (it reads the vcluster kubeconfig Secret
	// the in-cluster provisioner publishes). Other distributions report nothing for now.
	if cluster.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return controller.ObservedStatus{}, nil
	}

	name := controller.ProvisionedName(cluster)
	namespace := vclusterNamespacePrefix + name
	secretName := vclusterSecretPrefix + name

	var secret corev1.Secret

	err := hub.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, &secret)
	if apierrors.IsNotFound(err) {
		// The provisioner has not published the kubeconfig yet; nothing to report this pass.
		return controller.ObservedStatus{}, nil
	}

	if err != nil {
		return controller.ObservedStatus{}, fmt.Errorf("get vcluster kubeconfig secret: %w", err)
	}

	// In-cluster API endpoint: the vcluster Service is named <name> in namespace vcluster-<name>.
	endpoint := fmt.Sprintf("https://%s.%s.svc:%d", name, namespace, vclusterAPIPort)

	observed := controller.ObservedStatus{
		Endpoint: endpoint,
		KubeconfigSecret: &v1alpha1.SecretReference{
			Namespace: namespace,
			Name:      secretName,
		},
	}

	ready, total, err := countNodes(ctx, secret.Data[vclusterKubeconfigKey], endpoint)
	if err != nil {
		// Endpoint and Secret reference are still useful; surface the node-count failure for logging.
		return observed, fmt.Errorf("count vcluster nodes: %w", err)
	}

	observed.NodesReady = ready
	observed.NodesTotal = total
	observed.NodesObserved = true

	return observed, nil
}

// countNodes connects to the child cluster using its kubeconfig (reaching the API server at the
// in-cluster endpoint) and returns the number of Ready and total nodes.
func countNodes(
	ctx context.Context,
	kubeconfig []byte,
	endpoint string,
) (int32, int32, error) {
	if len(kubeconfig) == 0 {
		return 0, 0, errMissingKubeconfig
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return 0, 0, fmt.Errorf("parse kubeconfig: %w", err)
	}

	// The kubeconfig points at a local port-forward address; reach the API server at its in-cluster
	// Service instead, verifying the served certificate against its actual SAN.
	restConfig.Host = endpoint
	restConfig.ServerName = vclusterServerName

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return 0, 0, fmt.Errorf("build clientset: %w", err)
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("list nodes: %w", err)
	}

	var ready, total int32

	for i := range nodes.Items {
		total++

		if isNodeReady(&nodes.Items[i]) {
			ready++
		}
	}

	return ready, total, nil
}

// isNodeReady reports whether a node's Ready condition is True.
func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}
