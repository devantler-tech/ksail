package moduleintegrity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/moduleintegrity"
	"github.com/stretchr/testify/require"
)

const (
	fixturePrefix = "example.invalid/fixture@v1.0.0"
	fixtureSum    = "h1:FaDXuH+aUAlD5lJj7Abt6R9mYtDLv7OCzA4aBtx+Oz0="
)

func TestVerifyRejectsChangedExtractedSource(t *testing.T) {
	t.Parallel()

	for _, mutation := range []string{"changed", "extra", "missing"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()

			directory := moduleFixture(t)
			require.NoError(t, moduleintegrity.Verify(directory, fixturePrefix, fixtureSum))

			switch mutation {
			case "changed":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "fixture.go"),
					[]byte("package fixture\n// undeclared edit\n"), 0o600))
			case "extra":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "extra.go"),
					[]byte("package fixture\n"), 0o600))
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(directory, "fixture.go")))
			}

			require.ErrorIs(t, moduleintegrity.Verify(directory, fixturePrefix, fixtureSum),
				moduleintegrity.ErrChecksumMismatch)
		})
	}
}

func TestVerifyRejectsLinkedSource(t *testing.T) {
	t.Parallel()

	for _, mutation := range []string{"root", "root-slash", "root-dot", "file"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()

			directory := moduleFixture(t)
			target := directory
			link := filepath.Join(t.TempDir(), "link")

			if mutation == "file" {
				target = filepath.Join(directory, "fixture.go")
				require.NoError(t, os.Rename(target, link))
				target, link = link, target
			}

			require.NoError(t, os.Symlink(target, link))

			if mutation != "file" {
				directory = link
			}

			switch mutation {
			case "root-slash":
				directory += string(filepath.Separator)
			case "root-dot":
				directory += string(filepath.Separator) + "."
			}

			require.ErrorIs(t, moduleintegrity.Verify(directory, fixturePrefix, fixtureSum),
				moduleintegrity.ErrInvalidTree)
		})
	}
}

func moduleFixture(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"),
		[]byte("module example.invalid/fixture\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "fixture.go"),
		[]byte("package fixture\n"), 0o600))

	return directory
}
