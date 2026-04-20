package argocdinstaller_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldEnableSOPS(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		sops v1alpha1.SOPS
		env  map[string]string // environment variables to set
		want bool
	}{
		{
			name: "explicitly disabled returns false",
			sops: v1alpha1.SOPS{Enabled: &falseVal},
			want: false,
		},
		{
			name: "explicitly enabled returns true",
			sops: v1alpha1.SOPS{Enabled: &trueVal, AgeKeyEnvVar: "NONEXISTENT_KEY_99999"},
			want: true,
		},
		{
			name: "auto-detect with key available returns true",
			sops: v1alpha1.SOPS{AgeKeyEnvVar: "TEST_ARGOCD_CMP_SOPS_KEY"},
			env: map[string]string{
				"TEST_ARGOCD_CMP_SOPS_KEY": "AGE-SECRET-KEY-1TESTKEY" +
					"000000000000000000000000000000000000000000000000",
			},
			want: true,
		},
		{
			name: "auto-detect with no key returns false",
			sops: v1alpha1.SOPS{AgeKeyEnvVar: "TEST_ARGOCD_CMP_SOPS_NONEXISTENT_99999"},
			env: map[string]string{
				"SOPS_AGE_KEY_FILE": "/tmp/nonexistent-ksail-argocd-cmp-test.txt",
			},
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			for k, v := range testCase.env {
				t.Setenv(k, v)
			}

			got := argocdinstaller.ShouldEnableSOPS(testCase.sops)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestBuildSOPSValuesYaml(t *testing.T) {
	t.Parallel()

	yaml := argocdinstaller.BuildSOPSValuesYaml()

	require.NotEmpty(t, yaml)

	// Verify CMP plugin configuration is present
	assert.Contains(t, yaml, "configs:")
	assert.Contains(t, yaml, "cmp:")
	assert.Contains(t, yaml, "create: true")
	assert.Contains(t, yaml, "kustomize-sops:")
	assert.Contains(t, yaml, "discover:")
	assert.Contains(t, yaml, "generate:")

	// Verify content-based SOPS detection (grep for sops: metadata)
	assert.Contains(t, yaml, "grep -l '^sops:'")

	// Verify init container for SOPS binary (uses official image)
	assert.Contains(t, yaml, "install-sops")
	assert.Contains(t, yaml, "ghcr.io/getsops/sops:")
	assert.Contains(t, yaml, "custom-tools")

	// Verify CMP sidecar container
	assert.Contains(t, yaml, "cmp-kustomize-sops")
	assert.Contains(t, yaml, "argocd-cmp-server")
	assert.Contains(t, yaml, "SOPS_AGE_KEY_FILE")
	assert.Contains(t, yaml, "/sops/age/sops.agekey")

	// Verify sops-age secret volume with optional flag
	assert.Contains(t, yaml, "sops-age")
	assert.Contains(t, yaml, "optional: true")

	// Verify Kustomize + plain YAML fallback
	assert.Contains(t, yaml, "kustomize build .")
	assert.Contains(t, yaml, "kustomization.yaml")

	// Verify ArgoCD image is used for sidecar
	assert.Contains(t, yaml, "quay.io/argoproj/argocd:")
}

func TestBuildSOPSValuesYaml_SOPSVersionPresent(t *testing.T) {
	t.Parallel()

	yaml := argocdinstaller.BuildSOPSValuesYaml()

	// Verify the SOPS version is included in the init container image tag
	assert.Regexp(t, `getsops/sops:v\d+\.\d+\.\d+-alpine`, yaml)
}
