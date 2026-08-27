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
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
	"k8s.io/client-go/kubernetes"
)

// Installer implements the installer.Installer interface for Calico.
type Installer struct {
	*cni.InstallerBase

	distribution v1alpha1.Distribution
	haEnabled    bool
	// apiServerChecker is called before Helm operations to ensure the API server
	// is stable. It defaults to WaitForAPIServerStability and can be overridden
	// in tests to avoid needing a real cluster.
	apiServerChecker func(ctx context.Context) error
	// retryBackoff waits between transient API-unavailable Helm install retries.
	// It defaults to a fixed delay and can be overridden in tests.
	retryBackoff func(ctx context.Context) error
}

const (
	// calicoInstallRetryAttempts is the maximum number of Calico install attempts.
	// Two transient, bootstrap-related failures are retried (see shouldRetryInstall):
	// the tigera-operator CRD-establishment race on a freshly created cluster (all
	// distributions), and K3s's transient API-server-unavailable errors that occur
	// during its bootstrap sequence even after the pre-install stability check passes.
	// Eight attempts with 15s backoff applies up to 7 waits between attempts,
	// for a maximum of 105s ((8-1)×15s) of additional waiting.
	// This covers the observed K3s stabilization window in CI (see recurring issues #4196, #4315).
	// The previous 5×10s (40s) window proved insufficient after 5+ occurrences.
	calicoInstallRetryAttempts = 8
	calicoInstallRetryBackoff  = 15 * time.Second

	// calicoChartsRepoURL is the Tigera Helm chart repository serving the Calico charts.
	calicoChartsRepoURL = "https://docs.tigera.io/calico/charts"
)

// NewInstaller creates a new Calico installer with distribution-specific configuration.
// When haEnabled is true the chart is configured with HA defaults
// for the Typha control plane (controlPlaneReplicas).
func NewInstaller(
	client helm.Interface,
	kubeconfig, kubeContext string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
	haEnabled bool,
) *Installer {
	calicoInstaller := &Installer{
		distribution: distribution,
		haEnabled:    haEnabled,
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

	// Since Calico v3.30 the CRDs are managed by a separate release (see
	// crdChartSpec). Remove it after the operator release so uninstall is symmetric
	// with install; this deletes the operator.tigera.io and projectcalico.org CRDs and
	// any remaining custom resources of those kinds. Guard with ReleaseExists so
	// uninstall stays a no-op on clusters created before the two-phase install (or where
	// the CRD install never completed) rather than failing on a missing release.
	crdsExist, existsErr := client.ReleaseExists(ctx, "calico-crds", "tigera-operator")
	if existsErr != nil {
		return fmt.Errorf("check calico CRDs release: %w", existsErr)
	}

	if crdsExist {
		err = client.UninstallRelease(ctx, "calico-crds", "tigera-operator")
		if err != nil {
			return fmt.Errorf("failed to uninstall calico CRDs release: %w", err)
		}
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
		RepoURL:         calicoChartsRepoURL,
		CreateNamespace: true,
		SetJSONVals:     c.getCalicoValues(),
		Timeout:         c.GetTimeout(),
	}
}

// crdChartSpec returns the spec for the projectcalico.org.v3 chart, which
// installs the projectcalico.org/v3 and operator.tigera.io CRDs. Since Calico
// v3.30 these CRDs are no longer bundled in the tigera-operator chart, so they
// must be installed first; otherwise the operator chart's Installation/APIServer
// custom resources fail Helm manifest validation on a fresh cluster.
func (c *Installer) crdChartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     "calico-crds",
		ChartName:       "projectcalico/projectcalico.org.v3",
		Namespace:       "tigera-operator",
		Version:         chartVersion(),
		RepoURL:         calicoChartsRepoURL,
		CreateNamespace: true,
		Timeout:         c.GetTimeout(),
	}
}

// --- internals ---

func (c *Installer) helmInstallOrUpgradeCalico(ctx context.Context) error {
	skipped, ownershipErr := c.CheckGitOpsOwnership(ctx, "calico", "calico", "tigera-operator")
	if ownershipErr != nil {
		return fmt.Errorf("check calico ownership: %w", ownershipErr)
	}

	if skipped {
		return nil
	}

	return c.installCalico(ctx)
}

func (c *Installer) installCalico(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	// Add the Calico Helm repository once before any install attempts to avoid
	// repeating the AddRepository call on every outer retry iteration.
	repoEntry := &helm.RepositoryEntry{
		Name: "projectcalico",
		URL:  calicoChartsRepoURL,
	}

	addErr := client.AddRepository(ctx, repoEntry, c.GetTimeout())
	if addErr != nil {
		return fmt.Errorf("add calico repository: %w", addErr)
	}

	// Phase 1: install the CRDs (separate projectcalico.org.v3 chart since v3.30).
	crdErr := c.runInstallWithRetry(ctx, client, c.crdChartSpec())
	if crdErr != nil {
		return fmt.Errorf("install calico CRDs: %w", crdErr)
	}

	// Phase 2: refresh Helm's cached API discovery so the operator chart install
	// observes the operator.tigera.io CRDs just installed; otherwise its
	// Installation/APIServer custom resources fail validation with
	// "ensure CRDs are installed first".
	refreshErr := client.RefreshDiscovery()
	if refreshErr != nil {
		return fmt.Errorf("refresh discovery after calico CRD install: %w", refreshErr)
	}

	// Phase 3: install the operator chart and its default custom resources.
	operatorErr := c.runInstallWithRetry(ctx, client, c.chartSpec())
	if operatorErr != nil {
		return fmt.Errorf("install or upgrade calico: %w", operatorErr)
	}

	return nil
}

// runInstallWithRetry installs a chart, retrying transient bootstrap failures
// (see shouldRetryInstall) with backoff. When a retry is triggered by an
// API-discovery error, it refreshes Helm's cached discovery first so the retry
// observes any CRDs registered since the previous attempt.
func (c *Installer) runInstallWithRetry(
	ctx context.Context,
	client helm.Interface,
	spec *helm.ChartSpec,
) error {
	spec.Atomic = true
	spec.Silent = true
	spec.UpgradeCRDs = true
	spec.Wait = false
	spec.WaitForJobs = false

	var err error

	for attempt := 1; attempt <= calicoInstallRetryAttempts; attempt++ {
		err = c.attemptCalicoInstall(ctx, client, spec)
		if err == nil {
			return nil
		}

		if !c.shouldRetryInstall(err) || attempt == calicoInstallRetryAttempts {
			break
		}

		if isAPIDiscoveryError(err) {
			refreshErr := client.RefreshDiscovery()
			if refreshErr != nil {
				return fmt.Errorf("refresh discovery before calico install retry: %w", refreshErr)
			}
		}

		waitErr := c.retryBackoff(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait before calico install retry: %w", waitErr)
		}
	}

	return err
}

// shouldRetryInstall reports whether a failed Calico install attempt should be
// retried with backoff.
//
// The operator chart validates its Installation/APIServer custom resources
// against the cluster's API discovery during install. Right after the CRD chart
// installs the operator.tigera.io CRDs, Helm's cached discovery may not yet list
// them, so the operator install is rejected with an API-discovery error. The
// retry succeeds once the CRDs are established and Helm's discovery cache has
// been refreshed (see runInstallWithRetry); this transient race is retried for
// every distribution.
//
// K3s additionally emits transient API-server-unavailable errors during its
// bootstrap sequence even after the pre-install stability check passes; for
// other distributions such an error indicates a genuine problem and is not
// retried.
func (c *Installer) shouldRetryInstall(err error) bool {
	if isAPIDiscoveryError(err) {
		return true
	}

	return c.distribution == v1alpha1.DistributionK3s && isAPIServerUnavailableError(err)
}

// attemptCalicoInstall performs a single Helm install (or upgrade) attempt.
// Retrying transient bootstrap failures is the caller's responsibility; see
// shouldRetryInstall.
func (c *Installer) attemptCalicoInstall(
	ctx context.Context,
	client helm.Interface,
	spec *helm.ChartSpec,
) error {
	installCtx, cancel := context.WithTimeout(ctx, c.GetTimeout()+helm.ContextTimeoutBuffer)
	defer cancel()

	err := helm.InstallChartWithRetry(installCtx, client, spec, spec.ReleaseName)
	if err != nil {
		return fmt.Errorf("helm install %s: %w", spec.ReleaseName, err)
	}

	return nil
}

// getCalicoValues returns the Helm values for Calico based on the distribution.
func (c *Installer) getCalicoValues() map[string]string {
	values := c.defaultCalicoValues()

	// Add distribution-specific values
	switch c.distribution {
	case v1alpha1.DistributionTalos:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico
		maps.Copy(values, talosCalicoValues())
	case v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
		v1alpha1.DistributionEKS,
		v1alpha1.DistributionGKE,
		v1alpha1.DistributionAKS:
		// Vanilla, K3s, VCluster, KWOK, EKS, GKE, and AKS use default values.
	}

	return values
}

func (c *Installer) defaultCalicoValues() map[string]string {
	vals := map[string]string{
		// Goldmane (network flow collector) and Whisker (observability UI) are optional
		// components added in Calico v3.32. They require CRDs that the operator creates
		// after it starts, but Helm's schema validation rejects their CRs before any
		// resource is deployed — including the operator Deployment itself. This deadlock
		// prevents the operator from ever starting. Disabling them here allows the operator
		// to deploy and register CRDs without schema-validation failures. Goldmane and
		// Whisker are not required for CNI functionality.
		"goldmane.enabled": "false",
		"whisker.enabled":  "false",
	}
	if c.haEnabled {
		vals["installation.controlPlaneReplicas"] = "2"
	}

	return vals
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
