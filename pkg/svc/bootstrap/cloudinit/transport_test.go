package cloudinitbootstrap_test

import (
	"testing"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransportUserDataMatchesBuilder(t *testing.T) {
	t.Parallel()

	commands := []string{"echo hi", "echo bye"}

	got, err := cloudinitbootstrap.New().UserData(commands)
	require.NoError(t, err)

	want, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{Commands: commands})
	require.NoError(t, err)

	// New() applies the default paths, so the transport and the bare builder agree.
	assert.Equal(t, want, got)
}

func TestTransportUserDataHonoursPaths(t *testing.T) {
	t.Parallel()

	transport := &cloudinitbootstrap.Transport{
		ScriptPath: "/opt/ksail/boot.sh",
		LogPath:    "/var/log/custom.log",
	}

	got, err := transport.UserData([]string{"echo hi"})
	require.NoError(t, err)

	want, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands:   []string{"echo hi"},
		ScriptPath: "/opt/ksail/boot.sh",
		LogPath:    "/var/log/custom.log",
	})
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestTransportUserDataPropagatesError(t *testing.T) {
	t.Parallel()

	out, err := cloudinitbootstrap.New().UserData(nil)
	require.ErrorIs(t, err, cloudinitbootstrap.ErrNoCommands)
	assert.Empty(t, out)
}

func TestTransportSatisfiesUserDataProvider(t *testing.T) {
	t.Parallel()

	var provider cloudinitbootstrap.UserDataProvider = cloudinitbootstrap.New()

	out, err := provider.UserData([]string{"echo hi"})
	require.NoError(t, err)
	assert.Contains(t, out, "#cloud-config")
}
