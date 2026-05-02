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
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := hetzner.BuildServerCreateOptsForTest(testCase.opts)

			require.NoError(t, err)
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
		})
	}
}

func TestBuildServerCreateOpts_InvalidArgs(t *testing.T) {
	t.Parallel()

	t.Run("BothImageAndISO_ReturnsError", func(t *testing.T) {
		t.Parallel()

		_, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			ImageID:    67890,
			ISOID:      12345,
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, hetzner.ErrImageAndISOBothSet)
	})

	t.Run("NoImageNoISO_ReturnsError", func(t *testing.T) {
		t.Parallel()

		_, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, hetzner.ErrImageOrISORequired)
	})
}

func TestBuildServerCreateOpts_NetworkFields(t *testing.T) {
	t.Parallel()

	t.Run("WithNetwork", func(t *testing.T) {
		t.Parallel()

		result, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			ImageID:    1,
			NetworkID:  42,
		})

		require.NoError(t, err)
		require.Len(t, result.Networks, 1)
		assert.Equal(t, int64(42), result.Networks[0].ID)
	})

	t.Run("WithPlacementGroup", func(t *testing.T) {
		t.Parallel()

		result, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:             "test-node",
			ServerType:       "cx22",
			Location:         "fsn1",
			ImageID:          1,
			PlacementGroupID: 99,
		})

		require.NoError(t, err)
		require.NotNil(t, result.PlacementGroup)
		assert.Equal(t, int64(99), result.PlacementGroup.ID)
	})

	t.Run("WithSSHKey", func(t *testing.T) {
		t.Parallel()

		result, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			ImageID:    1,
			SSHKeyID:   7,
		})

		require.NoError(t, err)
		require.Len(t, result.SSHKeys, 1)
		assert.Equal(t, int64(7), result.SSHKeys[0].ID)
	})
}

func TestBuildServerCreateOpts_ServerConfig(t *testing.T) {
	t.Parallel()

	t.Run("WithFirewalls", func(t *testing.T) {
		t.Parallel()

		result, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:        "test-node",
			ServerType:  "cx22",
			Location:    "fsn1",
			ImageID:     1,
			FirewallIDs: []int64{10, 20},
		})

		require.NoError(t, err)
		require.Len(t, result.Firewalls, 2)
		assert.Equal(t, int64(10), result.Firewalls[0].Firewall.ID)
		assert.Equal(t, int64(20), result.Firewalls[1].Firewall.ID)
	})

	t.Run("WithUserData", func(t *testing.T) {
		t.Parallel()

		result, err := hetzner.BuildServerCreateOptsForTest(hetzner.CreateServerOpts{
			Name:       "test-node",
			ServerType: "cx22",
			Location:   "fsn1",
			ImageID:    1,
			UserData:   "#cloud-config",
		})

		require.NoError(t, err)
		assert.Equal(t, "#cloud-config", result.UserData)
	})
}
