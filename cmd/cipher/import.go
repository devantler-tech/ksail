package cipher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	errInvalidAgeKey        = errors.New("invalid age key format")
	errFailedToCreateDir    = errors.New("failed to create directory")
	errFailedToWriteKey     = errors.New("failed to write key")
	errAppDataNotSet        = errors.New("AppData environment variable not set")
	errFailedToGetUserHome  = errors.New("failed to get user home directory")
	errFailedToDetermineAge = errors.New("failed to determine age key path")
)

const (
	ageKeyFilePermissions = 0o600
	ageKeyDirPermissions  = 0o700
	ageKeyPrefix          = "AGE-SECRET-KEY-"
	minAgeKeyLength       = 60
)

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
			return "", errAppDataNotSet
		}

		return filepath.Join(appData, "sops", "age", "keys.txt"), nil

	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %w", errFailedToGetUserHome, err)
		}

		return filepath.Join(
			homeDir,
			"Library",
			"Application Support",
			"sops",
			"age",
			"keys.txt",
		), nil

	default: // Linux and other Unix-like systems
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %w", errFailedToGetUserHome, err)
		}

		return filepath.Join(homeDir, ".config", "sops", "age", "keys.txt"), nil
	}
}

// validateAgeKey performs basic validation on an age key.
// Age keys should contain at least one line starting with "AGE-SECRET-KEY-".
// The file may contain comment lines (starting with #) which are ignored during validation.
func validateAgeKey(keyContent string) error {
	keyContent = strings.TrimSpace(keyContent)

	if keyContent == "" {
		return fmt.Errorf("%w: key is empty", errInvalidAgeKey)
	}

	// Parse lines and find the secret key
	lines := strings.Split(keyContent, "\n")
	foundKey := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check if this line is an age secret key
		if strings.HasPrefix(line, ageKeyPrefix) {
			// Basic length check: AGE-SECRET-KEY- (15) + base64 chars (should be around 59-74 chars total)
			if len(line) < minAgeKeyLength {
				return fmt.Errorf("%w: key is too short", errInvalidAgeKey)
			}

			foundKey = true

			break
		}

		// If we hit a non-comment, non-key line, it's invalid
		return fmt.Errorf("%w: file contains non-comment line that is not an age key", errInvalidAgeKey)
	}

	if !foundKey {
		return fmt.Errorf("%w: no age secret key found (must start with '%s')", errInvalidAgeKey, ageKeyPrefix)
	}

	return nil
}

// formatAgeKeyWithMetadata formats an age private key with metadata comments.
func formatAgeKeyWithMetadata(privateKey, publicKey string) string {
	var builder strings.Builder

	// Add creation timestamp
	builder.WriteString("# created: ")
	builder.WriteString(time.Now().UTC().Format(time.RFC3339))
	builder.WriteString("\n")

	// Add public key if provided
	if publicKey != "" {
		builder.WriteString("# public key: ")
		builder.WriteString(publicKey)
		builder.WriteString("\n")
	}

	// Add the private key
	builder.WriteString(privateKey)

	// Ensure trailing newline
	if !strings.HasSuffix(privateKey, "\n") {
		builder.WriteString("\n")
	}

	return builder.String()
}

// importKey imports an age private key with optional metadata to the keys file.
func importKey(privateKey, publicKey string) error {
	// Validate the private key
	err := validateAgeKey(privateKey)
	if err != nil {
		return err
	}

	// Get target path
	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	// Create directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)

	err = os.MkdirAll(targetDir, ageKeyDirPermissions)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errFailedToCreateDir, targetDir, err)
	}

	// Format key with metadata
	formattedKey := formatAgeKeyWithMetadata(privateKey, publicKey)

	// Write key to file
	err = os.WriteFile(targetPath, []byte(formattedKey), ageKeyFilePermissions)
	if err != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
	}

	return nil
}

// NewImportCmd creates and returns the import command.
func NewImportCmd() *cobra.Command {
	var publicKey string

	cmd := &cobra.Command{
		Use:   "import PRIVATE_KEY",
		Short: "Import an age key to the system's SOPS key location",
		Long: `Import an age private key to the system's default SOPS age key location.

The private key must be provided as a command argument (not a file path).
An optional public key can be provided via the --public-key flag.

The command will automatically add metadata including:
  - Creation timestamp
  - Public key (if provided)

Platform-specific key locations:
  Linux:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/.config/sops/age/keys.txt
  macOS:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/Library/Application Support/sops/age/keys.txt
  Windows: %AppData%\sops\age\keys.txt

The private key must be in age format (starting with "AGE-SECRET-KEY-").

Examples:
  # Import a private key
  ksail cipher import AGE-SECRET-KEY-1ABCDEF...

  # Import with public key metadata
  ksail cipher import AGE-SECRET-KEY-1ABCDEF... --public-key age1abc...`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleImportRunE(cmd, args[0], publicKey)
		},
	}

	cmd.Flags().StringVar(&publicKey, "public-key", "", "optional public key to include in metadata")

	return cmd
}

// handleImportRunE is the main handler for the import command.
func handleImportRunE(cmd *cobra.Command, privateKey, publicKey string) error {
	err := importKey(privateKey, publicKey)
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
