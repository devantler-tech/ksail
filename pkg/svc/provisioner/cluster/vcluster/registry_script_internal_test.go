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
	binDir := filepath.Join(tempDir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0o750))
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
	containerd := `#!/bin/sh
test "$1 $2" = "config dump" || exit 1
if test -f "$KSAIL_TEST_CONFIG_DIR/config.toml"; then
  cat "$KSAIL_TEST_CONFIG_DIR/config.toml"
else
  cat "$KSAIL_TEST_FIXTURE"
fi
`
	containerdPath := filepath.Join(binDir, "containerd")
	require.NoError(t, os.WriteFile(containerdPath, []byte(containerd), 0o600))
	require.NoError(t, os.Chmod(containerdPath, 0o500))
	systemctlLog := filepath.Join(tempDir, "systemctl.log")
	systemctl := `#!/bin/sh
echo "$*" >> "$KSAIL_TEST_SYSTEMCTL_LOG"
`
	systemctlPath := filepath.Join(binDir, "systemctl")
	require.NoError(t, os.WriteFile(systemctlPath, []byte(systemctl), 0o600))
	require.NoError(t, os.Chmod(systemctlPath, 0o500))

	run := func() {
		command := exec.CommandContext(t.Context(), "sh", "-c", enableContainerdRegistryHosts, "ksail", ".")
		command.Dir = configDir
		command.Env = append(os.Environ(),
			"PATH="+binDir+":"+os.Getenv("PATH"),
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
