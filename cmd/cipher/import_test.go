package cipher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/cmd/cipher"
	rtruntime "github.com/devantler-tech/ksail/pkg/di"
)

const (
	validAgeKey      = "AGE-SECRET-KEY-1ABCDEFGHIJKLMNOPQRSTUVWXYZ234567890ABCDEFGHIJKLMNOP"
	validPublicKey   = "age1abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqr"
	windowsOS        = "windows"
	darwinOS         = "darwin"
	keyFileName      = "keys.txt"
	sopsSubdir       = "sops"
	ageSubdir        = "age"
	filePermissions  = 0o600
	expectedPerm     = os.FileMode(filePermissions)
	xdgConfigHomeEnv = "XDG_CONFIG_HOME"
	homeEnv          = "HOME"
	appDataEnv       = "AppData"
)

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

	// Verify public-key flag exists
	publicKeyFlag := cmd.Flags().Lookup("public-key")
	if publicKeyFlag == nil {
		t.Error("expected public-key flag to be registered")
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

	if !strings.Contains(output, "public-key") {
		t.Error("expected help output to mention public-key flag")
	}
}

func TestImportKeyBasic(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Set HOME to temp directory
	originalHome := os.Getenv(homeEnv)
	t.Cleanup(func() {
		_ = os.Setenv(homeEnv, originalHome)
	})
	err := os.Setenv(homeEnv, tmpDir)
	if err != nil {
		t.Fatalf("failed to set HOME: %v", err)
	}

	// Clear XDG_CONFIG_HOME
	originalXDG := os.Getenv(xdgConfigHomeEnv)
	t.Cleanup(func() {
		if originalXDG != "" {
			_ = os.Setenv(xdgConfigHomeEnv, originalXDG)
		} else {
			_ = os.Unsetenv(xdgConfigHomeEnv)
		}
	})
	_ = os.Unsetenv(xdgConfigHomeEnv)

	// On Windows, set AppData
	if runtime.GOOS == windowsOS {
		originalAppData := os.Getenv(appDataEnv)
		t.Cleanup(func() {
			_ = os.Setenv(appDataEnv, originalAppData)
		})
		err = os.Setenv(appDataEnv, tmpDir)
		if err != nil {
			t.Fatalf("failed to set AppData: %v", err)
		}
	}

	// Execute import command with just the private key
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err = cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Successfully imported") {
		t.Errorf("expected success message, got: %s", output)
	}

	// Verify the key was written to the correct location
	var expectedPath string

	switch runtime.GOOS {
	case windowsOS:
		expectedPath = filepath.Join(tmpDir, sopsSubdir, ageSubdir, keyFileName)
	case darwinOS:
		expectedPath = filepath.Join(
			tmpDir,
			"Library",
			"Application Support",
			sopsSubdir,
			ageSubdir,
			keyFileName,
		)
	default:
		expectedPath = filepath.Join(tmpDir, ".config", sopsSubdir, ageSubdir, keyFileName)
	}

	content, err := os.ReadFile(expectedPath) //#nosec G304 -- test file path
	if err != nil {
		t.Errorf("expected key file to exist at %s, got error: %v", expectedPath, err)
	}

	contentStr := string(content)

	// Verify the key contains metadata
	if !strings.Contains(contentStr, "# created:") {
		t.Errorf("expected key file to contain creation timestamp")
	}

	// Verify the key is present
	if !strings.Contains(contentStr, validAgeKey) {
		t.Errorf("expected key file to contain the age key")
	}

	// Verify file permissions
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

func TestImportKeyWithPublicKey(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Set HOME to temp directory
	originalHome := os.Getenv(homeEnv)
	t.Cleanup(func() {
		_ = os.Setenv(homeEnv, originalHome)
	})
	err := os.Setenv(homeEnv, tmpDir)
	if err != nil {
		t.Fatalf("failed to set HOME: %v", err)
	}

	// Clear XDG_CONFIG_HOME
	originalXDG := os.Getenv(xdgConfigHomeEnv)
	t.Cleanup(func() {
		if originalXDG != "" {
			_ = os.Setenv(xdgConfigHomeEnv, originalXDG)
		} else {
			_ = os.Unsetenv(xdgConfigHomeEnv)
		}
	})
	_ = os.Unsetenv(xdgConfigHomeEnv)

	// On Windows, set AppData
	if runtime.GOOS == windowsOS {
		originalAppData := os.Getenv(appDataEnv)
		t.Cleanup(func() {
			_ = os.Setenv(appDataEnv, originalAppData)
		})
		err = os.Setenv(appDataEnv, tmpDir)
		if err != nil {
			t.Fatalf("failed to set AppData: %v", err)
		}
	}

	// Execute import command with private key and public key
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey, "--public-key", validPublicKey})

	err = cipherCmd.Execute()
	if err != nil {
		t.Errorf("expected no error executing import with public key, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Successfully imported") {
		t.Errorf("expected success message, got: %s", output)
	}

	// Verify the key was written to the correct location
	var expectedPath string

	switch runtime.GOOS {
	case windowsOS:
		expectedPath = filepath.Join(tmpDir, sopsSubdir, ageSubdir, keyFileName)
	case darwinOS:
		expectedPath = filepath.Join(
			tmpDir,
			"Library",
			"Application Support",
			sopsSubdir,
			ageSubdir,
			keyFileName,
		)
	default:
		expectedPath = filepath.Join(tmpDir, ".config", sopsSubdir, ageSubdir, keyFileName)
	}

	content, err := os.ReadFile(expectedPath) //#nosec G304 -- test file path
	if err != nil {
		t.Errorf("expected key file to exist at %s, got error: %v", expectedPath, err)
	}

	contentStr := string(content)

	// Verify the key contains creation timestamp
	if !strings.Contains(contentStr, "# created:") {
		t.Errorf("expected key file to contain creation timestamp")
	}

	// Verify the key contains public key
	if !strings.Contains(contentStr, "# public key: "+validPublicKey) {
		t.Errorf("expected key file to contain public key comment")
	}

	// Verify the private key is present
	if !strings.Contains(contentStr, validAgeKey) {
		t.Errorf("expected key file to contain the age key")
	}
}

func TestImportKeyWithXDGConfigHome(t *testing.T) {
	// Note: Not running in parallel due to environment variable modifications

	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	xdgConfigDir := filepath.Join(tmpDir, "xdg-config")

	// Set XDG_CONFIG_HOME
	originalXDG := os.Getenv(xdgConfigHomeEnv)
	t.Cleanup(func() {
		if originalXDG != "" {
			_ = os.Setenv(xdgConfigHomeEnv, originalXDG)
		} else {
			_ = os.Unsetenv(xdgConfigHomeEnv)
		}
	})
	err := os.Setenv(xdgConfigHomeEnv, xdgConfigDir)
	if err != nil {
		t.Fatalf("failed to set XDG_CONFIG_HOME: %v", err)
	}

	// Execute import command
	rt := rtruntime.NewRuntime()
	cipherCmd := cipher.NewCipherCmd(rt)

	var out bytes.Buffer
	cipherCmd.SetOut(&out)
	cipherCmd.SetArgs([]string{"import", validAgeKey})

	err = cipherCmd.Execute()
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Note: Not running in parallel due to environment variable modifications

			// Create a temporary directory for testing
			tmpDir := t.TempDir()

			// Set HOME to temp directory
			originalHome := os.Getenv(homeEnv)
			t.Cleanup(func() {
				_ = os.Setenv(homeEnv, originalHome)
			})
			err := os.Setenv(homeEnv, tmpDir)
			if err != nil {
				t.Fatalf("failed to set HOME: %v", err)
			}

			// Clear XDG_CONFIG_HOME
			originalXDG := os.Getenv(xdgConfigHomeEnv)
			t.Cleanup(func() {
				if originalXDG != "" {
					_ = os.Setenv(xdgConfigHomeEnv, originalXDG)
				} else {
					_ = os.Unsetenv(xdgConfigHomeEnv)
				}
			})
			_ = os.Unsetenv(xdgConfigHomeEnv)

			// On Windows, set AppData
			if runtime.GOOS == windowsOS {
				originalAppData := os.Getenv(appDataEnv)
				t.Cleanup(func() {
					_ = os.Setenv(appDataEnv, originalAppData)
				})
				err = os.Setenv(appDataEnv, tmpDir)
				if err != nil {
					t.Fatalf("failed to set AppData: %v", err)
				}
			}

			// Execute import command with invalid key
			rt := rtruntime.NewRuntime()
			cipherCmd := cipher.NewCipherCmd(rt)

			var out bytes.Buffer
			cipherCmd.SetOut(&out)
			cipherCmd.SetArgs([]string{"import", tc.keyData})

			err = cipherCmd.Execute()
			if err == nil {
				t.Errorf("expected error for invalid key, got none")
			}

			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("expected error to contain %q, got: %v", tc.errMsg, err)
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
