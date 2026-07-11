package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	check "github.com/siderolabs/talos/pkg/cluster/check"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubedrain "k8s.io/kubectl/pkg/drain"
)

var errUpdateApplyStepNotFoundForTest = errors.New("update apply step not found")

// NodeWithRoleForTest is the exported alias of nodeWithRole for testing.
type NodeWithRoleForTest = nodeWithRole

// CountNodeRolesForTest exposes countNodeRoles for unit testing.
func CountNodeRolesForTest(nodes []NodeWithRoleForTest) (int32, int32) {
	return countNodeRoles(nodes)
}

// LowestTalosVersionForTest exposes lowestTalosVersion for unit testing.
func LowestTalosVersionForTest(tags []string) (string, error) {
	return lowestTalosVersion(tags)
}

// RunningVersionMatchesTargetForTest exposes runningVersionMatchesTarget for unit testing.
func RunningVersionMatchesTargetForTest(running, target string) bool {
	return runningVersionMatchesTarget(running, target)
}

// NewNodeWithRoleForTest creates a nodeWithRole for unit testing.
func NewNodeWithRoleForTest(ip, role string) NodeWithRoleForTest {
	return nodeWithRole{IP: ip, Role: role}
}

// AvailableNodeIndicesForTest exposes availableNodeIndices for unit testing.
func AvailableNodeIndicesForTest(names []string, prefix string, count int) []int {
	return availableNodeIndices(names, prefix, count)
}

// AddDockerNodesForTest exposes addDockerNodes for unit testing.
func (p *Provisioner) AddDockerNodesForTest(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	return p.addDockerNodes(ctx, clusterName, role, count, result)
}

// RemoveDockerNodesForTest exposes removeDockerNodes for unit testing.
func (p *Provisioner) RemoveDockerNodesForTest(
	ctx context.Context,
	clusterName, role string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	return p.removeDockerNodes(ctx, clusterName, role, count, result)
}

// WithNodeReachabilityCheckForTest overrides the per-node Talos API reachability
// check. Full scale-up flow tests use it to avoid real TCP dials against the
// (unroutable) static container IPs assigned to mock-created containers.
func (p *Provisioner) WithNodeReachabilityCheckForTest(
	fn func(ctx context.Context, ip string) error,
) *Provisioner {
	p.nodeReachabilityCheck = fn

	return p
}

// WaitForNewDockerNodesReachableForTest exposes waitForNewDockerNodesReachable
// for unit testing. Each IP in nodeIPs is turned into a node spec (the IP doubles
// as the node name in log/error output).
func (p *Provisioner) WaitForNewDockerNodesReachableForTest(
	ctx context.Context,
	nodeIPs []string,
) error {
	specs := make([]nodeSpec, len(nodeIPs))
	for i, ip := range nodeIPs {
		specs[i] = nodeSpec{name: ip, ip: netip.MustParseAddr(ip)}
	}

	return p.waitForNewDockerNodesReachable(ctx, specs)
}

// WaitForNewHetznerNodesReachableForTest exposes waitForNewHetznerNodesReachable
// for unit testing.
func (p *Provisioner) WaitForNewHetznerNodesReachableForTest(
	ctx context.Context,
	servers []*hcloud.Server,
	role string,
) error {
	return p.waitForNewHetznerNodesReachable(ctx, servers, role)
}

// CreateOmniProviderForTest exposes createOmniProvider for unit testing.
func CreateOmniProviderForTest(opts v1alpha1.OptionsOmni) error {
	_, err := createOmniProvider(opts)

	return err
}

// ResolveOmniVersionsForTest exposes resolveOmniVersions for unit testing.
func (p *Provisioner) ResolveOmniVersionsForTest(
	ctx context.Context,
	omniProv *omniprovider.Provider,
) (string, string, error) {
	return p.resolveOmniVersions(ctx, omniProv)
}

// BuildOmniPatchInfosForTest exposes buildOmniPatchInfos for unit testing.
func (p *Provisioner) BuildOmniPatchInfosForTest() []omniprovider.PatchInfo {
	return p.buildOmniPatchInfos()
}

// SyncAndWaitOmniClusterForTest exposes syncAndWaitOmniCluster for unit testing.
func (p *Provisioner) SyncAndWaitOmniClusterForTest(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	params omniprovider.TemplateParams,
) error {
	return p.syncAndWaitOmniCluster(ctx, omniProv, params)
}

// SaveOmniKubeconfigForTest exposes saveOmniKubeconfig for unit testing.
func (p *Provisioner) SaveOmniKubeconfigForTest(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
) error {
	return p.saveOmniKubeconfig(ctx, omniProv, clusterName)
}

// SaveOmniTalosconfigForTest exposes saveOmniTalosconfig for unit testing.
func (p *Provisioner) SaveOmniTalosconfigForTest(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
) error {
	return p.saveOmniTalosconfig(ctx, omniProv, clusterName)
}

// GetOmniNodesByRoleForTest exposes getOmniNodesByRole for unit testing.
func (p *Provisioner) GetOmniNodesByRoleForTest(
	ctx context.Context,
	clusterName string,
) ([]NodeWithRoleForTest, error) {
	return p.getOmniNodesByRole(ctx, clusterName)
}

// ResolveOmniMachinesForTest exposes resolveOmniMachines for unit testing.
func (p *Provisioner) ResolveOmniMachinesForTest(
	ctx context.Context,
	omniProv *omniprovider.Provider,
) ([]string, error) {
	return p.resolveOmniMachines(ctx, omniProv)
}

// ApplyNodeScalingChangesForTest exposes applyNodeScalingChanges for unit testing.
func (p *Provisioner) ApplyNodeScalingChangesForTest(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) error {
	return p.applyNodeScalingChanges(ctx, clusterName, oldSpec, newSpec, result)
}

// SyncSecretsFromClusterForTest exposes syncSecretsFromCluster for unit testing.
func (p *Provisioner) SyncSecretsFromClusterForTest(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
) error {
	return p.syncSecretsFromCluster(ctx, clusterName, oldSpec, newSpec, diff)
}

// NeedsSecretSyncForTest exposes needsSecretSync for unit testing.
func (p *Provisioner) NeedsSecretSyncForTest(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
) bool {
	return p.needsSecretSync(oldSpec, newSpec, diff)
}

// DockerNodeNameForTest exposes dockerNodeName for unit testing.
func DockerNodeNameForTest(clusterName, role string, index int) string {
	return dockerNodeName(clusterName, role, index)
}

// TalosTypeFromRoleForTest exposes talosTypeFromRole for unit testing.
func TalosTypeFromRoleForTest(role string) string {
	return talosTypeFromRole(role)
}

// CalculateNodeIPForTest exposes calculateNodeIP for unit testing.
func CalculateNodeIPForTest(
	cidr netip.Prefix,
	role string,
	nodeIndex, cpCount int,
) (netip.Addr, error) {
	return calculateNodeIP(cidr, role, nodeIndex, cpCount)
}

// PreCalculateNodeSpecsForTest exposes preCalculateNodeSpecs for unit testing.
// It builds a contiguous index range from nextIndex for backwards-compatible test
// inputs. Returns node names and IPs as parallel slices (nodeSpec is unexported).
func PreCalculateNodeSpecsForTest(
	cidr netip.Prefix,
	clusterName, role string,
	nextIndex, count, cpCount int,
) ([]string, []netip.Addr, error) {
	indices := make([]int, count)
	for i := range count {
		indices[i] = nextIndex + i
	}

	specs, err := preCalculateNodeSpecs(cidr, clusterName, role, indices, cpCount)
	if err != nil {
		return nil, nil, err
	}

	names := make([]string, len(specs))

	ips := make([]netip.Addr, len(specs))
	for i, s := range specs {
		names[i] = s.name
		ips[i] = s.ip
	}

	return names, ips, nil
}

// RewriteKubeconfigEndpointForTest exposes rewriteKubeconfigEndpoint for unit testing.
func RewriteKubeconfigEndpointForTest(kubeconfigBytes []byte, endpoint string) ([]byte, error) {
	return rewriteKubeconfigEndpoint(kubeconfigBytes, endpoint)
}

// ApplyTalosDefaultsForTest exposes applyTalosDefaults for unit testing.
func ApplyTalosDefaultsForTest(opts v1alpha1.OptionsTalos) v1alpha1.OptionsTalos {
	return applyTalosDefaults(opts)
}

// ApplyHetznerDefaultsForTest exposes applyHetznerDefaults for unit testing.
func ApplyHetznerDefaultsForTest(opts v1alpha1.OptionsHetzner) v1alpha1.OptionsHetzner {
	return applyHetznerDefaults(opts)
}

// UpdateConfigsWithEndpointForTest exposes updateConfigsWithEndpoint for unit testing.
func (p *Provisioner) UpdateConfigsWithEndpointForTest(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	controlPlaneServers []*hcloud.Server,
) error {
	return p.updateConfigsWithEndpoint(ctx, hzProvider, clusterName, controlPlaneServers)
}

// MergeFloatingIPChangesForTest exposes mergeFloatingIPChanges for unit testing.
func (p *Provisioner) MergeFloatingIPChangesForTest(
	ctx context.Context,
	name string,
	diff *clusterupdate.UpdateResult,
) error {
	return p.mergeFloatingIPChanges(ctx, name, diff)
}

// ReconcileFloatingIPEndpointForTest exposes reconcileFloatingIPEndpoint for
// unit testing.
func (p *Provisioner) ReconcileFloatingIPEndpointForTest(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
) error {
	return p.reconcileFloatingIPEndpoint(ctx, clusterName, diff)
}

// RefreshFloatingIPEndpointAfterNodeChangesForTest exposes the post-topology
// refresh so tests can prove it runs without a floating-IP drift signal.
func (p *Provisioner) RefreshFloatingIPEndpointAfterNodeChangesForTest(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) error {
	return p.refreshFloatingIPEndpointAfterNodeChanges(
		ctx, clusterName, oldSpec, newSpec, result,
	)
}

// ValidateUpdatePlanForTest exposes the pre-mutation update-plan guard.
func (p *Provisioner) ValidateUpdatePlanForTest(result *clusterupdate.UpdateResult) error {
	return p.validateUpdatePlan(result)
}

// ReattachFloatingIPAfterControlPlaneReplacementForTest exposes
// reattachFloatingIPAfterControlPlaneReplacement for unit testing.
func (p *Provisioner) ReattachFloatingIPAfterControlPlaneReplacementForTest(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	oldServer, newServer *hcloud.Server,
) error {
	return p.reattachFloatingIPAfterControlPlaneReplacement(
		ctx, hzProvider, clusterName, oldServer, newServer,
	)
}

// FinishControlPlaneReplacementForTest exposes finishControlPlaneReplacement
// for unit testing.
func FinishControlPlaneReplacementForTest(reattach, waitReady func() error) error {
	return finishControlPlaneReplacement(reattach, waitReady)
}

// AllControlPlanesHaveHetznerFloatingIPConfigForTest exposes
// allControlPlanesHaveHetznerFloatingIPConfig for unit testing.
func AllControlPlanesHaveHetznerFloatingIPConfigForTest(
	configs []talosconfig.Provider,
	expectedIP string,
) bool {
	return allControlPlanesHaveHetznerFloatingIPConfig(configs, expectedIP)
}

// RecordAppliedChangeForTest exposes recordAppliedChange for unit testing.
func RecordAppliedChangeForTest(result *clusterupdate.UpdateResult, role, nodeName, action string) {
	recordAppliedChange(result, role, nodeName, action)
}

// RecordFailedChangeForTest exposes recordFailedChange for unit testing.
func RecordFailedChangeForTest(
	result *clusterupdate.UpdateResult,
	role, nodeName string,
	err error,
) {
	recordFailedChange(result, role, nodeName, err)
}

// ContainerNameForTest exposes containerName for unit testing.
func ContainerNameForTest(ctr container.Summary) string {
	return containerName(ctr)
}

// KubeconfigFetcherForTest is the exported alias of kubeconfigFetcher for testing.
type KubeconfigFetcherForTest = kubeconfigFetcher

// WithTalosClientFactoryForTest sets the talosClientFactory for testing,
// allowing injection of a mock that returns known kubeconfig bytes.
func (p *Provisioner) WithTalosClientFactoryForTest(
	f func(ctx context.Context, ip string) (KubeconfigFetcherForTest, error),
) *Provisioner {
	p.talosClientFactory = f

	return p
}

// FetchAndWriteKubeconfigForCPForTest exposes fetchAndWriteKubeconfigForCP for testing.
func (p *Provisioner) FetchAndWriteKubeconfigForCPForTest(
	ctx context.Context,
	talosEndpoint, k8sEndpoint string,
) error {
	return p.fetchAndWriteKubeconfigForCP(ctx, talosEndpoint, k8sEndpoint)
}

// GetMappedK8sAPIEndpointForTest exposes getMappedK8sAPIEndpoint for testing.
func (p *Provisioner) GetMappedK8sAPIEndpointForTest(
	ctx context.Context,
	clusterName string,
) (string, error) {
	return p.getMappedK8sAPIEndpoint(ctx, clusterName)
}

// NthIPInNetworkForTest exposes nthIPInNetwork for unit testing.
func NthIPInNetworkForTest(prefix netip.Prefix, offset int) (netip.Addr, error) {
	return nthIPInNetwork(prefix, offset)
}

// ExtractTagFromImageForTest exposes extractTagFromImage for unit testing.
func ExtractTagFromImageForTest(image string) string {
	return extractTagFromImage(image)
}

// InstallerImageFromTagForTest exposes installerImageFromTag for unit testing.
func InstallerImageFromTagForTest(tag string) string {
	return installerImageFromTag(tag)
}

// SupportsLifecycleUpgradeAPIForTest exposes supportsLifecycleUpgradeAPI for unit testing.
func SupportsLifecycleUpgradeAPIForTest(versionTag string) bool {
	return supportsLifecycleUpgradeAPI(versionTag)
}

// ResolveInstallerImageForTest exposes resolveInstallerImage for unit testing.
func (p *Provisioner) ResolveInstallerImageForTest(toVersion string) string {
	return p.resolveInstallerImage(toVersion)
}

// ResolveSchematicIDForTest exposes resolveSchematicID for unit testing.
func (p *Provisioner) ResolveSchematicIDForTest() string {
	return p.resolveSchematicID()
}

// HasSchematicConfiguredForTest exposes hasSchematicConfigured for unit testing.
func (p *Provisioner) HasSchematicConfiguredForTest() bool {
	return p.hasSchematicConfigured()
}

// RenameKubeconfigContextForTest exposes k8s.RenameKubeconfigContext for unit testing.
func RenameKubeconfigContextForTest(kubeconfigData []byte, desiredContext string) ([]byte, error) {
	result, err := k8s.RenameKubeconfigContext(kubeconfigData, desiredContext)
	if err != nil {
		return nil, fmt.Errorf("rename kubeconfig context: %w", err)
	}

	return result, nil
}

// RefreshOmniConfigsIfNeededForTest exposes refreshOmniConfigsIfNeeded for unit testing.
func (p *Provisioner) RefreshOmniConfigsIfNeededForTest(
	ctx context.Context,
	clusterName string,
) error {
	return p.refreshOmniConfigsIfNeeded(ctx, clusterName)
}

// IsDockerProviderForTest exposes isDockerProvider for unit testing.
func (p *Provisioner) IsDockerProviderForTest() bool {
	return p.isDockerProvider()
}

// ClusterReadinessChecksCountForTest returns the number of checks from clusterReadinessChecks for unit testing.
func (p *Provisioner) ClusterReadinessChecksCountForTest() int {
	return len(p.clusterReadinessChecks())
}

// PreBootChecksCountForTest returns just the pre-boot check count for unit testing,
// isolating the pre-boot sequence selection from the k8s component checks.
// Only valid when skipNodeReadiness is true (CNI disabled or SkipCNIChecks set);
// when skipNodeReadiness is false, production code uses DefaultClusterChecks()
// which does not separate pre-boot from k8s component checks.
func (p *Provisioner) PreBootChecksCountForTest() int {
	skipNodeReadiness := (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) ||
		p.options.SkipCNIChecks

	if !skipNodeReadiness {
		panic("PreBootChecksCountForTest is only valid when skipNodeReadiness is true")
	}

	switch {
	case p.isDockerProvider():
		return len(dockerPreBootSequenceChecks())
	case p.talosConfigs != nil && p.talosConfigs.IsKubeletCertRotationEnabled():
		return len(preBootSequenceChecksSkipDiagnostics())
	default:
		return len(check.PreBootSequenceChecks())
	}
}

// EnsureAutoscalerSecretIfNeededForTest exposes ensureAutoscalerSecretIfNeeded for unit testing.
// diff and result default to a nil diff and a fresh result when callers exercise the
// guard/no-op paths; pass explicit values to cover the recycle-vs-in-place branch.
func (p *Provisioner) EnsureAutoscalerSecretIfNeededForTest(
	ctx context.Context,
	clusterName string,
) error {
	return p.ensureAutoscalerSecretIfNeeded(
		ctx, clusterName, nil, clusterupdate.NewEmptyUpdateResult(),
	)
}

// AutoscalerRecycleRequiredForTest exposes autoscalerRecycleRequired for unit testing.
func AutoscalerRecycleRequiredForTest(diff *clusterupdate.UpdateResult, imageChanged bool) bool {
	return autoscalerRecycleRequired(diff, imageChanged)
}

// AutoscalerRebootRequiredForTest exposes autoscalerRebootRequired for unit testing.
func AutoscalerRebootRequiredForTest(diff *clusterupdate.UpdateResult) bool {
	return autoscalerRebootRequired(diff)
}

// UpdateApplyStepNamesForTest returns the ordered names of the post-PrepareUpdate
// apply steps, so tests can assert the step ordering (notably that the autoscaler
// baseline refresh runs before static-node scaling — #5219) without driving a
// live cluster. The names do not depend on the spec/diff arguments.
func (p *Provisioner) UpdateApplyStepNamesForTest() []string {
	steps := p.updateApplySteps(
		"test-cluster",
		&v1alpha1.ClusterSpec{},
		&v1alpha1.ClusterSpec{},
		clusterupdate.NewEmptyUpdateResult(),
		clusterupdate.NewEmptyUpdateResult(),
		clusterupdate.UpdateOptions{},
	)

	names := make([]string, len(steps))
	for index, step := range steps {
		names[index] = step.name
	}

	return names
}

// RunUpdateApplyStepForTest runs one named post-PrepareUpdate apply step with
// caller-supplied update state, so tests can exercise ordering-sensitive steps
// without driving the full cluster update sequence.
func (p *Provisioner) RunUpdateApplyStepForTest(
	ctx context.Context,
	name, clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	diff, result *clusterupdate.UpdateResult,
) error {
	steps := p.updateApplySteps(
		clusterName,
		oldSpec,
		newSpec,
		diff,
		result,
		clusterupdate.UpdateOptions{},
	)

	for _, step := range steps {
		if step.name == name {
			return step.run(ctx)
		}
	}

	return fmt.Errorf("%w: %s", errUpdateApplyStepNotFoundForTest, name)
}

// SnapshotImageIDFromSecretForTest exposes snapshotImageIDFromSecret for unit testing.
func SnapshotImageIDFromSecretForTest(secret *corev1.Secret) string {
	return snapshotImageIDFromSecret(secret)
}

// CurrentAutoscalerSnapshotImageIDForTest exposes currentAutoscalerSnapshotImageID
// for unit testing.
func (p *Provisioner) CurrentAutoscalerSnapshotImageIDForTest(ctx context.Context) string {
	return p.currentAutoscalerSnapshotImageID(ctx)
}

// AutoscalerTemplateDriftForTest exposes autoscalerTemplateDrift for unit testing.
func AutoscalerTemplateDriftForTest(
	existing *corev1.Secret,
	pools []AutoscalerPoolConfig,
) ([]clusterupdate.Change, error) {
	return autoscalerTemplateDrift(existing, pools)
}

// DetectAutoscalerTemplateDriftForTest exposes detectAutoscalerTemplateDrift for
// unit testing.
func (p *Provisioner) DetectAutoscalerTemplateDriftForTest(
	ctx context.Context,
) ([]clusterupdate.Change, error) {
	return p.detectAutoscalerTemplateDrift(ctx)
}

// ApplyInPlaceToAutoscalerNodesForTest exposes applyInPlaceToAutoscalerNodes for unit testing.
func (p *Provisioner) ApplyInPlaceToAutoscalerNodesForTest(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	return p.applyInPlaceToAutoscalerNodes(ctx, clusterName, result)
}

// RollingRebootAutoscalerNodesForTest exposes rollingRebootAutoscalerNodes for unit testing.
func (p *Provisioner) RollingRebootAutoscalerNodesForTest(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	return p.rollingRebootAutoscalerNodes(ctx, clusterName, result)
}

// RestartAutoscalerAfterConfigChangeForTest exposes restartAutoscalerAfterConfigChange
// for unit testing.
func (p *Provisioner) RestartAutoscalerAfterConfigChangeForTest(
	ctx context.Context,
	kubeclient kubernetes.Interface,
) error {
	return p.restartAutoscalerAfterConfigChange(ctx, kubeclient)
}

// SortServersByNameForTest exposes sortServersByName for unit testing.
func SortServersByNameForTest(servers []*hcloud.Server) []*hcloud.Server {
	return sortServersByName(servers)
}

// RecycleAutoscalerNodesForTest exposes recycleAutoscalerNodes for unit testing.
func (p *Provisioner) RecycleAutoscalerNodesForTest(
	ctx context.Context,
	clusterName string,
) error {
	return p.recycleAutoscalerNodes(ctx, clusterName)
}

// WaitForAutoscalerRolloutForTest exposes waitForAutoscalerRollout for unit testing.
func (p *Provisioner) WaitForAutoscalerRolloutForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
) error {
	return p.waitForAutoscalerRollout(ctx, clientset)
}

// DrainResolvedNodeForTest exposes drainResolvedNode for unit testing.
func (p *Provisioner) DrainResolvedNodeForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeIP string,
) (string, error) {
	return p.drainResolvedNode(ctx, clientset, nodeIP)
}

// CordonAndDrainForTest exposes cordonAndDrain for unit testing.
func (p *Provisioner) CordonAndDrainForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	return p.cordonAndDrain(ctx, clientset, nodeName)
}

// UncordonAfterUpgradeForTest exposes uncordonAfterUpgrade for unit testing.
func (p *Provisioner) UncordonAfterUpgradeForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	return p.uncordonAfterUpgrade(ctx, clientset, nodeName)
}

// K8sClientOrWarnForUpgradeForTest exposes k8sClientOrWarnForUpgrade for unit testing.
func (p *Provisioner) K8sClientOrWarnForUpgradeForTest(clusterName string) kubernetes.Interface {
	return p.k8sClientOrWarnForUpgrade(clusterName)
}

// DrainTimeoutForTest exposes drainTimeout for unit testing.
func (p *Provisioner) DrainTimeoutForTest() time.Duration {
	return p.drainTimeout()
}

// SetDrainForceForTest sets the request-scoped drainForce flag for unit testing.
func (p *Provisioner) SetDrainForceForTest(force bool) {
	p.drainForce = force
}

// NewDrainHelperForTest exposes newDrainHelper for unit testing.
func NewDrainHelperForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	timeout time.Duration,
	disableEviction bool,
	logWriter io.Writer,
) *kubedrain.Helper {
	return newDrainHelper(ctx, clientset, timeout, disableEviction, logWriter)
}

// WithTalosOptsForTest sets talosOpts on the provisioner for unit testing.
func (p *Provisioner) WithTalosOptsForTest(
	opts *v1alpha1.OptionsTalos,
) *Provisioner {
	p.talosOpts = opts

	return p
}

// TalosConfigsForTest returns the provisioner's current talosConfigs for unit
// testing (e.g. asserting the endpoint/cert-SANs rendered by
// updateConfigsWithEndpoint).
func (p *Provisioner) TalosConfigsForTest() *talosconfigmanager.Configs {
	return p.talosConfigs
}

// WithTalosConfigsForTest sets talosConfigs on the provisioner for unit testing.
func (p *Provisioner) WithTalosConfigsForTest(
	configs *talosconfigmanager.Configs,
) *Provisioner {
	p.talosConfigs = configs

	return p
}

// MergeTalosconfigBytesForTest exposes mergeTalosconfigBytes for unit testing.
func MergeTalosconfigBytesForTest(talosconfigPath string, newData []byte) error {
	return mergeTalosconfigBytes(talosconfigPath, newData)
}

// RepresentativeServerTypeForTest exports representativeServerType for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var RepresentativeServerTypeForTest = representativeServerType

// CountServerNodesByRoleForTest exports countServerNodesByRole for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var CountServerNodesByRoleForTest = countServerNodesByRole

// MachineClusterConfigForTest is the exported alias of machineClusterConfig for testing.
type MachineClusterConfigForTest = machineClusterConfig

// DetectVolumeEncryptionChangesForTest exposes detectVolumeEncryptionChanges for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var DetectVolumeEncryptionChangesForTest = detectVolumeEncryptionChanges

// EncryptionProviderNameForTest exposes encryptionProviderName for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var EncryptionProviderNameForTest = encryptionProviderName

// ClassifyMachineConfigChangesForTest exposes classifyMachineConfigChanges for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ClassifyMachineConfigChangesForTest = classifyMachineConfigChanges

// DetectCNIChangesForTest exposes detectCNIChanges for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var DetectCNIChangesForTest = detectCNIChanges

// DetectDiskQuotaChangesForTest exposes detectDiskQuotaChanges for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var DetectDiskQuotaChangesForTest = detectDiskQuotaChanges

// CNINameForTest exposes cniName for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var CNINameForTest = cniName

// DiskQuotaEnabledForTest exposes diskQuotaEnabled for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var DiskQuotaEnabledForTest = diskQuotaEnabled

// MachineConfigDiffForTest exposes machineConfigDiff for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var MachineConfigDiffForTest = machineConfigDiff

// ConfigFingerprintForTest exposes configFingerprint for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ConfigFingerprintForTest = configFingerprint

// GraftNodeManagedSectionsForTest exposes graftNodeManagedSections for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var GraftNodeManagedSectionsForTest = graftNodeManagedSections

// BuildDesiredNodeConfigForTest exposes buildDesiredNodeConfig for unit testing.
func (p *Provisioner) BuildDesiredNodeConfigForTest(
	running talosconfig.Provider,
	secretsSource talosconfig.Provider,
	role string,
) (talosconfig.Provider, error) {
	return p.buildDesiredNodeConfig(running, secretsSource, role)
}

// HasDesiredHetznerFloatingIPEndpointForTest exposes the authoritative desired
// endpoint predicate so endpoint-preservation tests prove they exercise its
// true branch rather than passing through unrelated config grafting.
func (p *Provisioner) HasDesiredHetznerFloatingIPEndpointForTest() bool {
	return p.hasDesiredHetznerFloatingIPEndpoint()
}

// DetectRoleMachineConfigDriftForTest exposes detectRoleMachineConfigDrift for
// unit testing per-role (control-plane vs worker) patch drift detection.
func (p *Provisioner) DetectRoleMachineConfigDriftForTest(
	running talosconfig.Provider,
	secretsSource talosconfig.Provider,
	role string,
) ([]clusterupdate.Change, error) {
	return p.detectRoleMachineConfigDrift(running, secretsSource, role)
}

// WithNodeConfigFetcherForTest overrides the running-config fetcher so unit tests
// can drive the per-node desired-config rebuild (fetchAndBuildDesiredNodeConfig)
// without real Talos API connectivity.
func (p *Provisioner) WithNodeConfigFetcherForTest(
	fn func(ctx context.Context, nodeIP string) (talosconfig.Provider, error),
) *Provisioner {
	p.nodeConfigFetcher = fn

	return p
}

// FetchAndBuildDesiredNodeConfigForTest exposes fetchAndBuildDesiredNodeConfig — the
// per-node desired-config rebuild shared by the rolling-reboot staging path and the
// pre-upgrade reconcile — for unit testing.
func (p *Provisioner) FetchAndBuildDesiredNodeConfigForTest(
	ctx context.Context,
	node NodeWithRoleForTest,
	secretsSource talosconfig.Provider,
) (talosconfig.Provider, error) {
	return p.fetchAndBuildDesiredNodeConfig(ctx, node, secretsSource)
}

// ReconcileNodeConfigBeforeUpgradeForTest exposes reconcileNodeConfigBeforeUpgrade —
// the NO_REBOOT config reconcile applied to a node ahead of its Talos OS upgrade
// (issue #5294) — for unit testing.
func (p *Provisioner) ReconcileNodeConfigBeforeUpgradeForTest(
	ctx context.Context,
	node NodeWithRoleForTest,
	secretsSource talosconfig.Provider,
) error {
	return p.reconcileNodeConfigBeforeUpgrade(ctx, node, secretsSource)
}

// MachineConfigFieldForTest exposes the machine.config change field for unit testing.
const MachineConfigFieldForTest = MachineConfigField

// ValidateCurrentContextCAForTest exposes validateCurrentContextCA for unit testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var ValidateCurrentContextCAForTest = validateCurrentContextCA

// SortNodesWorkersFirstForTest exposes sortNodesWorkersFirst for unit testing.
func SortNodesWorkersFirstForTest(nodes []NodeWithRoleForTest) []NodeWithRoleForTest {
	return sortNodesWorkersFirst(nodes)
}

// ApplyWipeRequiredChangesForTest exposes applyWipeRequiredChanges for unit testing.
func (p *Provisioner) ApplyWipeRequiredChangesForTest(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	return p.applyWipeRequiredChanges(ctx, clusterName, result)
}

// PartitionWipeDecisionForTest exposes partitionWipeDecision for unit testing.
func PartitionWipeDecisionForTest(changes []clusterupdate.Change) (bool, bool) {
	return partitionWipeDecision(changes)
}

// MergePersistedStateForTest exposes mergePersistedState for unit testing.
func (p *Provisioner) MergePersistedStateForTest(
	spec *v1alpha1.ClusterSpec,
	clusterName string,
) error {
	return p.mergePersistedState(spec, clusterName)
}

// WithKernelModuleLoaderForTest overrides the kernel module loader so unit tests
// can exercise the Docker provisioning path without invoking modprobe.
func (p *Provisioner) WithKernelModuleLoaderForTest(
	f func(ctx context.Context, logWriter io.Writer) error,
) *Provisioner {
	p.kernelModuleLoader = f

	return p
}

// RolesFromRollingChangesForTest exposes rolesFromRollingChanges for unit testing.
func RolesFromRollingChangesForTest(changes []clusterupdate.Change) (bool, bool) {
	return rolesFromRollingChanges(changes)
}

// ApplyRollingRecreateChangesForTest exposes applyRollingRecreateChanges for unit testing.
func (p *Provisioner) ApplyRollingRecreateChangesForTest(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	return p.applyRollingRecreateChanges(ctx, clusterName, result)
}

// ServersNeedingReplacementForTest exposes serversNeedingReplacement for unit testing.
func ServersNeedingReplacementForTest(
	servers []*hcloud.Server,
	desiredType string,
) []*hcloud.Server {
	return serversNeedingReplacement(servers, desiredType)
}

// AppendServerTypeChangeForTest exposes appendServerTypeChange for unit testing.
func AppendServerTypeChangeForTest(
	diff *clusterupdate.UpdateResult,
	role, current, desired string,
	category clusterupdate.ChangeCategory,
) {
	appendServerTypeChange(diff, role, current, desired, category)
}

// NodeMatchesServerForTest exposes nodeMatchesServer for unit testing.
func NodeMatchesServerForTest(node *corev1.Node, serverName, serverIP string) bool {
	return nodeMatchesServer(node, serverName, serverIP)
}

// NodeIsReadyForTest exposes nodeIsReady for unit testing.
func NodeIsReadyForTest(node *corev1.Node) bool {
	return nodeIsReady(node)
}

// PropagateAutoscalerBaselineForTest exposes propagateAutoscalerBaseline for unit
// testing — the #5219 dispatcher routing existing autoscaler nodes to the recycle,
// in-place reboot, or NO_REBOOT in-place path based on how disruptive the diff is.
func (p *Provisioner) PropagateAutoscalerBaselineForTest(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	imageChanged bool,
	result *clusterupdate.UpdateResult,
) error {
	return p.propagateAutoscalerBaseline(ctx, clusterName, diff, imageChanged, result)
}

// --- between-node storage-health gate (#5467) test seam ---

// StorageHealthProberForTest is the test view of the unexported storageHealthProber
// seam: a func returning the namespaced names of currently-unhealthy volumes.
type StorageHealthProberForTest func(ctx context.Context) ([]string, error)

// degradedVolumes makes StorageHealthProberForTest satisfy the storageHealthProber
// seam so tests can stub volume health without a live cluster.
func (f StorageHealthProberForTest) degradedVolumes(ctx context.Context) ([]string, error) {
	return f(ctx)
}

// WaitForStorageHealthyForTest exercises the between-node storage-health gate with a
// stubbed prober. A nil prober exercises the "no backend detected / gate disabled"
// no-op path.
func (p *Provisioner) WaitForStorageHealthyForTest(
	ctx context.Context,
	prober StorageHealthProberForTest,
	timeout time.Duration,
) error {
	if prober == nil {
		return p.waitForStorageHealthy(ctx, nil, timeout)
	}

	return p.waitForStorageHealthy(ctx, prober, timeout)
}

// LonghornDetectedForTest exposes longhornDetected for unit testing the storage
// backend detection, returning the detection result alongside any non-NotFound
// lookup error.
func (p *Provisioner) LonghornDetectedForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
) (bool, error) {
	return p.longhornDetected(ctx, clientset)
}

// LonghornDegradedVolumesForTest exposes the Longhorn volume robustness
// classification against a (fake) dynamic client.
func LonghornDegradedVolumesForTest(
	ctx context.Context,
	client dynamic.Interface,
) ([]string, error) {
	prober := &longhornVolumeProber{client: client}

	return prober.degradedVolumes(ctx)
}

// LonghornVolumeGVRForTest exposes the Longhorn Volume GroupVersionResource so tests
// can register matching objects with the dynamic fake.
func LonghornVolumeGVRForTest() schema.GroupVersionResource {
	return longhornVolumeGVR()
}

// GenericDegradedVolumesForTest exposes the backend-agnostic prober's PV / PVC /
// VolumeAttachment classification against a (fake) clientset.
func GenericDegradedVolumesForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
) ([]string, error) {
	prober := &genericStorageProber{clientset: clientset}

	return prober.degradedVolumes(ctx)
}

// MultiDegradedVolumesForTest exposes the composed prober's union semantics over
// stubbed probers.
func MultiDegradedVolumesForTest(
	ctx context.Context,
	probers ...StorageHealthProberForTest,
) ([]string, error) {
	inner := make([]storageHealthProber, 0, len(probers))
	for _, prober := range probers {
		inner = append(inner, prober)
	}

	return (&multiStorageProber{probers: inner}).degradedVolumes(ctx)
}

// StorageHealthTimeoutForTest exposes the resolved gate timeout (0 = disabled).
func (p *Provisioner) StorageHealthTimeoutForTest() time.Duration {
	return p.storageHealthTimeout()
}

// BuildStorageHealthProberForTest exercises buildStorageHealthProber, returning
// whether a (non-nil) prober was built alongside any error — so the composition
// (generic prober always; backend prober when detected) is testable without a live
// cluster.
func (p *Provisioner) BuildStorageHealthProberForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
) (bool, error) {
	prober, err := p.buildStorageHealthProber(ctx, clientset, clusterName)

	return prober != nil, err
}

// BuildStorageHealthProberOrWarnForTest exercises the graceful-degrade wrapper shared
// by the primary and autoscaler rolls, returning whether a (non-nil) prober was built.
func (p *Provisioner) BuildStorageHealthProberOrWarnForTest(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
) bool {
	return p.buildStorageHealthProberOrWarn(ctx, clientset, clusterName) != nil
}

// PublishConnectorKubeconfigForTest exercises the create-time Connector Secret publish with
// caller-supplied kubeconfig bytes (the seam replaces the live Talos clusterAccess fetch).
func (p *KubernetesProvisioner) PublishConnectorKubeconfigForTest(
	ctx context.Context,
	clusterName string,
	raw []byte,
) error {
	return p.publishConnectorKubeconfig(ctx, clusterName, raw)
}
