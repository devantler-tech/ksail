package kubescape

import "github.com/kubescape/kubescape/v3/core/cautils"

// BuildScanInfo exports buildScanInfo for testing the option-to-ScanInfo mapping.
func BuildScanInfo(path string, opts *ScanOptions) *cautils.ScanInfo {
	return buildScanInfo(path, opts)
}
