package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sysctlRmemMax = "net.core.rmem_max"

// wrapConfig wraps a v1alpha1.Config as a full Talos config provider (with Bytes()).
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

func TestDiffMachineConfig(t *testing.T) { //nolint:funlen // table-driven tests
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

			changes, err := talosprovisioner.DiffMachineConfigForTest(
				wrapConfig(running),
				wrapConfig(desired),
			)
			require.NoError(t, err)

			if !testCase.wantDrift {
				assert.Empty(t, changes, "identical configs must not report drift")

				return
			}

			require.Len(t, changes, 1)
			assert.Equal(t, talosprovisioner.MachineConfigFieldForTest, changes[0].Field)
			assert.Equal(t, clusterupdate.ChangeCategoryInPlace, changes[0].Category)
			assert.NotEqual(
				t,
				changes[0].OldValue,
				changes[0].NewValue,
				"fingerprints must differ when configs drift",
			)
		})
	}
}

func TestConfigFingerprint(t *testing.T) {
	t.Parallel()

	first := talosprovisioner.ConfigFingerprintForTest([]byte("alpha"))
	second := talosprovisioner.ConfigFingerprintForTest([]byte("beta"))

	assert.Len(t, first, 12, "fingerprint length is fixed")
	assert.NotEqual(t, first, second, "different inputs produce different fingerprints")
	assert.Equal(
		t,
		first,
		talosprovisioner.ConfigFingerprintForTest([]byte("alpha")),
		"fingerprint is deterministic",
	)
}
