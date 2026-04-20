package kubeconfighook

import (
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ResolveClusterNameForTest exports resolveClusterName for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ResolveClusterNameForTest = resolveClusterName

// ClusterNameFromDistConfigForTest exports clusterNameFromDistConfig for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ClusterNameFromDistConfigForTest = clusterNameFromDistConfig

// ClusterNameFromKubeconfigForTest exports clusterNameFromKubeconfig for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var ClusterNameFromKubeconfigForTest = clusterNameFromKubeconfig

// AtomicWriteFileForTest exports atomicWriteFile for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions.
var AtomicWriteFileForTest = atomicWriteFile

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
