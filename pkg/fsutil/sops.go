package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// errAppDataNotSet is returned when %AppData% is empty on Windows.
var errAppDataNotSet = errors.New("AppData environment variable not set")

// SOPSAgeKeyPath returns the platform-specific path for the SOPS age keys file.
// It follows the SOPS convention:
//   - First checks SOPS_AGE_KEY_FILE environment variable
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func SOPSAgeKeyPath() (string, error) {
	if sopsAgeKeyFile := os.Getenv("SOPS_AGE_KEY_FILE"); sopsAgeKeyFile != "" {
		return sopsAgeKeyFile, nil
	}

	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "sops", "age", "keys.txt"), nil
	}

	if runtime.GOOS == "windows" {
		appData := os.Getenv("AppData")
		if appData == "" {
			return "", errAppDataNotSet
		}

		return filepath.Join(appData, "sops", "age", "keys.txt"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	if runtime.GOOS == "darwin" {
		return filepath.Join(
			homeDir, "Library", "Application Support", "sops", "age", "keys.txt",
		), nil
	}

	return filepath.Join(homeDir, ".config", "sops", "age", "keys.txt"), nil
}
