package talosprovisioner

import (
	"context"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	omniprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clusterupdate"
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

// ExtractTagFromImageForTest exposes extractTagFromImage for unit testing.
func ExtractTagFromImageForTest(image string) string {
	return extractTagFromImage(image)
}

// InstallerImageFromTagForTest exposes installerImageFromTag for unit testing.
func InstallerImageFromTagForTest(tag string) string {
	return installerImageFromTag(tag)
}

// RenameKubeconfigContextForTest exposes renameKubeconfigContext for unit testing.
func RenameKubeconfigContextForTest(kubeconfigData []byte, desiredContext string) ([]byte, error) {
	return renameKubeconfigContext(kubeconfigData, desiredContext)
}
