// Package buildmeta holds build-time version information injected via ldflags.
//
// These variables are set at build time using:
//
//	go build -ldflags="-X github.com/devantler-tech/ksail/v5/internal/buildmeta.Version=v1.0.0 ..."
//
//nolint:gochecknoglobals
package buildmeta

var (
	// Version is the semantic version of the build (e.g., "v1.0.0").
	Version = "dev"
	// Commit is the Git SHA of the build.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"
)
