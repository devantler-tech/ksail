package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"net/netip"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	check "github.com/siderolabs/talos/pkg/cluster/check"
	corev1 "k8s.io/api/core/v1"
)

// NodeWithRoleForTest is the exported alias of nodeWithRole for testing.
type NodeWithRoleForTest = nodeWithRole

// CountNodeRolesForTest exposes countNodeRoles for unit testing.
func CountNodeRolesForTest(nodes []NodeWithRoleForTest) (int32, int32) {
	return countNodeRoles(nodes)
}

// NewNodeWithRoleForTest creates a nodeWithRole for unit testing.
func NewNodeWithRoleForTest(ip, role string) NodeWithRoleForTest {
	return nodeWithRole{IP: ip, Role: role}
}

// NextNodeIndexFromNamesForTest exposes nextNodeIndexFromNames for unit testing.
func NextNodeIndexFromNamesForTest(names []string, prefix string) int {
	return nextNodeIndexFromNames(names, prefix)
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
// Returns node names and IPs as parallel slices (nodeSpec is unexported).
func PreCalculateNodeSpecsForTest(
	cidr netip.Prefix,
	clusterName, role string,
	nextIndex, count, cpCount int,
) ([]string, []netip.Addr, error) {
	specs, err := preCalculateNodeSpecs(cidr, clusterName, role, nextIndex, count, cpCount)
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
func (p *Provisioner) EnsureAutoscalerSecretIfNeededForTest(
	ctx context.Context,
	clusterName string,
) error {
	return p.ensureAutoscalerSecretIfNeeded(ctx, clusterName)
}

// WithTalosOptsForTest sets talosOpts on the provisioner for unit testing.
func (p *Provisioner) WithTalosOptsForTest(
	opts *v1alpha1.OptionsTalos,
) *Provisioner {
	p.talosOpts = opts

	return p
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
