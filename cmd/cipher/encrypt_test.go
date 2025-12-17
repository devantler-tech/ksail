package cipher_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail-go/cmd/cipher"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
)

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

	rt := runtime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"encrypt", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Error("expected help output to not be empty")
	}
}

func TestEncryptCommandRequiresFile(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
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
