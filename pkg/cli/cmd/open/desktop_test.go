package open_test

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errLookPathMiss = errors.New("not found on PATH")

// capture records what the faked launch dependencies were asked to do.
type capture struct {
	startedPath string
	ranArgs     []string
	lookPathArg string
}

func (c *capture) start(cmd *exec.Cmd) error {
	c.startedPath = cmd.Args[0]

	return nil
}

func (c *capture) run(cmd *exec.Cmd) error {
	c.ranArgs = cmd.Args

	return nil
}

const fakeExecutable = "/opt/ksail/ksail"

func fixedExecutable() func() (string, error) {
	return func() (string, error) { return fakeExecutable, nil }
}

func TestNewDesktopCmd(t *testing.T) {
	t.Parallel()

	cmd := open.NewDesktopCmd()

	assert.Equal(t, "desktop", cmd.Name())
	assert.Equal(t, "true", cmd.Annotations[annotations.AnnotationExclude])
}

func TestLaunchUsesBinaryNextToExecutable(t *testing.T) {
	t.Parallel()

	rec := &capture{}
	wantPath := filepath.Join("/opt/ksail", "ksail-desktop")

	err := open.LaunchForTest(
		context.Background(),
		io.Discard,
		"linux",
		fixedExecutable(),
		func(string) (string, error) { return "", errLookPathMiss },
		func(path string) bool { return path == wantPath },
		rec.start,
		rec.run,
	)

	require.NoError(t, err)
	assert.Equal(t, wantPath, rec.startedPath)
	assert.Nil(t, rec.ranArgs)
}

func TestLaunchFallsBackToPath(t *testing.T) {
	t.Parallel()

	rec := &capture{}

	err := open.LaunchForTest(
		context.Background(),
		io.Discard,
		"linux",
		fixedExecutable(),
		func(name string) (string, error) {
			rec.lookPathArg = name

			return "/usr/local/bin/ksail-desktop", nil
		},
		func(string) bool { return false },
		rec.start,
		rec.run,
	)

	require.NoError(t, err)
	assert.Equal(t, "ksail-desktop", rec.lookPathArg)
	assert.Equal(t, "/usr/local/bin/ksail-desktop", rec.startedPath)
}

func TestLaunchLooksForExeSuffixOnWindows(t *testing.T) {
	t.Parallel()

	rec := &capture{}

	err := open.LaunchForTest(
		context.Background(),
		io.Discard,
		"windows",
		func() (string, error) { return "", errLookPathMiss },
		func(name string) (string, error) {
			rec.lookPathArg = name

			return `C:\tools\ksail-desktop.exe`, nil
		},
		func(string) bool { return false },
		rec.start,
		rec.run,
	)

	require.NoError(t, err)
	assert.Equal(t, "ksail-desktop.exe", rec.lookPathArg)
}

func TestLaunchUsesMacAppWhenNoBinary(t *testing.T) {
	t.Parallel()

	rec := &capture{}
	output := &strings.Builder{}

	err := open.LaunchForTest(
		context.Background(),
		output,
		"darwin",
		fixedExecutable(),
		func(string) (string, error) { return "", errLookPathMiss },
		func(string) bool { return false },
		rec.start,
		rec.run,
	)

	require.NoError(t, err)
	assert.Empty(t, rec.startedPath)
	assert.Equal(t, []string{"open", "-a", "KSail"}, rec.ranArgs)
	assert.Contains(t, output.String(), "Launched the KSail desktop app")
}

func TestLaunchReturnsNotFoundWhenMissing(t *testing.T) {
	t.Parallel()

	rec := &capture{}

	err := open.LaunchForTest(
		context.Background(),
		io.Discard,
		"linux",
		fixedExecutable(),
		func(string) (string, error) { return "", errLookPathMiss },
		func(string) bool { return false },
		rec.start,
		rec.run,
	)

	require.ErrorIs(t, err, open.ErrDesktopAppNotFound)
	assert.Empty(t, rec.startedPath)
	assert.Nil(t, rec.ranArgs)
}

func TestLaunchReturnsNotFoundWhenMacAppFails(t *testing.T) {
	t.Parallel()

	rec := &capture{}

	err := open.LaunchForTest(
		context.Background(),
		io.Discard,
		"darwin",
		fixedExecutable(),
		func(string) (string, error) { return "", errLookPathMiss },
		func(string) bool { return false },
		rec.start,
		func(*exec.Cmd) error { return errLookPathMiss },
	)

	require.ErrorIs(t, err, open.ErrDesktopAppNotFound)
}
