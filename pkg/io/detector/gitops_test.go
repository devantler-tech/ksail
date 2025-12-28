package detector_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/detector"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

func TestGitOpsCRDetector_FindFluxInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupFiles     func(t *testing.T, dir string)
		expectFound    bool
		expectFilename string
	}{
		{
			name:        "empty directory returns empty string",
			setupFiles:  func(_ *testing.T, _ string) {},
			expectFound: false,
		},
		{
			name: "non-existent directory returns empty string",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				// Remove the directory to simulate non-existent
				err := os.RemoveAll(dir)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds FluxInstance by default name",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`
				err := os.WriteFile(filepath.Join(dir, "flux-instance.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "flux-instance.yaml",
		},
		{
			name: "finds FluxInstance by managed-by label",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: custom-flux
  namespace: flux-system
  labels:
    app.kubernetes.io/managed-by: ksail
spec:
  distribution:
    version: "2.x"
`
				err := os.WriteFile(filepath.Join(dir, "custom-flux.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "custom-flux.yaml",
		},
		{
			name: "ignores FluxInstance without matching name or label",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: other-flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`
				err := os.WriteFile(filepath.Join(dir, "other-flux.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds FluxInstance in subdirectory",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				subdir := filepath.Join(dir, "gitops", "flux")
				err := os.MkdirAll(subdir, 0o755)
				require.NoError(t, err)

				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`
				err = os.WriteFile(filepath.Join(subdir, "flux-instance.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "flux-instance.yaml",
		},
		{
			name: "handles multi-document YAML",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: v1
kind: Namespace
metadata:
  name: flux-system
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
				err := os.WriteFile(filepath.Join(dir, "multi-doc.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "multi-doc.yaml",
		},
		{
			name: "handles multi-document YAML starting with separator",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `---
apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
---
apiVersion: v1
kind: Namespace
metadata:
  name: flux-system
`
				err := os.WriteFile(filepath.Join(dir, "multi-doc-start.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "multi-doc-start.yaml",
		},
		{
			name: "ignores non-YAML files",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
`
				err := os.WriteFile(filepath.Join(dir, "flux-instance.txt"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "skips unparseable YAML files",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				// Write invalid YAML
				err := os.WriteFile(filepath.Join(dir, "invalid.yaml"), []byte(":::invalid yaml:::"), testFilePermissions)
				require.NoError(t, err)

				// Write valid FluxInstance
				content := `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
`
				err = os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "valid.yaml",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			testCase.setupFiles(t, tempDir)

			det := detector.NewGitOpsCRDetector(tempDir)
			result, err := det.FindFluxInstance()
			require.NoError(t, err)

			if testCase.expectFound {
				require.NotEmpty(t, result)
				require.Contains(t, result, testCase.expectFilename)
			} else {
				require.Empty(t, result)
			}
		})
	}
}

func TestGitOpsCRDetector_FindArgoCDApplication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupFiles     func(t *testing.T, dir string)
		expectFound    bool
		expectFilename string
	}{
		{
			name:        "empty directory returns empty string",
			setupFiles:  func(_ *testing.T, _ string) {},
			expectFound: false,
		},
		{
			name: "finds ArgoCD Application by default name",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ksail
  namespace: argocd
spec:
  project: default
`
				err := os.WriteFile(filepath.Join(dir, "application.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "application.yaml",
		},
		{
			name: "finds ArgoCD Application by managed-by label",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: custom-app
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
spec:
  project: default
`
				err := os.WriteFile(filepath.Join(dir, "custom-app.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "custom-app.yaml",
		},
		{
			name: "ignores ArgoCD Application without matching name or label",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: other-app
  namespace: argocd
spec:
  project: default
`
				err := os.WriteFile(filepath.Join(dir, "other-app.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds ArgoCD Application in subdirectory",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				subdir := filepath.Join(dir, "gitops", "argocd")
				err := os.MkdirAll(subdir, 0o755)
				require.NoError(t, err)

				content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ksail
  namespace: argocd
spec:
  project: default
`
				err = os.WriteFile(filepath.Join(subdir, "application.yaml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "application.yaml",
		},
		{
			name: "handles .yml extension",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()
				content := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ksail
  namespace: argocd
spec:
  project: default
`
				err := os.WriteFile(filepath.Join(dir, "application.yml"), []byte(content), testFilePermissions)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "application.yml",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			testCase.setupFiles(t, tempDir)

			det := detector.NewGitOpsCRDetector(tempDir)
			result, err := det.FindArgoCDApplication()
			require.NoError(t, err)

			if testCase.expectFound {
				require.NotEmpty(t, result)
				require.Contains(t, result, testCase.expectFilename)
			} else {
				require.Empty(t, result)
			}
		})
	}
}
