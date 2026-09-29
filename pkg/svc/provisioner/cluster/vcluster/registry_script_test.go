package vclusterprovisioner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnableContainerdRegistryHosts_ConfiguresCRIOnce(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0o755))
	configDir := filepath.Join(tempDir, "containerd")
	require.NoError(t, os.Mkdir(configDir, 0o755))
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
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "containerd"), []byte(containerd), 0o755))
	systemctlLog := filepath.Join(tempDir, "systemctl.log")
	systemctl := `#!/bin/sh
echo "$*" >> "$KSAIL_TEST_SYSTEMCTL_LOG"
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctl), 0o755))

	run := func() {
		command := exec.Command("sh", "-c", enableContainerdRegistryHosts, "ksail", configDir)
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
	configured, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(configured), "config_path = \"/etc/containerd/certs.d\"")
	require.Contains(t, string(configured), "max_concurrent_downloads = 3")
	require.Contains(t, string(configured), "[plugins.'io.containerd.transfer.v1.local']\n  config_path = ''")
	restarts, err := os.ReadFile(systemctlLog)
	require.NoError(t, err)
	require.Equal(t, "restart containerd\nis-active --quiet containerd", strings.TrimSpace(string(restarts)))
}
