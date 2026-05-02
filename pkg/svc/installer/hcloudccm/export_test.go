package hcloudccminstaller

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

// BuildSecretDataForTest exports buildSecretData for testing.
var BuildSecretDataForTest = buildSecretData //nolint:gochecknoglobals // Standard Go export_test.go pattern.
