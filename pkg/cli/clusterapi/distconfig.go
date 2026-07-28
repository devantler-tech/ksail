package clusterapi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"sigs.k8s.io/yaml"
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
		// BuildDistributionConfig returns (nil, nil) for the caller-specific distributions: EKS is
		// handled above; GKE and AKS are not yet creatable via the local backend (their scope/spec
		// resolution ships with the ksail CLI + operator paths first), so they report unavailable here.
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
	// A cluster that finished creating already has its region recorded in the eks.yaml written at
	// create time. Re-rendering that file from the ambient AWS_REGION for a later delete/start/stop
	// would point the action at a same-named cluster in whichever region is selected now, and would
	// overwrite the only local evidence of the original target, so an existing binding always wins.
	bound, err := boundEKSConfig(name)
	if err != nil {
		return nil, err
	}

	if bound != nil {
		return &clusterprovisioner.DistributionConfig{EKS: bound}, nil
	}

	region := os.Getenv(credentials.DefaultEnvVar(credentials.AWSRegion))

	configPath, err := writeEKSConfig(name, region)
	if err != nil {
		return nil, err
	}

	return &clusterprovisioner.DistributionConfig{
		EKS: &clusterprovisioner.EKSConfig{Name: name, Region: region, ConfigPath: configPath},
	}, nil
}

// eksConfigMetadata is the subset of an eksctl ClusterConfig this package reads back: the region
// bound when the cluster was created.
type eksConfigMetadata struct {
	Metadata struct {
		Region string `json:"region"`
	} `json:"metadata"`
}

// boundEKSConfig returns the EKS target recorded when the named cluster was created, or nil when
// there is no such record and the caller should render a fresh config.
//
// Persisted cluster state is the discriminator: it is written only after a provisioner reports a
// successful create, so its absence means either a first create or a retry after a failed one —
// both of which must honour the region selected now. Once it exists, every subsequent action is a
// mutation of an existing remote cluster and is bound to the region that created it.
//
// Missing or unreadable evidence for a cluster that did complete creation is an error rather than a
// silent fall back to the ambient region: the fallback is precisely the redirect being prevented.
func boundEKSConfig(name string) (*clusterprovisioner.EKSConfig, error) {
	if name == "" {
		return nil, nil //nolint:nilnil // no cluster to bind: discovery-time factory construction.
	}

	_, err := state.LoadClusterSpec(name)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			return nil, nil //nolint:nilnil // not created yet: caller renders from the ambient region.
		}

		return nil, fmt.Errorf(
			"%w: read local KSail state for EKS cluster %q: %w", api.ErrInvalid, name, err,
		)
	}

	configPath, err := eksConfigPath(name, false)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // configPath is contained within ~/.ksail/clusters (see canonicalClusterDir).
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: read the eks config that binds cluster %q to its region: %w",
			api.ErrInvalid, name, err,
		)
	}

	var parsed eksConfigMetadata

	err = yaml.Unmarshal(content, &parsed)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse the eks config that binds cluster %q to its region: %w",
			api.ErrInvalid, name, err,
		)
	}

	if parsed.Metadata.Region == "" {
		return nil, fmt.Errorf(
			"%w: the eks config for cluster %q records no region, so its target cannot be"+
				" confirmed; run `ksail cluster rebind-eks-ownership --name %s` to re-establish it",
			api.ErrInvalid, name, name,
		)
	}

	return &clusterprovisioner.EKSConfig{
		Name:       name,
		Region:     parsed.Metadata.Region,
		ConfigPath: configPath,
	}, nil
}

// eksConfigPath resolves ~/.ksail/clusters/<name>/eks.yaml.
//
// The name becomes exactly one directory under ~/.ksail/clusters, so it must be a single path
// segment. filepath.IsLocal alone is insufficient — it still permits multi-segment names like
// "foo/bar" and ".", which would redirect the path into an unintended nested directory — so the
// name must also equal its own base element, and the "." / ".." specials are rejected. The resolved
// directory is then verified to stay under ~/.ksail/clusters even after symlink resolution, so
// neither a crafted name nor a symlinked cluster directory can redirect it out of the intended tree.
//
// createDir distinguishes the two callers: the write path creates the directory, while reading an
// existing cluster's binding must not bring one into existence.
func eksConfigPath(name string, createDir bool) (string, error) {
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

	if createDir {
		mkErr := os.MkdirAll(filepath.Join(clustersRoot, name), eksConfigDirMode)
		if mkErr != nil {
			return "", fmt.Errorf("create eks config directory: %w", mkErr)
		}
	}

	dir, err := canonicalClusterDir(clustersRoot, name)
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, scaffolder.EKSConfigFile), nil
}

// writeEKSConfig renders and writes the eks.yaml for a cluster, returning its path.
func writeEKSConfig(name, region string) (string, error) {
	configPath, err := eksConfigPath(name, true)
	if err != nil {
		return "", err
	}

	content := scaffolder.RenderEKSConfig(scaffolder.DefaultEKSConfigParams(name, region))

	//nolint:gosec // eksConfigPath canonicalizes and contains configPath within ~/.ksail/clusters.
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
