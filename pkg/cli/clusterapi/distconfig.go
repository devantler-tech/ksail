package clusterapi

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// File modes for the generated EKS config under ~/.ksail/clusters/<name>.
const (
	eksConfigDirMode  = 0o700
	eksConfigFileMode = 0o600
)

// distributionConfig builds the in-memory distribution config the local provisioner factory needs for
// the given cluster name. Every distribution except EKS is built by the shared
// clusterprovisioner.BuildDistributionConfig (converged with the operator backend, honoring Talos
// version pins and applying the local Kind defaulting an empty Kind config requires). EKS is local-
// specific: it renders an on-disk eksctl.yaml the EKS provisioner reads, so it is handled here.
func distributionConfig(
	distribution v1alpha1.Distribution,
	name string,
) (*clusterprovisioner.DistributionConfig, error) {
	if distribution == v1alpha1.DistributionEKS {
		return eksDistributionConfig(name)
	}

	cluster := &v1alpha1.Cluster{}
	cluster.Name = name
	cluster.Spec.Cluster.Distribution = distribution

	config, err := clusterprovisioner.BuildDistributionConfig(cluster, name, true)
	if err != nil {
		return nil, fmt.Errorf("build distribution config: %w", err)
	}

	if config == nil {
		// BuildDistributionConfig returns (nil, nil) only for EKS, handled above; any other distribution
		// without a config is unavailable locally.
		return nil, errDistributionUnavailable(distribution)
	}

	return config, nil
}

// eksDistributionConfig renders an eksctl ClusterConfig (region from the AWS_REGION environment,
// which the credential overlay populates from Settings) and writes it under ~/.ksail/clusters/<name>
// so the EKS provisioner has the on-disk config it requires to create the cluster.
func eksDistributionConfig(
	name string,
) (*clusterprovisioner.DistributionConfig, error) {
	region := os.Getenv(credentials.DefaultEnvVar(credentials.AWSRegion))

	configPath, err := writeEKSConfig(name, region)
	if err != nil {
		return nil, err
	}

	return &clusterprovisioner.DistributionConfig{
		EKS: &clusterprovisioner.EKSConfig{Name: name, Region: region, ConfigPath: configPath},
	}, nil
}

// writeEKSConfig renders and writes the eks.yaml for a cluster, returning its path. The name must be
// a single path segment, and the resolved directory is verified to stay under ~/.ksail/clusters even
// after symlink resolution, so neither a crafted name nor a symlinked cluster directory can redirect
// the write outside the intended tree.
func writeEKSConfig(name, region string) (string, error) {
	// The name becomes exactly one directory under ~/.ksail/clusters, so it must be a single path
	// segment. filepath.IsLocal alone is insufficient — it still permits multi-segment names like
	// "foo/bar" and ".", which would redirect the write into an unintended nested directory — so also
	// require the name to equal its own base element and reject the "." / ".." specials.
	if !filepath.IsLocal(name) || name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf(
			"%w: cluster name %q must be a single path segment",
			api.ErrInvalid,
			name,
		)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	clustersRoot := filepath.Join(home, ".ksail", "clusters")

	mkErr := os.MkdirAll(filepath.Join(clustersRoot, name), eksConfigDirMode)
	if mkErr != nil {
		return "", fmt.Errorf("create eks config directory: %w", mkErr)
	}

	dir, err := canonicalClusterDir(clustersRoot, name)
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(dir, scaffolder.EKSConfigFile)
	content := scaffolder.RenderEKSConfig(scaffolder.DefaultEKSConfigParams(name, region))

	// dir is canonicalized and verified within ~/.ksail/clusters by canonicalClusterDir above.
	//nolint:gosec // configPath is contained within ~/.ksail/clusters (see canonicalClusterDir)
	writeErr := os.WriteFile(configPath, content, eksConfigFileMode)
	if writeErr != nil {
		return "", fmt.Errorf("write eks config: %w", writeErr)
	}

	return configPath, nil
}

// canonicalClusterDir canonicalizes ~/.ksail/clusters/<name> (resolving symlinks) and confirms it
// remains within the canonical clusters root, rejecting any path that escapes it. The containment
// check is delegated to fsutil.IsPathWithinDirectory so it cannot drift from the other callers of
// the shared symlink-escape guard.
func canonicalClusterDir(clustersRoot, name string) (string, error) {
	canonicalRoot, err := fsutil.EvalCanonicalPath(clustersRoot)
	if err != nil {
		return "", fmt.Errorf("canonicalize clusters directory: %w", err)
	}

	canonicalDir, err := fsutil.EvalCanonicalPath(filepath.Join(clustersRoot, name))
	if err != nil {
		return "", fmt.Errorf("canonicalize eks config directory: %w", err)
	}

	if !fsutil.IsPathWithinDirectory(canonicalDir, canonicalRoot) {
		return "", fmt.Errorf("%w: eks config path escapes %s", api.ErrInvalid, canonicalRoot)
	}

	return canonicalDir, nil
}

// errDistributionUnavailable reports that a distribution cannot be provisioned by the local backend.
func errDistributionUnavailable(distribution v1alpha1.Distribution) error {
	return fmt.Errorf(
		"%w: distribution %q is not available locally",
		api.ErrNotSupported,
		distribution,
	)
}
