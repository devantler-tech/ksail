package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// metadataFileName is the name of the metadata document inside a backup archive.
const metadataFileName = "backup-metadata.json"

// BackupMetadata contains metadata about a backup. It is the cross-version
// archive contract: changes to its JSON shape affect whether archives produced
// by one release remain restorable by later releases.
//
//nolint:revive // BackupMetadata reads clearly at call sites despite the package-name stutter.
type BackupMetadata struct {
	Version       string    `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
	ClusterName   string    `json:"clusterName"`
	Distribution  string    `json:"distribution"`
	Provider      string    `json:"provider"`
	KSailVersion  string    `json:"ksailVersion"`
	ResourceCount int       `json:"resourceCount"`
	ResourceTypes []string  `json:"resourceTypes"`
}

// writeMetadata serializes metadata as indented JSON to path.
func writeMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = os.WriteFile(path, data, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// readBackupMetadata loads and parses backup-metadata.json from tmpDir.
func readBackupMetadata(tmpDir string) (*BackupMetadata, error) {
	metadataPath := filepath.Join(tmpDir, metadataFileName)

	metadataData, err := os.ReadFile(metadataPath) //nolint:gosec // path is constructed internally
	if err != nil {
		return nil, fmt.Errorf("failed to read backup metadata: %w", err)
	}

	var metadata BackupMetadata

	err = json.Unmarshal(metadataData, &metadata)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to parse backup metadata: %w", err,
		)
	}

	return &metadata, nil
}
