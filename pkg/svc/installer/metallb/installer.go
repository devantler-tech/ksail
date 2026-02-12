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
	// Talos enforces PodSecurity Standards by default. MetalLB speaker requires
	// NET_ADMIN, NET_RAW, SYS_ADMIN capabilities and host networking, so the
	// namespace must be labelled "privileged" before Helm installs the chart.
	err := m.ensurePrivilegedNamespace(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure privileged namespace: %w", err)
	}

	err = m.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install metallb: %w", err)
	}

	err = m.configureMetalLB(ctx)
	if err != nil {
		return fmt.Errorf("failed to configure metallb: %w", err)
	}

	return nil
}

// ensurePrivilegedNamespace creates or updates the metallb-system namespace
// with PodSecurity Standard "privileged" labels so that the speaker DaemonSet
// can obtain the elevated privileges it needs (host networking, NET_ADMIN, etc.).
func (m *Installer) ensurePrivilegedNamespace(ctx context.Context) error {
	clientset, err := k8s.NewClientset(m.kubeconfig, m.context)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	err = k8s.EnsurePrivilegedNamespace(ctx, clientset, metallbNamespace)
	if err != nil {
		return fmt.Errorf("ensure privileged namespace: %w", err)
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
// Only CRD-not-yet-registered errors (404 Not Found) are retried; other
// errors (RBAC, network, auth) cause an immediate failure.
func (m *Installer) waitForCRDs(ctx context.Context, client dynamic.Interface) error {
	ipAddressPoolGVR := schema.GroupVersionResource{
		Group:    "metallb.io",
		Version:  "v1beta1",
		Resource: "ipaddresspools",
	}

	deadline := time.After(crdPollTimeout)

	ticker := time.NewTicker(crdPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled waiting for metallb CRDs: %w", ctx.Err())
		case <-deadline:
			return fmt.Errorf(
				"timed out waiting for metallb CRDs after %s: %w",
				crdPollTimeout, lastErr,
			)
		case <-ticker.C:
			_, err := client.Resource(ipAddressPoolGVR).Namespace(metallbNamespace).
				List(ctx, metav1.ListOptions{Limit: 1})
			if err == nil {
				return nil
			}

			// Only retry on 404 (CRD not yet registered).
			// Fail immediately on unexpected errors (RBAC, network, auth).
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("unexpected error checking metallb CRDs: %w", err)
			}

			lastErr = err
		}
	}
}

// ensureIPAddressPool applies the default IPAddressPool using Server-Side Apply.
// Only fields owned by the "ksail" field manager are managed; user customizations
// to other fields are preserved.
func (m *Installer) ensureIPAddressPool(
	ctx context.Context,
	client dynamic.Interface,
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

	_, err := client.Resource(gvr).Namespace(metallbNamespace).
		Apply(ctx, "default-pool", pool, metav1.ApplyOptions{FieldManager: "ksail"})
	if err != nil {
		return fmt.Errorf("failed to apply IPAddressPool: %w", err)
	}

	return nil
}

// ensureL2Advertisement applies the default L2Advertisement using Server-Side Apply.
// Only fields owned by the "ksail" field manager are managed; user customizations
// to other fields are preserved.
func (m *Installer) ensureL2Advertisement(
	ctx context.Context,
	client dynamic.Interface,
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

	_, err := client.Resource(gvr).Namespace(metallbNamespace).
		Apply(ctx, "default-l2-advert", advert, metav1.ApplyOptions{FieldManager: "ksail"})
	if err != nil {
		return fmt.Errorf("failed to apply L2Advertisement: %w", err)
	}

	return nil
}
