package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinnedTalos112 is a Talos version whose supported Kubernetes window
// (<= 1.35) is older than the built-in default, used to exercise capping.
const pinnedTalos112 = "v1.12.4"

func TestParseVersionContract(t *testing.T) {
	t.Parallel()

	defaultContract, err := talos.ParseVersionContract("")
	require.NoError(t, err)
	assert.False(t, defaultContract.MultidocKubernetesConfigSupported())

	multiDocumentContract, err := talos.ParseVersionContract("1.14.0-alpha.2")
	require.NoError(t, err)
	assert.True(t, multiDocumentContract.MultidocKubernetesConfigSupported())

	_, err = talos.ParseVersionContract("not-a-version")
	require.Error(t, err)
}

//nolint:gochecknoglobals // table-driven test cases shared by the resolver test.
var resolveKubernetesVersionCases = []struct {
	name        string
	pinnedTalos string
	pinnedK8s   string
	want        string
}{
	{
		name:      "explicit pin honoured",
		pinnedK8s: "1.31.4",
		want:      "1.31.4",
	},
	{
		name:        "explicit pin normalises v prefix",
		pinnedTalos: pinnedTalos112,
		pinnedK8s:   "v1.33.0",
		want:        "1.33.0",
	},
	{
		name:        "explicit pin wins even when incompatible with talos",
		pinnedTalos: pinnedTalos112,
		pinnedK8s:   "1.36.0",
		want:        "1.36.0",
	},
	{
		name: "no pins returns built-in default",
		want: talos.DefaultKubernetesVersion,
	},
	{
		// Talos 1.12 supports Kubernetes <= 1.35, so the 1.36 default is capped.
		name:        "default capped for older pinned talos",
		pinnedTalos: pinnedTalos112,
		want:        "1.35.0",
	},
	{
		name:        "default capped accepts talos version without v prefix",
		pinnedTalos: "1.12.4",
		want:        "1.35.0",
	},
	{
		name:        "default kept when pinned talos supports it",
		pinnedTalos: "v1.13.2",
		want:        talos.DefaultKubernetesVersion,
	},
	{
		name:        "unparseable talos version falls back to default",
		pinnedTalos: "not-a-version",
		want:        talos.DefaultKubernetesVersion,
	},
}

func TestResolveKubernetesVersion(t *testing.T) {
	t.Parallel()

	for _, testCase := range resolveKubernetesVersionCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talos.ResolveKubernetesVersion(testCase.pinnedTalos, testCase.pinnedK8s)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestResolveKubernetesVersion_CappedIsTalosCompatible guards the invariant that
// the capped default is always one the pinned Talos release can actually run.
func TestResolveKubernetesVersion_CappedIsTalosCompatible(t *testing.T) {
	t.Parallel()

	// Default (1.36.x) is too new for Talos 1.12; the resolved version must be lower.
	resolved := talos.ResolveKubernetesVersion(pinnedTalos112, "")
	assert.NotEqual(t, talos.DefaultKubernetesVersion, resolved,
		"default should be capped for Talos 1.12")
	assert.Equal(t, "1.35.0", resolved)
}

func TestNewDefaultConfigsWithVersionAndPatches(t *testing.T) {
	t.Parallel()

	t.Run("targets the given version", func(t *testing.T) {
		t.Parallel()

		configs, err := talos.NewDefaultConfigsWithVersionAndPatches("1.35.0", nil)
		require.NoError(t, err)
		assert.Equal(t, "1.35.0", configs.KubernetesVersion())
	})

	t.Run("empty version falls back to the default", func(t *testing.T) {
		t.Parallel()

		configs, err := talos.NewDefaultConfigsWithVersionAndPatches("", nil)
		require.NoError(t, err)
		assert.Equal(t, talos.DefaultKubernetesVersion, configs.KubernetesVersion())
	})
}

func TestKubernetesVersionFromProvider(t *testing.T) {
	t.Parallel()

	t.Run("reads version from a generated config", func(t *testing.T) {
		t.Parallel()

		configs, err := talos.NewDefaultConfigsWithPatches(nil)
		require.NoError(t, err)

		configs, err = configs.WithKubernetesVersion("1.32.0")
		require.NoError(t, err)

		got := talos.KubernetesVersionFromProvider(configs.ControlPlane())
		assert.Equal(t, "1.32.0", got)
	})

	t.Run("returns empty for nil provider", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, talos.KubernetesVersionFromProvider(nil))
	})
}
