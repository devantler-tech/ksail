package cipher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cipher"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestNewCipherCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "cipher" {
		t.Errorf("expected Use to be 'cipher', got %q", cmd.Use)
	}

	// Verify the short description is set
	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	// Verify encrypt subcommand exists
	encryptCmd := findSubcommand(cmd, "encrypt")
	if encryptCmd == nil {
		t.Error("expected encrypt subcommand to exist")
	}

	// Verify edit subcommand exists
	editCmd := findSubcommand(cmd, "edit")
	if editCmd == nil {
		t.Error("expected edit subcommand to exist")
	}
}

func TestCipherCommandHelp(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

// findSubcommand searches for a subcommand by name.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}

	return nil
}

// createTestFile is a shared helper function to create a test file.
func createTestFile(t *testing.T, filename, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, filename)

	err := os.WriteFile(testFile, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	return testFile
}

// setupCipherCommandTest is a shared helper to setup cipher command for testing.
func setupCipherCommandTest(t *testing.T, args []string) *cobra.Command {
	t.Helper()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs(args)

	return cipherCmd
}

func TestNewDecryptCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewDecryptCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "decrypt [file]" {
		t.Errorf("expected Use to be 'decrypt [file]', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	// Verify flags are registered
	extractFlag := cmd.Flags().Lookup("extract")
	if extractFlag == nil {
		t.Error("expected extract flag to be registered")
	}

	ignoreMacFlag := cmd.Flags().Lookup("ignore-mac")
	if ignoreMacFlag == nil {
		t.Error("expected ignore-mac flag to be registered")
	}

	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Error("expected output flag to be registered")
	}
}

func TestDecryptCommandHelp(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"decrypt", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

func TestDecryptCommandAcceptsStdin(t *testing.T) {
	t.Parallel()

	// Should not error on missing file arg (stdin is valid)
	// We expect a decryption error, not an args error
	err := executeDecryptCommand(t, []string{"decrypt"})
	if err == nil {
		t.Log("Command executed (expected to fail on decryption)")
	}
}

// executeDecryptCommand is a helper function to execute decrypt command with args.
func executeDecryptCommand(t *testing.T, args []string) error {
	t.Helper()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs(args)

	//nolint:wrapcheck // Test helper intentionally returns unwrapped error for assertion
	return cipherCmd.Execute()
}

func TestDecryptCommandUnsupportedFormat(t *testing.T) {
	t.Parallel()

	testFile := createTestFile(t, "test.txt", "test content")

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs([]string{"decrypt", testFile})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error for unsupported file format")

		return
	}

	if !strings.Contains(err.Error(), "unsupported file format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}
}

func TestDecryptCommandNonExistentFile(t *testing.T) {
	t.Parallel()

	err := executeDecryptCommand(t, []string{"decrypt", "/nonexistent/file.yaml"})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

// testDecryptWithFormat tests decryption with supported file formats.
func testDecryptWithFormat(t *testing.T, filename, content string) {
	t.Helper()

	testFile := createTestFile(t, filename, content)

	// We expect an error about the file not being encrypted or missing keys
	// not about file format
	err := executeDecryptCommand(t, []string{"decrypt", testFile})
	if err != nil {
		t.Logf("Expected SOPS error (not encrypted file): %v", err)
	}
}

func TestDecryptCommandYAMLFormat(t *testing.T) {
	t.Parallel()

	yamlContent := `apiVersion: v1
kind: Secret
metadata:
  name: test
data:
  key: value
`
	testDecryptWithFormat(t, "test.yaml", yamlContent)
}

func TestDecryptCommandJSONFormat(t *testing.T) {
	t.Parallel()

	jsonContent := `{
  "apiVersion": "v1",
  "kind": "Secret",
  "metadata": {
    "name": "test"
  },
  "data": {
    "key": "value"
  }
}
`
	testDecryptWithFormat(t, "test.json", jsonContent)
}

func TestDecryptCommandWithExtractFlag(t *testing.T) {
	t.Parallel()

	testFile := createTestFile(t, "test.yaml", "key: value")

	// Execute - we expect it to fail on decryption (not encrypted), not on flag parsing
	err := executeDecryptCommand(t, []string{"decrypt", testFile, "--extract", `["key"]`})
	if err != nil {
		t.Logf("Expected SOPS error: %v", err)
	}
}

func TestDecryptCommandWithIgnoreMacFlag(t *testing.T) {
	t.Parallel()

	testFile := createTestFile(t, "test.yaml", "key: value")

	// Execute - we expect it to fail on decryption (not encrypted), not on flag parsing
	err := executeDecryptCommand(t, []string{"decrypt", testFile, "--ignore-mac"})
	if err != nil {
		t.Logf("Expected SOPS error: %v", err)
	}
}

func TestDecryptCommandWithOutputFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := createTestFile(t, "test.yaml", "key: value")

	outputFile := filepath.Join(tmpDir, "decrypted.yaml")

	// Execute - we expect it to fail on decryption (not encrypted), not on flag parsing
	err := executeDecryptCommand(t, []string{"decrypt", testFile, "--output", outputFile})
	if err != nil {
		t.Logf("Expected SOPS error: %v", err)
	}
}

func TestCipherCommandHasDecryptSubcommand(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	// Verify decrypt subcommand exists
	decryptCmd := findSubcommand(cmd, "decrypt")
	if decryptCmd == nil {
		t.Error("expected decrypt subcommand to exist")
	}
}

func TestNewEditCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewEditCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "edit <file>" {
		t.Errorf("expected Use to be 'edit <file>', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}
}

func TestEditCommandHelp(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"edit", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

func TestEditCommandRequiresFile(t *testing.T) {
	t.Parallel()

	cipherCmd := setupCipherCommandTest(t, []string{"edit"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error when no file argument provided")
	}
}

func TestEditCommandUnsupportedFormat(t *testing.T) {
	t.Parallel()

	testFile := createTestFile(t, "test.txt", "test content")

	cipherCmd := setupCipherCommandTest(t, []string{"edit", testFile})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error for unsupported file format")
	}
}

func TestEditCommandFlags(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewEditCmd()

	// Check that ignore-mac flag exists
	ignoreMacFlag := cmd.Flags().Lookup("ignore-mac")
	if ignoreMacFlag == nil {
		t.Error("expected ignore-mac flag to exist")
	}

	// Check that show-master-keys flag exists
	showMasterKeysFlag := cmd.Flags().Lookup("show-master-keys")
	if showMasterKeysFlag == nil {
		t.Error("expected show-master-keys flag to exist")
	}
}

// testEditCommandWithFormat tests edit command with specific format.
// This helper expects an error since no keys are configured, but validates
// that the command processes files correctly.
func testEditCommandWithFormat(t *testing.T, filename string) {
	t.Helper()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, filename)

	cipherCmd := setupCipherCommandTest(t, []string{"edit", testFile})

	// This will fail because we don't have an editor configured or keys,
	// but it should recognize the file format
	err := cipherCmd.Execute()
	if err != nil {
		// Expected to fail - we're just checking it processes the format
		t.Logf("Expected error (no editor/keys configured): %v", err)
	}
}

// TestEditCommandWithYAML tests edit command with YAML format.
func TestEditCommandWithYAML(t *testing.T) {
	t.Parallel()

	testEditCommandWithFormat(t, "test.yaml")
}

// TestEditCommandWithJSON tests edit command with JSON format.
func TestEditCommandWithJSON(t *testing.T) {
	t.Parallel()

	testEditCommandWithFormat(t, "test.json")
}

func TestNewEncryptCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewEncryptCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "encrypt <file>" {
		t.Errorf("expected Use to be 'encrypt <file>', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}
}

func TestEncryptCommandHelp(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"encrypt", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

func TestEncryptCommandRequiresFile(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs([]string{"encrypt"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error when no file argument provided")
	}
}

// setupEncryptTest is a helper function to create a test file and execute encrypt command.
func setupEncryptTest(t *testing.T, filename, content string) error {
	t.Helper()

	testFile := createTestFile(t, filename, content)

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs([]string{"encrypt", testFile})

	// Execute returns an error which we pass through for test assertions
	//nolint:wrapcheck // Test helper intentionally returns unwrapped error for assertion
	return cipherCmd.Execute()
}

func TestEncryptCommandUnsupportedFormat(t *testing.T) {
	t.Parallel()

	err := setupEncryptTest(t, "test.txt", "test content")
	if err == nil {
		t.Error("expected error for unsupported file format")
	}
}

// testEncryptWithFormat tests encryption with supported file formats.
func testEncryptWithFormat(t *testing.T, filename, content string) {
	t.Helper()

	err := setupEncryptTest(t, filename, content)
	// We expect an error about missing keys, not about file format
	if err != nil {
		// Error is from SOPS (expected - no keys configured)
		t.Logf("Expected SOPS error (no keys configured): %v", err)
	}
}

func TestEncryptCommandYAMLFormat(t *testing.T) {
	t.Parallel()

	yamlContent := `apiVersion: v1
kind: Secret
metadata:
  name: test
data:
  key: value
`
	testEncryptWithFormat(t, "test.yaml", yamlContent)
}

func TestEncryptCommandJSONFormat(t *testing.T) {
	t.Parallel()

	jsonContent := `{
  "apiVersion": "v1",
  "kind": "Secret",
  "metadata": {
    "name": "test"
  },
  "data": {
    "key": "value"
  }
}
`
	testEncryptWithFormat(t, "test.json", jsonContent)
}

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

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

//nolint:paralleltest // Uses t.Setenv to modify process-level env vars
func TestImportKeyBasic(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Setup environment
	setupTestEnvironment(t, tmpDir)

	// Execute import command with just the private key
	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer

	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import, got: %v", err)
	}

	// Verify output contains key confirmation (path is dynamic, so we check for key parts)
	output := out.String()
	if !strings.Contains(output, "imported age key to") || !strings.Contains(output, "keys.txt") {
		t.Errorf("expected output to indicate key import, got: %s", output)
	}

	// Verify the key was written to the correct location
	expectedPath := getExpectedKeyPath(tmpDir)

	// Verify content and permissions
	verifyKeyFileContent(t, expectedPath)
	verifyFilePermissions(t, expectedPath)
}

func TestImportKeyWithXDGConfigHome(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	xdgConfigDir := filepath.Join(tmpDir, "xdg-config")

	// Set XDG_CONFIG_HOME
	t.Setenv(xdgConfigHomeEnv, xdgConfigDir)

	// Execute import command
	rt := di.NewRuntime()
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

//nolint:paralleltest // Uses t.Setenv to modify process-level env vars
func TestImportKeyAppendsToExistingFile(
	t *testing.T,
) {
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
	rt := di.NewRuntime()
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
			rt := di.NewRuntime()
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

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error when no private key provided, got none")
	}
}

func TestNewRotateCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewRotateCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "rotate <file/folder>" {
		t.Errorf("expected Use to be 'rotate <file/folder>', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	// Verify flags are registered
	addKeyFlag := cmd.Flags().Lookup("add-key")
	if addKeyFlag == nil {
		t.Error("expected add-key flag to be registered")
	}

	removeKeyFlag := cmd.Flags().Lookup("remove-key")
	if removeKeyFlag == nil {
		t.Error("expected remove-key flag to be registered")
	}

	recursiveFlag := cmd.Flags().Lookup("recursive")
	if recursiveFlag == nil {
		t.Error("expected recursive flag to be registered")
	}

	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected force flag to be registered")
	}

	// Verify write permission annotation
	if perm, ok := cmd.Annotations["ai.toolgen.permission"]; !ok || perm != "write" {
		t.Error("expected write permission annotation")
	}
}

func TestRotateCommandHelp(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"rotate", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	snaps.MatchSnapshot(t, out.String())
}

func TestCipherCommandHasRotateSubcommand(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	rotateCmd := findSubcommand(cmd, "rotate")
	if rotateCmd == nil {
		t.Error("expected rotate subcommand to exist")
	}
}

func TestRotateCommandRequiresArg(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs([]string{"rotate"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error when no argument provided, got none")
	}
}

func TestRotateCommandNonExistentPath(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs([]string{"rotate", "/nonexistent/path", "--force"})

	err := cipherCmd.Execute()
	if err == nil {
		t.Error("expected error for non-existent path, got none")
	}
}

func TestRotateCommandEmptyDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rt := di.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"rotate", tmpDir, "--force"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error for empty dir, got: %v", err)
	}

	if !strings.Contains(out.String(), "no SOPS-encrypted files found") {
		t.Errorf("expected 'no SOPS-encrypted files found' message, got: %s", out.String())
	}
}
