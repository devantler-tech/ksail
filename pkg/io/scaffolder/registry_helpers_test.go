package scaffolder_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail-go/pkg/io/scaffolder"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestScaffolderForKind() *scaffolder.Scaffolder {
	cluster := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.io/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Distribution: v1alpha1.DistributionKind,
		},
	}

	return scaffolder.NewScaffolder(*cluster, io.Discard)
}

func createTestScaffolderForK3d() *scaffolder.Scaffolder {
	cluster := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ksail.io/v1alpha1",
			Kind:       "Cluster",
		},
		Spec: v1alpha1.Spec{
			Distribution: v1alpha1.DistributionK3d,
		},
	}

	return scaffolder.NewScaffolder(*cluster, io.Discard)
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
					"http://docker.io:5000",
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
					"http://docker.io:5000",
					"http://ghcr.io:5000",
					"https://registry-1.docker.io",
					"https://ghcr.io",
				},
			},
		},
	}
}

func TestGenerateContainerdPatches(t *testing.T) {
	t.Parallel()

	for _, testCase := range containerdPatchCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scaf := createTestScaffolderForKind()
			scaf.MirrorRegistries = testCase.mirrors

			patches := scaf.GenerateContainerdPatches()
			assertContainerdPatches(t, patches, testCase)
		})
	}
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

func assertContainerdPatches(t *testing.T, patches []string, testCase containerdPatchCase) {
	t.Helper()

	if testCase.expectEmpty {
		require.Empty(t, patches)

		return
	}

	require.Len(t, patches, len(testCase.expected))

	for idx, expected := range testCase.expected {
		require.Contains(t, patches[idx], expected.host)

		if expected.fallback != "" {
			require.Contains(t, patches[idx], expected.fallback)
		}
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
