package calicoinstaller

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Installer implements the installer.Installer interface for Calico.
type Installer struct {
	*cni.InstallerBase

	distribution v1alpha1.Distribution
	// apiServerChecker is called before Helm operations to ensure the API server
	// is stable. It defaults to WaitForAPIServerStability and can be overridden
	// in tests to avoid needing a real cluster.
	apiServerChecker func(ctx context.Context) error
	// retryBackoff waits between transient API-unavailable Helm install retries.
	// It defaults to a fixed delay and can be overridden in tests.
	retryBackoff func(ctx context.Context) error
}

const (
	// calicoInstallRetryAttempts is the number of install attempts for K3s.
	// K3s may experience transient API-unavailable errors during bootstrap even
	// after the pre-install stability check passes, due to K3s's bootstrap sequence.
	// Five attempts with 10s backoff (up to 40s of additional wait) is sufficient
	// to cover the observed K3s stabilization window in CI (see recurring issue #4196).
	calicoInstallRetryAttempts = 5
	calicoInstallRetryBackoff  = 10 * time.Second
)

// NewInstaller creates a new Calico installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, kubeContext string,
	timeout time.Duration,
) *Installer {
	return NewInstallerWithDistribution(client, kubeconfig, kubeContext, timeout, "")
}

// NewInstallerWithDistribution creates a new Calico installer with distribution-specific configuration.
func NewInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, kubeContext string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *Installer {
	calicoInstaller := &Installer{
		distribution: distribution,
	}
	calicoInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		kubeContext,
		timeout,
	)
	calicoInstaller.apiServerChecker = calicoInstaller.WaitForAPIServerStability
	calicoInstaller.retryBackoff = func(ctx context.Context) error {
		timer := time.NewTimer(calicoInstallRetryBackoff)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return fmt.Errorf("context done while waiting for retry backoff: %w", ctx.Err())
		case <-timer.C:
			return nil
		}
	}

	return calicoInstaller
}

// Install installs or upgrades Calico via its Helm chart.
func (c *Installer) Install(ctx context.Context) error {
	err := c.PrepareInstall(ctx, c.distribution, c.apiServerChecker)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}

	// For Talos, we need to create namespaces with PSS labels before installing
	// because Talos has PSS enforcement enabled by default.
	if c.distribution == v1alpha1.DistributionTalos {
		err = c.ensurePrivilegedNamespaces(ctx)
		if err != nil {
			return fmt.Errorf("failed to create privileged namespaces: %w", err)
		}
	}

	err = c.helmInstallOrUpgradeCalico(ctx)
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
		Version:         chartVersion(),
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

	// Add the Calico Helm repository once before any install attempts to avoid
	// repeating the AddRepository call on every outer retry iteration.
	repoEntry := &helm.RepositoryEntry{
		Name: "projectcalico",
		URL:  "https://docs.tigera.io/calico/charts",
	}

	addErr := client.AddRepository(ctx, repoEntry, c.GetTimeout())
	if addErr != nil {
		return fmt.Errorf("add calico repository: %w", addErr)
	}

	spec := c.chartSpec()
	spec.Atomic = true
	spec.Silent = true
	spec.UpgradeCRDs = true
	spec.Wait = false
	spec.WaitForJobs = false

	// K3s can have transient API-unavailable failures during bootstrap even
	// after the pre-install stability check passes, due to K3s's bootstrap
	// sequence. Retry with backoff for K3s only; a single attempt is correct
	// for other distributions where such errors indicate a genuine problem.
	maxAttempts := 1
	if c.distribution == v1alpha1.DistributionK3s {
		maxAttempts = calicoInstallRetryAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = c.attemptCalicoInstall(ctx, client, spec)
		if err == nil {
			return nil
		}

		if !isAPIServerUnavailableError(err) || attempt == maxAttempts {
			break
		}

		waitErr := c.retryBackoff(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait before calico install retry: %w", waitErr)
		}
	}

	return fmt.Errorf("install or upgrade calico: %w", err)
}

// attemptCalicoInstall performs a single Helm install attempt. If the install
// fails with an API discovery error (CRDs not yet registered), it waits for
// the CRDs to become established and retries once before returning.
func (c *Installer) attemptCalicoInstall(
	ctx context.Context,
	client helm.Interface,
	spec *helm.ChartSpec,
) error {
	installCtx, cancel := context.WithTimeout(ctx, c.GetTimeout()+helm.ContextTimeoutBuffer)
	defer cancel()

	err := helm.InstallChartWithRetry(installCtx, client, spec, "calico")
	if err == nil {
		return nil
	}

	if !isAPIDiscoveryError(err) {
		return fmt.Errorf("helm install calico: %w", err)
	}

	waitErr := c.waitForCalicoCRDs(ctx)
	if waitErr != nil {
		return fmt.Errorf("wait for calico CRDs: %w", waitErr)
	}

	retryCtx, retryCancel := context.WithTimeout(ctx, c.GetTimeout()+helm.ContextTimeoutBuffer)
	defer retryCancel()

	retryErr := helm.InstallChartWithRetry(retryCtx, client, spec, "calico")
	if retryErr != nil {
		return fmt.Errorf("helm install calico after CRD wait: %w", retryErr)
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
	case v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
		v1alpha1.DistributionEKS:
		// Vanilla, K3s, VCluster, KWOK, and EKS use default values.
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
				if k8serrors.IsNotFound(getErr) {
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

func isAPIServerUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	return strings.Contains(errMsg, "cluster reachability check failed") ||
		strings.Contains(errMsg, "kubernetes cluster unreachable") ||
		strings.Contains(errMsg, "the server is currently unable to handle the request")
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
