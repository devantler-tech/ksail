package calicoinstaller

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Installer implements the installer.Installer interface for Calico.
type Installer struct {
	*cni.InstallerBase

	distribution v1alpha1.Distribution
}

// NewInstaller creates a new Calico installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return NewInstallerWithDistribution(client, kubeconfig, context, timeout, "")
}

// NewInstallerWithDistribution creates a new Calico installer with distribution-specific configuration.
func NewInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *Installer {
	calicoInstaller := &Installer{
		distribution: distribution,
	}
	calicoInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
	)

	return calicoInstaller
}

// Install installs or upgrades Calico via its Helm chart.
func (c *Installer) Install(ctx context.Context) error {
	// For Talos, we need to create namespaces with PSS labels before installing
	// because Talos has PSS enforcement enabled by default.
	// We also need to wait for API server stability as the API server may be
	// unstable immediately after bootstrap.
	if c.distribution == v1alpha1.DistributionTalos {
		err := c.WaitForAPIServerStability(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for API server stability: %w", err)
		}

		err = c.ensurePrivilegedNamespaces(ctx)
		if err != nil {
			return fmt.Errorf("failed to create privileged namespaces: %w", err)
		}
	}

	err := c.helmInstallOrUpgradeCalico(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Calico: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for Calico.
func (c *Installer) Uninstall(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	err = client.UninstallRelease(ctx, "calico", "tigera-operator")
	if err != nil {
		return fmt.Errorf("failed to uninstall calico release: %w", err)
	}

	return nil
}

// Images returns the container images used by Calico.
func (c *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := c.ImagesFromChart(ctx, c.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("get calico images: %w", err)
	}

	return images, nil
}

func (c *Installer) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     "calico",
		ChartName:       "projectcalico/tigera-operator",
		Namespace:       "tigera-operator",
		RepoURL:         "https://docs.tigera.io/calico/charts",
		CreateNamespace: true,
		SetJSONVals:     c.getCalicoValues(),
		Timeout:         c.GetTimeout(),
	}
}

// --- internals ---

func (c *Installer) helmInstallOrUpgradeCalico(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	repoConfig := helm.RepoConfig{
		Name:     "projectcalico",
		URL:      "https://docs.tigera.io/calico/charts",
		RepoName: "calico",
	}

	chartConfig := helm.ChartConfig{
		ReleaseName:     "calico",
		ChartName:       "projectcalico/tigera-operator",
		Namespace:       "tigera-operator",
		RepoURL:         "https://docs.tigera.io/calico/charts",
		CreateNamespace: true,
		SetJSONVals:     c.getCalicoValues(),
		SkipWait:        true,
	}

	err = helm.InstallOrUpgradeChart(ctx, client, repoConfig, chartConfig, c.GetTimeout())
	if err != nil && isAPIDiscoveryError(err) {
		waitErr := c.waitForCalicoCRDs(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait for calico CRDs: %w", waitErr)
		}

		err = helm.InstallOrUpgradeChart(ctx, client, repoConfig, chartConfig, c.GetTimeout())
	}

	if err != nil {
		return fmt.Errorf("install or upgrade calico: %w", err)
	}

	return nil
}

// getCalicoValues returns the Helm values for Calico based on the distribution.
func (c *Installer) getCalicoValues() map[string]string {
	values := defaultCalicoValues()

	// Add distribution-specific values
	switch c.distribution {
	case v1alpha1.DistributionTalos:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico
		maps.Copy(values, talosCalicoValues())
	case v1alpha1.DistributionVanilla, v1alpha1.DistributionK3s:
		// Vanilla and K3s use default values
	}

	return values
}

func defaultCalicoValues() map[string]string {
	return map[string]string{}
}

// talosCalicoValues returns Talos-specific Calico configuration.
// These settings are required for Calico to work correctly on Talos Linux.
// See: https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico
// See: https://github.com/projectcalico/calico/blob/main/charts/tigera-operator/values.yaml
func talosCalicoValues() map[string]string {
	return map[string]string{
		// Talos uses a read-only filesystem, so kubelet volume plugin path must be None
		// This is under installation. in the Helm chart values.yaml
		"installation.kubeletVolumePluginPath": `"None"`,
		// Use NFTables dataplane which is recommended for Talos
		"installation.calicoNetwork.linuxDataplane": `"Nftables"`,
		// Disable BGP for Docker-based environments
		"installation.calicoNetwork.bgp": `"Disabled"`,
		// IP pool configuration matching Siderolabs docs exactly
		"installation.calicoNetwork.ipPools[0].name":          `"default-ipv4-ippool"`,
		"installation.calicoNetwork.ipPools[0].blockSize":     "26",
		"installation.calicoNetwork.ipPools[0].cidr":          `"10.244.0.0/16"`,
		"installation.calicoNetwork.ipPools[0].encapsulation": `"VXLAN"`,
		"installation.calicoNetwork.ipPools[0].natOutgoing":   `"Enabled"`,
		"installation.calicoNetwork.ipPools[0].nodeSelector":  `"all()"`,
	}
}

func (c *Installer) waitForCalicoCRDs(ctx context.Context) error {
	restConfig, err := k8s.BuildRESTConfig(c.GetKubeconfig(), c.GetContext())
	if err != nil {
		return fmt.Errorf("build REST config: %w", err)
	}

	client, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create apiextensions client: %w", err)
	}

	for _, name := range calicoCRDNames() {
		pollErr := readiness.PollForReadiness(
			ctx,
			c.GetTimeout(),
			func(ctx context.Context) (bool, error) {
				crd, getErr := client.
					ApiextensionsV1().
					CustomResourceDefinitions().
					Get(ctx, name, metav1.GetOptions{})
				if errors.IsNotFound(getErr) {
					return false, nil
				}

				if getErr != nil {
					return false, fmt.Errorf("get CRD %s: %w", name, getErr)
				}

				return isCRDEstablished(crd), nil
			},
		)
		if pollErr != nil {
			return fmt.Errorf("wait for CRD %s: %w", name, pollErr)
		}
	}

	return nil
}

func isAPIDiscoveryError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	return strings.Contains(errMsg, "no matches for kind") ||
		strings.Contains(errMsg, "could not find the requested resource")
}

func isCRDEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established &&
			cond.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}

	return false
}

func calicoCRDNames() []string {
	return []string{
		"goldmanes.operator.tigera.io",
		"imagesets.operator.tigera.io",
		"installations.operator.tigera.io",
		"managementclusterconnections.operator.tigera.io",
		"tigerastatuses.operator.tigera.io",
		"whiskers.operator.tigera.io",
	}
}

func calicoNamespaces() []string {
	return []string{"tigera-operator", "calico-system"}
}

// ensurePrivilegedNamespaces creates the required namespaces with PSS labels for Talos.
// Talos has PodSecurity Standard enforcement enabled by default, so we need to label
// the namespaces as "privileged" to allow the CNI pods to run.
func (c *Installer) ensurePrivilegedNamespaces(ctx context.Context) error {
	clientset, err := k8s.NewClientset(c.GetKubeconfig(), c.GetContext())
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	for _, ns := range calicoNamespaces() {
		err := c.ensurePrivilegedNamespace(ctx, clientset, ns)
		if err != nil {
			return fmt.Errorf("ensure namespace %s: %w", ns, err)
		}
	}

	return nil
}

// ensurePrivilegedNamespace delegates to k8s.EnsurePrivilegedNamespace to create
// or update a namespace with PSS privileged labels.
func (c *Installer) ensurePrivilegedNamespace(
	ctx context.Context,
	clientset kubernetes.Interface,
	name string,
) error {
	err := k8s.EnsurePrivilegedNamespace(ctx, clientset, name)
	if err != nil {
		return fmt.Errorf("ensure privileged namespace %s: %w", name, err)
	}

	return nil
}
