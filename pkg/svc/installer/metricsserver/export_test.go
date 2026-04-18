//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package metricsserverinstaller

import "github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"

// BuildValuesYaml exposes buildValuesYaml for testing.
var BuildValuesYaml = func(distribution v1alpha1.Distribution) string {
	return buildValuesYaml(distribution)
}
