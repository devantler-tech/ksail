package metallbinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	metallbRepoName  = "metallb"
	metallbRepoURL   = "https://metallb.github.io/metallb"
	metallbRelease   = "metallb"
	metallbNamespace = "metallb-system"
	metallbChartName = "metallb/metallb"
	
	// Default IP range for Docker bridge network (172.18.0.0/16)
	// MetalLB will allocate IPs from this range for LoadBalancer services
	defaultIPRange   = "172.18.255.200-172.18.255.250"
)

// MetalLBInstaller installs or upgrades MetalLB.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
//
// MetalLB provides LoadBalancer implementation for bare metal and local development
// clusters. It works by allocating IP addresses from a configured pool and announcing
// them via Layer 2 (ARP/NDP) or BGP.
type MetalLBInstaller struct {
	client     helm.Interface
	kubeconfig string
	context    string
	timeout    time.Duration
	ipRange    string
}

// NewMetalLBInstaller creates a new MetalLB installer instance.
// If ipRange is empty, it uses the default range suitable for Docker networks.
func NewMetalLBInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	ipRange string,
) *MetalLBInstaller {
	if ipRange == "" {
		ipRange = defaultIPRange
	}
	
	return &MetalLBInstaller{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
		ipRange:    ipRange,
	}
}

// Install installs or upgrades MetalLB via its Helm chart.
// After installation, it configures the IP address pool and L2 advertisement.
func (m *MetalLBInstaller) Install(ctx context.Context) error {
	err := m.helmInstallOrUpgradeMetalLB(ctx)
	if err != nil {
		return fmt.Errorf("failed to install metallb: %w", err)
	}
	
	// Configure IP address pool and L2 advertisement
	err = m.configureMetalLB(ctx)
	if err != nil {
		return fmt.Errorf("failed to configure metallb: %w", err)
	}
	
	return nil
}

// Uninstall removes the Helm release for MetalLB.
func (m *MetalLBInstaller) Uninstall(ctx context.Context) error {
	err := m.client.UninstallRelease(ctx, metallbRelease, metallbNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall metallb release: %w", err)
	}
	
	return nil
}

func (m *MetalLBInstaller) helmInstallOrUpgradeMetalLB(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: metallbRepoName, URL: metallbRepoURL}
	
	err := m.client.AddRepository(ctx, repoEntry, m.timeout)
	if err != nil {
		return fmt.Errorf("failed to add metallb repository: %w", err)
	}
	
	spec := &helm.ChartSpec{
		ReleaseName:     metallbRelease,
		ChartName:       metallbChartName,
		Namespace:       metallbNamespace,
		RepoURL:         metallbRepoURL,
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         m.timeout,
	}
	
	_, err = m.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install metallb chart: %w", err)
	}
	
	return nil
}

// configureMetalLB creates the IPAddressPool and L2Advertisement resources.
// These resources configure MetalLB to allocate IPs from the specified range
// and announce them via Layer 2 (ARP).
func (m *MetalLBInstaller) configureMetalLB(ctx context.Context) error {
	// Create Kubernetes dynamic client
	dynamicClient, err := k8s.NewDynamicClient(m.kubeconfig, m.context)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}
	
	// Create IPAddressPool
	err = m.createIPAddressPool(ctx, dynamicClient)
	if err != nil {
		return fmt.Errorf("failed to create ip address pool: %w", err)
	}
	
	// Create L2Advertisement
	err = m.createL2Advertisement(ctx, dynamicClient)
	if err != nil {
		return fmt.Errorf("failed to create l2 advertisement: %w", err)
	}
	
	return nil
}

func (m *MetalLBInstaller) createIPAddressPool(
	ctx context.Context,
	dynamicClient *k8s.DynamicClient,
) error {
	ipAddressPoolGVR := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "ipaddresspools",
	}
	
	ipPool := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "metallb.io/v1beta1",
			"kind":       "IPAddressPool",
			"metadata": map[string]interface{}{
				"name":      "default-pool",
				"namespace": metallbNamespace,
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					m.ipRange,
				},
			},
		},
	}
	
	_, err := dynamicClient.Resource(ipAddressPoolGVR).Namespace(metallbNamespace).
		Create(ctx, ipPool, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create IPAddressPool: %w", err)
	}
	
	return nil
}

func (m *MetalLBInstaller) createL2Advertisement(
	ctx context.Context,
	dynamicClient *k8s.DynamicClient,
) error {
	l2AdvertisementGVR := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "l2advertisements",
	}
	
	l2Advert := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "metallb.io/v1beta1",
			"kind":       "L2Advertisement",
			"metadata": map[string]interface{}{
				"name":      "default-l2-advert",
				"namespace": metallbNamespace,
			},
			"spec": map[string]interface{}{
				"ipAddressPools": []interface{}{
					"default-pool",
				},
			},
		},
	}
	
	_, err := dynamicClient.Resource(l2AdvertisementGVR).Namespace(metallbNamespace).
		Create(ctx, l2Advert, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create L2Advertisement: %w", err)
	}
	
	return nil
}
