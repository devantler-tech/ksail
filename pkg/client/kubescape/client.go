package kubescape

import (
	"context"
	"errors"
	"fmt"
	"math"

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
	// Exceptions is the path to a Kubescape exceptions file (a JSON array of
	// PostureExceptionPolicy objects). Forwarded to Kubescape's --exceptions to
	// suppress justified findings. Empty means no exceptions. Loaded from the
	// local file with no cloud account; the scan stays fully offline.
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
		// UseExceptions is the field Kubescape's --exceptions flag sets; the
		// embedded runner loads exceptions from this local file (NewLoadPolicy)
		// and keeps the scan air-gapped.
		scanInfo.UseExceptions = opts.Exceptions
	}

	if len(opts.Frameworks) > 0 {
		scanInfo.SetPolicyIdentifiers(opts.Frameworks, apisv1.KindFramework)
		scanInfo.FrameworkScan = true
	}

	return scanInfo
}

// ScanResult is the outcome of a scan that produced results.
type ScanResult struct {
	// ComplianceScore is the measured compliance score (0-100) across the
	// scanned frameworks.
	ComplianceScore float32
}

// ScanDirectory scans Kubernetes manifests in the given directory path
// against the configured security frameworks. It is ScanDirectoryWithResult
// for callers that need only the verdict.
func (c *Client) ScanDirectory(ctx context.Context, path string, opts *ScanOptions) error {
	_, err := c.ScanDirectoryWithResult(ctx, path, opts)

	return err
}

// ScanDirectoryWithResult scans Kubernetes manifests in the given directory
// path against the configured security frameworks and returns the measured
// compliance score. A score below a positive ComplianceThreshold fails the
// scan with an error that states both values; the result is returned with
// that error so the caller can still report the score. Any other error
// returns no result.
func (c *Client) ScanDirectoryWithResult(
	ctx context.Context,
	path string,
	opts *ScanOptions,
) (*ScanResult, error) {
	if opts == nil {
		opts = &ScanOptions{}
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w", ctx.Err())
	}

	kubescapeRunner := core.NewKubescape(ctx)
	scanInfo := buildScanInfo(path, opts)

	results, err := kubescapeRunner.Scan(scanInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrScanFailed, err)
	}

	err = results.HandleResults(ctx, scanInfo)
	if err != nil {
		return nil, fmt.Errorf("handle scan results: %w", err)
	}

	result := &ScanResult{ComplianceScore: results.GetComplianceScore()}

	return result, checkComplianceThreshold(result.ComplianceScore, opts.ComplianceThreshold)
}

// checkComplianceThreshold fails when a positive threshold is set and the
// score is below it, or is not a number, so a scan that measured nothing never
// passes the gate. A zero threshold disables the gate.
func checkComplianceThreshold(score, threshold float32) error {
	if threshold > 0 && (math.IsNaN(float64(score)) || score < threshold) {
		return fmt.Errorf(
			"%w: compliance score %.2f%% is below threshold %.2f%%",
			ErrScanFailed, score, threshold,
		)
	}

	return nil
}
