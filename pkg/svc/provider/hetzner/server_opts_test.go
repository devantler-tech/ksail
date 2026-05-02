package hetzner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildServerCreateOpts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		opts                 hetzner.CreateServerOpts
		wantStartAfterCreate bool
		wantImageName        string
		wantImageID          int64
	}{
		{
			name: "ISOBoot_ServerNotStartedAutomatically",
			opts: hetzner.CreateServerOpts{
				Name:       "test-node",
				ServerType: "cx22",
				ISOID:      12345,
				Location:   "fsn1",
			},
			wantStartAfterCreate: false,
			wantImageName:        "debian-13",
			wantImageID:          0,
		},
		{
			name: "SnapshotBoot_ServerStartedAutomatically",
			opts: hetzner.CreateServerOpts{
				Name:       "test-node",
				ServerType: "cx22",
				ImageID:    67890,
				Location:   "fsn1",
			},
			wantStartAfterCreate: true,
			wantImageName:        "",
			wantImageID:          67890,
		},
		{
			name: "NoImageNoISO_ServerStartedAutomatically",
			opts: hetzner.CreateServerOpts{
				Name:       "test-node",
				ServerType: "cx22",
				Location:   "fsn1",
			},
			wantStartAfterCreate: true,
			wantImageName:        "",
			wantImageID:          0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := hetzner.BuildServerCreateOptsForTest(testCase.opts)

			require.NotNil(t, result.StartAfterCreate, "StartAfterCreate must be set")
			assert.Equal(t, testCase.wantStartAfterCreate, *result.StartAfterCreate)

			if testCase.wantImageName != "" {
				require.NotNil(t, result.Image, "Image must be set")
				assert.Equal(t, testCase.wantImageName, result.Image.Name)
			}

			if testCase.wantImageID > 0 {
				require.NotNil(t, result.Image, "Image must be set")
				assert.Equal(t, testCase.wantImageID, result.Image.ID)
			}

			if testCase.wantImageName == "" && testCase.wantImageID == 0 {
				assert.Nil(t, result.Image, "Image should be nil when neither ISO nor snapshot is used")
			}
		})
	}
}

func TestBuildServerCreateOpts_OptionalFields(t *testing.T) {
	t.Parallel()

	t.Run("WithNetwork", func(t *testing.T) {
		t.Parallel()

		result := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			NetworkID:  42,
		})

		require.Len(t, result.Networks, 1)
		assert.Equal(t, int64(42), result.Networks[0].ID)
	})

	t.Run("WithPlacementGroup", func(t *testing.T) {
		t.Parallel()

		result := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:             "test-node",
			ServerType:       "cx22",
			Location:         "fsn1",
			PlacementGroupID: 99,
		})

		require.NotNil(t, result.PlacementGroup)
		assert.Equal(t, int64(99), result.PlacementGroup.ID)
	})

	t.Run("WithSSHKey", func(t *testing.T) {
		t.Parallel()

		result := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			SSHKeyID:   7,
		})

		require.Len(t, result.SSHKeys, 1)
		assert.Equal(t, int64(7), result.SSHKeys[0].ID)
	})

	t.Run("WithFirewalls", func(t *testing.T) {
		t.Parallel()

		result := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:        "test-node",
			ServerType:  "cx22",
			Location:    "fsn1",
			FirewallIDs: []int64{10, 20},
		})

		require.Len(t, result.Firewalls, 2)
		assert.Equal(t, int64(10), result.Firewalls[0].Firewall.ID)
		assert.Equal(t, int64(20), result.Firewalls[1].Firewall.ID)
	})

	t.Run("WithUserData", func(t *testing.T) {
		t.Parallel()

		result := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			UserData:   "#cloud-config",
		})

		assert.Equal(t, "#cloud-config", result.UserData)
	})
}
