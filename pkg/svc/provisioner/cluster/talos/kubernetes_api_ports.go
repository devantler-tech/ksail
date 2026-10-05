package talosprovisioner

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/provision"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *KubernetesProvisioner) prepareNestedAPI(
	ctx context.Context,
	clusterName string,
) (*kubeprovider.ExposureResult, string, error) {
	err := p.setupDinD(ctx, clusterName)
	if err != nil {
		return nil, "", err
	}

	podIP, err := p.nestedAPIHost(ctx, clusterName)
	if err != nil {
		return nil, "", err
	}

	exposure, err := p.prepareExposure(ctx, clusterName)
	if err != nil {
		return nil, "", err
	}

	return exposure, podIP, nil
}

func (p *KubernetesProvisioner) nestedAPIHost(
	ctx context.Context,
	clusterName string,
) (string, error) {
	if p.hostClientset == nil {
		return "", ErrInvalidDinDAPIHost
	}

	namespace := kubeprovider.NamespaceName(clusterName)

	pod, err := p.hostClientset.CoreV1().Pods(namespace).
		Get(ctx, kubeprovider.DinDPodName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read DinD API binding address: %w", err)
	}

	if !ownedDinDAPIHostPod(pod, namespace, clusterName) {
		return "", ErrInvalidDinDAPIHost
	}

	address, err := netip.ParseAddr(pod.Status.PodIP)
	// The selected Talos SDK port-map parser only supports IPv4 host addresses.
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() || !dinDAPIHostReady(pod) {
		return "", ErrInvalidDinDAPIHost
	}

	return address.String(), nil
}

func ownedDinDAPIHostPod(pod *corev1.Pod, namespace, clusterName string) bool {
	return pod.Name == kubeprovider.DinDPodName && pod.Namespace == namespace &&
		pod.Labels[kubeprovider.LabelManagedBy] == kubeprovider.LabelManagedByValue &&
		pod.Labels[kubeprovider.LabelClusterName] == clusterName &&
		pod.Labels[kubeprovider.LabelApp] == kubeprovider.DinDPodName &&
		!pod.Spec.HostNetwork && pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning
}

func dinDAPIHostReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

func (p *KubernetesProvisioner) provisionNestedCluster(
	ctx context.Context,
	clusterName string,
	configs *bundle.Bundle,
	podIP string,
) (provision.Cluster, error) {
	// The SDK's loopback mapping remains available to pod port-forward during
	// bootstrap. This additional mapping accepts only Service traffic to this pod.
	return p.inner.provisionCluster(ctx, clusterName, configs,
		podIP+"::"+strconv.Itoa(k8sAPIPort)+"/tcp")
}

func (p *KubernetesProvisioner) discoverServicePort(
	ctx context.Context,
	clusterName, podIP string,
) (int, error) {
	endpoint, err := p.inner.getMappedPortEndpoint(ctx, clusterName, k8sAPIPort, podIP)
	if err != nil {
		return 0, fmt.Errorf("get Service API endpoint: %w", err)
	}

	port, err := parsePort(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse Service API port: %w", err)
	}

	return port, nil
}
