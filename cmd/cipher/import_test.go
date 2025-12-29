package cipher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/cmd/cipher"
	rtruntime "github.com/devantler-tech/ksail/v5/pkg/di"
)

const (
	validAgeKey = "AGE-SECRET-KEY-12CDPTUPWF92L47FH8TK6P7N2S53J0KZTQUJTUZQCCA4NW87C8JHQSP4L99"

	validPublicKey = "age1nr74d6yw2mp7tcvj5zgv7e9wj8gnae408kczun3ktggsdqdp3ymszg6s7w"
	windowsOS      = "windows"
	darwinOS       = "darwin"
	keyFileName    = "keys.txt"
	sopsSubdir     = "sops"
	ageSubdir      = "age"

	filePermissions = 0o600
	expectedPerm    = os.FileMode(filePermissions)

	xdgConfigHomeEnv = "XDG_CONFIG_HOME"
	homeEnv          = "HOME"
	appDataEnv       = "AppData"
)

// getExpectedKeyPath returns the expected path for age keys based on the OS and tmpDir.
func getExpectedKeyPath(tmpDir string) string {
	switch runtime.GOOS {
	case windowsOS:
		return filepath.Join(tmpDir, sopsSubdir, ageSubdir, keyFileName)
	case darwinOS:
		return filepath.Join(
			tmpDir,
			"Library",
			"Application Support",
			sopsSubdir,
			ageSubdir,
			keyFileName,
		)
	default:
		return filepath.Join(tmpDir, ".config", sopsSubdir, ageSubdir, keyFileName)
	}
}

// setupTestEnvironment sets up test environment variables.
func setupTestEnvironment(t *testing.T, tmpDir string) {
	t.Helper()
	t.Setenv(homeEnv, tmpDir)

	_ = os.Unsetenv(xdgConfigHomeEnv)

	if runtime.GOOS == windowsOS {
		t.Setenv(appDataEnv, tmpDir)
	}
}

// verifyKeyFileContent verifies the content of the key file.
func verifyKeyFileContent(t *testing.T, expectedPath string) {
	t.Helper()

	content, err := os.ReadFile(expectedPath) //#nosec G304 -- test file path
	if err != nil {
		t.Errorf("expected key file to exist at %s, got error: %v", expectedPath, err)
	}

	contentStr := string(content)

	// Verify the key contains metadata
	if !strings.Contains(contentStr, "# created:") {
		t.Errorf("expected key file to contain creation timestamp")
	}

	// Verify the public key is automatically derived and included
	if !strings.Contains(contentStr, "# public key:") {
		t.Errorf("expected key file to contain public key comment")
	}

	// Verify the derived public key matches the expected value
	if !strings.Contains(contentStr, validPublicKey) {
		t.Errorf("expected key file to contain the correct public key %s", validPublicKey)
	}

	// Verify the key is present
	if !strings.Contains(contentStr, validAgeKey) {
		t.Errorf("expected key file to contain the age key")
	}
}

// verifyFilePermissions verifies the file permissions.
func verifyFilePermissions(t *testing.T, expectedPath string) {
	t.Helper()

	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("failed to stat key file: %v", err)
	}

	// Check permissions (on Unix-like systems)
	if runtime.GOOS != windowsOS {
		perm := info.Mode().Perm()

		if perm != expectedPerm {
			t.Errorf("expected file permissions %o, got %o", expectedPerm, perm)
		}
	}
}

func TestNewImportCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewImportCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "import PRIVATE_KEY" {
		t.Errorf("expected Use to be 'import PRIVATE_KEY', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}
}

func TestImportCommandHelp(t *testing.T) {
	t.Parallel()

	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Error("expected help output to not be empty")
	}

	// Verify help output mentions key features
	if !strings.Contains(output, "import") {
		t.Error("expected help output to mention import")
	}

	if !strings.Contains(output, "age") {
		t.Error("expected help output to mention age")
	}

	// Verify help output mentions automatic public key derivation
	if !strings.Contains(output, "derived") {
		t.Error("expected help output to mention public key derivation")
	}
}

func TestImportKeyBasic(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Setup environment
	setupTestEnvironment(t, tmpDir)

	// Execute import command with just the private key
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer

	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "imported") {
		t.Errorf("expected success message, got: %s", output)
	}

	// Verify the key was written to the correct location
	expectedPath := getExpectedKeyPath(tmpDir)

	// Verify content and permissions
	verifyKeyFileContent(t, expectedPath)
	verifyFilePermissions(t, expectedPath)
}

func TestImportKeyWithXDGConfigHome(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	xdgConfigDir := filepath.Join(tmpDir, "xdg-config")

	// Set XDG_CONFIG_HOME
	t.Setenv(xdgConfigHomeEnv, xdgConfigDir)

	// Execute import command
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer

	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import, got: %v", err)
	}

	// Verify the key was written to XDG_CONFIG_HOME location
	expectedPath := filepath.Join(xdgConfigDir, sopsSubdir, ageSubdir, keyFileName)

	content, err := os.ReadFile(expectedPath) //#nosec G304 -- test file path
	if err != nil {
		t.Errorf("expected key file to exist at %s, got error: %v", expectedPath, err)
	}

	if !strings.Contains(string(content), validAgeKey) {
		t.Errorf("expected key file to contain the age key")
	}
}

func TestImportKeyAppendsToExistingFile(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Setup environment
	setupTestEnvironment(t, tmpDir)

	// Determine the expected path
	expectedPath := getExpectedKeyPath(tmpDir)

	// Create directory and pre-populate with an existing key
	err := os.MkdirAll(filepath.Dir(expectedPath), 0o700)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	existingKey := `# created: 2024-01-01T00:00:00Z
# public key: age1existing123
AGE-SECRET-KEY-1EXISTINGKEYFORTEST123456789012345678901234567890ABC
`

	err = os.WriteFile(expectedPath, []byte(existingKey), 0o600)
	if err != nil {
		t.Fatalf("failed to write existing key: %v", err)
	}

	// Execute import command with a new key
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer

	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err = cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import, got: %v", err)
	}

	// Read the file content
	content, err := os.ReadFile(expectedPath) //#nosec G304 -- test file path
	if err != nil {
		t.Errorf("expected key file to exist at %s, got error: %v", expectedPath, err)
	}

	contentStr := string(content)

	// Verify both keys are present (old and new)
	existingKeyStr := "AGE-SECRET-KEY-1EXISTINGKEYFORTEST123456789012345678901234567890ABC"
	if !strings.Contains(contentStr, existingKeyStr) {
		t.Errorf("expected existing key to be preserved")
	}

	if !strings.Contains(contentStr, validAgeKey) {
		t.Errorf("expected new key to be appended")
	}

	// Verify both metadata sections are present
	countCreated := strings.Count(contentStr, "# created:")
	if countCreated != 2 {
		t.Errorf("expected 2 'created' metadata lines (one for each key), got %d", countCreated)
	}
}

func TestImportInvalidKey(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications
	testCases := []struct {
		name    string
		keyData string
		errMsg  string
	}{
		{
			name:    "empty key",
			keyData: "",
			errMsg:  "invalid age key format",
		},
		{
			name:    "wrong prefix",
			keyData: "WRONG-PREFIX-1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ234567890ABCDEF",
			errMsg:  "invalid age key format",
		},
		{
			name:    "too short",
			keyData: "AGE-SECRET-KEY-123",
			errMsg:  "invalid age key format",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Note: Not running in parallel due to environment variable modifications

			// Create a temporary directory for testing
			tmpDir := t.TempDir()

			// Set HOME to temp directory
			t.Setenv(homeEnv, tmpDir)

			// Clear XDG_CONFIG_HOME
			_ = os.Unsetenv(xdgConfigHomeEnv)

			// On Windows, set AppData
			if runtime.GOOS == windowsOS {
				t.Setenv(appDataEnv, tmpDir)
			}

			// Execute import command with invalid key
			rt := rtruntime.NewRuntime()
			cipherCmd := cipher.NewCipherCmd(rt)

			var out bytes.Buffer

			cipherCmd.SetOut(&out)
			cipherCmd.SetArgs([]string{"import", testCase.keyData})

			err := cipherCmd.Execute()
			if err == nil {
				t.Errorf("expected error for invalid key, got none")
			}

			if !strings.Contains(err.Error(), testCase.errMsg) {
				t.Errorf("expected error to contain %q, got: %v", testCase.errMsg, err)
			}
		})
	}
}

func TestImportRequiresPrivateKey(t *testing.T) {
	t.Parallel()

	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error when no private key provided, got none")
	}
}
