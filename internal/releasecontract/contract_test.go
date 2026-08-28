package releasecontract_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

var versionedKSailInstallCommand = regexp.MustCompile(
	`go[\t ]+install[\t ]+github\.com/devantler-tech/ksail/v7@`,
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

		if containsVersionedKSailInstallCommand(contents) {
			offenders = append(offenders, relativePath)
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

func containsVersionedKSailInstallCommand(contents []byte) bool {
	return versionedKSailInstallCommand.Match(contents)
}

func TestContainsVersionedKSailInstallCommand(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		contents string
		want     bool
	}{
		{
			name:     "latest",
			contents: "go install github.com/devantler-tech/ksail/v7@latest",
			want:     true,
		},
		{
			name:     "release tag",
			contents: "go install github.com/devantler-tech/ksail/v7@v7.180.5",
			want:     true,
		},
		{
			name:     "template tag",
			contents: "go install github.com/devantler-tech/ksail/v7@{{ .Tag }}",
			want:     true,
		},
		{name: "unversioned source build", contents: "go install ./cmd/ksail", want: false},
		{name: "different module", contents: "go install example.com/tool@v1.2.3", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := containsVersionedKSailInstallCommand([]byte(testCase.contents))
			if got != testCase.want {
				t.Fatalf("containsVersionedKSailInstallCommand() = %v, want %v", got, testCase.want)
			}
		})
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
