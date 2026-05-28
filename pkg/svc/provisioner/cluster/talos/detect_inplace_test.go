package talosprovisioner_test

import (
	"testing"
	"time"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sysctlRmemMax = "net.core.rmem_max"

// wrapConfig wraps a v1alpha1.Config as a full Talos config provider (with EncodeBytes()).
func wrapConfig(cfg *v1alpha1.Config) talosconfig.Provider {
	return taloscontainer.NewV1Alpha1(cfg)
}

// baseConfig returns a minimal config used as the "running" baseline; tests
// clone it and mutate individual fields to assert that any drift is detected.
func baseConfig() *v1alpha1.Config {
	return &v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{
			MachineSysctls: map[string]string{sysctlRmemMax: "1"},
		},
		ClusterConfig: &v1alpha1.ClusterConfig{},
	}
}

func TestMachineConfigDiff(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*v1alpha1.Config)
		wantDrift bool
	}{
		{
			name:      "no drift when configs are identical",
			mutate:    func(*v1alpha1.Config) {},
			wantDrift: false,
		},
		{
			name: "drift on sysctl added (the platform#1618 hardening case)",
			mutate: func(c *v1alpha1.Config) {
				c.MachineConfig.MachineSysctls["kernel.kptr_restrict"] = "2"
			},
			wantDrift: true,
		},
		{
			name: "drift on sysctl removed",
			mutate: func(c *v1alpha1.Config) {
				c.MachineConfig.MachineSysctls = map[string]string{}
			},
			wantDrift: true,
		},
		{
			name: "drift on machine.files change (not a sysctl)",
			mutate: func(c *v1alpha1.Config) {
				c.MachineConfig.MachineFiles = []*v1alpha1.MachineFile{
					{FilePath: "/var/etc/foo", FileContent: "bar", FilePermissions: 0o644},
				}
			},
			wantDrift: true,
		},
		{
			name: "drift on cluster-level change (not machine-scoped)",
			mutate: func(c *v1alpha1.Config) {
				c.ClusterConfig.ClusterID = "changed-cluster-id"
			},
			wantDrift: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			running := baseConfig()
			desired := baseConfig()
			testCase.mutate(desired)

			diff, err := talosprovisioner.MachineConfigDiffForTest(
				wrapConfig(running),
				wrapConfig(desired),
			)
			require.NoError(t, err)

			if testCase.wantDrift {
				assert.NotEmpty(t, diff, "drift must produce a non-empty diff")
			} else {
				assert.Empty(t, diff, "identical configs must produce an empty diff")
			}
		})
	}
}

// TestMachineConfigDiff_NoFalsePositiveOnUnchangedConfig guards the
// generation+alignment path against phantom drift: it generates a real ksail
// Talos config, parses it back (as Talos does on apply+store+read), then runs
// the detection's alignment (extract secrets+endpoint, regenerate) and asserts
// the diff is empty. This catches regressions where alignment or rendering
// introduces drift on an unchanged config. (It cannot cover node-side
// serialization, which only the Talos system test exercises.)
func TestMachineConfigDiff_NoFalsePositiveOnUnchangedConfig(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	appliedBytes, err := configs.ControlPlane().Bytes()
	require.NoError(t, err)

	// "running" = node-like config: a parse round-trip of what ksail applied.
	running, err := configloader.NewFromBytes(appliedBytes)
	require.NoError(t, err)

	// Reproduce alignedDesiredControlPlaneConfig against the running config.
	bundle := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), running)
	aligned, err := configs.WithSecrets(bundle)
	require.NoError(t, err)

	if endpoint := running.Cluster().Endpoint().Hostname(); endpoint != "" {
		aligned, err = aligned.WithEndpoint(endpoint)
		require.NoError(t, err)
	}

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, aligned.ControlPlane())
	require.NoError(t, err)

	assert.Empty(t, diff, "an unchanged config must not report drift after alignment")
}

func TestConfigFingerprint(t *testing.T) {
	t.Parallel()

	running := wrapConfig(baseConfig())

	changed := baseConfig()
	changed.MachineConfig.MachineSysctls["kernel.kptr_restrict"] = "2"

	fpRunning := talosprovisioner.ConfigFingerprintForTest(running)
	fpChanged := talosprovisioner.ConfigFingerprintForTest(wrapConfig(changed))

	assert.Len(t, fpRunning, 12, "fingerprint length is fixed")
	assert.Equal(
		t,
		fpRunning,
		talosprovisioner.ConfigFingerprintForTest(wrapConfig(baseConfig())),
		"fingerprint is deterministic for equal configs",
	)
	assert.NotEqual(t, fpRunning, fpChanged, "different configs produce different fingerprints")
}
