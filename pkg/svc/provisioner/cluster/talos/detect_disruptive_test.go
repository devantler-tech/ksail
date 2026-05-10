package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectVolumeEncryptionChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		running        talosconfig.SystemDiskEncryption
		desired        talosconfig.SystemDiskEncryption
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
		encryption     talosconfig.SystemDiskEncryption
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
