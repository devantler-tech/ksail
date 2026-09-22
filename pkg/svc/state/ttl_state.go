package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ttlFileName = "ttl.json"
	// eksTTLFileNameFormat is region-scoped because EKS cluster names are unique only within one AWS
	// region: same-named clusters in two regions each keep their own TTL.
	eksTTLFileNameFormat = "ttl-%s.json"
)

// ErrNonPositiveTTL is returned when a non-positive TTL duration is provided.
var ErrNonPositiveTTL = errors.New("TTL duration must be positive")

// ErrTTLNotSet is returned when no TTL has been set for a cluster.
var ErrTTLNotSet = errors.New("cluster TTL not set")

// ErrAmbiguousTTLRegion is returned when an EKS cluster's TTL is read without a region and
// same-named clusters in several regions each have one, so none can be chosen.
var ErrAmbiguousTTLRegion = errors.New("EKS cluster TTL recorded in several regions")

// TTLInfo holds time-to-live information for a cluster.
type TTLInfo struct {
	// ExpiresAt is the UTC time when the cluster should be destroyed.
	ExpiresAt time.Time `json:"expiresAt"`
	// Duration is the normalized string representation of the TTL (e.g. "2h0m0s").
	Duration string `json:"duration"`
}

// Remaining returns the duration remaining until TTL expiry.
// Returns a non-positive duration if the TTL has already expired.
func (t *TTLInfo) Remaining() time.Duration {
	return time.Until(t.ExpiresAt)
}

// IsExpired reports whether the cluster TTL has passed.
func (t *TTLInfo) IsExpired() bool {
	return t.Remaining() <= 0
}

// SaveClusterTTL persists TTL information for a cluster.
// ttl must be a positive duration. Returns ErrNonPositiveTTL otherwise.
// The expiry time is calculated as now + ttl.
func SaveClusterTTL(clusterName string, ttl time.Duration) error {
	if ttl <= 0 {
		return ErrNonPositiveTTL
	}

	ttlPath, err := clusterTTLPath(clusterName)
	if err != nil {
		return err
	}

	return writeTTL(ttlPath, ttl)
}

// SaveEKSClusterTTL persists TTL information for the EKS cluster clusterName in region. It is kept
// apart from any same-named cluster's TTL in another region. ttl must be a positive duration.
func SaveEKSClusterTTL(clusterName, region string, ttl time.Duration) error {
	if ttl <= 0 {
		return ErrNonPositiveTTL
	}

	ttlPath, err := eksTTLPath(clusterName, region)
	if err != nil {
		return err
	}

	return writeTTL(ttlPath, ttl)
}

// LoadClusterTTL loads TTL information for a cluster.
// Returns ErrTTLNotSet if no TTL has been set for the cluster.
func LoadClusterTTL(clusterName string) (*TTLInfo, error) {
	ttlPath, err := clusterTTLPath(clusterName)
	if err != nil {
		return nil, err
	}

	return readTTL(ttlPath)
}

// LoadEKSClusterTTL loads TTL information for the EKS cluster clusterName in region.
//
// An empty region is for a caller that knows the cluster but not its region: the TTL is returned
// when exactly one region has one, and ErrAmbiguousTTLRegion when several do, since the name can
// exist in each of them.
//
// A TTL saved before TTLs were region-scoped lives in the name-scoped file shared by every region.
// It is returned when no region-scoped TTL applies, so clusters created before the change keep
// their TTL. Returns ErrTTLNotSet when there is neither.
func LoadEKSClusterTTL(clusterName, region string) (*TTLInfo, error) {
	ttlPath, found, err := findEKSTTLPath(clusterName, strings.TrimSpace(region))
	if err != nil {
		return nil, err
	}

	if !found {
		return LoadClusterTTL(clusterName)
	}

	return readTTL(ttlPath)
}

// findEKSTTLPath returns the region-scoped TTL file that applies to clusterName in region, and
// whether one exists. With region empty it looks across every region.
func findEKSTTLPath(clusterName, region string) (string, bool, error) {
	if region != "" {
		ttlPath, err := eksTTLPath(clusterName, region)
		if err != nil {
			return "", false, err
		}

		found, err := fileExists(ttlPath)

		return ttlPath, found, err
	}

	paths, err := eksTTLPaths(clusterName)
	if err != nil {
		return "", false, err
	}

	switch len(paths) {
	case 0:
		return "", false, nil
	case 1:
		return paths[0], true, nil
	default:
		return "", false, fmt.Errorf("%w: %s", ErrAmbiguousTTLRegion, clusterName)
	}
}

// eksTTLPaths lists every region-scoped TTL file recorded for clusterName.
func eksTTLPaths(clusterName string) ([]string, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return nil, err
	}

	exists, err := clusterStateDirExists(dir)
	if err != nil || !exists {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list cluster TTL state: %w", err)
	}

	var paths []string

	for _, entry := range entries {
		region, named := eksTTLFileRegion(entry)
		if !named {
			continue
		}

		// Only a file whose name round-trips through the validated path is a region's TTL.
		ttlPath, pathErr := eksTTLPath(clusterName, region)
		if pathErr == nil && filepath.Base(ttlPath) == entry.Name() {
			paths = append(paths, ttlPath)
		}
	}

	return paths, nil
}

// eksTTLFileRegion returns the region a directory entry is named after when its name has the
// shape of a region-scoped TTL file.
func eksTTLFileRegion(entry os.DirEntry) (string, bool) {
	if entry.IsDir() {
		return "", false
	}

	region, hasPrefix := strings.CutPrefix(entry.Name(), "ttl-")
	region, hasSuffix := strings.CutSuffix(region, ".json")

	return region, hasPrefix && hasSuffix
}

// eksRegionTTLPaths returns the TTL files a delete of the EKS cluster clusterName in region
// removes: that region's own TTL, plus the name-scoped TTL saved before TTLs were region-scoped
// when the region has no TTL of its own.
//
// The name-scoped TTL cannot say which region it belongs to. When the region has no TTL of its
// own, the name-scoped one may be this region's, and removing it keeps a stale TTL from attaching
// to a later same-named cluster. When the region has its own TTL, its cluster was created after
// TTLs became region-scoped, so the name-scoped TTL is another region's and is kept.
func eksRegionTTLPaths(clusterName, region string) ([]string, error) {
	regionPath, err := eksTTLPath(clusterName, region)
	if err != nil {
		return nil, err
	}

	hasOwn, err := fileExists(regionPath)
	if err != nil {
		return nil, err
	}

	if hasOwn {
		return []string{regionPath}, nil
	}

	namePath, err := clusterTTLPath(clusterName)
	if err != nil {
		return nil, err
	}

	return []string{regionPath, namePath}, nil
}

// clusterTTLPath returns the path to the TTL file for a given cluster name.
func clusterTTLPath(clusterName string) (string, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, ttlFileName), nil
}

// eksTTLPath returns the path to the region-scoped TTL file of an EKS cluster.
func eksTTLPath(clusterName, region string) (string, error) {
	return eksRegionScopedStatePath(clusterName, region, eksTTLFileNameFormat)
}

func writeTTL(ttlPath string, ttl time.Duration) error {
	dir := filepath.Dir(ttlPath)

	err := os.MkdirAll(dir, dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create state directory %s: %w", dir, err)
	}

	info := TTLInfo{
		ExpiresAt: time.Now().UTC().Add(ttl),
		Duration:  ttl.String(),
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ttl info: %w", err)
	}

	err = os.WriteFile(ttlPath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write ttl state: %w", err)
	}

	return nil
}

func readTTL(ttlPath string) (*TTLInfo, error) {
	//nolint:gosec // ttlPath is derived via clusterStateDir(clusterName), which validates clusterName
	// and constrains the path under the cluster state root
	data, err := os.ReadFile(ttlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTTLNotSet
		}

		return nil, fmt.Errorf("failed to read ttl state: %w", err)
	}

	var info TTLInfo

	err = json.Unmarshal(data, &info)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ttl info: %w", err)
	}

	return &info, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	return false, fmt.Errorf("inspect %s: %w", path, err)
}
