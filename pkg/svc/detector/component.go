package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// containerCheckMaxAttempts is the number of attempts for Docker container
	// existence checks. Docker container state can be momentarily inconsistent
	// during high container churn (e.g., system tests), so retrying mitigates
	// false-negative container lookups.
	containerCheckMaxAttempts = 3

	// containerCheckRetryDelay is the delay between retry attempts for Docker
	// container existence checks.
	containerCheckRetryDelay = 1 * time.Second
)

// ComponentDetector detects installed KSail components by querying the
// Kubernetes API (Helm releases, Deployments) and Docker daemon.
type ComponentDetector struct {
	helmClient   helm.Interface
	k8sClientset kubernetes.Interface
	dockerClient dockerclient.Client
	retryDelay   time.Duration
}

// NewComponentDetector creates a detector with the required clients.
// dockerClient may be nil for non-Docker providers.
func NewComponentDetector(
	helmClient helm.Interface,
	k8sClientset kubernetes.Interface,
	dockerClient dockerclient.Client,
) *ComponentDetector {
	return &ComponentDetector{
		helmClient:   helmClient,
		k8sClientset: k8sClientset,
		dockerClient: dockerClient,
		retryDelay:   containerCheckRetryDelay,
	}
}

// releaseSet is an in-memory index of Helm releases keyed by
// name+namespace for O(1) existence checks after a single ListReleases call.
type releaseSet map[releaseKey]struct{}

type releaseKey struct {
	name      string
	namespace string
}

// cachedHelmClient wraps a helm.Interface and serves ReleaseExists lookups from
// a pre-fetched in-memory releaseSet, eliminating per-call API roundtrips while
// delegating all other operations to the underlying client.
type cachedHelmClient struct {
	helm.Interface

	set releaseSet
}

func (c *cachedHelmClient) ReleaseExists(
	ctx context.Context,
	name, namespace string,
) (bool, error) {
	// Delegate to the wrapped client for invalid inputs so that validation
	// errors (e.g., ErrReleaseNameRequired for an empty name) are preserved.
	if name == "" || namespace == "" {
		exists, err := c.Interface.ReleaseExists(ctx, name, namespace)
		if err != nil {
			return false, fmt.Errorf("release exists check: %w", err)
		}

		return exists, nil
	}

	_, ok := c.set[releaseKey{name: name, namespace: namespace}]

	return ok, nil
}

// DetectComponents probes the running cluster to populate a ClusterSpec that
// reflects the actual installed components. Distribution and provider are set
// from the caller's known values; all other fields are detected.
//
// A single ListReleases call fetches all Helm releases across namespaces
// upfront, replacing N sequential ReleaseExists roundtrips with one.
func (d *ComponentDetector) DetectComponents(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) (*v1alpha1.ClusterSpec, error) {
	// Fetch all releases in a single API call, then use an in-memory cache for
	// the individual detect functions to avoid N separate Helm roundtrips.
	releases, err := d.helmClient.ListReleases(ctx)
	if err != nil {
		// If the context was cancelled or timed out, propagate the context
		// error so callers see context.Canceled/DeadlineExceeded rather than
		// a potentially misleading RBAC or network error.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("list helm releases: %w", ctx.Err())
		}

		// Otherwise (e.g., restricted RBAC), fall back to per-release checks so
		// that detection still works with namespaced Helm access.
		return d.detectAllComponents(ctx, distribution, provider)
	}

	releaseIndex := make(releaseSet, len(releases))
	for _, r := range releases {
		releaseIndex[releaseKey{name: r.Name, namespace: r.Namespace}] = struct{}{}
	}

	cached := &ComponentDetector{
		helmClient:   &cachedHelmClient{Interface: d.helmClient, set: releaseIndex},
		k8sClientset: d.k8sClientset,
		dockerClient: d.dockerClient,
	}

	return cached.detectAllComponents(ctx, distribution, provider)
}

// detectAllComponents runs all individual detection functions using the receiver's
// helmClient. When called from DetectComponents the helmClient is a cachedHelmClient
// if ListReleases succeeded, or the original client when DetectComponents falls back
// to per-release checks (e.g., restricted RBAC).
func (d *ComponentDetector) detectAllComponents(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) (*v1alpha1.ClusterSpec, error) {
	spec := &v1alpha1.ClusterSpec{
		Distribution: distribution,
		Provider:     provider,
	}

	var err error

	spec.CNI, err = d.detectCNI(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect CNI: %w", err)
	}

	spec.CSI, err = d.detectCSI(ctx, distribution, provider)
	if err != nil {
		return nil, fmt.Errorf("detect CSI: %w", err)
	}

	spec.MetricsServer, err = d.detectMetricsServer(ctx, distribution)
	if err != nil {
		return nil, fmt.Errorf("detect MetricsServer: %w", err)
	}

	spec.LoadBalancer, err = d.detectLoadBalancer(ctx, distribution, provider)
	if err != nil {
		return nil, fmt.Errorf("detect LoadBalancer: %w", err)
	}

	err = d.detectEKSComponents(ctx, spec, distribution, provider)
	if err != nil {
		return nil, err
	}

	spec.CertManager, err = d.detectCertManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect CertManager: %w", err)
	}

	spec.PolicyEngine, err = d.detectPolicyEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect PolicyEngine: %w", err)
	}

	spec.GitOpsEngine, err = d.detectGitOpsEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect GitOpsEngine: %w", err)
	}

	spec.Autoscaler.Node, err = d.detectNodeAutoscaler(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect NodeAutoscaler: %w", err)
	}

	return spec, nil
}

func (d *ComponentDetector) detectEKSComponents(
	ctx context.Context,
	spec *v1alpha1.ClusterSpec,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) error {
	if distribution != v1alpha1.DistributionEKS || provider != v1alpha1.ProviderAWS {
		return nil
	}

	installed, err := d.detectEKSLoadBalancerController(ctx)
	if err != nil {
		return err
	}

	spec.EKS.ExperimentalAWSLoadBalancerController = installed
	if installed {
		spec.LoadBalancer = v1alpha1.LoadBalancerEnabled
	}

	return nil
}

func (d *ComponentDetector) detectEKSLoadBalancerController(ctx context.Context) (bool, error) {
	exists, err := d.helmClient.ReleaseExists(
		ctx,
		ReleaseAWSLoadBalancerController,
		NamespaceAWSLoadBalancerController,
	)
	if err != nil {
		return false, fmt.Errorf("detect AWS Load Balancer Controller: %w", err)
	}

	return exists, nil
}

// releaseMapping maps a Helm release to the value returned when the release is found.
type releaseMapping[T ~string] struct {
	release   string
	namespace string
	value     T
}

// detectFirstRelease checks Helm releases in priority order and returns the
// value associated with the first release that exists. Returns defaultVal when
// no release is found.
func detectFirstRelease[T ~string](
	ctx context.Context,
	helmClient helm.Interface,
	mappings []releaseMapping[T],
	defaultVal T,
) (T, error) {
	for _, mapping := range mappings {
		exists, err := helmClient.ReleaseExists(ctx, mapping.release, mapping.namespace)
		if err != nil {
			return defaultVal, fmt.Errorf("check %s release: %w", mapping.release, err)
		}

		if exists {
			return mapping.value, nil
		}
	}

	return defaultVal, nil
}

// detectCNI probes for Cilium or Calico Helm releases.
func (d *ComponentDetector) detectCNI(ctx context.Context) (v1alpha1.CNI, error) {
	return detectFirstRelease(ctx, d.helmClient, []releaseMapping[v1alpha1.CNI]{
		{ReleaseCilium, NamespaceCilium, v1alpha1.CNICilium},
		{ReleaseCalico, NamespaceCalico, v1alpha1.CNICalico},
	}, v1alpha1.CNIDefault)
}

// detectBundledCSI checks for a bundled local-path-provisioner deployment and
// returns CSIDefault when present (distribution's default state, appropriate for
// distributions where ProvidesCSIByDefault returns true, such as K3s) or
// CSIDisabled when absent.
func (d *ComponentDetector) detectBundledCSI(
	ctx context.Context,
	deployment, namespace string,
) (v1alpha1.CSI, error) {
	if d.deploymentExists(ctx, deployment, namespace) {
		return v1alpha1.CSIDefault, nil
	}

	return v1alpha1.CSIDisabled, nil
}

// detectCSI determines the CSI setting based on distribution, provider, and
// whether a KSail-managed CSI component is installed.
func (d *ComponentDetector) detectCSI(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) (v1alpha1.CSI, error) {
	// K3s bundles local-path-provisioner in kube-system. When the user disables
	// CSI (--csi Disabled), K3s is started with --disable=local-storage and the
	// deployment won't exist. Probe the cluster to distinguish Default from Disabled.
	if distribution == v1alpha1.DistributionK3s {
		return d.detectBundledCSI(ctx, DeploymentLocalPathProvisionerK3s, NamespaceKubeSystem)
	}

	// Talos+Hetzner: check for hcloud-csi
	if distribution == v1alpha1.DistributionTalos && provider == v1alpha1.ProviderHetzner {
		exists, err := d.helmClient.ReleaseExists(ctx, ReleaseHCloudCSI, NamespaceHCloudCSI)
		if err != nil {
			return v1alpha1.CSIDefault, fmt.Errorf("check hcloud-csi release: %w", err)
		}

		if exists {
			return v1alpha1.CSIEnabled, nil
		}

		return v1alpha1.CSIDisabled, nil
	}

	// Vanilla (Kind) and VCluster (Vind with k8s distro): both typically run
	// local-path-provisioner. Since Distribution.ProvidesCSIByDefault() returns
	// false for these distributions, treat the presence of the deployment as
	// CSIEnabled and its absence as CSIDisabled to keep semantics consistent.
	if distribution == v1alpha1.DistributionVanilla ||
		distribution == v1alpha1.DistributionVCluster {
		if d.deploymentExists(ctx, DeploymentLocalPathProvisioner, NamespaceLocalPathStorage) {
			return v1alpha1.CSIEnabled, nil
		}

		return v1alpha1.CSIDisabled, nil
	}

	// Talos-Docker: check for user-installed local-path-provisioner Deployment
	if d.deploymentExists(ctx, DeploymentLocalPathProvisioner, NamespaceLocalPathStorage) {
		return v1alpha1.CSIEnabled, nil
	}

	return v1alpha1.CSIDefault, nil
}

// detectMetricsServer checks for a KSail-managed metrics-server Helm release.
// For K3s, it also probes the built-in metrics-server deployment to detect
// whether the user disabled it via --disable=metrics-server.
func (d *ComponentDetector) detectMetricsServer(
	ctx context.Context,
	distribution v1alpha1.Distribution,
) (v1alpha1.MetricsServer, error) {
	exists, err := d.helmClient.ReleaseExists(
		ctx, ReleaseMetricsServer, NamespaceMetricsServer,
	)
	if err != nil {
		return v1alpha1.MetricsServerDefault, fmt.Errorf("check metrics-server release: %w", err)
	}

	if exists {
		return v1alpha1.MetricsServerEnabled, nil
	}

	// K3s bundles metrics-server in kube-system. When the user disables it
	// (--metrics-server Disabled), K3s is started with --disable=metrics-server
	// and the deployment won't exist. Probe the cluster to distinguish
	// Default (built-in running) from Disabled (explicitly turned off).
	if distribution.ProvidesMetricsServerByDefault() {
		if d.deploymentExists(ctx, DeploymentMetricsServerK3s, NamespaceKubeSystem) {
			return v1alpha1.MetricsServerDefault, nil
		}

		return v1alpha1.MetricsServerDisabled, nil
	}

	return v1alpha1.MetricsServerDefault, nil
}

// detectLoadBalancer determines whether cloud-provider-kind or K3s ServiceLB
// is active.
func (d *ComponentDetector) detectLoadBalancer(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	_ v1alpha1.Provider,
) (v1alpha1.LoadBalancer, error) {
	// K3s bundles ServiceLB. When the user disables it (--load-balancer Disabled),
	// K3s is started with --disable=servicelb and no svclb DaemonSets are created.
	// Probe the cluster for svclb DaemonSets to distinguish Default from Disabled.
	if distribution == v1alpha1.DistributionK3s {
		if d.daemonSetExistsWithLabel(ctx, LabelServiceLBK3s) {
			return v1alpha1.LoadBalancerDefault, nil
		}

		// K3s Traefik (installed by default) creates a LoadBalancer service that
		// triggers svclb DaemonSets. If Traefik is running but no svclb DaemonSets
		// exist, ServiceLB was explicitly disabled.
		if d.deploymentExists(ctx, "traefik", NamespaceKubeSystem) {
			return v1alpha1.LoadBalancerDisabled, nil
		}

		// Traefik is also disabled — no evidence either way.
		// Return Default since we cannot determine the state definitively.
		return v1alpha1.LoadBalancerDefault, nil
	}

	// Vanilla: check for Docker container with retry to handle transient
	// Docker state inconsistency (container visibility gaps during high churn).
	if distribution == v1alpha1.DistributionVanilla && d.dockerClient != nil {
		found, err := d.containerExistsWithRetry(ctx, ContainerCloudProviderKind)
		if err != nil {
			return v1alpha1.LoadBalancerDefault, fmt.Errorf(
				"check cloud-provider-kind container: %w", err,
			)
		}

		if found {
			return v1alpha1.LoadBalancerEnabled, nil
		}
	}

	// Talos: check for MetalLB Helm release (used by Talos × Docker;
	// Talos × Hetzner also resolves correctly via ProvidesLoadBalancerByDefault).
	if distribution == v1alpha1.DistributionTalos {
		return d.detectMetalLB(ctx)
	}

	// KWOK: simulated pods have no real network dataplane, so LoadBalancer is
	// never installed. Return Disabled to match applyDistributionSpecOverrides
	// and prevent false-positive diffs in update dry-runs.
	if distribution == v1alpha1.DistributionKWOK {
		return v1alpha1.LoadBalancerDisabled, nil
	}

	return v1alpha1.LoadBalancerDefault, nil
}

// containerExistsWithRetry checks if a container exists, retrying up to
// containerCheckMaxAttempts times with d.retryDelay between each attempt.
// It returns true as soon as any attempt finds the container. An error is
// returned only when the final attempt itself fails; errors from earlier
// attempts that are followed by a successful probe are silently discarded.
// If the context is cancelled (either while probing or while waiting between
// retries), the function returns immediately with false (and no error, unless
// the last probe errored).
func (d *ComponentDetector) containerExistsWithRetry(
	ctx context.Context,
	containerName string,
) (bool, error) {
	var lastErr error

	for attempt := 1; attempt <= containerCheckMaxAttempts; attempt++ {
		found, err := d.containerExists(ctx, containerName)
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil

			if found {
				return true, nil
			}
		}

		if attempt < containerCheckMaxAttempts {
			// Check for cancellation before sleeping to avoid a race when
			// d.retryDelay is zero (both ctx.Done() and a zero timer fire
			// simultaneously, and Go's select picks randomly between them).
			select {
			case <-ctx.Done():
				return false, lastErr
			default:
			}

			timer := time.NewTimer(d.retryDelay)

			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}

				return false, lastErr
			case <-timer.C:
			}
		}
	}

	return false, lastErr
}

// detectMetalLB checks for a MetalLB Helm release.
func (d *ComponentDetector) detectMetalLB(ctx context.Context) (v1alpha1.LoadBalancer, error) {
	exists, err := d.helmClient.ReleaseExists(ctx, ReleaseMetalLB, NamespaceMetalLB)
	if err != nil {
		return v1alpha1.LoadBalancerDefault, fmt.Errorf("check metallb release: %w", err)
	}

	if exists {
		return v1alpha1.LoadBalancerEnabled, nil
	}

	return v1alpha1.LoadBalancerDefault, nil
}

// detectCertManager checks for a cert-manager Helm release.
func (d *ComponentDetector) detectCertManager(ctx context.Context) (v1alpha1.CertManager, error) {
	return detectFirstRelease(ctx, d.helmClient, []releaseMapping[v1alpha1.CertManager]{
		{ReleaseCertManager, NamespaceCertManager, v1alpha1.CertManagerEnabled},
	}, v1alpha1.CertManagerDisabled)
}

// detectPolicyEngine checks for Kyverno or Gatekeeper Helm releases.
func (d *ComponentDetector) detectPolicyEngine(
	ctx context.Context,
) (v1alpha1.PolicyEngine, error) {
	return detectFirstRelease(ctx, d.helmClient, []releaseMapping[v1alpha1.PolicyEngine]{
		{ReleaseKyverno, NamespaceKyverno, v1alpha1.PolicyEngineKyverno},
		{ReleaseGatekeeper, NamespaceGatekeeper, v1alpha1.PolicyEngineGatekeeper},
	}, v1alpha1.PolicyEngineNone)
}

// gitOpsEngineReleaseMappings is the single source of truth for the Helm
// releases that identify a KSail-managed GitOps engine, in priority order.
func gitOpsEngineReleaseMappings() []releaseMapping[v1alpha1.GitOpsEngine] {
	return []releaseMapping[v1alpha1.GitOpsEngine]{
		{ReleaseFluxOperator, NamespaceFluxOperator, v1alpha1.GitOpsEngineFlux},
		{ReleaseArgoCD, NamespaceArgoCD, v1alpha1.GitOpsEngineArgoCD},
	}
}

// DetectGitOpsEngine reports which GitOps engine is installed in the cluster by
// checking the flux-operator / argocd Helm releases in priority order. It
// returns GitOpsEngineNone when neither release is present, allowing callers to
// fall back to a secondary signal (e.g. a namespace probe for non-KSail-managed
// installs). The mapping is shared with the ComponentDetector's per-spec
// detection so the two never disagree.
func DetectGitOpsEngine(
	ctx context.Context,
	helmClient helm.Interface,
) (v1alpha1.GitOpsEngine, error) {
	return detectFirstRelease(
		ctx, helmClient, gitOpsEngineReleaseMappings(), v1alpha1.GitOpsEngineNone,
	)
}

// detectGitOpsEngine checks for Flux or ArgoCD Helm releases.
func (d *ComponentDetector) detectGitOpsEngine(
	ctx context.Context,
) (v1alpha1.GitOpsEngine, error) {
	return detectFirstRelease(
		ctx, d.helmClient, gitOpsEngineReleaseMappings(), v1alpha1.GitOpsEngineNone,
	)
}

// deploymentExists checks whether a Deployment with the given name exists in
// the specified namespace.
func (d *ComponentDetector) deploymentExists(
	ctx context.Context,
	name, namespace string,
) bool {
	if d.k8sClientset == nil {
		return false
	}

	_, err := d.k8sClientset.AppsV1().Deployments(namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err != nil {
		// Any error (including not-found) means the deployment is not available.
		return false
	}

	return true
}

// daemonSetExistsWithLabel checks whether any DaemonSet with the given label
// key exists across all namespaces. This is used to detect K3s ServiceLB, which
// creates DaemonSets labeled with svccontroller.k3s.cattle.io/svcname.
func (d *ComponentDetector) daemonSetExistsWithLabel(
	ctx context.Context,
	labelKey string,
) bool {
	if d.k8sClientset == nil {
		return false
	}

	daemonSets, err := d.k8sClientset.AppsV1().DaemonSets("").List(
		ctx, metav1.ListOptions{
			LabelSelector: labelKey,
			Limit:         1,
		},
	)
	if err != nil {
		return false
	}

	return len(daemonSets.Items) > 0
}

// containerExists checks whether a Docker container with the given name is
// running.
func (d *ComponentDetector) containerExists(
	ctx context.Context,
	containerName string,
) (bool, error) {
	if d.dockerClient == nil {
		return false, nil
	}

	containers, err := d.dockerClient.ContainerList(
		ctx,
		container.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("name", "^/"+containerName+"$"),
			),
		},
	)
	if err != nil {
		return false, fmt.Errorf("list containers: %w", err)
	}

	return len(containers) > 0, nil
}

// detectNodeAutoscaler detects the Cluster Autoscaler configuration from the
// live cluster by checking for the Helm release and reading its values.
func (d *ComponentDetector) detectNodeAutoscaler(
	ctx context.Context,
) (v1alpha1.NodeAutoscalerConfig, error) {
	var cfg v1alpha1.NodeAutoscalerConfig

	exists, err := d.helmClient.ReleaseExists(
		ctx, ReleaseClusterAutoscaler, NamespaceClusterAutoscaler,
	)
	if err != nil {
		return cfg, fmt.Errorf("check cluster-autoscaler release: %w", err)
	}

	if !exists {
		return cfg, nil
	}

	cfg.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	values, err := d.helmClient.GetReleaseValues(
		ctx, ReleaseClusterAutoscaler, NamespaceClusterAutoscaler,
	)
	if err != nil {
		// Propagate context cancellation/timeout — the caller has given up.
		if ctx.Err() != nil {
			return cfg, fmt.Errorf("detect node autoscaler: %w", ctx.Err())
		}
		// Release exists but values unreadable — return enabled with defaults.
		return cfg, nil
	}

	parseAutoscalerValues(&cfg, values)

	return cfg, nil
}

// parseAutoscalerValues extracts autoscaler config from Helm release values.
func parseAutoscalerValues(cfg *v1alpha1.NodeAutoscalerConfig, values map[string]any) {
	if extraArgs, exists := values["extraArgs"].(map[string]any); exists {
		parseAutoscalerExtraArgs(cfg, extraArgs)
	}

	if groups, exists := values["autoscalingGroups"].([]any); exists {
		cfg.Pools = parseAutoscalingGroups(groups)
	}
}

// parseAutoscalerExtraArgs extracts scalar config from the chart's extraArgs.
func parseAutoscalerExtraArgs(cfg *v1alpha1.NodeAutoscalerConfig, args map[string]any) {
	if expander, ok := args["expander"].(string); ok {
		cfg.Expander = helmExpandersToEnum(expander)
	}

	if maxNodes, ok := toInt32(args["max-nodes-total"]); ok {
		cfg.MaxNodesTotal = maxNodes
	}

	if scaleDown, ok := args["scale-down-unneeded-time"].(string); ok {
		cfg.ScaleDownUnneededTime = scaleDown
	}

	if threshold, ok := args["scale-down-utilization-threshold"].(string); ok {
		cfg.ScaleDownUtilizationThreshold = threshold
	}

	// The installer renders both capacity-buffer flags together; the controller
	// flag is the canonical marker for the capacityBuffers option. An absent key
	// means the feature is off (the installer omits the flags when disabled).
	if capacityBuffers, ok := args["capacity-buffer-controller-enabled"].(bool); ok {
		cfg.CapacityBuffers = capacityBuffers
	}

	// ignoreDaemonsetsUtilization is a plain bool (off by default); the installer
	// omits the flag when false, so an absent key leaves cfg's zero value (false).
	if ignoreDaemonsets, ok := args["ignore-daemonsets-utilization"].(bool); ok {
		cfg.IgnoreDaemonsetsUtilization = ignoreDaemonsets
	}

	// skipNodesWith* are *bool: the installer omits the flag when unset, so an
	// absent key leaves cfg's nil pointer (inherits the upstream default true).
	// A present key is an explicit true/false and is preserved as a non-nil value.
	if skipLocalStorage, ok := args["skip-nodes-with-local-storage"].(bool); ok {
		cfg.SkipNodesWithLocalStorage = &skipLocalStorage
	}

	if skipSystemPods, ok := args["skip-nodes-with-system-pods"].(bool); ok {
		cfg.SkipNodesWithSystemPods = &skipSystemPods
	}
}

// helmExpandersToEnum reverses the Helm chart's expander value — a single
// strategy or a comma-separated priority list (e.g. "least-nodes,least-waste") —
// back into a [v1alpha1.AutoscalerExpanderList]. An empty value yields a nil list.
// Must stay in sync with expandersToHelmValue in the installer.
func helmExpandersToEnum(value string) v1alpha1.AutoscalerExpanderList {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	expanders := make(v1alpha1.AutoscalerExpanderList, 0, len(parts))

	for _, part := range parts {
		expanders = append(expanders, helmExpanderToEnum(strings.TrimSpace(part)))
	}

	return expanders
}

// helmExpanderToEnum reverses the Helm chart's lowercase expander value to the
// KSail enum. Must stay in sync with expanderToHelmValue in the installer.
func helmExpanderToEnum(value string) v1alpha1.AutoscalerExpander {
	switch value {
	case "price":
		return v1alpha1.AutoscalerExpanderPrice
	case "least-nodes":
		return v1alpha1.AutoscalerExpanderLeastNodes
	case "random":
		return v1alpha1.AutoscalerExpanderRandom
	case "least-waste":
		return v1alpha1.AutoscalerExpanderLeastWaste
	default:
		return v1alpha1.AutoscalerExpanderLeastWaste
	}
}

// parseAutoscalingGroups converts the chart's autoscalingGroups list to NodePool slices.
func parseAutoscalingGroups(groups []any) []v1alpha1.NodePool {
	pools := make([]v1alpha1.NodePool, 0, len(groups))

	for _, g := range groups {
		group, isMap := g.(map[string]any)
		if !isMap {
			continue
		}

		pool := v1alpha1.NodePool{}

		name, hasName := group["name"].(string)
		if !hasName || name == "" {
			continue
		}

		pool.Name = name

		if st, ok := group["instanceType"].(string); ok {
			pool.ServerType = st
		}

		if loc, ok := group["region"].(string); ok {
			pool.Location = loc
		}

		if minSize, ok := toInt32(group["minSize"]); ok {
			pool.Min = minSize
		}

		if maxSize, ok := toInt32(group["maxSize"]); ok {
			pool.Max = maxSize
		}

		pools = append(pools, pool)
	}

	return pools
}

const (
	minInt32Float float64 = -1 << 31
	maxInt32Float float64 = 1<<31 - 1
)

// float64ToInt32 converts a float64 to int32, validating that it is a whole
// number within the int32 range. Returns (0, false) when invalid.
func float64ToInt32(f float64) (int32, bool) {
	if f != math.Trunc(f) || f < minInt32Float || f > maxInt32Float {
		return 0, false
	}

	return int32(f), true
}

// toInt32 converts a Helm values entry (which may be float64, json.Number, int, int32,
// or int64) to int32. Returns (0, false) when the value is not a numeric type, is
// non-integral, or falls outside the int32 range.
func toInt32(v any) (int32, bool) {
	switch num := v.(type) {
	case float64:
		return float64ToInt32(num)
	case json.Number:
		f, err := num.Float64()
		if err != nil {
			return 0, false
		}

		return float64ToInt32(f)
	case int:
		return float64ToInt32(float64(num))
	case int32:
		return num, true
	case int64:
		return float64ToInt32(float64(num))
	default:
		return 0, false
	}
}
