package metallbinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ErrCRDTimeout is returned when MetalLB CRDs are not available within the expected time.
var ErrCRDTimeout = fmt.Errorf("timed out waiting for metallb CRDs after %s", crdPollTimeout)

const (
	metallbRepoName  = "metallb"
	metallbRepoURL   = "https://metallb.github.io/metallb"
	metallbRelease   = "metallb"
	metallbNamespace = "metallb-system"
	metallbChartName = "metallb/metallb"
	defaultIPRange   = "172.18.255.200-172.18.255.250"

	// crdPollInterval is the interval between CRD readiness checks.
	crdPollInterval = 2 * time.Second
	// crdPollTimeout is the maximum time to wait for MetalLB CRDs to be registered.
	crdPollTimeout = 60 * time.Second
)

// Installer installs or upgrades MetalLB.
//
// It embeds helmutil.Base for the Helm lifecycle and adds a post-install step
// that creates the required IPAddressPool and L2Advertisement resources.
//
// MetalLB provides LoadBalancer implementation for bare metal and local development
// clusters. It works by allocating IP addresses from a configured pool and announcing
// them via Layer 2 (ARP/NDP).
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
	ipRange    string
}

// NewInstaller creates a new MetalLB installer instance.
// If ipRange is empty, it uses the default range suitable for Docker networks.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	ipRange string,
) *Installer {
	if ipRange == "" {
		ipRange = defaultIPRange
	}

	return &Installer{
		Base: helmutil.NewBase(
			"metallb",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: metallbRepoName,
				URL:  metallbRepoURL,
			},
			&helm.ChartSpec{
				ReleaseName:     metallbRelease,
				ChartName:       metallbChartName,
				Namespace:       metallbNamespace,
				RepoURL:         metallbRepoURL,
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
			},
		),
		kubeconfig: kubeconfig,
		context:    context,
		ipRange:    ipRange,
	}
}

// Install installs or upgrades MetalLB via its Helm chart, then configures
// the IPAddressPool and L2Advertisement resources.
func (m *Installer) Install(ctx context.Context) error {
	err := m.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install metallb: %w", err)
	}

	err = m.configureMetalLB(ctx)
	if err != nil {
		return fmt.Errorf("failed to configure metallb: %w", err)
	}

	return nil
}

// configureMetalLB waits for the MetalLB CRDs to become available and then
// creates or updates the IPAddressPool and L2Advertisement resources.
func (m *Installer) configureMetalLB(ctx context.Context) error {
	dynamicClient, err := k8s.NewDynamicClient(m.kubeconfig, m.context)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Wait for MetalLB CRDs to be registered before creating resources.
	err = m.waitForCRDs(ctx, dynamicClient)
	if err != nil {
		return fmt.Errorf("metallb CRDs not ready: %w", err)
	}

	err = m.ensureIPAddressPool(ctx, dynamicClient)
	if err != nil {
		return fmt.Errorf("failed to ensure ip address pool: %w", err)
	}

	err = m.ensureL2Advertisement(ctx, dynamicClient)
	if err != nil {
		return fmt.Errorf("failed to ensure l2 advertisement: %w", err)
	}

	return nil
}

// waitForCRDs polls the MetalLB IPAddressPool CRD until it is available.
func (m *Installer) waitForCRDs(ctx context.Context, client *dynamic.DynamicClient) error {
	ipAddressPoolGVR := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "ipaddresspools",
	}

	deadline := time.After(crdPollTimeout)

	ticker := time.NewTicker(crdPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled waiting for metallb CRDs: %w", ctx.Err())
		case <-deadline:
			return ErrCRDTimeout
		case <-ticker.C:
			_, err := client.Resource(ipAddressPoolGVR).Namespace(metallbNamespace).
				List(ctx, metav1.ListOptions{Limit: 1})
			if err == nil {
				return nil
			}
		}
	}
}

// ensureIPAddressPool creates or updates the default IPAddressPool.
func (m *Installer) ensureIPAddressPool(
	ctx context.Context,
	client *dynamic.DynamicClient,
) error {
	gvr := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "ipaddresspools",
	}

	pool := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "metallb.io/v1beta1",
			"kind":       "IPAddressPool",
			"metadata": map[string]any{
				"name":      "default-pool",
				"namespace": metallbNamespace,
			},
			"spec": map[string]any{
				"addresses": []any{
					m.ipRange,
				},
			},
		},
	}

	existing, err := client.Resource(gvr).Namespace(metallbNamespace).
		Get(ctx, "default-pool", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.Resource(gvr).Namespace(metallbNamespace).
			Create(ctx, pool, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create IPAddressPool: %w", err)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to get IPAddressPool: %w", err)
	}

	// Update existing resource
	pool.SetResourceVersion(existing.GetResourceVersion())

	_, err = client.Resource(gvr).Namespace(metallbNamespace).
		Update(ctx, pool, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update IPAddressPool: %w", err)
	}

	return nil
}

// ensureL2Advertisement creates or updates the default L2Advertisement.
func (m *Installer) ensureL2Advertisement(
	ctx context.Context,
	client *dynamic.DynamicClient,
) error {
	gvr := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "l2advertisements",
	}

	advert := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "metallb.io/v1beta1",
			"kind":       "L2Advertisement",
			"metadata": map[string]any{
				"name":      "default-l2-advert",
				"namespace": metallbNamespace,
			},
			"spec": map[string]any{
				"ipAddressPools": []any{
					"default-pool",
				},
			},
		},
	}

	existing, err := client.Resource(gvr).Namespace(metallbNamespace).
		Get(ctx, "default-l2-advert", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.Resource(gvr).Namespace(metallbNamespace).
			Create(ctx, advert, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create L2Advertisement: %w", err)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to get L2Advertisement: %w", err)
	}

	// Update existing resource
	advert.SetResourceVersion(existing.GetResourceVersion())

	_, err = client.Resource(gvr).Namespace(metallbNamespace).
		Update(ctx, advert, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update L2Advertisement: %w", err)
	}

	return nil
}
