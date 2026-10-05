package analysisrunner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveSmokePreservesRunnerConfiguration(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root, err := os.OpenRoot(home)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	require.NoError(t, os.Mkdir(filepath.Join(home, "bin"), 0o750))
	writeExecutable(
		t,
		filepath.Join(home, "bin", "Runner.Listener"),
		"printf 'listener-version\\n'",
	)

	for _, name := range []string{".runner", ".credentials"} {
		require.NoError(t, root.WriteFile(name, []byte("existing configuration\n"), 0o600))
	}

	command := exec.CommandContext(t.Context(), "/bin/sh", "smoke.sh", "--live-runner")

	command.Env = append(
		os.Environ(),
		"HOME="+home,
		"PATH="+liveSmokeTools(t)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(
		t,
		string(output),
		"PASS: non-root analysis toolchain and writable runner home",
	)

	for _, name := range []string{".runner", ".credentials"} {
		configuration, readErr := root.ReadFile(name)
		require.NoError(t, readErr)
		require.Equal(t, "existing configuration\n", string(configuration))
	}
}

func liveSmokeTools(t *testing.T) string {
	t.Helper()

	tools := t.TempDir()
	// Tool fixtures isolate control flow; the real image/compiler proof runs in Docker.
	fixtures := map[string]string{
		"id": "printf '1001\\n'",
		"awk": `case "$1" in
*CapEff*) printf '0000000000000000\n' ;;
*NoNewPrivs*) printf '1\n' ;;
*) test "$2" = '/proc/mounts' ;;
esac`,
		"touch":      "exit 1",
		"node":       "printf 'v22.23.3\\n'",
		"pkg-config": "exit 0",
		"cp":         "echo 'live smoke attempted to bootstrap the active runner' >&2; exit 99",
		"go": `case "$1" in
version) printf 'go version go1.26.8 linux/amd64\n' ;;
env) case "$2" in
  GOARCH) printf 'amd64\n' ;;
  CGO_ENABLED) printf '1\n' ;;
  GOFLAGS) printf '%s\n' '-tags=desktop' ;;
  *) exit 1 ;;
  esac ;;
build) test "$2" = '-o'; printf '#!/bin/sh\nexit 0\n' > "$3"; chmod +x "$3" ;;
*) exit 1 ;;
esac`,
	}
	for name, body := range fixtures {
		writeExecutable(t, filepath.Join(tools, name), body)
	}

	return tools
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	name := filepath.Base(path)
	require.NoError(t, root.WriteFile(name, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o600))
	require.NoError(t, root.Chmod(name, 0o500))
}
