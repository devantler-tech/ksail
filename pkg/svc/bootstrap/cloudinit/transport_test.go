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

func TestTransportZeroValueBehavesLikeNew(t *testing.T) {
	t.Parallel()

	// A zero-value Transport (direct struct init without New()) must be
	// equivalent to New(): both apply the default paths.
	commands := []string{"echo hi"}

	fromZero, err := (&cloudinitbootstrap.Transport{}).UserData(commands)
	require.NoError(t, err)

	fromNew, err := cloudinitbootstrap.New().UserData(commands)
	require.NoError(t, err)

	assert.Equal(t, fromNew, fromZero,
		"zero-value Transport and New() must produce identical output")
}

func TestTransportOnlyScriptPathSet(t *testing.T) {
	t.Parallel()

	// When only ScriptPath is overridden, LogPath must fall back to the default.
	transport := &cloudinitbootstrap.Transport{ScriptPath: "/opt/custom/boot.sh"}

	got, err := transport.UserData([]string{"echo hi"})
	require.NoError(t, err)

	want, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands:   []string{"echo hi"},
		ScriptPath: "/opt/custom/boot.sh",
	})
	require.NoError(t, err)

	assert.Equal(t, want, got)
	assert.Contains(t, got, cloudinitbootstrap.DefaultLogPath,
		"log path must fall back to the default when not overridden")
}

func TestTransportOnlyLogPathSet(t *testing.T) {
	t.Parallel()

	// When only LogPath is overridden, ScriptPath must fall back to the default.
	transport := &cloudinitbootstrap.Transport{LogPath: "/opt/custom/boot.log"}

	got, err := transport.UserData([]string{"echo hi"})
	require.NoError(t, err)

	want, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		LogPath:  "/opt/custom/boot.log",
	})
	require.NoError(t, err)

	assert.Equal(t, want, got)
	assert.Contains(t, got, cloudinitbootstrap.DefaultScriptPath,
		"script path must fall back to the default when not overridden")
}

func TestTransportPropagatesInvalidCommand(t *testing.T) {
	t.Parallel()

	// ErrInvalidCommand (command with embedded newline) must bubble up through
	// the transport layer unchanged.
	out, err := cloudinitbootstrap.New().UserData([]string{"echo a\necho b"})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrInvalidCommand)
	assert.Empty(t, out)
}

func TestTransportPropagatesPathNotAbsolute(t *testing.T) {
	t.Parallel()

	// ErrPathNotAbsolute must bubble up when the transport carries a relative path.
	transport := &cloudinitbootstrap.Transport{ScriptPath: "relative/boot.sh"}

	out, err := transport.UserData([]string{"echo hi"})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrPathNotAbsolute)
	assert.Empty(t, out)
}
