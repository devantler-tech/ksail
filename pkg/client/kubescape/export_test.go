package kubescape

import "github.com/kubescape/kubescape/v3/core/cautils"

// BuildScanInfo exports buildScanInfo for testing the option-to-ScanInfo mapping.
func BuildScanInfo(path string, opts *ScanOptions) *cautils.ScanInfo {
	return buildScanInfo(path, opts)
}

// CheckComplianceThreshold exports checkComplianceThreshold for testing the gate.
func CheckComplianceThreshold(score, threshold float32) error {
	return checkComplianceThreshold(score, threshold)
}
