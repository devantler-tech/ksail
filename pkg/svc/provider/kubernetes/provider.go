package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Provider implements provider.Provider for Kubernetes-hosted nested clusters.
// Nested cluster nodes run as pods inside a dedicated namespace on the host cluster.
type Provider struct {
	client kubernetes.Interface
	opts   v1alpha1.OptionsKubernetes
}

// NewProvider creates a new Kubernetes provider with the given host cluster client and options.
func NewProvider(client kubernetes.Interface, opts v1alpha1.OptionsKubernetes) (*Provider, error) {
	if client == nil {
		return nil, ErrHostClientRequired
	}

	return &Provider{
		client: client,
		opts:   opts,
	}, nil
}

// StartNodes scales the nested cluster's node pods back to the desired replica count.
// For StatefulSets this resumes the pods that were scaled to 0 by StopNodes.
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

// StopNodes scales the nested cluster's node pods to 0.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	pods, err := p.listClusterPods(ctx, ns, clusterName)
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("%w: %s", provider.ErrNoNodes, clusterName)
	}

	// Delete pods to "stop" the cluster. StatefulSet controller will not recreate
	// them if the StatefulSet itself has been scaled to 0 replicas.
	for i := range pods {
		err := p.client.CoreV1().Pods(ns).Delete(ctx, pods[i].Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("stop nodes: delete pod %s: %w", pods[i].Name, err)
		}
	}

	return nil
}

// ListNodes returns all node pods for the given nested cluster.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	ns := NamespaceName(clusterName)

	pods, err := p.listClusterPods(ctx, ns, clusterName)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	nodes := make([]provider.NodeInfo, 0, len(pods))

	for i := range pods {
		nodes = append(nodes, podToNodeInfo(&pods[i], clusterName))
	}

	return nodes, nil
}

// ListAllClusters returns the names of all nested clusters managed by this provider.
// It identifies clusters by listing namespaces with the KSail managed-by label.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	labelSelector := LabelManagedBy + "=" + LabelManagedByValue

	namespaces, err := p.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list all clusters: %w", err)
	}

	clusters := make([]string, 0, len(namespaces.Items))

	for i := range namespaces.Items {
		name := namespaces.Items[i].Name
		if clusterName, ok := strings.CutPrefix(name, NamespacePrefix); ok {
			clusters = append(clusters, clusterName)
		}
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
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	// Delete the namespace (cascading delete removes all resources within it)
	err := p.client.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	if err != nil {
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
	ns := NamespaceName(clusterName)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: CommonLabels(clusterName),
		},
	}

	_, err := p.client.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("ensure namespace %s: %w", ns, err)
	}

	return nil
}

// listClusterPods returns all pods in the cluster namespace with the cluster label.
func (p *Provider) listClusterPods(
	ctx context.Context,
	namespace, clusterName string,
) ([]corev1.Pod, error) {
	labelSelector := LabelClusterName + "=" + clusterName

	podList, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
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
