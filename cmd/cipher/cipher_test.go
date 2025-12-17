package cipher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail-go/cmd/cipher"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	"github.com/spf13/cobra"
)

func TestNewCipherCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
	cmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing --help, got: %v", err)
	}

	// Verify help output contains information about cipher
	if out.Len() == 0 {
		t.Error("expected help output to not be empty")
	}
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

	rt := runtime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out, errOut bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetErr(&errOut)
	cipherCmd.SetArgs(args)

	return cipherCmd
}
