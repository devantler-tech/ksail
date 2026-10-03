package ciharness_test

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Every source byte, including embedded data and checkout metadata, must select
// the required integrity tests even when the shared Go validator skips testing.
func TestAnalysisSourceIntegrityRunsForEveryCopiedByte(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	job, found := workflow.Jobs["verify-todos-workflow-contract"]
	require.True(t, found)
	assert.Equal(
		t,
		"github.event_name != 'merge_group' && needs.changes.outputs.todos-contract == 'true'",
		job.If,
	)
	assert.False(t, job.ContinueOnError)
	assert.Contains(
		t,
		workflow.Jobs["require-checks-in-pr"].Needs,
		"verify-todos-workflow-contract",
	)

	filter := findHarnessStep(t, workflow.Jobs["changes"].Steps, "🔍 Filter paths")

	var filters map[string][]string

	require.NoError(t, yaml.Unmarshal([]byte(stringValue(filter.With["filters"])), &filters))

	patterns := filters["todos-contract"]
	for _, input := range []string{".gitattributes", ".mega-linter.yml", ".jscpd.json", "go.mod", "go.sum"} {
		assert.Truef(
			t,
			contractFilterMatches(t, patterns, input),
			"%s must run source authentication",
			input,
		)
	}

	for _, source := range authenticatedAnalysisSources() {
		assertAnalysisSourceFilter(t, patterns, source.directory)
	}
}

// Foreign source classification must not hide KSail's maintained code or its
// compatibility patches from mutating linters.
func TestAnalysisSourceClassificationKeepsOwnedCodeChecked(t *testing.T) {
	t.Parallel()

	var configuration map[string]any
	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".mega-linter.yml"), &configuration))
	filter, err := regexp.Compile(stringValue(configuration["FILTER_REGEX_EXCLUDE"]))
	require.NoError(t, err)

	for path, foreign := range map[string]bool{
		"third_party/cel-go/cel/env.go":                 true,
		"third_party/glamour/glamour.go":                true,
		"third_party/go-macholibre/universal_binary.go": true,
		"third_party/kyverno-jmespath/api.go":           true,
		"third_party/jmespath/api.go":                   true,
		"third_party/ansi/truncate.go":                  true,
		"third_party/ansi-runtime/truncate.go":          true,
		"third_party/ansi-runtime-owned/main.go":        false,
		"third_party/redisotel/tracing.go":              true,
		"third_party/rediscmd/rediscmd.go":              true,
		"third_party/dynamiclistener/cert/cert.go":      true,
		"third_party/dynamiclistener-owned/main.go":     false,
		"third_party/redisotel-owned/main.go":           false,
		"third_party/rediscmd-owned/main.go":            false,
		"third_party/ansi-owned/main.go":                false,
		"third_party/cel-golang/main.go":                false,
		"third_party/go-archive/compat_legacy.go":       false,
		"third_party/otelzap/otelzap.go":                false,
		"internal/ciharness/analysis_source_test.go":    false,
		"internal/ciharness/ansi_protocol_test.go":      false,
		"internal/ciharness/cel_unknown_test.go":        false,
		"internal/moduleintegrity/source.go":            false,
		".github/scripts/verify-desktop-codeql.sh":      false,
		"pkg/cli/ui/chat/markdown.go":                   false,
	} {
		assert.Equalf(
			t,
			foreign,
			filter.MatchString(path),
			"incorrect foreign-source classification for %s",
			path,
		)
	}
}

func assertAnalysisSourceFilter(t *testing.T, patterns []string, directory string) {
	t.Helper()

	root := filepath.Join("..", "..")
	count := 0
	err := filepath.WalkDir(
		filepath.Join(root, "third_party", directory),
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}

			input, relativeErr := filepath.Rel(root, path)
			require.NoError(t, relativeErr)
			assert.Truef(t, contractFilterMatches(t, patterns, filepath.ToSlash(input)),
				"changing only %s must run complete source authentication", input)

			count++

			return nil
		},
	)
	require.NoError(t, err)
	require.Positive(t, count, "source filter proof must examine actual copied files")
}
