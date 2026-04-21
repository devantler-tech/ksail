package gitops_test

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/detector/gitops"
	"github.com/stretchr/testify/require"
)

// TestCRDetector_FindFluxInstance_WalkDirError verifies that a walk directory
// error is propagated. This covers the WalkDir error path in findCR.
//
//nolint:gosec // Test-only fixtures use controlled temp paths and permissions.
func TestCRDetector_FindFluxInstance_WalkDirError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission checks are not reliable on Windows")
	}

	currentUser, err := user.Current()
	require.NoError(t, err)

	if currentUser.Uid == "0" {
		t.Skip("permission-based test is not reliable when running as root")
	}

	tempDir := t.TempDir()

	// Create a subdirectory with restricted permissions to trigger a walk error.
	restrictedDir := filepath.Join(tempDir, "restricted")
	err = os.MkdirAll(restrictedDir, 0o750)
	require.NoError(t, err)

	// Write a file inside it so the walker tries to descend.
	err = os.WriteFile(filepath.Join(restrictedDir, "test.yaml"), []byte("test"), 0o600)
	require.NoError(t, err)

	// Remove permissions from the subdirectory after creating the file.
	err = os.Chmod(restrictedDir, 0o000)
	require.NoError(t, err)

	t.Cleanup(func() {
		// Restore permissions so cleanup can remove the dir.
		_ = os.Chmod(restrictedDir, 0o750)
	})

	det := gitops.NewCRDetector(tempDir)
	_, err = det.FindFluxInstance()

	require.Error(t, err)
	require.Contains(t, err.Error(), "walking source directory")
}

// TestCRDetector_FindFluxInstance_NonExistentDirReturnsEmpty verifies that
// calling with a non-existent sourceDir returns empty string and no error.
// This specifically tests the os.IsNotExist(err) path.
func TestCRDetector_FindFluxInstance_NonExistentDirReturnsEmpty(t *testing.T) {
	t.Parallel()

	det := gitops.NewCRDetector(filepath.Join(t.TempDir(), "does", "not", "exist"))
	result, err := det.FindFluxInstance()

	require.NoError(t, err)
	require.Empty(t, result)
}

// TestCRDetector_FindArgoCDApplication_NonExistentDirReturnsEmpty is the ArgoCD
// counterpart of the non-existent directory test.
func TestCRDetector_FindArgoCDApplication_NonExistentDirReturnsEmpty(t *testing.T) {
	t.Parallel()

	det := gitops.NewCRDetector("/non/existent/path/that/does/not/exist")
	result, err := det.FindArgoCDApplication()

	require.NoError(t, err)
	require.Empty(t, result)
}

// TestCRDetector_FindFluxInstance_EmptyYAMLDocument verifies that empty YAML
// documents are skipped.
func TestCRDetector_FindFluxInstance_EmptyYAMLDocument(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write a multi-document YAML with an empty document between valid ones.
	content := `---

---
apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`
	err := os.WriteFile(
		filepath.Join(tempDir, "empty-doc.yaml"),
		[]byte(content),
		0o600,
	)
	require.NoError(t, err)

	det := gitops.NewCRDetector(tempDir)
	result, err := det.FindFluxInstance()

	require.NoError(t, err)
	require.NotEmpty(t, result)
	require.Contains(t, result, "empty-doc.yaml")
}
