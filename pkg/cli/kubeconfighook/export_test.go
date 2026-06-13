package kubeconfighook

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ResolveClusterNameForTest exports resolveClusterName for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ResolveClusterNameForTest = resolveClusterName

// ClusterNameFromDistConfigForTest exports the shared distribution-config name
// extraction (now owned by lifecycle.ClusterNameFromDistributionConfig) that the
// Omni hook delegates to, for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ClusterNameFromDistConfigForTest = lifecycle.ClusterNameFromDistributionConfig

// ClusterNameFromKubeconfigForTest exports clusterNameFromKubeconfig for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ClusterNameFromKubeconfigForTest = clusterNameFromKubeconfig

// AtomicWriteFileForTest exports fsutil.AtomicWriteFile for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var AtomicWriteFileForTest = fsutil.AtomicWriteFile

// IsKubeconfigFlagExplicitForTest exports isKubeconfigFlagExplicit for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var IsKubeconfigFlagExplicitForTest = isKubeconfigFlagExplicit

// JwtExpiryForTest exports jwtExpiry for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var JwtExpiryForTest = jwtExpiry

// DistributionConfig is a type alias for convenience in test files.
type DistributionConfig = clusterprovisioner.DistributionConfig
