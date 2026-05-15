package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Provider implements provider.Provider for Kubernetes-hosted nested clusters.
// Nested cluster nodes run as pods inside a dedicated namespace on the host cluster.
type Provider struct {
	client kubernetes.Interface
}

// NewProvider creates a new Kubernetes provider with the given host cluster client and options.
func NewProvider(client kubernetes.Interface, _ v1alpha1.OptionsKubernetes) (*Provider, error) {
	if client == nil {
		return nil, ErrHostClientRequired
	}

	return &Provider{
		client: client,
	}, nil
}

// StartNodes verifies that the nested cluster's node pods are running.
// Actual pod creation is handled by the provisioner; this method checks
// that nodes exist and returns an error if none are found.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	pods, err := p.listClusterPods(ctx, ns, clusterName)
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("%w: %s", provider.ErrNoNodes, clusterName)
	}

	return nil
}

// StopNodes deletes the nested cluster's node pods.
// The pods are not managed by a controller (they are standalone),
// so deletion effectively stops the cluster.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	namespace := NamespaceName(clusterName)

	pods, err := p.listClusterPods(ctx, namespace, clusterName)
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("%w: %s", provider.ErrNoNodes, clusterName)
	}

	// Delete pods to "stop" the cluster. KSail's Kubernetes provider uses standalone
	// (unmanaged) pods, so deleting them stops the DinD processes without a controller
	// recreating them.
	for i := range pods {
		err := p.client.CoreV1().Pods(namespace).Delete(ctx, pods[i].Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("stop nodes: delete pod %s: %w", pods[i].Name, err)
		}
	}

	return nil
}

// ListNodes returns all node pods for the given nested cluster.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	namespace := NamespaceName(clusterName)

	pods, err := p.listClusterPods(ctx, namespace, clusterName)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	nodes := make([]provider.NodeInfo, 0, len(pods))

	for i := range pods {
		nodes = append(nodes, podToNodeInfo(&pods[i], clusterName))
	}

	return nodes, nil
}

// ClusterInfo holds the name and distribution of a discovered nested cluster.
type ClusterInfo struct {
	Name         string
	Distribution string
}

// ListAllClusters returns the names of all nested clusters managed by this provider.
// It identifies clusters by listing namespaces with the KSail managed-by label.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	infos, err := p.ListAllClustersWithDistribution(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}

	return names, nil
}

// ListAllClustersWithDistribution returns the names and distributions of all
// nested clusters managed by this provider. It identifies clusters by listing
// namespaces with the KSail managed-by label, then queries pod labels for
// the distribution.
func (p *Provider) ListAllClustersWithDistribution(ctx context.Context) ([]ClusterInfo, error) {
	labelSelector := LabelManagedBy + "=" + LabelManagedByValue

	namespaces, err := p.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list all clusters: %w", err)
	}

	clusters := make([]ClusterInfo, 0, len(namespaces.Items))

	for i := range namespaces.Items {
		nsItem := namespaces.Items[i]

		// Prefer the ksail.io/cluster label for the cluster name; this works
		// regardless of namespace prefix (ksail-<name>, vcluster-<name>, etc.).
		clusterName := nsItem.Labels[LabelClusterName]
		if clusterName == "" {
			// Fall back to stripping the ksail- prefix for older namespaces.
			var ok bool

			clusterName, ok = strings.CutPrefix(nsItem.Name, NamespacePrefix)
			if !ok {
				continue
			}
		}

		distribution := p.detectDistribution(ctx, nsItem.Name, clusterName)
		clusters = append(clusters, ClusterInfo{Name: clusterName, Distribution: distribution})
	}

	return clusters, nil
}

// NodesExist returns true if node pods exist for the given nested cluster.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("kubernetes: check nodes exist: %w", err)
	}

	return exists, nil
}

// DeleteNodes removes all resources for the given nested cluster:
// pods, services, gateway resources, and the cluster namespace.
// It is idempotent: deleting an already-removed cluster is not an error.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	// Verify the namespace exists and has KSail ownership labels before deletion
	namespace, err := p.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Namespace already gone — idempotent success
			return nil
		}
		return fmt.Errorf("get namespace %s: %w", ns, err)
	}

	// Verify KSail ownership
	if namespace.Labels[LabelManagedBy] != LabelManagedByValue ||
		namespace.Labels[LabelClusterName] != clusterName {
		return fmt.Errorf("namespace %s: %w", ns, ErrNamespaceNotOwnedByKSail)
	}

	// Delete the namespace (cascading delete removes all resources within it)
	err = p.client.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete nodes: remove namespace %s: %w", ns, err)
	}

	return nil
}

// GetClusterStatus returns the provider-level status of a Kubernetes-hosted nested cluster.
func (p *Provider) GetClusterStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	result, err := provider.GetClusterStatusFromLister(
		ctx, p, clusterName, "Running",
	)
	if err != nil {
		return nil, fmt.Errorf("kubernetes cluster status: %w", err)
	}

	return result, nil
}

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil
}

// EnsureNamespace creates the cluster namespace if it does not exist.
func (p *Provider) EnsureNamespace(ctx context.Context, clusterName string) error {
	namespaceName := NamespaceName(clusterName)

	// Create or update namespace with PodSecurity privileged labels
	if err := k8s.EnsurePrivilegedNamespace(ctx, p.client, namespaceName); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", namespaceName, err)
	}

	// Merge in KSail ownership labels
	namespace, err := p.client.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get namespace %s: %w", namespaceName, err)
	}

	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}

	// Check if any KSail labels need to be added
	needsUpdate := false
	for k, v := range CommonLabels(clusterName) {
		if namespace.Labels[k] != v {
			namespace.Labels[k] = v
			needsUpdate = true
		}
	}

	if needsUpdate {
		_, err := p.client.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update namespace labels %s: %w", namespaceName, err)
		}
	}

	return nil
}

// detectDistribution queries the first pod in the cluster namespace for the
// distribution label. Returns an empty string if the distribution is unknown.
func (p *Provider) detectDistribution(ctx context.Context, namespace, clusterName string) string {
	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelClusterName + "=" + clusterName,
		Limit:         1,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}

	return pods.Items[0].Labels[LabelDistribution]
}

// listClusterPods returns all pods in the cluster namespace with the cluster label.
// Returns an empty slice (not an error) when the namespace does not exist yet.
func (p *Provider) listClusterPods(
	ctx context.Context,
	namespace, clusterName string,
) ([]corev1.Pod, error) {
	labelSelector := LabelClusterName + "=" + clusterName

	podList, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("list cluster pods: %w", err)
	}

	return podList.Items, nil
}

// podToNodeInfo converts a Kubernetes pod to a provider.NodeInfo.
func podToNodeInfo(pod *corev1.Pod, clusterName string) provider.NodeInfo {
	role := pod.Labels[LabelNodeRole]
	if role == "" {
		role = "unknown"
	}

	state := string(pod.Status.Phase)

	return provider.NodeInfo{
		Name:        pod.Name,
		ClusterName: clusterName,
		Role:        role,
		State:       state,
	}
}
