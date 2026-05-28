package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sysctlRmemMax = "net.core.rmem_max"
	sysctlKptr    = "kernel.kptr_restrict"
	fieldSysctls  = "machine.sysctls"
	fieldSysfs    = "machine.sysfs"
)

// machineCfgWithTunables builds a minimal Talos config carrying only the
// in-place-applicable machine tunables (sysctls/sysfs) under test.
func machineCfgWithTunables(sysctls, sysfs map[string]string) *v1alpha1.Config {
	return &v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{
			MachineSysctls: sysctls,
			MachineSysfs:   sysfs,
		},
		ClusterConfig: &v1alpha1.ClusterConfig{},
	}
}

func TestDetectInPlaceMachineConfigChanges(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name           string
		running        talosprovisioner.MachineClusterConfigForTest
		desired        talosprovisioner.MachineClusterConfigForTest
		expectedFields []string
	}{
		{
			name: "no changes when sysctls and sysfs match",
			running: machineCfgWithTunables(
				map[string]string{sysctlRmemMax: "1"},
				nil,
			),
			desired: machineCfgWithTunables(
				map[string]string{sysctlRmemMax: "1"},
				nil,
			),
			expectedFields: nil,
		},
		{
			name:           "no changes when nil and empty sysctls are equivalent",
			running:        machineCfgWithTunables(nil, nil),
			desired:        machineCfgWithTunables(map[string]string{}, map[string]string{}),
			expectedFields: nil,
		},
		{
			name:    "detect sysctl added (the platform#1618 hardening case)",
			running: machineCfgWithTunables(map[string]string{sysctlRmemMax: "1"}, nil),
			desired: machineCfgWithTunables(map[string]string{
				sysctlRmemMax: "1",
				sysctlKptr:    "2",
			}, nil),
			expectedFields: []string{fieldSysctls},
		},
		{
			name: "detect sysctl value changed",
			running: machineCfgWithTunables(
				map[string]string{sysctlKptr: "1"},
				nil,
			),
			desired: machineCfgWithTunables(
				map[string]string{sysctlKptr: "2"},
				nil,
			),
			expectedFields: []string{fieldSysctls},
		},
		{
			name:           "detect sysctl removed",
			running:        machineCfgWithTunables(map[string]string{"a": "1", "b": "2"}, nil),
			desired:        machineCfgWithTunables(map[string]string{"a": "1"}, nil),
			expectedFields: []string{fieldSysctls},
		},
		{
			name:           "detect sysfs changed",
			running:        machineCfgWithTunables(nil, map[string]string{"x": "1"}),
			desired:        machineCfgWithTunables(nil, map[string]string{"x": "2"}),
			expectedFields: []string{fieldSysfs},
		},
		{
			name: "detect both sysctls and sysfs changed",
			running: machineCfgWithTunables(
				map[string]string{"a": "1"},
				map[string]string{"x": "1"},
			),
			desired: machineCfgWithTunables(
				map[string]string{"a": "2"},
				map[string]string{"x": "2"},
			),
			expectedFields: []string{fieldSysctls, fieldSysfs},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changes := talosprovisioner.DetectInPlaceMachineConfigChangesForTest(
				testCase.running,
				testCase.desired,
			)

			require.Len(t, changes, len(testCase.expectedFields))

			for i, field := range testCase.expectedFields {
				assert.Equal(t, field, changes[i].Field)
				assert.Equal(
					t,
					clusterupdate.ChangeCategoryInPlace,
					changes[i].Category,
					"machine tunable drift must be classified in-place (applied without reboot)",
				)
			}
		})
	}
}

func TestStringMapsEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		left     map[string]string
		right    map[string]string
		expected bool
	}{
		{name: "both nil", left: nil, right: nil, expected: true},
		{name: "nil and empty", left: nil, right: map[string]string{}, expected: true},
		{
			name:     "equal single entry",
			left:     map[string]string{"a": "1"},
			right:    map[string]string{"a": "1"},
			expected: true,
		},
		{
			name:     "different value",
			left:     map[string]string{"a": "1"},
			right:    map[string]string{"a": "2"},
			expected: false,
		},
		{
			name:     "different length",
			left:     map[string]string{"a": "1"},
			right:    map[string]string{"a": "1", "b": "2"},
			expected: false,
		},
		{
			name:     "different key same length",
			left:     map[string]string{"a": "1"},
			right:    map[string]string{"b": "1"},
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(
				t,
				testCase.expected,
				talosprovisioner.StringMapsEqualForTest(testCase.left, testCase.right),
			)
		})
	}
}
