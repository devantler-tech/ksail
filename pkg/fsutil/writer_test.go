package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testContent     = "test content"
	originalContent = "original content"
)

func TestTryWriteFile(t *testing.T) {
	t.Parallel()

	t.Run("validation errors", func(t *testing.T) {
		t.Parallel()
		runTryWriteFileValidationTests(t)
	})

	t.Run("successful operations", func(t *testing.T) {
		t.Parallel()
		runTryWriteFileSuccessTests(t)
	})

	t.Run("file system errors", func(t *testing.T) {
		t.Parallel()
		runTryWriteFileErrorTests(t)
	})
}

func runTryWriteFileValidationTests(t *testing.T) {
	t.Helper()

	tests := []struct {
		name       string
		content    string
		outputPath string
		force      bool
	}{
		{
			name:       "empty output",
			content:    testContent,
			outputPath: "",
			force:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := fsutil.TryWriteFile(test.content, test.outputPath, test.force)

			require.Error(t, err, "TryWriteFile()")
			assert.Empty(t, result, "TryWriteFile() result on error")
		})
	}
}

func runTryWriteFileSuccessTests(t *testing.T) {
	t.Helper()

	tests := []struct {
		name         string
		setupTest    func(t *testing.T) (content, outputPath string, force bool)
		verifyResult func(t *testing.T, tempDir, outputPath, content, result string)
	}{
		{
			name:         "new file",
			setupTest:    setupTryWriteFileNewFile,
			verifyResult: verifyTryWriteFileContentsEqual,
		},
		{
			name:         "existing file no force",
			setupTest:    setupTryWriteFileExistingNoForce,
			verifyResult: verifyTryWriteFileContentsPreserved,
		},
		{
			name:         "existing file force",
			setupTest:    setupTryWriteFileExistingForce,
			verifyResult: verifyTryWriteFileContentsEqual,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content, outputPath, force := test.setupTest(t)
			result, err := fsutil.TryWriteFile(content, outputPath, force)

			require.NoError(t, err, "TryWriteFile()")
			assert.Equal(t, content, result, "TryWriteFile()")

			if test.verifyResult != nil {
				tempDir := filepath.Dir(outputPath)
				test.verifyResult(t, tempDir, outputPath, content, result)
			}
		})
	}
}

func setupTryWriteFileNewFile(t *testing.T) (string, string, bool) {
	t.Helper()

	content := "new file content"
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "test.txt")

	return content, outputPath, false
}

func setupTryWriteFileExistingNoForce(t *testing.T) (string, string, bool) {
	t.Helper()

	newContent := "new content"
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "existing.txt")

	err := os.WriteFile(outputPath, []byte(originalContent), 0o600)
	require.NoError(t, err, "WriteFile() setup")

	return newContent, outputPath, false
}

func setupTryWriteFileExistingForce(t *testing.T) (string, string, bool) {
	t.Helper()

	newContent := "new content forced"
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "existing-force.txt")

	err := os.WriteFile(outputPath, []byte(originalContent), 0o600)
	require.NoError(t, err, "WriteFile() setup")

	return newContent, outputPath, true
}

func verifyTryWriteFileContentsEqual(t *testing.T, tempDir, outputPath, content, _ string) {
	t.Helper()

	writtenContent, err := fsutil.ReadFileSafe(tempDir, outputPath)
	require.NoError(t, err, "ReadFile()")
	assert.Equal(t, content, string(writtenContent), "written file content")
}

func verifyTryWriteFileContentsPreserved(t *testing.T, tempDir, outputPath, _, _ string) {
	t.Helper()

	writtenContent, err := fsutil.ReadFileSafe(tempDir, outputPath)
	require.NoError(t, err, "ReadFile()")

	assert.Equal(t, originalContent, string(writtenContent),
		"file content (should not be overwritten)")
}

func runTryWriteFileErrorTests(t *testing.T) {
	t.Helper()

	tests := []struct {
		name               string
		setupTest          func(t *testing.T) (content, outputPath string, force bool)
		expectedErrMessage string
	}{
		{
			name:               "stat error",
			setupTest:          setupTryWriteFileStatError,
			expectedErrMessage: "failed to check file",
		},
		{
			name:               "write error",
			setupTest:          setupTryWriteFileWriteError,
			expectedErrMessage: "failed to create directory",
		},
		{
			name:               "file write error",
			setupTest:          setupTryWriteFilePermissionError,
			expectedErrMessage: "failed to write file",
		},
	}

	runErrorTestsWithTwoParams(t, tests, fsutil.TryWriteFile, "TryWriteFile")
}

func setupTryWriteFileStatError(t *testing.T) (string, string, bool) {
	t.Helper()

	content := "content for stat error test"
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "restricted", "file.txt")

	restrictedDir := filepath.Join(tempDir, "restricted")
	err := os.Mkdir(restrictedDir, 0o000)
	require.NoError(t, err, "Mkdir() setup")

	return content, outputPath, false
}

func setupTryWriteFileWriteError(_ *testing.T) (string, string, bool) {
	content := "content for write error test"
	invalidPath := "/invalid/nonexistent/deeply/nested/path/file.txt"

	return content, invalidPath, false
}

func setupTryWriteFilePermissionError(t *testing.T) (string, string, bool) {
	t.Helper()

	content := "content for file write error test"
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "readonly.txt")

	err := os.WriteFile(outputPath, []byte("existing"), 0o000)
	require.NoError(t, err, "WriteFile() setup")

	return content, outputPath, true
}

// runErrorTestsWithTwoParams runs error tests for functions with two parameters (content, outputPath).
func runErrorTestsWithTwoParams(
	t *testing.T,
	tests []struct {
		name               string
		setupTest          func(t *testing.T) (content, outputPath string, force bool)
		expectedErrMessage string
	},
	testFunc func(content, outputPath string, force bool) (string, error),
	functionName string,
) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content, outputPath, force := test.setupTest(t)
			result, err := testFunc(content, outputPath, force)

			require.Error(t, err, functionName)
			assert.Empty(t, result, functionName+" result on error")

			if test.expectedErrMessage == "" {
				assert.Error(t, err, functionName)
			} else {
				assert.ErrorContains(t, err, test.expectedErrMessage, functionName)
			}
		})
	}
}
