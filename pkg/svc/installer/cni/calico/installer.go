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
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CalicoInstaller implements the installer.Installer interface for Calico.
type CalicoInstaller struct {
	*cni.InstallerBase

	distribution v1alpha1.Distribution
}

// NewCalicoInstaller creates a new Calico installer instance.
func NewCalicoInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *CalicoInstaller {
	return NewCalicoInstallerWithDistribution(client, kubeconfig, context, timeout, "")
}

// NewCalicoInstallerWithDistribution creates a new Calico installer with distribution-specific configuration.
func NewCalicoInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *CalicoInstaller {
	calicoInstaller := &CalicoInstaller{
		distribution: distribution,
	}
	calicoInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
		calicoInstaller.waitForReadiness,
	)

	return calicoInstaller
}

// Install installs or upgrades Calico via its Helm chart.
func (c *CalicoInstaller) Install(ctx context.Context) error {
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

// SetWaitForReadinessFunc overrides the readiness wait function. Primarily used for testing.
func (c *CalicoInstaller) SetWaitForReadinessFunc(waitFunc func(context.Context) error) {
	c.InstallerBase.SetWaitForReadinessFunc(waitFunc, c.waitForReadiness)
}

// Uninstall removes the Helm release for Calico.
func (c *CalicoInstaller) Uninstall(ctx context.Context) error {
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

// --- internals ---

func (c *CalicoInstaller) helmInstallOrUpgradeCalico(ctx context.Context) error {
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
func (c *CalicoInstaller) getCalicoValues() map[string]string {
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

func (c *CalicoInstaller) waitForReadiness(ctx context.Context) error {
	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "tigera-operator", Name: "tigera-operator"},
		{Type: "daemonset", Namespace: "calico-system", Name: "calico-node"},
		{Type: "deployment", Namespace: "calico-system", Name: "calico-kube-controllers"},
	}

	err := installer.WaitForResourceReadiness(
		ctx,
		c.GetKubeconfig(),
		c.GetContext(),
		checks,
		c.GetTimeout(),
		"calico",
	)
	if err != nil {
		return fmt.Errorf("wait for calico readiness: %w", err)
	}

	return nil
}

func (c *CalicoInstaller) waitForCalicoCRDs(ctx context.Context) error {
	restConfig, err := k8s.BuildRESTConfig(c.GetKubeconfig(), c.GetContext())
	if err != nil {
		return fmt.Errorf("build REST config: %w", err)
	}

	client, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create apiextensions client: %w", err)
	}

	for _, name := range calicoCRDNames() {
		pollErr := k8s.PollForReadiness(
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
		if cond.Type == "Established" && cond.Status == "True" {
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
func (c *CalicoInstaller) ensurePrivilegedNamespaces(ctx context.Context) error {
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

// ensurePrivilegedNamespace creates or updates a namespace with PSS privileged labels.
func (c *CalicoInstaller) ensurePrivilegedNamespace(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	name string,
) error {
	pssLabels := map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}

	namespace, err := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the namespace with PSS labels
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: pssLabels,
				},
			}

			_, err = clientset.CoreV1().Namespaces().Create(ctx, newNS, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("create namespace: %w", err)
			}

			return nil
		}

		return fmt.Errorf("get namespace: %w", err)
	}

	// Namespace exists, ensure PSS labels are set
	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}

	updated := false

	for k, v := range pssLabels {
		if namespace.Labels[k] != v {
			namespace.Labels[k] = v
			updated = true
		}
	}

	if updated {
		_, err = clientset.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update namespace labels: %w", err)
		}
	}

	return nil
}
