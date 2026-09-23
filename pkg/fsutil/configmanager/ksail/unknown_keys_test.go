package configmanager_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// These tests pin issue #6980: a key the loader ignores must be surfaced,
// naming its path and the fix, instead of reading as a successful load.

// A misspelled nested key is reported by its path, with the key it most likely
// meant.
func TestFindUnknownKeysReportsMisspelledNestedKey(t *testing.T) {
	t.Parallel()

	content := []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: EKS
    provider: AWS
    eks:
      experimentalControlPlaneUpgradeTYPO: true
`)

	assert.Equal(t, []configmanager.UnknownKey{{
		Path:       "spec.cluster.eks.experimentalControlPlaneUpgradeTYPO",
		Suggestion: "experimentalControlPlaneUpgrade",
	}}, configmanager.FindUnknownKeys(content))
}

// Unknown keys are reported at the top level and inside spec.cluster; nothing is
// close to either key, so neither carries a suggestion.
func TestFindUnknownKeysReportsUnknownTopLevelAndClusterKeys(t *testing.T) {
	t.Parallel()

	content := []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
totallyBogusTopLevelField: 12345
spec:
  cluster:
    distribution: Vanilla
    totallyBogusTopLevelField: 12345
`)

	assert.Equal(t, []configmanager.UnknownKey{
		{Path: "spec.cluster.totallyBogusTopLevelField"},
		{Path: "totallyBogusTopLevelField"},
	}, configmanager.FindUnknownKeys(content))
}

// A key inside a list element is reported with the element's index in its path.
func TestFindUnknownKeysReportsKeysInsideListElements(t *testing.T) {
	t.Parallel()

	content := []byte(`spec:
  cluster:
    talos:
      extraPortMappings:
        - containerPort: 80
          hostPrt: 8080
`)

	assert.Equal(t, []configmanager.UnknownKey{{
		Path:       "spec.cluster.talos.extraPortMappings[0].hostPrt",
		Suggestion: "hostPort",
	}}, configmanager.FindUnknownKeys(content))
}

// A valid key at the wrong level is reported: distribution belongs under
// spec.cluster, and one level up the loader drops it.
func TestFindUnknownKeysReportsMisplacedKey(t *testing.T) {
	t.Parallel()

	content := []byte(`spec:
  distribution: Vanilla
`)

	unknown := configmanager.FindUnknownKeys(content)
	require.Len(t, unknown, 1)
	assert.Equal(t, "spec.distribution", unknown[0].Path)
}

// Keys matched case-insensitively, squashed type metadata, object metadata,
// free-form maps and values converted by the loader's decode hooks are all read,
// so none of them is unknown.
func TestFindUnknownKeysAcceptsEveryKeyTheLoaderReads(t *testing.T) {
	t.Parallel()

	content := []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: dev
  labels:
    team: platform
spec:
  editor: code --wait
  cluster:
    distribution: Talos
    provider: Docker
    certManager: true
    connection:
      timeout: 5m
    talos:
      extraPortMappings:
        - containerPort: 80
          hostPort: 8080
  workload:
    sourceDirectory: k8s
    kustomizationfile: clusters/dev
`)

	assert.Empty(t, configmanager.FindUnknownKeys(content))
}

// Every key KSail itself writes (the scaffold's serialised form) must be one the
// loader reads; otherwise the warning would fire on generated configs.
func TestFindUnknownKeysAcceptsAFullySerialisedDefaultConfig(t *testing.T) {
	t.Parallel()

	content, err := yaml.Marshal(v1alpha1.NewCluster())
	require.NoError(t, err)

	assert.Empty(t, configmanager.FindUnknownKeys(content), string(content))
}

// Content that is not a YAML mapping yields no keys; the load reports it.
func TestFindUnknownKeysIgnoresNonMappingContent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, configmanager.FindUnknownKeys([]byte("- not\n- a mapping\n")))
	assert.Empty(t, configmanager.FindUnknownKeys([]byte("key: [unterminated\n")))
}

// misspelledWorkloadKeyYAML appends a misspelled spec.workload key to
// ksailClusterBaseYAML.
const misspelledWorkloadKeyYAML = "  workload:\n" +
	"    kustomizationFil: clusters/dev\n"

// writeUnknownKeyConfig writes a loadable Vanilla ksail.yaml (plus kind.yaml)
// carrying the extra, unknown-key YAML into dir and returns the config path.
func writeUnknownKeyConfig(t *testing.T, dir, extra string) string {
	t.Helper()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "kind.yaml"), []byte(kindClusterConfigYAML), 0o600,
	))

	configPath := filepath.Join(dir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(ksailClusterBaseYAML+extra), 0o600))

	return configPath
}

// Loading warns about an unknown key with its field and fix. It is a warning,
// not an error: configs carrying stray keys keep loading.
func TestLoadWarnsAboutUnknownKeysWithFieldAndFix(t *testing.T) {
	t.Parallel()

	configPath := writeUnknownKeyConfig(t, t.TempDir(), misspelledWorkloadKeyYAML)

	var output bytes.Buffer

	manager := configmanager.NewConfigManager(&output, configPath)

	cfg, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Contains(t, output.String(), "⚠ unknown key in ksail.yaml is ignored\n"+
		"  field: spec.workload.kustomizationFil\n"+
		"  fix: did you mean 'kustomizationFile'? Rename the key, or remove it\n")
	assert.Contains(t, output.String(), "config loaded")
}

// When no valid key is close, the fix points at the configuration reference.
func TestLoadWarnsAboutUnknownTopLevelKeyWithReferenceFix(t *testing.T) {
	t.Parallel()

	configPath := writeUnknownKeyConfig(t, t.TempDir(), "totallyBogusTopLevelField: 12345\n")

	var output bytes.Buffer

	manager := configmanager.NewConfigManager(&output, configPath)

	cfg, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Contains(t, output.String(), "⚠ unknown key in ksail.yaml is ignored\n"+
		"  field: totallyBogusTopLevelField\n"+
		"  fix: remove the key, or check its spelling and nesting against "+
		"https://ksail.devantler.tech/configuration/declarative-configuration/\n")
}

// A silent load does not warn about unknown keys.
func TestLoadSilentDoesNotWarnAboutUnknownKeys(t *testing.T) {
	t.Parallel()

	configPath := writeUnknownKeyConfig(t, t.TempDir(), misspelledWorkloadKeyYAML)

	var output bytes.Buffer

	manager := configmanager.NewConfigManager(&output, configPath)

	_, err := manager.Load(configmanagerinterface.LoadOptions{Silent: true})
	require.NoError(t, err)
	assert.NotContains(t, output.String(), "unknown key")
}

// The loader reads the file through viper, which splits a dotted key into its
// nested path, so the key is applied and must not be reported.
func TestFindUnknownKeysAcceptsADottedKeyTheLoaderApplies(t *testing.T) {
	t.Parallel()

	content := []byte(ksailClusterBaseYAML + "spec.cluster.connection.context: dotted-ctx\n")

	assert.Empty(t, configmanager.FindUnknownKeys(content))
}

// A numeric key is valid YAML; the loader ignores it, so it is reported rather
// than aborting the detection.
func TestFindUnknownKeysReportsANonStringNestedKey(t *testing.T) {
	t.Parallel()

	content := []byte(ksailClusterBaseYAML + "  workload:\n    1: stray\n")

	assert.Equal(t, []configmanager.UnknownKey{{Path: "spec.workload.1"}},
		configmanager.FindUnknownKeys(content))
}

// Viper splits a dotted key into its nested path, so the decoder names it by
// that path; the warning still names the key as it is written in the file.
func TestFindUnknownKeysReportsAMisspelledDottedKeyAsWritten(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"at the root":      "spec.cluster.connection.Contex: dotted-ctx\n",
		"inside a mapping": "  cluster.connection.Contex: dotted-ctx\n",
	}

	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := []byte(ksailClusterBaseYAML + line)

			assert.Equal(t, []configmanager.UnknownKey{{
				Path:       "spec.cluster.connection.Contex",
				Suggestion: "context",
			}}, configmanager.FindUnknownKeys(content))
		})
	}
}

// Loading applies a dotted key's value and does not warn about it.
func TestLoadAppliesADottedKeyWithoutWarning(t *testing.T) {
	t.Parallel()

	configPath := writeUnknownKeyConfig(
		t,
		t.TempDir(),
		"spec.cluster.connection.context: dotted-ctx\n",
	)

	var output bytes.Buffer

	manager := configmanager.NewConfigManager(&output, configPath)

	cfg, err := manager.Load(configmanagerinterface.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "dotted-ctx", cfg.Spec.Cluster.Connection.Context)
	assert.NotContains(t, output.String(), "unknown key")
}
