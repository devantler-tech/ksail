package cipher

import (
	"errors"
	"fmt"
	"io"
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
	errNoStdinData          = errors.New("no data provided via stdin")
)

const (
	ageKeyFilePermissions = 0o600
	ageKeyDirPermissions  = 0o700
	ageKeyPrefix          = "AGE-SECRET-KEY-"
	minAgeKeyLength       = 60
)

// getAgeKeyPath returns the platform-specific path for the age keys file.
// It follows the SOPS convention:
//   - First checks SOPS_AGE_KEY_FILE environment variable
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func getAgeKeyPath() (string, error) {
	// Check SOPS_AGE_KEY_FILE first (highest priority)
	if sopsAgeKeyFile := os.Getenv("SOPS_AGE_KEY_FILE"); sopsAgeKeyFile != "" {
		return sopsAgeKeyFile, nil
	}

	// Check XDG_CONFIG_HOME (works on all platforms)
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
		return fmt.Errorf(
			"%w: input contains non-comment line that is not an age key",
			errInvalidAgeKey,
		)
	}

	if !foundKey {
		return fmt.Errorf(
			"%w: no age secret key found (must start with '%s')",
			errInvalidAgeKey,
			ageKeyPrefix,
		)
	}

	return nil
}

// extractPrivateKey extracts the private key line from input (may contain comments).
func extractPrivateKey(keyData string) (string, error) {
	keyData = strings.TrimSpace(keyData)
	lines := strings.Split(keyData, "\n") //nolint:modernize // Not using iter-based APIs yet

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ageKeyPrefix) {
			return line, nil
		}
	}

	return "", fmt.Errorf("%w: no age secret key found in input", errInvalidAgeKey)
}

// keyExistsInFile checks if a private key already exists in the file.
func keyExistsInFile(filePath, privateKey string) (bool, error) {
	// If file doesn't exist, key doesn't exist
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, nil
	}

	content, err := os.ReadFile(filePath) //#nosec G304 -- filePath comes from getAgeKeyPath
	if err != nil {
		return false, fmt.Errorf("failed to read existing key file: %w", err)
	}

	// Extract just the private key part for comparison
	privateKey = strings.TrimSpace(privateKey)

	return strings.Contains(string(content), privateKey), nil
}

// readKeyFromStdin reads key data from stdin.
func readKeyFromStdin() (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat stdin: %w", err)
	}

	// Check if stdin has data
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", errNoStdinData
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read from stdin: %w", err)
	}

	return string(data), nil
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

// writeKeyToFile writes the formatted key to the target path, either creating a new file or appending to existing one.
func writeKeyToFile(targetPath, formattedKey string) error {
	_, statErr := os.Stat(targetPath)
	if statErr != nil {
		return handleNewFile(targetPath, formattedKey, statErr)
	}

	return appendToExistingFile(targetPath, formattedKey)
}

// handleNewFile creates a new file with the formatted key or returns an error if stat failed for other reasons.
func handleNewFile(targetPath, formattedKey string, statErr error) error {
	if errors.Is(statErr, os.ErrNotExist) {
		// File does not exist yet; create it
		err := os.WriteFile(targetPath, []byte(formattedKey), ageKeyFilePermissions)
		if err != nil {
			return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
		}

		return nil
	}

	// Some other error accessing the file
	return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, statErr)
}

// appendToExistingFile appends the formatted key to an existing file.
func appendToExistingFile(targetPath, formattedKey string) error {
	//#nosec G304 -- targetPath comes from getAgeKeyPath
	file, openErr := os.OpenFile(
		targetPath,
		os.O_APPEND|os.O_WRONLY,
		ageKeyFilePermissions,
	)
	if openErr != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, openErr)
	}

	var err error

	defer func() {
		cerr := file.Close()
		if cerr != nil && err == nil {
			err = fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, cerr)
		}
	}()

	_, err = file.WriteString("\n" + formattedKey)
	if err != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
	}

	return nil
}

// importKey imports an age private key and automatically derives the public key.
// It is idempotent - if the key already exists in the file, it skips importing.
func importKey(keyData string) error {
	// Validate and trim the input
	keyData = strings.TrimSpace(keyData)

	err := validateAgeKey(keyData)
	if err != nil {
		return err
	}

	// Extract the private key line
	privateKey, err := extractPrivateKey(keyData)
	if err != nil {
		return err
	}

	// Get target path
	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	// Check if key already exists (idempotency)
	exists, err := keyExistsInFile(targetPath, privateKey)
	if err != nil {
		return err
	}

	if exists {
		// Key already exists, skip import (idempotent)
		return nil
	}

	// Derive the public key from the private key
	publicKey, err := derivePublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to derive public key: %w", err)
	}

	// Create directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)

	err = os.MkdirAll(targetDir, ageKeyDirPermissions)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errFailedToCreateDir, targetDir, err)
	}

	// Format key with metadata
	formattedKey := formatAgeKeyWithMetadata(privateKey, publicKey)

	// Write or append key to file
	return writeKeyToFile(targetPath, formattedKey)
}

// NewImportCmd creates and returns the import command.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [key-file]",
		Short: "Import an age key to the system's SOPS key location",
		Long: `Import an age private key to the system's default SOPS age key location.

The key can be provided in three ways:
  1. From a file: ksail cipher import my-key.txt
  2. From stdin: cat my-key.txt | ksail cipher import
  3. From stdin: echo "AGE-SECRET-KEY-..." | ksail cipher import

The public key will be automatically derived from the private key.
The command is idempotent - it will not import duplicate keys.

The command will automatically add metadata including:
  - Creation timestamp
  - Public key (derived from private key)

Key file location (checked in order):
  1. SOPS_AGE_KEY_FILE environment variable
  2. $XDG_CONFIG_HOME/sops/age/keys.txt (if XDG_CONFIG_HOME is set)
  3. Platform-specific defaults:
     Linux:   $HOME/.config/sops/age/keys.txt
     macOS:   $HOME/Library/Application Support/sops/age/keys.txt
     Windows: %AppData%\sops\age\keys.txt

The private key must be in age format (starting with "AGE-SECRET-KEY-").
Input is trimmed and tolerates extra newlines and comment lines.

Examples:
  # Import from a file
  ksail cipher import my-key.txt

  # Import from stdin
  cat my-key.txt | ksail cipher import
  
  # Import directly
  echo "AGE-SECRET-KEY-1ABCDEF..." | ksail cipher import`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE:         handleImportRunE,
	}

	return cmd
}

// handleImportRunE is the main handler for the import command.
func handleImportRunE(cmd *cobra.Command, args []string) error {
	var keyData string

	var err error

	// Determine input source
	if len(args) > 0 {
		// Read from file
		filePath := args[0]

		data, readErr := os.ReadFile(filePath) //#nosec G304 -- user-provided file path
		if readErr != nil {
			return fmt.Errorf("failed to read key file %s: %w", filePath, readErr)
		}

		keyData = string(data)
	} else {
		// Read from stdin
		keyData, err = readKeyFromStdin()
		if err != nil {
			return err
		}
	}

	// Import the key
	err = importKey(keyData)
	if err != nil {
		return fmt.Errorf("failed to import age key: %w", err)
	}

	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully imported age key to %s\n", targetPath)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}
