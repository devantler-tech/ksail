package localregistry

import (
	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/client/oci"
	clusterprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ResolveClusterNameForTest exports resolveClusterName for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ResolveClusterNameForTest = resolveClusterName

// ResolveNetworkNameForTest exports resolveNetworkName for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ResolveNetworkNameForTest = resolveNetworkName

// ResolveStageForTest exports resolveStage for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ResolveStageForTest = resolveStage

// BuildVerifyOptionsForTest exports buildVerifyOptions for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var BuildVerifyOptionsForTest = buildVerifyOptions

// ShouldSkipK3dForTest exports shouldSkipK3d for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ShouldSkipK3dForTest = shouldSkipK3d

// IsCloudProviderForTest exports isCloudProvider for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var IsCloudProviderForTest = isCloudProvider

// NewRegistryContextForTest exports newRegistryContext for testing.
func NewRegistryContextForTest(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig interface{},
	vclusterConfig *clusterprovisioner.VClusterConfig,
) (string, string) {
	// We can't expose the registryContext type directly, so return its fields.
	ctx := newRegistryContext(clusterCfg, kindConfig, k3dConfig, nil, vclusterConfig)
	return ctx.clusterName, ctx.networkName
}

// NewCreateOptionsForTest exports newCreateOptions for testing.
func NewCreateOptionsForTest(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	networkName string,
) registry.CreateOptions {
	ctx := registryContext{clusterName: clusterName, networkName: networkName}
	return newCreateOptions(clusterCfg, ctx)
}

// BuildVerifyOptionsType is the VerifyOptions type alias for testing.
type BuildVerifyOptionsType = oci.VerifyOptions
