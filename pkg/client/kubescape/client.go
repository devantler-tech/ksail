package kubescape

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubescape/kubescape/v3/core/cautils"
	"github.com/kubescape/kubescape/v3/core/core"
	apisv1 "github.com/kubescape/opa-utils/httpserver/apis/v1"
)

// ErrScanFailed indicates that the security scan encountered an error.
var ErrScanFailed = errors.New("security scan failed")

// ScanOptions configures security scan behavior.
type ScanOptions struct {
	// Frameworks is the list of security frameworks to scan against (e.g. "nsa", "mitre", "cis").
	Frameworks []string
	// Format is the output format (e.g. "pretty-printer", "json", "sarif", "junit").
	Format string
	// Output is the file path to write results to. Empty means stdout.
	Output string
	// ComplianceThreshold fails the scan if the compliance score is below this value (0-100).
	ComplianceThreshold float32
	// Verbose shows all resources in the output, not just failed ones.
	Verbose bool
	// Exceptions is the path to a Kubescape exceptions file used to suppress
	// justified findings (e.g. runtime-enforced controls). Empty means no local
	// exceptions file (Kubescape's default behavior). Forwarded to Kubescape's
	// --exceptions / ScanInfo.UseExceptions.
	Exceptions string
}

// Client provides Kubescape security scanning functionality.
type Client struct{}

// NewClient creates a new Kubescape client.
func NewClient() *Client {
	return &Client{}
}

// buildScanInfo constructs a ScanInfo from the given path and options.
func buildScanInfo(path string, opts *ScanOptions) *cautils.ScanInfo {
	scanInfo := &cautils.ScanInfo{
		InputPatterns: []string{path},
		Local:         true,
		VerboseMode:   opts.Verbose,
		ScanType:      cautils.ScanTypeRepo,
	}

	if opts.Format != "" {
		scanInfo.Format = opts.Format
	}

	if opts.Output != "" {
		scanInfo.Output = opts.Output
	}

	if opts.ComplianceThreshold > 0 {
		scanInfo.ComplianceThreshold = opts.ComplianceThreshold
	}

	if opts.Exceptions != "" {
		scanInfo.UseExceptions = opts.Exceptions
	}

	if len(opts.Frameworks) > 0 {
		scanInfo.SetPolicyIdentifiers(opts.Frameworks, apisv1.KindFramework)
		scanInfo.FrameworkScan = true
	}

	return scanInfo
}

// ScanDirectory scans Kubernetes manifests in the given directory path
// against the configured security frameworks.
func (c *Client) ScanDirectory(ctx context.Context, path string, opts *ScanOptions) error {
	if opts == nil {
		opts = &ScanOptions{}
	}

	if ctx.Err() != nil {
		return fmt.Errorf("%w", ctx.Err())
	}

	kubescapeRunner := core.NewKubescape(ctx)
	scanInfo := buildScanInfo(path, opts)

	results, err := kubescapeRunner.Scan(scanInfo)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrScanFailed, err)
	}

	err = results.HandleResults(ctx, scanInfo)
	if err != nil {
		return fmt.Errorf("handle scan results: %w", err)
	}

	if opts.ComplianceThreshold > 0 &&
		results.GetComplianceScore() < float32(opts.ComplianceThreshold) {
		return fmt.Errorf(
			"%w: compliance score %.2f%% is below threshold %.2f%%",
			ErrScanFailed, results.GetComplianceScore(), opts.ComplianceThreshold,
		)
	}

	return nil
}
