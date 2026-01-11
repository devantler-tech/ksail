package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyEngineSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantValue v1alpha1.PolicyEngine
		wantError bool
	}{
		{
			name:      "sets_kyverno",
			input:     "Kyverno",
			wantValue: v1alpha1.PolicyEngineKyverno,
			wantError: false,
		},
		{
			name:      "sets_gatekeeper",
			input:     "Gatekeeper",
			wantValue: v1alpha1.PolicyEngineGatekeeper,
			wantError: false,
		},
		{
			name:      "sets_none",
			input:     "None",
			wantValue: v1alpha1.PolicyEngineNone,
			wantError: false,
		},
		{
			name:      "case_insensitive_kyverno",
			input:     "kyverno",
			wantValue: v1alpha1.PolicyEngineKyverno,
			wantError: false,
		},
		{
			name:      "invalid_value",
			input:     "invalid",
			wantValue: "",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var policyEngine v1alpha1.PolicyEngine

			err := policyEngine.Set(testCase.input)

			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantValue, policyEngine)
			}
		})
	}
}

func TestDistribution_ProvidesMetricsServerByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         bool
		description  string
	}{
		{
			name:         "returns_true_for_k3d",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
			description:  "K3d should provide metrics-server by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
			description:  "Kind should not provide metrics-server by default",
		},
		{
			name:         "returns_false_for_unknown_distribution",
			distribution: v1alpha1.Distribution("unknown"),
			want:         false,
			description:  "Unknown distributions should not provide metrics-server by default",
		},
		{
			name:         "returns_false_for_empty_distribution",
			distribution: v1alpha1.Distribution(""),
			want:         false,
			description:  "Empty distribution should not provide metrics-server by default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesMetricsServerByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}
}

func TestDistribution_ProvidesStorageByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         bool
		description  string
	}{
		{
			name:         "returns_true_for_k3d",
			distribution: v1alpha1.DistributionK3s,
			want:         true,
			description:  "K3d should provide storage by default",
		},
		{
			name:         "returns_false_for_kind",
			distribution: v1alpha1.DistributionVanilla,
			want:         false,
			description:  "Kind should not provide storage by default",
		},
		{
			name:         "returns_false_for_unknown_distribution",
			distribution: v1alpha1.Distribution("unknown"),
			want:         false,
			description:  "Unknown distributions should not provide storage by default",
		},
		{
			name:         "returns_false_for_empty_distribution",
			distribution: v1alpha1.Distribution(""),
			want:         false,
			description:  "Empty distribution should not provide storage by default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := testCase.distribution.ProvidesStorageByDefault()

			assert.Equal(t, testCase.want, result, testCase.description)
		})
	}
}
