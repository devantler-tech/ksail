package clusterapi

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

	// Validate the name first so a malformed one keeps reporting itself. The region check below
	// would otherwise fire for every name whenever no region is selected, masking the name error
	// behind an environment one. This is the name check alone — resolving the full path here would
	// canonicalize a ~/.ksail/clusters that a first create has not created yet.
	err = validateClusterSegmentName(name)
	if err != nil {
		return nil, err
	}

	// Resolve the region HERE rather than letting the scaffolder substitute its default further
	// down. Both paths would otherwise derive it independently: writeEKSConfig renders through
	// scaffolder.DefaultEKSConfigParams, which replaces an empty region with its own default, so
	// metadata.region would carry that default while the region bound into persisted state stayed
	// empty. boundEKSConfig then compares the two and rejects the file permanently — a cluster that
	// can never be deleted, started or stopped through KSail.
	//
	// Refusing the create instead would be the wrong fix for that: an unset AWS_REGION is a
	// supported path (credentials or a named profile are enough for AWS discovery), and the empty
	// region it was said to stamp could never reach the file. One resolved value, used by both
	// sides, keeps the binding honest without removing the path.
	region := os.Getenv(credentials.DefaultEnvVar(credentials.AWSRegion))
	if region == "" {
		region = scaffolder.DefaultEKSConfigParams(name, "").Region
	}

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

// eksCreateCompleted reports whether persisted state shows an EKS create completed for name.
//
// State is written for EVERY distribution, so its existence alone does not prove that: it may
// belong to a Kind or Talos cluster of the same name. Entering the bound path on that would look
// for an eks.yaml nothing wrote and report a missing-file read error, hiding the name collision the
// user actually has. A collision is reported rather than treated as a fresh create, because two
// clusters would otherwise share one state directory and the second create would overwrite the
// first record.
func eksCreateCompleted(name string) (bool, error) {
	spec, err := state.LoadClusterSpec(name)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			// Not created yet: the caller renders from the ambient region.
			return false, nil
		}

		return false, fmt.Errorf(
			"%w: read local KSail state for EKS cluster %q: %w", api.ErrInvalid, name, err,
		)
	}

	if spec != nil && spec.Distribution != v1alpha1.DistributionEKS {
		return false, fmt.Errorf(
			"%w: local KSail state for cluster %q records the %s distribution, not EKS; "+
				"delete that cluster or choose another name before creating it on EKS",
			api.ErrInvalid, name, spec.Distribution,
		)
	}

	return true, nil
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

	created, err := eksCreateCompleted(name)
	if err != nil || !created {
		return nil, err
	}

	configPath, err := eksConfigPath(name, false)
	if err != nil {
		return nil, err
	}

	clustersRoot, err := eksClustersRoot()
	if err != nil {
		return nil, err
	}

	content, err := fsutil.ReadFileSafe(clustersRoot, configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return bindFromOwnershipRecord(name)
		}

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
				" confirmed; set metadata.region in %s to the region the cluster was created in",
			api.ErrInvalid, name, configPath,
		)
	}

	return &clusterprovisioner.EKSConfig{
		Name:       name,
		Region:     parsed.Metadata.Region,
		ConfigPath: configPath,
	}, nil
}

// bindFromOwnershipRecord binds a cluster to its creation region using the immutable EKS ownership
// record, for a cluster whose eks.yaml is not in the state directory.
//
// `ksail cluster create` scaffolds eks.yaml into the PROJECT directory, while only the local API
// backend writes one under ~/.ksail/clusters/<name>. Persisted cluster state is written by both, so
// a CLI-created cluster reached the bound path and then failed reading a file nothing had put
// there — breaking every start, stop and delete for exactly the clusters created the normal way.
//
// The ownership record is the better binding for both: it is the immutable, region-scoped identity
// captured after AWS confirmed the create, so it cannot drift with the ambient region the way a
// re-rendered config can. The config is materialised from that recorded region so the provisioner
// still gets the on-disk file it requires.
//
// Several records mean several same-named clusters in different regions, which is a question only
// the operator can answer. Picking one would aim a destructive action at a cluster nobody named, so
// this refuses and lists them.
func bindFromOwnershipRecord(name string) (*clusterprovisioner.EKSConfig, error) {
	ownerships, err := state.ListEKSOwnershipStates(name)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: cluster %q has local KSail state but no eks config and no ownership record to bind"+
				" it to a region; run `ksail cluster eks-bind` to record the region it was created"+
				" in before starting, stopping or deleting it: %w",
			api.ErrInvalid,
			name,
			err,
		)
	}

	if len(ownerships) > 1 {
		regions := make([]string, 0, len(ownerships))
		for _, ownership := range ownerships {
			regions = append(regions, ownership.Region)
		}

		return nil, fmt.Errorf(
			"%w: cluster %q has ownership records in more than one region (%s), so KSail cannot tell"+
				" which cluster this action means; delete the records for the regions you do not mean"+
				" before retrying",
			api.ErrInvalid,
			name,
			strings.Join(regions, ", "),
		)
	}

	region := ownerships[0].Region

	configPath, err := writeEKSConfig(name, region)
	if err != nil {
		return nil, err
	}

	return &clusterprovisioner.EKSConfig{
		Name:       name,
		Region:     region,
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
	err := validateClusterSegmentName(name)
	if err != nil {
		return "", err
	}

	clustersRoot, err := eksClustersRoot()
	if err != nil {
		return "", err
	}

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

// validateClusterSegmentName rejects any cluster name that would not become exactly one directory
// under ~/.ksail/clusters. filepath.IsLocal alone is insufficient — it still permits multi-segment
// names like "foo/bar" and ".", which would redirect the path into an unintended nested directory —
// so the name must also equal its own base element, and the "." / ".." specials are rejected.
//
// It is separated from eksConfigPath so callers can reject a bad name without touching the
// filesystem, which matters before ~/.ksail/clusters exists.
func validateClusterSegmentName(name string) error {
	if !filepath.IsLocal(name) || name != filepath.Base(name) || name == "." || name == ".." {
		return fmt.Errorf(
			"%w: cluster name %q must be a single path segment",
			api.ErrInvalid,
			name,
		)
	}

	return nil
}

// eksClustersRoot resolves ~/.ksail/clusters — the single directory every generated cluster config
// is confined to, and therefore the base every read of one is bounded by.
func eksClustersRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ksail", "clusters"), nil
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
