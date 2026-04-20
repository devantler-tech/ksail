package sops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// GetAgeKeyPath returns the platform-specific path for the age keys file.
// It follows the SOPS convention:
//   - First checks SOPS_AGE_KEY_FILE environment variable
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func GetAgeKeyPath() (string, error) {
	p, err := fsutil.SOPSAgeKeyPath()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFailedToDetermineAge, err)
	}

	return p, nil
}

// ValidateAgeKey performs basic validation on an age private key string.
// The input must start with "AGE-SECRET-KEY-" and meet minimum length requirements.
func ValidateAgeKey(privateKey string) error {
	privateKey = strings.TrimSpace(privateKey)

	if privateKey == "" {
		return fmt.Errorf("%w: key is empty", ErrInvalidAgeKey)
	}

	if !strings.HasPrefix(privateKey, AgeKeyPrefix) {
		return fmt.Errorf("%w: key must start with %s", ErrInvalidAgeKey, AgeKeyPrefix)
	}

	if len(privateKey) < MinAgeKeyLength {
		return fmt.Errorf(
			"%w: key is too short (minimum %d characters)",
			ErrInvalidAgeKey,
			MinAgeKeyLength,
		)
	}

	return nil
}

// DerivePublicKey derives the public key from an age private key.
func DerivePublicKey(privateKey string) (string, error) {
	// Parse the private key to get the identity
	identity, err := age.ParseX25519Identity(strings.TrimSpace(privateKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Get the recipient (public key) from the identity
	recipient := identity.Recipient()

	return recipient.String(), nil
}

// FormatAgeKeyWithMetadata formats an age private key with metadata comments.
func FormatAgeKeyWithMetadata(privateKey, publicKey string) string {
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

// WriteKeyToFile writes the formatted key to the target path, either creating a new file or appending to existing one.
func WriteKeyToFile(targetPath, formattedKey string) error {
	_, statErr := os.Stat(targetPath)
	if statErr != nil {
		return HandleNewFile(targetPath, formattedKey, statErr)
	}

	return AppendToExistingFile(targetPath, formattedKey)
}

// HandleNewFile creates a new file with the formatted key or returns an error if stat failed for other reasons.
func HandleNewFile(targetPath, formattedKey string, statErr error) error {
	if errors.Is(statErr, os.ErrNotExist) {
		// File does not exist yet; create it
		err := os.WriteFile(targetPath, []byte(formattedKey), AgeKeyFilePermissions)
		if err != nil {
			return fmt.Errorf("%w to %s: %w", ErrFailedToWriteKey, targetPath, err)
		}

		return nil
	}

	// Some other error accessing the file
	return fmt.Errorf("%w to %s: %w", ErrFailedToWriteKey, targetPath, statErr)
}

// AppendToExistingFile appends the formatted key to an existing file.
func AppendToExistingFile(targetPath, formattedKey string) error {
	//#nosec G304 -- targetPath comes from GetAgeKeyPath
	file, openErr := os.OpenFile(
		targetPath,
		os.O_APPEND|os.O_WRONLY,
		AgeKeyFilePermissions,
	)
	if openErr != nil {
		return fmt.Errorf("%w to %s: %w", ErrFailedToWriteKey, targetPath, openErr)
	}

	var err error

	defer func() {
		cerr := file.Close()
		if cerr != nil && err == nil {
			err = fmt.Errorf("%w to %s: %w", ErrFailedToWriteKey, targetPath, cerr)
		}
	}()

	_, err = file.WriteString("\n" + formattedKey)
	if err != nil {
		return fmt.Errorf("%w to %s: %w", ErrFailedToWriteKey, targetPath, err)
	}

	return nil
}

// ImportKey imports an age private key and automatically derives the public key.
func ImportKey(privateKey string) error {
	// Validate the private key
	err := ValidateAgeKey(privateKey)
	if err != nil {
		return err
	}

	// Derive the public key from the private key
	publicKey, err := DerivePublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to derive public key: %w", err)
	}

	// Get target path
	targetPath, err := GetAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToDetermineAge, err)
	}

	// Create directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)

	err = os.MkdirAll(targetDir, AgeKeyDirPermissions)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrFailedToCreateDir, targetDir, err)
	}

	// Format key with metadata
	formattedKey := FormatAgeKeyWithMetadata(privateKey, publicKey)

	// Write or append key to file
	return WriteKeyToFile(targetPath, formattedKey)
}
