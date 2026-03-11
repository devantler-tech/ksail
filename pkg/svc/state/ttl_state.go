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

const ttlFileName = "ttl.json"

// ErrNonPositiveTTL is returned when a non-positive TTL duration is provided.
var ErrNonPositiveTTL = errors.New("TTL duration must be positive")

// ErrTTLNotSet is returned when no TTL has been set for a cluster.
var ErrTTLNotSet = errors.New("cluster TTL not set")

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

	dir := filepath.Dir(ttlPath)

	err = os.MkdirAll(dir, dirPermissions)
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

// LoadClusterTTL loads TTL information for a cluster.
// Returns ErrTTLNotSet if no TTL has been set for the cluster.
func LoadClusterTTL(clusterName string) (*TTLInfo, error) {
	ttlPath, err := clusterTTLPath(clusterName)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // path is constructed from user home + constant subpath, not user input
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

// clusterTTLPath returns the path to the TTL file for a given cluster name.
func clusterTTLPath(clusterName string) (string, error) {
	if strings.Contains(clusterName, "/") ||
		strings.Contains(clusterName, "\\") ||
		strings.Contains(clusterName, "..") {
		return "", ErrInvalidClusterName
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(home, stateDir, clustersSubDir, clusterName, ttlFileName), nil
}
