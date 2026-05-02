package hcloudccminstaller

// BuildValuesYamlForTest exports buildValuesYaml for testing.
var BuildValuesYamlForTest = func(networkName string) string { return buildValuesYaml(networkName, false) } //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// BuildSecretDataForTest exports buildSecretData for testing.
var BuildSecretDataForTest = buildSecretData //nolint:gochecknoglobals // Standard Go export_test.go pattern.
