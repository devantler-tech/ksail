package generator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil/generator"
	"github.com/stretchr/testify/require"
)

type buildOCIURLTestCase struct {
	name        string
	host        string
	port        int32
	projectName string
	expected    string
}

func getDefaultAndCustomValueTestCases() []buildOCIURLTestCase {
	return []buildOCIURLTestCase{
		{
			name:        "with all default values",
			host:        "",
			port:        0,
			projectName: "",
			expected:    "oci://ksail-registry.localhost:5000/ksail",
		},
		{
			name:        "with custom host",
			host:        "custom-registry.localhost",
			port:        0,
			projectName: "",
			expected:    "oci://custom-registry.localhost:5000/ksail",
		},
		{
			name:        "with custom port",
			host:        "",
			port:        8080,
			projectName: "",
			expected:    "oci://ksail-registry.localhost:8080/ksail",
		},
		{
			name:        "with custom project name",
			host:        "",
			port:        0,
			projectName: "my-project",
			expected:    "oci://ksail-registry.localhost:5000/my-project",
		},
		{
			name:        "with all custom values",
			host:        "registry.example.com",
			port:        9000,
			projectName: "test-app",
			expected:    "oci://registry.example.com:9000/test-app",
		},
	}
}

func getIPAndEdgeCaseTestCases() []buildOCIURLTestCase {
	return []buildOCIURLTestCase{
		{
			name:        "with IPv4 host",
			host:        "192.168.1.100",
			port:        5000,
			projectName: "project",
			expected:    "oci://192.168.1.100:5000/project",
		},
		{
			name:        "with IPv6 host",
			host:        "::1",
			port:        5000,
			projectName: "project",
			expected:    "oci://[::1]:5000/project",
		},
		{
			name:        "with project name containing hyphens",
			host:        "registry.localhost",
			port:        5000,
			projectName: "my-awesome-project",
			expected:    "oci://registry.localhost:5000/my-awesome-project",
		},
		{
			name:        "with negative port for external HTTPS registry",
			host:        "ghcr.io",
			port:        -1,
			projectName: "org/repo",
			expected:    "oci://ghcr.io/org/repo",
		},
		{
			name:        "with negative port and default host",
			host:        "",
			port:        -1,
			projectName: "my-project",
			expected:    "oci://ksail-registry.localhost/my-project",
		},
	}
}

func getBuildOCIURLTestCases() []buildOCIURLTestCase {
	tests := getDefaultAndCustomValueTestCases()
	tests = append(tests, getIPAndEdgeCaseTestCases()...)

	return tests
}

func TestBuildOCIURL(t *testing.T) {
	t.Parallel()

	tests := getBuildOCIURLTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := generator.BuildOCIURL(testCase.host, testCase.port, testCase.projectName)
			require.Equal(t, testCase.expected, result)
		})
	}
}
