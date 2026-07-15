package docs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/spf13/cobra"
)

// countCommands returns the number of non-helper leaf and group-root
// commands in the Cobra tree (matching the shell script's behaviour).
func countCommands(c *cobra.Command) int {
	subs := make([]*cobra.Command, 0, len(c.Commands()))

	for _, sub := range c.Commands() {
		if docs.HiddenFromDocs(sub) {
			continue
		}

		subs = append(subs, sub)
	}

	if len(subs) == 0 {
		return 1 // leaf
	}

	count := 1 // group root page

	for _, sub := range subs {
		count += countCommands(sub)
	}

	return count
}

// skipIfDirMissing skips the test when the directory does not exist.
func skipIfDirMissing(t *testing.T, dir string) {
	t.Helper()

	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		t.Skipf("docs not generated yet (run go generate ./docs/...): %s", dir)
	}
}

// readOrSkip reads a file's content or skips the test if the file does not exist.
func readOrSkip(t *testing.T, path string) string {
	t.Helper()

	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Skipf("file not generated yet (run go generate ./docs/...): %s", path)
	}

	content, err := os.ReadFile(path) //nolint:gosec // path from test constant, not user input
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(content)
}

func TestCLIFlagsDocsExist(t *testing.T) {
	t.Parallel()

	dir := "src/content/docs/cli-flags"
	skipIfDirMissing(t, dir)

	rootCmd := cmd.NewRootCmd("test", "", "")

	// We don't generate a page for the bare "ksail" root, so subtract 1.
	expected := countCommands(rootCmd) - 1

	var actual int

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		isMDX := strings.HasSuffix(path, ".mdx")
		isIndex := strings.HasSuffix(path, "index.mdx")

		if !info.IsDir() && isMDX && !isIndex {
			actual++
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	if actual != expected {
		t.Errorf("expected %d CLI flags pages, got %d", expected, actual)
	}
}

// TestCLIFlagsDocsExcludeHiddenCommands verifies gen_docs skips Hidden commands
// so the generated tree never advertises deprecated flat delegates,
// experimental-gated commands, or internal entrypoints (operator / steer-agent).
// Regression guard for #6063: the walker previously only filtered help/completion.
func TestCLIFlagsDocsExcludeHiddenCommands(t *testing.T) {
	t.Parallel()

	dir := "src/content/docs/cli-flags"
	skipIfDirMissing(t, dir)

	// Every command HiddenFromDocs excludes must have no generated page. Match on
	// the command's full dash-joined path (e.g. "cluster-add-environment") — the
	// exact page-slug gen_docs writes — so a hidden "cluster-init" never
	// false-matches a visible "project-init" page that shares a leaf name.
	hidden := hiddenCommandPaths(cmd.NewRootCmd("test", "", ""), nil)
	if len(hidden) == 0 {
		t.Skip("no hidden commands in the tree to assert on")
	}

	offenders := findHiddenCommandPages(t, dir, hidden)
	if len(offenders) > 0 {
		t.Errorf("generated docs contain pages for hidden commands (should be skipped):\n%s",
			strings.Join(offenders, "\n"))
	}
}

// findHiddenCommandPages walks the generated cli-flags tree and returns any page
// whose slug belongs to a HiddenFromDocs command.
func findHiddenCommandPages(t *testing.T, dir string, hidden []string) []string {
	t.Helper()

	var offenders []string

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !isGeneratedCommandPage(path, info) {
			return nil
		}

		if slug := matchHiddenSlug(filepath.Base(path), hidden); slug != "" {
			offenders = append(offenders, path+" (hidden command "+slug+")")
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", dir, walkErr)
	}

	return offenders
}

// isGeneratedCommandPage reports whether path is a per-command MDX page (not a
// directory and not the group index).
func isGeneratedCommandPage(path string, info os.FileInfo) bool {
	if info.IsDir() || !strings.HasSuffix(path, ".mdx") {
		return false
	}

	return !strings.HasSuffix(path, "index.mdx")
}

// matchHiddenSlug returns the hidden slug a page basename belongs to, or "".
// A hidden leaf writes "<slug>.mdx"; a hidden group writes "<slug>-root.mdx" and
// "<slug>-<child>.mdx" — all prefixed "<slug>-".
func matchHiddenSlug(base string, hidden []string) string {
	base = strings.TrimSuffix(base, ".mdx")

	for _, slug := range hidden {
		if base == slug || strings.HasPrefix(base, slug+"-") {
			return slug
		}
	}

	return ""
}

// hiddenCommandPaths returns the dash-joined page slug of every command
// HiddenFromDocs excludes (e.g. "cluster-add-environment", "operator"), tracking
// the live command tree rather than a hard-coded list. A hidden command's whole
// subtree is skipped, mirroring gen_docs.
func hiddenCommandPaths(c *cobra.Command, parents []string) []string {
	var paths []string

	for _, sub := range c.Commands() {
		if sub.Name() == helpName || sub.Name() == completionName {
			continue
		}

		names := append(append([]string{}, parents...), sub.Name())
		if sub.Hidden {
			paths = append(paths, strings.Join(names, "-"))

			continue
		}

		paths = append(paths, hiddenCommandPaths(sub, names)...)
	}

	return paths
}

const (
	helpName       = "help"
	completionName = "completion"
)

// checkFrontmatter validates that a single MDX file contains the expected frontmatter.
func checkFrontmatter(t *testing.T, path string, content []byte) {
	t.Helper()

	text := string(content)

	if !strings.HasPrefix(text, "---\n") {
		t.Errorf("%s: missing frontmatter (should start with ---)", path)
	}

	if !strings.Contains(text, "title:") {
		t.Errorf("%s: missing title in frontmatter", path)
	}

	if !strings.Contains(text, "description:") {
		t.Errorf("%s: missing description in frontmatter", path)
	}
}

func TestCLIFlagsPagesHaveFrontmatter(t *testing.T) {
	t.Parallel()

	dir := "src/content/docs/cli-flags"
	skipIfDirMissing(t, dir)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() || !strings.HasSuffix(path, ".mdx") ||
			strings.HasSuffix(path, "index.mdx") {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // path from filepath.Walk
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		checkFrontmatter(t, path, content)

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
}

func TestIndexMDXContent(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/content/docs/cli-flags/index.mdx")

	if !strings.Contains(text, "title: CLI Flags") {
		t.Error("index.mdx: missing expected title")
	}

	if !strings.Contains(text, "Command Groups") {
		t.Error("index.mdx: missing Command Groups section")
	}
}

func TestConfigReferenceExists(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/content/docs/configuration/declarative-configuration.mdx")

	t.Run("has frontmatter", func(t *testing.T) {
		t.Parallel()

		if !strings.HasPrefix(text, "---\n") {
			t.Error("missing frontmatter")
		}
	})

	t.Run("has auto-gen tag", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(text, "auto-generated by go generate") {
			t.Error("missing auto-generation tag")
		}
	})

	t.Run("has configuration reference section", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(text, "## Configuration Reference") {
			t.Error("missing Configuration Reference section")
		}
	})

	t.Run("has enum values from types", func(t *testing.T) {
		t.Parallel()

		assertEnumValues(t, text)
	})

	t.Run("has environment variable expansion section", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(text, "Environment Variable Expansion") {
			t.Error("missing Environment Variable Expansion section")
		}
	})

	t.Run("has schema support section", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(text, "Schema Support") {
			t.Error("missing Schema Support section")
		}
	})
}

// assertEnumValues checks that known enum values appear in the config reference text.
func assertEnumValues(t *testing.T, text string) {
	t.Helper()

	dist := v1alpha1.Distribution("")
	for _, v := range dist.ValidValues() {
		if !strings.Contains(text, "`"+v+"`") {
			t.Errorf("missing distribution value %q", v)
		}
	}

	cni := v1alpha1.CNI("")
	for _, v := range cni.ValidValues() {
		if !strings.Contains(text, "`"+v+"`") {
			t.Errorf("missing CNI value %q", v)
		}
	}
}
