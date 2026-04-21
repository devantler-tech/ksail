package localregistry

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
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
