package scaffolder_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestScaffolderForK3d() *scaffolder.Scaffolder {
	cluster := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.io/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3d,
			},
		},
	}

	return scaffolder.NewScaffolder(*cluster, io.Discard, nil)
}

type containerdPatchExpectation struct {
	host     string
	fallback string
}

type containerdPatchCase struct {
	name        string
	mirrors     []string
	expected    []containerdPatchExpectation
	expectEmpty bool
}

func containerdPatchCases() []containerdPatchCase {
	return []containerdPatchCase{
		containerdSingleMirrorCase(),
		containerdMultipleMirrorCase(),
		containerdNoMirrorCase(),
		containerdInvalidMirrorCase(),
		containerdCustomPortCase(),
	}
}

func containerdSingleMirrorCase() containerdPatchCase {
	return containerdPatchCase{
		name:    "single mirror registry",
		mirrors: []string{"docker.io=https://registry-1.docker.io"},
		expected: []containerdPatchExpectation{
			{host: "docker.io", fallback: "http://docker.io:5000"},
		},
	}
}

func containerdMultipleMirrorCase() containerdPatchCase {
	return containerdPatchCase{
		name: "multiple mirror registries",
		mirrors: []string{
			"docker.io=https://registry-1.docker.io",
			"ghcr.io=https://ghcr.io",
			"quay.io=https://quay.io",
		},
		expected: []containerdPatchExpectation{
			{host: "docker.io", fallback: "http://docker.io:5000"},
			{host: "ghcr.io", fallback: "http://ghcr.io:5000"},
			{host: "quay.io", fallback: "http://quay.io:5000"},
		},
	}
}

func containerdNoMirrorCase() containerdPatchCase {
	return containerdPatchCase{
		name:        "no mirror registries",
		mirrors:     []string{},
		expectEmpty: true,
	}
}

func containerdInvalidMirrorCase() containerdPatchCase {
	return containerdPatchCase{
		name: "invalid mirror spec skipped",
		mirrors: []string{
			"docker.io=https://registry-1.docker.io",
			"invalid-spec-no-equals",
			"ghcr.io=https://ghcr.io",
		},
		expected: []containerdPatchExpectation{
			{host: "docker.io", fallback: "http://docker.io:5000"},
			{host: "ghcr.io", fallback: "http://ghcr.io:5000"},
		},
	}
}

func containerdCustomPortCase() containerdPatchCase {
	return containerdPatchCase{
		name:    "custom port in upstream URL",
		mirrors: []string{"localhost=http://localhost:5001"},
		expected: []containerdPatchExpectation{
			{host: "localhost", fallback: "http://localhost:5000"},
		},
	}
}

type k3dRegistryExpectation struct {
	use               []string
	contains          []string
	notContains       []string
	expectEmptyConfig bool
}

type k3dRegistryConfigCase struct {
	name     string
	mirrors  []string
	expected k3dRegistryExpectation
}

func k3dRegistryConfigCases() []k3dRegistryConfigCase {
	return []k3dRegistryConfigCase{
		{
			name:    "single mirror registry",
			mirrors: []string{"docker.io=https://registry-1.docker.io"},
			expected: k3dRegistryExpectation{
				contains: []string{
					"\"docker.io\":",
					"http://k3d-default-docker.io:5000",
					"https://registry-1.docker.io",
				},
			},
		},
		{
			name:    "no mirror registries",
			mirrors: []string{},
			expected: k3dRegistryExpectation{
				expectEmptyConfig: true,
			},
		},
		{
			name:    "invalid mirror spec",
			mirrors: []string{"invalid-no-equals"},
			expected: k3dRegistryExpectation{
				expectEmptyConfig: true,
			},
		},
		{
			name: "multiple mirror registries",
			mirrors: []string{
				"docker.io=https://registry-1.docker.io",
				"ghcr.io=https://ghcr.io",
			},
			expected: k3dRegistryExpectation{
				contains: []string{
					"\"docker.io\":",
					"\"ghcr.io\":",
					"http://k3d-default-docker.io:5000",
					"http://k3d-default-ghcr.io:5000",
					"https://registry-1.docker.io",
					"https://ghcr.io",
				},
			},
		},
	}
}

func TestGenerateScaffoldedHostsToml(t *testing.T) {
	t.Parallel()

	// Test that GenerateScaffoldedHostsToml produces correct hosts.toml content
	// for the scaffolded kind/mirrors directory pattern.
	// The scaffolded hosts.toml should point to the local registry container.
	for _, testCase := range containerdPatchCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.expectEmpty || len(testCase.mirrors) == 0 {
				return // Skip empty cases
			}

			// Parse mirror specs from the test case
			for _, mirrorSpec := range testCase.mirrors {
				parts := splitMirrorSpec(mirrorSpec)
				if len(parts) != 2 {
					continue // Skip invalid specs
				}

				spec := registry.MirrorSpec{
					Host:   parts[0],
					Remote: parts[1],
				}

				content := registry.GenerateScaffoldedHostsToml(spec)

				// Verify the content structure
				// Server should be the upstream URL (fallback)
				require.Contains(t, content, "server = \""+spec.Remote+"\"")
				// Host block should point to the local registry container
				localMirrorURL := "http://" + spec.Host + ":5000"
				require.Contains(t, content, "[host.\""+localMirrorURL+"\"]")
				require.Contains(t, content, "capabilities = [\"pull\", \"resolve\"]")
			}
		})
	}
}

// splitMirrorSpec is a test helper that splits a mirror spec string.
func splitMirrorSpec(spec string) []string {
	idx := 0

	for i, c := range spec {
		if c == '=' {
			idx = i

			break
		}
	}

	if idx == 0 || idx == len(spec)-1 {
		return nil
	}

	return []string{spec[:idx], spec[idx+1:]}
}

func TestGenerateK3dRegistryConfig(t *testing.T) {
	t.Parallel()

	for _, testCase := range k3dRegistryConfigCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scaf := createTestScaffolderForK3d()
			scaf.MirrorRegistries = testCase.mirrors

			registryConfig := scaf.GenerateK3dRegistryConfig()
			assertK3dRegistryConfig(t, registryConfig, testCase.expected)
		})
	}
}

func assertK3dRegistryConfig(
	t *testing.T,
	config k3dv1alpha5.SimpleConfigRegistries,
	expected k3dRegistryExpectation,
) {
	t.Helper()

	require.Nil(t, config.Create)

	if len(expected.use) == 0 {
		require.Empty(t, config.Use)
	} else {
		require.ElementsMatch(t, expected.use, config.Use)
	}

	if expected.expectEmptyConfig {
		require.Empty(t, config.Config)

		return
	}

	require.NotEmpty(t, config.Config)

	for _, contains := range expected.contains {
		require.Contains(t, config.Config, contains)
	}

	for _, notContains := range expected.notContains {
		require.NotContains(t, config.Config, notContains)
	}
}
