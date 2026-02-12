package gitops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/detector/gitops"
	"github.com/stretchr/testify/require"
)

const (
	testFilePermissions = 0o600
	testDirPermissions  = 0o750

	fluxInstanceContent = `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`

	argoCDApplicationContent = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ksail
  namespace: argocd
spec:
  project: default
`
)

func TestCRDetector_FindFluxInstance(t *testing.T) {
	t.Parallel()

	tests := getFluxInstanceTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			testCase.setupFiles(t, tempDir)

			det := gitops.NewCRDetector(tempDir)
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

type fluxTestCase struct {
	name           string
	setupFiles     func(t *testing.T, dir string)
	expectFound    bool
	expectFilename string
}

//nolint:funlen // Test case definitions are necessarily verbose
func getFluxInstanceTestCases() []fluxTestCase {
	return []fluxTestCase{
		{
			name:        "empty directory returns empty string",
			setupFiles:  func(_ *testing.T, _ string) {},
			expectFound: false,
		},
		{
			name: "non-existent directory returns empty string",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				err := os.RemoveAll(dir)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds FluxInstance by default name",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				err := os.WriteFile(
					filepath.Join(dir, "flux-instance.yaml"),
					[]byte(fluxInstanceContent),
					testFilePermissions,
				)
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
				err := os.WriteFile(
					filepath.Join(dir, "custom-flux.yaml"),
					[]byte(content),
					testFilePermissions,
				)
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
				err := os.WriteFile(
					filepath.Join(dir, "other-flux.yaml"),
					[]byte(content),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds FluxInstance in subdirectory",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				subdir := filepath.Join(dir, "gitops", "flux")

				err := os.MkdirAll(subdir, testDirPermissions)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(subdir, "flux-instance.yaml"),
					[]byte(fluxInstanceContent),
					testFilePermissions,
				)
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
` + fluxInstanceContent
				err := os.WriteFile(
					filepath.Join(dir, "multi-doc.yaml"),
					[]byte(content),
					testFilePermissions,
				)
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
` + fluxInstanceContent + `---
apiVersion: v1
kind: Namespace
metadata:
  name: flux-system
`
				err := os.WriteFile(
					filepath.Join(dir, "multi-doc-start.yaml"),
					[]byte(content),
					testFilePermissions,
				)
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
				err := os.WriteFile(
					filepath.Join(dir, "flux-instance.txt"),
					[]byte(content),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "skips unparseable YAML files",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				err := os.WriteFile(
					filepath.Join(dir, "invalid.yaml"),
					[]byte(":::invalid yaml:::"),
					testFilePermissions,
				)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(dir, "valid.yaml"),
					[]byte(fluxInstanceContent),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "valid.yaml",
		},
	}
}

func TestCRDetector_FindArgoCDApplication(t *testing.T) {
	t.Parallel()

	tests := getArgoCDApplicationTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			testCase.setupFiles(t, tempDir)

			det := gitops.NewCRDetector(tempDir)
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

type argoCDTestCase struct {
	name           string
	setupFiles     func(t *testing.T, dir string)
	expectFound    bool
	expectFilename string
}

//nolint:funlen // Test case definitions are necessarily verbose
func getArgoCDApplicationTestCases() []argoCDTestCase {
	return []argoCDTestCase{
		{
			name:        "empty directory returns empty string",
			setupFiles:  func(_ *testing.T, _ string) {},
			expectFound: false,
		},
		{
			name: "finds ArgoCD Application by default name",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				err := os.WriteFile(
					filepath.Join(dir, "application.yaml"),
					[]byte(argoCDApplicationContent),
					testFilePermissions,
				)
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
				err := os.WriteFile(
					filepath.Join(dir, "custom-app.yaml"),
					[]byte(content),
					testFilePermissions,
				)
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
				err := os.WriteFile(
					filepath.Join(dir, "other-app.yaml"),
					[]byte(content),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound: false,
		},
		{
			name: "finds ArgoCD Application in subdirectory",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				subdir := filepath.Join(dir, "gitops", "argocd")

				err := os.MkdirAll(subdir, testDirPermissions)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(subdir, "application.yaml"),
					[]byte(argoCDApplicationContent),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "application.yaml",
		},
		{
			name: "handles .yml extension",
			setupFiles: func(t *testing.T, dir string) {
				t.Helper()

				err := os.WriteFile(
					filepath.Join(dir, "application.yml"),
					[]byte(argoCDApplicationContent),
					testFilePermissions,
				)
				require.NoError(t, err)
			},
			expectFound:    true,
			expectFilename: "application.yml",
		},
	}
}
