package vclusterprovisioner

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnableContainerdRegistryHosts_ConfiguresCRIOnce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "containerd")
	require.NoError(t, os.Mkdir(configDir, 0o750))
	fixturePath := filepath.Join(tempDir, "original.toml")
	fixture := `version = 3
[plugins.'io.containerd.cri.v1.images'.registry]
  config_path = ''
[plugins.'io.containerd.cri.v1.runtime']
  max_concurrent_downloads = 3
[plugins.'io.containerd.transfer.v1.local']
  config_path = ''
`
	require.NoError(t, os.WriteFile(fixturePath, []byte(fixture), 0o600))
	systemctlLog := filepath.Join(tempDir, "systemctl.log")
	commandStubs := `containerd() {
  test "$1 $2" = "config dump" || return 1
  if test -f "$KSAIL_TEST_CONFIG_DIR/config.toml"; then
    cat "$KSAIL_TEST_CONFIG_DIR/config.toml"
  else
    cat "$KSAIL_TEST_FIXTURE"
  fi
}
systemctl() {
  printf '%s\n' "$*" >> "$KSAIL_TEST_SYSTEMCTL_LOG"
}
`

	run := func() {
		command := exec.CommandContext(t.Context(), "sh", "-c", commandStubs+enableContainerdRegistryHosts, "ksail", ".")
		command.Dir = configDir
		command.Env = append(os.Environ(),
			"KSAIL_TEST_CONFIG_DIR="+configDir,
			"KSAIL_TEST_FIXTURE="+fixturePath,
			"KSAIL_TEST_SYSTEMCTL_LOG="+systemctlLog,
		)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}

	run()
	run()
	configured, err := fs.ReadFile(os.DirFS(configDir), "config.toml")
	require.NoError(t, err)
	require.Contains(t, string(configured), "config_path = \"/etc/containerd/certs.d\"")
	require.Contains(t, string(configured), "max_concurrent_downloads = 3")
	require.Contains(t, string(configured), "[plugins.'io.containerd.transfer.v1.local']\n  config_path = ''")
	restarts, err := fs.ReadFile(os.DirFS(tempDir), "systemctl.log")
	require.NoError(t, err)
	require.Equal(t, "restart containerd\nis-active --quiet containerd", strings.TrimSpace(string(restarts)))
}
