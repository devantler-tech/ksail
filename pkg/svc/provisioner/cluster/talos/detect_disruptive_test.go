package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfigtypes "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestDetectVolumeEncryptionChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		running        talosconfigtypes.SystemDiskEncryption
		desired        talosconfigtypes.SystemDiskEncryption
		expectedCount  int
		expectedFields []string
	}{
		{
			name:          "no changes when both configs have no encryption",
			running:       &v1alpha1.SystemDiskEncryptionConfig{},
			desired:       &v1alpha1.SystemDiskEncryptionConfig{},
			expectedCount: 0,
		},
		{
			name:    "detect EPHEMERAL encryption added",
			running: &v1alpha1.SystemDiskEncryptionConfig{},
			desired: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			expectedCount:  1,
			expectedFields: []string{"machine.systemDiskEncryption.ephemeral"},
		},
		{
			name:    "detect STATE encryption added",
			running: &v1alpha1.SystemDiskEncryptionConfig{},
			desired: &v1alpha1.SystemDiskEncryptionConfig{
				StatePartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			expectedCount:  1,
			expectedFields: []string{"machine.systemDiskEncryption.state"},
		},
		{
			name:    "detect both partitions changed",
			running: &v1alpha1.SystemDiskEncryptionConfig{},
			desired: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
				StatePartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			expectedCount: 2,
			expectedFields: []string{
				"machine.systemDiskEncryption.ephemeral",
				"machine.systemDiskEncryption.state",
			},
		},
		{
			name: "no changes when encryption matches",
			running: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			desired: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			expectedCount: 0,
		},
		{
			name:          "handle nil running encryption",
			running:       nil,
			desired:       &v1alpha1.SystemDiskEncryptionConfig{},
			expectedCount: 0,
		},
		{
			name:          "handle nil desired encryption",
			running:       &v1alpha1.SystemDiskEncryptionConfig{},
			desired:       nil,
			expectedCount: 0,
		},
		{
			name:          "handle both nil encryptions",
			running:       nil,
			desired:       nil,
			expectedCount: 0,
		},
		{
			name: "detect encryption removed from EPHEMERAL",
			running: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			desired:        &v1alpha1.SystemDiskEncryptionConfig{},
			expectedCount:  1,
			expectedFields: []string{"machine.systemDiskEncryption.ephemeral"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changes := talosprovisioner.DetectVolumeEncryptionChangesForTest(tt.running, tt.desired)

			require.Len(t, changes, tt.expectedCount)

			for i, field := range tt.expectedFields {
				assert.Equal(t, field, changes[i].Field)
				assert.Equal(t, clusterupdate.ChangeCategoryWipeRequired, changes[i].Category)
			}
		})
	}
}

func TestEncryptionProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		encryption     talosconfigtypes.SystemDiskEncryption
		partitionLabel string
		expected       string
	}{
		{
			name:           "nil encryption returns none",
			encryption:     nil,
			partitionLabel: constants.EphemeralPartitionLabel,
			expected:       "none",
		},
		{
			name:           "empty encryption returns none for ephemeral",
			encryption:     &v1alpha1.SystemDiskEncryptionConfig{},
			partitionLabel: constants.EphemeralPartitionLabel,
			expected:       "none",
		},
		{
			name:           "empty encryption returns none for state",
			encryption:     &v1alpha1.SystemDiskEncryptionConfig{},
			partitionLabel: constants.StatePartitionLabel,
			expected:       "none",
		},
		{
			name: "luks2 ephemeral encryption",
			encryption: &v1alpha1.SystemDiskEncryptionConfig{
				EphemeralPartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			partitionLabel: constants.EphemeralPartitionLabel,
			expected:       "luks2",
		},
		{
			name: "luks2 state encryption",
			encryption: &v1alpha1.SystemDiskEncryptionConfig{
				StatePartition: &v1alpha1.EncryptionConfig{
					EncryptionProvider: "luks2",
				},
			},
			partitionLabel: constants.StatePartitionLabel,
			expected:       "luks2",
		},
		{
			name:           "unknown partition label returns none",
			encryption:     &v1alpha1.SystemDiskEncryptionConfig{},
			partitionLabel: "UNKNOWN",
			expected:       "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.EncryptionProviderNameForTest(tt.encryption, tt.partitionLabel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectCNIChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		running        talosprovisioner.MachineClusterConfigForTest
		desired        talosprovisioner.MachineClusterConfigForTest
		expectedCount  int
		expectedFields []string
	}{
		{
			name: "no changes when CNI matches",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expectedCount: 0,
		},
		{
			name: "detect CNI changed from flannel to none",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "none"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expectedCount:  1,
			expectedFields: []string{"cluster.network.cni.name"},
		},
		{
			name: "detect CNI changed from none to custom",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "none"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "custom"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expectedCount:  1,
			expectedFields: []string{"cluster.network.cni.name"},
		},
		{
			name: "no changes when both have nil CNI",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changes := talosprovisioner.DetectCNIChangesForTest(tt.running, tt.desired)

			require.Len(t, changes, tt.expectedCount)

			for i, field := range tt.expectedFields {
				assert.Equal(t, field, changes[i].Field)
				assert.Equal(t, clusterupdate.ChangeCategoryRebootRequired, changes[i].Category)
			}
		})
	}
}

func TestDetectDiskQuotaChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		running        talosprovisioner.MachineClusterConfigForTest
		desired        talosprovisioner.MachineClusterConfigForTest
		expectedCount  int
		expectedFields []string
	}{
		{
			name: "no changes when disk quota matches (both disabled)",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			expectedCount: 0,
		},
		{
			name: "detect disk quota enabled",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expectedCount:  1,
			expectedFields: []string{"machine.features.diskQuotaSupport"},
		},
		{
			name: "detect disk quota disabled",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			expectedCount:  1,
			expectedFields: []string{"machine.features.diskQuotaSupport"},
		},
		{
			name: "no changes when both enabled",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "handle nil features on running config",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expectedCount:  1,
			expectedFields: []string{"machine.features.diskQuotaSupport"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changes := talosprovisioner.DetectDiskQuotaChangesForTest(tt.running, tt.desired)

			require.Len(t, changes, tt.expectedCount)

			for i, field := range tt.expectedFields {
				assert.Equal(t, field, changes[i].Field)
				assert.Equal(t, clusterupdate.ChangeCategoryRebootRequired, changes[i].Category)
			}
		})
	}
}

func TestClassifyMachineConfigChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		running           talosprovisioner.MachineClusterConfigForTest
		desired           talosprovisioner.MachineClusterConfigForTest
		expectedWipe      int
		expectedReboot    int
		expectedTotal     int
	}{
		{
			name: "no changes when configs match",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			expectedWipe:   0,
			expectedReboot: 0,
			expectedTotal:  0,
		},
		{
			name: "combined encryption and CNI changes",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineSystemDiskEncryption: &v1alpha1.SystemDiskEncryptionConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "none"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineSystemDiskEncryption: &v1alpha1.SystemDiskEncryptionConfig{
						EphemeralPartition: &v1alpha1.EncryptionConfig{
							EncryptionProvider: "luks2",
						},
					},
				},
			},
			expectedWipe:   1,
			expectedReboot: 1,
			expectedTotal:  2,
		},
		{
			name: "combined encryption and disk quota changes",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineSystemDiskEncryption: &v1alpha1.SystemDiskEncryptionConfig{
						EphemeralPartition: &v1alpha1.EncryptionConfig{
							EncryptionProvider: "luks2",
						},
					},
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineSystemDiskEncryption: &v1alpha1.SystemDiskEncryptionConfig{},
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expectedWipe:   1,
			expectedReboot: 1,
			expectedTotal:  2,
		},
		{
			name: "only reboot-required changes",
			running: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			desired: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "none"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expectedWipe:   0,
			expectedReboot: 2,
			expectedTotal:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changes := talosprovisioner.ClassifyMachineConfigChangesForTest(tt.running, tt.desired)

			require.Len(t, changes, tt.expectedTotal)

			var wipeCount, rebootCount int
			for _, c := range changes {
				switch c.Category {
				case clusterupdate.ChangeCategoryWipeRequired:
					wipeCount++
				case clusterupdate.ChangeCategoryRebootRequired:
					rebootCount++
				}
			}

			assert.Equal(t, tt.expectedWipe, wipeCount, "wipe-required count")
			assert.Equal(t, tt.expectedReboot, rebootCount, "reboot-required count")
		})
	}
}

func TestCNIName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   talosprovisioner.MachineClusterConfigForTest
		expected string
	}{
		{
			name:     "nil config returns empty string",
			config:   nil,
			expected: "",
		},
		{
			name: "nil network returns flannel default",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expected: "flannel",
		},
		{
			name: "nil CNI returns flannel default",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expected: "flannel",
		},
		{
			name: "flannel CNI",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "flannel"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expected: "flannel",
		},
		{
			name: "none CNI",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{
					ClusterNetwork: &v1alpha1.ClusterNetworkConfig{
						CNI: &v1alpha1.CNIConfig{CNIName: "none"},
					},
				},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expected: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.CNINameForTest(tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDiskQuotaEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   talosprovisioner.MachineClusterConfigForTest
		expected bool
	}{
		{
			name:     "nil config returns false",
			config:   nil,
			expected: false,
		},
		{
			name: "nil features returns false",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{},
			},
			expected: false,
		},
		{
			name: "empty features returns false",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{},
				},
			},
			expected: false,
		},
		{
			name: "disk quota enabled",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(true),
					},
				},
			},
			expected: true,
		},
		{
			name: "disk quota explicitly disabled",
			config: &v1alpha1.Config{
				ClusterConfig: &v1alpha1.ClusterConfig{},
				MachineConfig: &v1alpha1.MachineConfig{
					MachineFeatures: &v1alpha1.FeaturesConfig{
						DiskQuotaSupport: boolPtr(false),
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.DiskQuotaEnabledForTest(tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}
