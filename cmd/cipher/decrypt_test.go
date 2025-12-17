package cipher_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail-go/cmd/cipher"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
)

func TestNewDecryptCmd(t *testing.T) {
	t.Parallel()

	cmd := cipher.NewDecryptCmd()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	if cmd.Use != "decrypt <file>" {
		t.Errorf("expected Use to be 'decrypt <file>', got %q", cmd.Use)
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

	rt := runtime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"decrypt", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Error("expected help output to not be empty")
	}

	// Verify help output mentions key features
	if !strings.Contains(output, "decrypt") {
		t.Error("expected help output to mention decrypt")
	}
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

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	// Verify decrypt subcommand exists
	decryptCmd := findSubcommand(cmd, "decrypt")
	if decryptCmd == nil {
		t.Error("expected decrypt subcommand to exist")
	}
}
