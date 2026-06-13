// Package backup provides the cluster backup and restore engine.
//
// A backup is a compressed tarball (.tar.gz) containing Kubernetes resource
// manifests organized by resource type, plus a backup-metadata.json file that
// describes the archive (see [BackupMetadata]). The archive format is the
// cross-version contract: a tarball produced by one release must remain
// restorable by later releases.
//
// The engine backs up resource manifests (YAML) only — persistent volume
// contents are not included. Resources are exported in parallel via kubectl,
// sanitized for portability (server-assigned metadata, status blocks, and
// non-portable fields such as Service ClusterIPs are stripped), and written
// to the archive. On restore, resources are applied in dependency order
// (CRDs first, then namespaces, storage, workloads) with traversal-safe tar
// extraction and restore labels injected for traceability.
//
// [Backupper] and [Restorer] expose the engine as reusable services; their
// option structs mirror the CLI flags and progress is written to a caller
// supplied io.Writer so the engine is usable beyond the cobra command layer.
package backup

import "errors"

const (
	// dirPerm is the permission mode for directories created by the engine.
	dirPerm = 0o750
	// filePerm is the permission mode for files written by the engine.
	filePerm = 0o600
	// minCompressionLevel is the minimum gzip compression level.
	minCompressionLevel = -1
	// maxCompressionLevel is the maximum gzip compression level.
	maxCompressionLevel = 9
)

// DefaultCompressionLevel uses gzip.DefaultCompression so the constant stays
// co-located with the gzip import and avoids a magic number at call sites.
const DefaultCompressionLevel = -1

const (
	// PolicyNone skips resources that already exist in the cluster.
	PolicyNone = "none"
	// PolicyUpdate updates resources that already exist in the cluster.
	PolicyUpdate = "update"
)

// ErrInvalidCompressionLevel is returned when the compression level is
// outside the valid range.
var ErrInvalidCompressionLevel = errors.New(
	"compression level out of range",
)

// ErrInvalidResourcePolicy is returned when an unsupported
// existing-resource-policy value is provided.
var ErrInvalidResourcePolicy = errors.New(
	"invalid existing-resource-policy: must be 'none' or 'update'",
)

// ErrInvalidTarPath is returned when a tar entry contains a path
// traversal attempt.
var ErrInvalidTarPath = errors.New("invalid tar entry path")

// ErrSymlinkInArchive is returned when a tar archive contains
// symbolic or hard links, which are not supported.
var ErrSymlinkInArchive = errors.New(
	"symbolic and hard links are not supported in backup archives",
)

// ErrRestoreFailed is returned when one or more resources fail to restore.
var ErrRestoreFailed = errors.New("resource restore failed")
