package hcloudccminstaller_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildValuesYaml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		networkName    string
		wantEmpty      bool
		wantContain    []string
		wantNotContain []string
	}{
		{
			name:        "empty network name returns empty values",
			networkName: "",
			wantEmpty:   true,
		},
		{
			name:        "network name enables networking without inline value",
			networkName: "dev-network",
			wantContain: []string{
				"networking:",
				"enabled: true",
				"clusterCIDR: " + hcloudccminstaller.DefaultClusterCIDR,
			},
			wantNotContain: []string{
				"value:",
				"network:",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hcloudccminstaller.BuildValuesYamlForTest(testCase.networkName)

			if testCase.wantEmpty {
				assert.Empty(t, result)
			} else {
				for _, s := range testCase.wantContain {
					assert.Contains(t, result, s)
				}

				for _, s := range testCase.wantNotContain {
					assert.NotContains(t, result, s)
				}
			}
		})
	}
}

func TestBuildSecretData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		networkName string
		wantNil     bool
		wantKey     string
		wantValue   string
	}{
		{
			name:        "empty network name returns nil",
			networkName: "",
			wantNil:     true,
		},
		{
			name:        "network name stored in secret data",
			networkName: "dev-network",
			wantKey:     "network",
			wantValue:   "dev-network",
		},
		{
			name:        "custom network name stored in secret data",
			networkName: "my-custom-net",
			wantKey:     "network",
			wantValue:   "my-custom-net",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hcloudccminstaller.BuildSecretDataForTest(testCase.networkName)

			if testCase.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, testCase.wantValue, string(result[testCase.wantKey]))
			}
		})
	}
}

func TestResolveHetznerNetworkName(t *testing.T) {
	t.Parallel()

	for _, testCase := range resolveNetworkNameTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hcloudccminstaller.ResolveHetznerNetworkName(testCase.cfg)

			require.Equal(t, testCase.expected, result)
		})
	}
}

type resolveNetworkNameTestCase struct {
	name     string
	cfg      *v1alpha1.Cluster
	expected string
}

func resolveNetworkNameTestCases() []resolveNetworkNameTestCase {
	return []resolveNetworkNameTestCase{
		{
			name:     "explicit name takes precedence over context-derived name",
			cfg:      clusterCfg("admin@dev", "custom-network"),
			expected: "custom-network",
		},
		{
			name:     "falls back to explicit name when context cannot be derived",
			cfg:      clusterCfg("kind-local", "custom-network"),
			expected: "custom-network",
		},
		{
			name:     "derives from Talos context when no explicit name",
			cfg:      clusterCfg("admin@dev", ""),
			expected: "dev-network",
		},
		{
			name:     "derives from Talos context with hyphenated cluster name",
			cfg:      clusterCfg("admin@my-production-cluster", ""),
			expected: "my-production-cluster-network",
		},
		{
			name:     "empty context returns empty",
			cfg:      clusterCfg("", ""),
			expected: "",
		},
		{
			name:     "non-Talos context without explicit name returns empty",
			cfg:      clusterCfg("kind-local", ""),
			expected: "",
		},
		{
			name:     "trims whitespace from context",
			cfg:      clusterCfg("  admin@dev  ", ""),
			expected: "dev-network",
		},
		{
			name:     "admin@ with no cluster name returns empty",
			cfg:      clusterCfg("admin@", ""),
			expected: "",
		},
	}
}

func clusterCfg(context, networkName string) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{Context: context},
			},
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{NetworkName: networkName},
			},
		},
	}
}
