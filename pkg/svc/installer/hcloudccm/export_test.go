package hcloudccminstaller

import "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"

// BuildValuesYamlForTest exports buildValuesYaml for testing (haEnabled=false).
//
//nolint:gochecknoglobals // Standard Go export_test.go pattern.
var BuildValuesYamlForTest = func(networkName string) string {
	return buildValuesYaml(networkName, false)
}

// BuildValuesYamlHAForTest exports buildValuesYaml for testing (haEnabled=true).
//
//nolint:gochecknoglobals // Standard Go export_test.go pattern.
var BuildValuesYamlHAForTest = func(networkName string) string {
	return buildValuesYaml(networkName, true)
}

// BuildSecretDataForTest exports BuildNetworkSecretData for testing.
var BuildSecretDataForTest = hetzner.BuildNetworkSecretData //nolint:gochecknoglobals // Standard Go export_test.go pattern.
