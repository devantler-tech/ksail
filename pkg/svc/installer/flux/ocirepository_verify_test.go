package fluxinstaller_test

import (
	"context"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shared literals, declared once to avoid goconst duplication across the table tests.
const (
	// blankWhitespace asserts verify config fields are trimmed before use.
	blankWhitespace = "   "
	providerCosign  = "cosign"
	providerKey     = "provider"
)

func TestWorkloadVerifySpecEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec v1alpha1.WorkloadVerifySpec
		want bool
	}{
		{
			name: "empty is disabled",
			spec: v1alpha1.WorkloadVerifySpec{},
			want: false,
		},
		{
			name: "whitespace provider is disabled",
			spec: v1alpha1.WorkloadVerifySpec{Provider: blankWhitespace},
			want: false,
		},
		{
			name: "matchers without provider is disabled",
			spec: v1alpha1.WorkloadVerifySpec{
				MatchOIDCIdentity: []v1alpha1.WorkloadVerifyOIDCIdentity{
					{Issuer: "x", Subject: "y"},
				},
			},
			want: false,
		},
		{
			name: "cosign provider is enabled",
			spec: v1alpha1.WorkloadVerifySpec{Provider: providerCosign},
			want: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.spec.Enabled())
		})
	}
}

func TestBuildVerifyPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec v1alpha1.WorkloadVerifySpec
		want map[string]any
	}{
		{
			name: "provider only",
			spec: v1alpha1.WorkloadVerifySpec{Provider: providerCosign},
			want: map[string]any{providerKey: providerCosign},
		},
		{
			name: "provider is trimmed",
			spec: v1alpha1.WorkloadVerifySpec{Provider: "  notation  "},
			want: map[string]any{providerKey: "notation"},
		},
		{
			name: "key-based with secretRef",
			spec: v1alpha1.WorkloadVerifySpec{
				Provider:  providerCosign,
				SecretRef: v1alpha1.WorkloadVerifySecretRef{Name: "cosign-public-key"},
			},
			want: map[string]any{
				providerKey: providerCosign,
				"secretRef": map[string]any{"name": "cosign-public-key"},
			},
		},
		{
			name: "blank secretRef name is omitted",
			spec: v1alpha1.WorkloadVerifySpec{
				Provider:  providerCosign,
				SecretRef: v1alpha1.WorkloadVerifySecretRef{Name: blankWhitespace},
			},
			want: map[string]any{providerKey: providerCosign},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, fluxinstaller.BuildVerifyPatch(testCase.spec))
		})
	}
}

func TestBuildVerifyPatchCosignKeyless(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.WorkloadVerifySpec{
		Provider: providerCosign,
		MatchOIDCIdentity: []v1alpha1.WorkloadVerifyOIDCIdentity{
			{
				Issuer:  `^https://token\.actions\.githubusercontent\.com$`,
				Subject: `^https://github\.com/org/repo/\.github/workflows/cd\.yaml@refs/.*$`,
			},
			{Issuer: "issuer2", Subject: "subject2"},
		},
	}

	want := map[string]any{
		providerKey: providerCosign,
		"matchOIDCIdentity": []any{
			map[string]any{
				"issuer":  `^https://token\.actions\.githubusercontent\.com$`,
				"subject": `^https://github\.com/org/repo/\.github/workflows/cd\.yaml@refs/.*$`,
			},
			map[string]any{"issuer": "issuer2", "subject": "subject2"},
		},
	}

	assert.Equal(t, want, fluxinstaller.BuildVerifyPatch(spec))
}

func TestApplyVerifySetsVerifyOnEmptyObject(t *testing.T) {
	t.Parallel()

	desired := fluxinstaller.BuildVerifyPatch(v1alpha1.WorkloadVerifySpec{
		Provider:          providerCosign,
		MatchOIDCIdentity: []v1alpha1.WorkloadVerifyOIDCIdentity{{Issuer: "i", Subject: "s"}},
	})
	obj := map[string]any{"spec": map[string]any{"url": "oci://example.com/repo"}}

	done, err := fluxinstaller.ApplyVerify(obj, desired)
	require.NoError(t, err)
	assert.False(t, done, "expected an update because verify was not yet set")

	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, desired, spec["verify"])
	assert.Equal(t, "oci://example.com/repo", spec["url"], "existing spec fields must be preserved")
}

func TestApplyVerifyIsIdempotent(t *testing.T) {
	t.Parallel()

	desired := fluxinstaller.BuildVerifyPatch(v1alpha1.WorkloadVerifySpec{Provider: providerCosign})
	obj := map[string]any{}

	// First application sets the field and reports an update is needed.
	done, err := fluxinstaller.ApplyVerify(obj, desired)
	require.NoError(t, err)
	assert.False(t, done)

	// Second application against the now-equal object reports done with no change.
	done, err = fluxinstaller.ApplyVerify(obj, desired)
	require.NoError(t, err)
	assert.True(t, done, "expected no update when verify already matches")
}

func TestApplyVerifyUpdatesWhenDifferent(t *testing.T) {
	t.Parallel()

	obj := map[string]any{
		"spec": map[string]any{"verify": map[string]any{providerKey: "notation"}},
	}
	desired := fluxinstaller.BuildVerifyPatch(v1alpha1.WorkloadVerifySpec{Provider: providerCosign})

	done, err := fluxinstaller.ApplyVerify(obj, desired)
	require.NoError(t, err)
	assert.False(t, done, "expected an update because the existing verify differs")

	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, desired, spec["verify"])
}

func TestEnsureVerifyIfConfiguredNoopWhenDisabled(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{}

	// Verification is not configured, so the guard must return nil without
	// touching the (nil) patcher.
	err := fluxinstaller.EnsureVerifyIfConfiguredNoop(context.Background(), clusterCfg)
	require.NoError(t, err)
}
