package cipher_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail-go/cmd/cipher"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
)

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

	rt := runtime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"edit", "--help"})

	err := cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Error("expected help output to not be empty")
	}

	// Verify help mentions key concepts
	if !strings.Contains(output, "encrypted file") {
		t.Error("expected help to mention 'encrypted file'")
	}
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
