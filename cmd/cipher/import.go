package cipher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"filippo.io/age"
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

// validateAgeKey performs basic validation on an age private key string.
// The input is expected to be the raw key text and must contain at least one line
// starting with "AGE-SECRET-KEY-". For robustness, empty lines and lines starting
// with "#" are skipped, but any other non-empty line that is not an age key causes validation to fail.
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
		return fmt.Errorf("%w: input contains non-comment line that is not an age key", errInvalidAgeKey)
	}

	if !foundKey {
		return fmt.Errorf("%w: no age secret key found (must start with '%s')", errInvalidAgeKey, ageKeyPrefix)
	}

	return nil
}

// derivePublicKey derives the public key from an age private key.
func derivePublicKey(privateKey string) (string, error) {
	// Parse the private key to get the identity
	identity, err := age.ParseX25519Identity(strings.TrimSpace(privateKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Get the recipient (public key) from the identity
	recipient := identity.Recipient()

	return recipient.String(), nil
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

// importKey imports an age private key and automatically derives the public key.
func importKey(privateKey string) error {
	// Validate the private key
	err := validateAgeKey(privateKey)
	if err != nil {
		return err
	}

	// Derive the public key from the private key
	publicKey, err := derivePublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to derive public key: %w", err)
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

	// Check if the file exists
	_, statErr := os.Stat(targetPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			// File does not exist yet; create it
			err = os.WriteFile(targetPath, []byte(formattedKey), ageKeyFilePermissions)
			if err != nil {
				return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
			}
		} else {
			// Some other error accessing the file
			return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, statErr)
		}
	} else {
		// File exists; append the new key instead of overwriting
		f, openErr := os.OpenFile(targetPath, os.O_APPEND|os.O_WRONLY, ageKeyFilePermissions)
		if openErr != nil {
			return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, openErr)
		}
		defer f.Close()

		if _, err = f.WriteString("\n" + formattedKey); err != nil {
			return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
		}
	}

	return nil
}

// NewImportCmd creates and returns the import command.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import PRIVATE_KEY",
		Short: "Import an age key to the system's SOPS key location",
		Long: `Import an age private key to the system's default SOPS age key location.

The private key must be provided as a command argument (not a file path).
The public key will be automatically derived from the private key.

The command will automatically add metadata including:
  - Creation timestamp
  - Public key (derived from private key)

Platform-specific key locations:
  Linux:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/.config/sops/age/keys.txt
  macOS:   $XDG_CONFIG_HOME/sops/age/keys.txt
           or $HOME/Library/Application Support/sops/age/keys.txt
  Windows: %AppData%\sops\age\keys.txt

The private key must be in age format (starting with "AGE-SECRET-KEY-").

Examples:
  # Import a private key (public key will be derived automatically)
  ksail cipher import AGE-SECRET-KEY-1ABCDEF...`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleImportRunE(cmd, args[0])
		},
	}

	return cmd
}

// handleImportRunE is the main handler for the import command.
func handleImportRunE(cmd *cobra.Command, privateKey string) error {
	err := importKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to import age key: %w", err)
	}

	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %v", errFailedToDetermineAge, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully imported age key to %s\n", targetPath)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}
