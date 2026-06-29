package ksail_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/validator"
	ksailvalidator "github.com/devantler-tech/ksail/v7/pkg/fsutil/validator/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const installImageSkewField = "spec.cluster.talos.extensions"

// loadTalosConfigsForSkew builds a Talos Configs, optionally with extensions and a
// user-provided machine.install.image patch.
func loadTalosConfigsForSkew(
	t *testing.T,
	extensions []string,
	withInstallImagePatch bool,
) *talosconfigmanager.Configs {
	t.Helper()

	manager := talosconfigmanager.NewConfigManager("", "skew-test", "1.32.0", "10.5.0.0/24")
	if len(extensions) > 0 {
		manager = manager.WithExtensions(extensions)
	}

	if withInstallImagePatch {
		manager = manager.WithAdditionalPatches([]talosconfigmanager.Patch{{
			Path:  "talos/cluster/install-image.yaml",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(
				"machine:\n  install:\n    image: factory.talos.dev/installer/deadbeef:v1.13.4\n",
			),
		}})
	}

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

func installImageSkewWarning(result *validator.ValidationResult) (validator.ValidationError, bool) {
	for _, warning := range result.Warnings {
		if warning.Field == installImageSkewField &&
			strings.Contains(warning.Message, "machine.install.image") {
			return warning, true
		}
	}

	return validator.ValidationError{}, false
}

type skewCase struct {
	name         string
	distribution v1alpha1.Distribution
	extensions   []string
	schematicID  string
	withPatch    bool
	noTalos      bool
	wantWarning  bool
}

func runSkewCase(t *testing.T, testCase skewCase) {
	t.Helper()

	distribution := testCase.distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionTalos
	}

	config := createValidKSailConfig(distribution)
	config.Spec.Cluster.Talos.Extensions = testCase.extensions
	config.Spec.Cluster.Talos.SchematicID = testCase.schematicID

	validatorInstance := ksailvalidator.NewValidator()

	if !testCase.noTalos {
		talosConfig := loadTalosConfigsForSkew(t, testCase.extensions, testCase.withPatch)
		validatorInstance = ksailvalidator.NewValidatorForTalos(talosConfig)
	}

	warning, ok := installImageSkewWarning(validatorInstance.Validate(config))
	assert.Equal(t, testCase.wantWarning, ok)

	if testCase.wantWarning {
		assert.Contains(t, warning.Message, "factory.talos.dev/installer/deadbeef:v1.13.4")
		assert.Contains(t, warning.FixSuggestion, "spec.cluster.talos.extensions")
	}
}

func TestValidateTalosInstallImageSkew(t *testing.T) {
	t.Parallel()

	exts := []string{"siderolabs/iscsi-tools"}

	tests := []skewCase{
		{
			name:        "patch coexists with extensions",
			extensions:  exts,
			withPatch:   true,
			wantWarning: true,
		},
		{
			name:        "schematicId takes precedence",
			extensions:  exts,
			schematicID: "abc123",
			withPatch:   true,
		},
		{name: "no extensions configured", withPatch: true},
		{name: "no install-image patch", extensions: exts},
		{name: "no Talos config", extensions: exts, noTalos: true},
		{
			name:         "non-Talos distribution with a Talos-backed validator",
			distribution: v1alpha1.DistributionVanilla,
			extensions:   exts,
			withPatch:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runSkewCase(t, testCase)
		})
	}
}
