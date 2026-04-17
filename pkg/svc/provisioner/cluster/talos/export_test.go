package talosprovisioner

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/k8s"
	omniprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
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

// SaveOmniConfigForTest exposes saveOmniConfig for unit testing.
func (p *Provisioner) SaveOmniConfigForTest(
	configData []byte,
	rawPath string,
	configLabel string,
) error {
	return p.saveOmniConfig(configData, rawPath, configLabel)
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
