package vulnidentity_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/vulnidentity"
	"github.com/stretchr/testify/require"
)

func TestCollectIncludesStandaloneVersions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "go.mod", `module example.test/root
go 1.26
require (
example.test/render v1.0.0
example.test/ansi v0.11.7
)
replace example.test/render v1.0.0 => ./render
replace example.test/ansi v0.11.7 => ./ansi-runtime
`)
	writeFixture(t, root, "render/go.mod", `module example.test/render
go 1.26
require example.test/ansi v0.10.2
replace example.test/ansi v0.10.2 => ../ansi-original
`)
	writeFixture(t, root, "ansi-runtime/go.mod", "module example.test/ansi\ngo 1.26\n")
	writeFixture(t, root, "ansi-original/go.mod", "module example.test/ansi\ngo 1.26\n")
	identities, err := vulnidentity.Collect(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, []string{
		"example.test/ansi@v0.10.2", "example.test/ansi@v0.11.7", "example.test/render@v1.0.0",
	}, identities)
}

func TestCollectResolvesUnqualifiedReplacementVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "go.mod", `module example.test/root
go 1.26
require example.test/dependency v1.2.3
replace example.test/dependency => ./dependency
`)
	writeFixture(t, root, "dependency/go.mod", "module example.test/dependency\ngo 1.26\n")
	identities, err := vulnidentity.Collect(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/dependency@v1.2.3"}, identities)
}

func TestCollectRejectsMissingAndMisidentifiedSources(t *testing.T) {
	t.Parallel()

	for name, dependency := range map[string]string{
		"missing":        "",
		"wrong identity": "module example.test/other\ngo 1.26\n",
		"malformed":      "module\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(
				t,
				root,
				"go.mod",
				"module example.test/root\ngo 1.26\nreplace example.test/dependency v1.2.3 => ./dependency\n",
			)

			if dependency != "" {
				writeFixture(t, root, "dependency/go.mod", dependency)
			}

			_, err := vulnidentity.Collect(context.Background(), root)
			require.Error(t, err)
		})
	}
}

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()

	directory, err := os.OpenRoot(root)
	require.NoError(t, err)
	require.NoError(t, directory.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, directory.WriteFile(path, []byte(contents), 0o600))
	require.NoError(t, directory.Close())
}

func TestCollectRejectsEscapedAndLinkedSources(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"escape", "symlink"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			target := t.TempDir()
			writeFixture(t, target, "go.mod", "module example.test/dependency\ngo 1.26\n")

			path := target
			if name == "symlink" {
				path = "./dependency"

				require.NoError(t, os.Symlink(target, filepath.Join(root, "dependency")))
			}

			writeFixture(t, root, "go.mod", "module example.test/root\ngo 1.26\n"+
				"replace example.test/dependency v1.2.3 => "+path+"\n")
			_, err := vulnidentity.Collect(t.Context(), root)
			require.ErrorIs(t, err, vulnidentity.ErrIdentity)
		})
	}
}

func TestCollectUsesOwnModuleGraph(t *testing.T) {
	root := t.TempDir()

	const contents = "module example.test/root\ngo 1.26\n" +
		"require example.test/dependency v1.2.3\nreplace example.test/dependency => ./dependency\n"
	writeFixture(t, root, "go.mod", contents)
	writeFixture(t, root, "alternate.mod", strings.ReplaceAll(contents, "v1.2.3", "v1.8.0"))
	writeFixture(t, root, "dependency/go.mod", "module example.test/dependency\ngo 1.26\n")
	t.Setenv("GOFLAGS", "-modfile="+filepath.Join(root, "alternate.mod"))
	identities, err := vulnidentity.Collect(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/dependency@v1.2.3"}, identities)
}
