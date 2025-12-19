package cipher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var (
	errInvalidAgeKey     = errors.New("invalid age key format")
	errKeyFileNotFound   = errors.New("key file not found")
	errFailedToCreateDir = errors.New("failed to create directory")
	errFailedToWriteKey  = errors.New("failed to write key")
)

const ageKeyFilePermissions = 0o600

// getAgeKeyPath returns the platform-specific path for the age keys file.
// It follows the SOPS convention:
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func getAgeKeyPath() (string, error) {
	// Check XDG_CONFIG_HOME first (works on all platforms)
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "sops", "age", "keys.txt"), nil
	}

	// Platform-specific fallbacks
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("AppData")
		if appData == "" {
			return "", fmt.Errorf("AppData environment variable not set")
		}

		return filepath.Join(appData, "sops", "age", "keys.txt"), nil

	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		return filepath.Join(homeDir, "Library", "Application Support", "sops", "age", "keys.txt"), nil

	default: // Linux and other Unix-like systems
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		return filepath.Join(homeDir, ".config", "sops", "age", "keys.txt"), nil
	}
}

// validateAgeKey performs basic validation on an age key.
// Age keys should start with "AGE-SECRET-KEY-".
func validateAgeKey(key string) error {
	key = strings.TrimSpace(key)

	if key == "" {
		return fmt.Errorf("%w: key is empty", errInvalidAgeKey)
	}

	if !strings.HasPrefix(key, "AGE-SECRET-KEY-") {
		return fmt.Errorf("%w: key must start with 'AGE-SECRET-KEY-'", errInvalidAgeKey)
	}

	// Basic length check: AGE-SECRET-KEY- (15) + base64 chars (should be around 59-74 chars total)
	if len(key) < 60 {
		return fmt.Errorf("%w: key is too short", errInvalidAgeKey)
	}

	return nil
}

// importKey reads a key from the specified source and writes it to the age keys file.
func importKey(keySource string, readFromStdin bool, stdin io.Reader) error {
	var keyData []byte

	var err error

	// Read key from stdin or file
	if readFromStdin {
		keyData, err = io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("failed to read key from stdin: %w", err)
		}
	} else {
		keyData, err = os.ReadFile(keySource)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: %s", errKeyFileNotFound, keySource)
			}

			return fmt.Errorf("failed to read key file: %w", err)
		}
	}

	keyString := string(keyData)

	// Validate the key
	if err := validateAgeKey(keyString); err != nil {
		return err
	}

	// Get target path
	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("failed to determine age key path: %w", err)
	}

	// Create directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)

	err = os.MkdirAll(targetDir, 0o700)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errFailedToCreateDir, targetDir, err)
	}

	// Ensure the key ends with a newline
	if !strings.HasSuffix(keyString, "\n") {
		keyString += "\n"
	}

	// Write key to file
	err = os.WriteFile(targetPath, []byte(keyString), ageKeyFilePermissions)
	if err != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
	}

	return nil
}

// NewImportCmd creates and returns the import command.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [key-file]",
		Short: "Import an age key to the system's SOPS key location",
		Long: `Import an age key to the system's default SOPS age key location.

The key can be provided via a file path or through stdin (pipe or redirect).

Platform-specific key locations:
  Linux:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/.config/sops/age/keys.txt
  macOS:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/Library/Application Support/sops/age/keys.txt
  Windows: %AppData%\sops\age\keys.txt

The key must be in age format (starting with "AGE-SECRET-KEY-").

Examples:
  # Import from a file
  ksail cipher import my-age-key.txt

  # Import from stdin
  cat my-age-key.txt | ksail cipher import

  # Import from stdin (redirect)
  ksail cipher import < my-age-key.txt`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE:         handleImportRunE,
	}

	return cmd
}

// handleImportRunE is the main handler for the import command.
func handleImportRunE(cmd *cobra.Command, args []string) error {
	readFromStdin := len(args) == 0

	var keySource string
	if !readFromStdin {
		keySource = args[0]
	}

	err := importKey(keySource, readFromStdin, cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("failed to import age key: %w", err)
	}

	targetPath, _ := getAgeKeyPath()

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully imported age key to %s\n", targetPath)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}
