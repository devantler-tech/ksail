package releasecontract_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestPublishedInstallationMatchesModuleContract(t *testing.T) {
	t.Parallel()

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	repository := os.DirFS(filepath.Join(filepath.Dir(testFile), "..", ".."))
	contents := readRepositoryFile(t, repository, "go.mod")

	moduleFile, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}

	graphOverrides := len(moduleFile.Replace) + len(moduleFile.Exclude)
	if graphOverrides == 0 {
		return
	}

	installCommands := [][]byte{
		[]byte("go install " + "github.com/devantler-tech/ksail/v7@latest"),
		[]byte("go install " + "github.com/devantler-tech/ksail/v7@{{ .Tag }}"),
	}
	userFacingPaths := []string{
		".goreleaser.yaml",
		"README.md",
		"docs/src/content/docs/faq.md",
		"docs/src/content/docs/installation.mdx",
		"pkg/fsutil/scaffolder/scaffolder_devcontainer.go",
		"pkg/fsutil/scaffolder/__snapshots__/scaffolder_devcontainer_test.snap",
		"pkg/svc/chat/docs_generated.go",
	}

	offenders := make([]string, 0, len(userFacingPaths))
	for _, relativePath := range userFacingPaths {
		contents := readRepositoryFile(t, repository, relativePath)

		for _, installCommand := range installCommands {
			if bytes.Contains(contents, installCommand) {
				offenders = append(offenders, relativePath)

				break
			}
		}
	}

	if len(offenders) != 0 {
		t.Fatalf(
			"%d module graph overrides make versioned Go installation unusable; advertised by: %s",
			graphOverrides,
			strings.Join(offenders, ", "),
		)
	}
}

func readRepositoryFile(t *testing.T, repository fs.FS, path string) []byte {
	t.Helper()

	contents, err := fs.ReadFile(repository, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return contents
}
