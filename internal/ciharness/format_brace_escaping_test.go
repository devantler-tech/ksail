package ciharness_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formatCallOpening matches the start of a format() call whose first argument is a string
// literal. The literal itself is read by readExpressionString, because GitHub expression strings
// escape a quote by doubling it and a regular expression cannot track that reliably.
var formatCallOpening = regexp.MustCompile(`\bformat\s*\(\s*'`)

// TestFormatStringsEscapeLiteralBraces fails when a workflow or composite action passes format()
// a string with an unescaped literal brace.
//
// GitHub's format() reads "{" as the start of a placeholder and requires literal braces to be
// written as "{{" and "}}". An unescaped one is an invalid format string, and the error is raised
// only when the expression is evaluated. ksail#6361 hit exactly that: the system-test matrix built
// its dispatch leg with format('[{"init":{0},...}]', inputs.init). The schedule path never
// evaluated it, so every scheduled run passed, and the first workflow_dispatch failed while
// expanding the matrix, before the system-test job was ever created. Nothing a linter reported
// flagged it.
func TestFormatStringsEscapeLiteralBraces(t *testing.T) {
	t.Parallel()

	workflows, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	require.NoError(t, err)
	require.NotEmpty(t, workflows)

	actions, err := filepath.Glob(
		filepath.Join("..", "..", ".github", "actions", "*", "action.y*ml"),
	)
	require.NoError(t, err)
	require.NotEmpty(t, actions)

	checked := 0

	for _, path := range append(workflows, actions...) {
		// The globs supply repository-owned workflow and action paths, never user input.
		contents, readErr := os.ReadFile(path) //nolint:gosec
		require.NoError(t, readErr)

		for _, literal := range formatStringLiterals(t, string(contents), path) {
			checked++

			require.NoErrorf(
				t,
				validateFormatString(literal),
				"%s passes format() an invalid format string %q; write literal braces as {{ and }}",
				filepath.Base(path),
				literal,
			)
		}
	}

	// Guard the guard: the system-test workflows build their dispatch matrix leg with format(),
	// so a scan that finds no call at all has stopped matching and would assert nothing.
	assert.GreaterOrEqual(t, checked, 2, "expected to find the system-test matrix format() calls")
}

// TestValidateFormatStringRejectsUnescapedBraces is the negative control for the check above: the
// validator must reject the exact string that broke ksail#6361 and accept its escaped form.
func TestValidateFormatStringRejectsUnescapedBraces(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{
		`[{"init":{0},"suffix":"dispatch","args":""}]`,
		`{"a":1}`,
		`trailing }`,
		`unterminated {0`,
		`not an index {x}`,
		`empty {}`,
	} {
		require.Errorf(t, validateFormatString(invalid), "must reject %q", invalid)
	}

	for _, valid := range []string{
		`[{{"init":{0},"suffix":"dispatch","args":""}}]`,
		`{0}-{1}`,
		`no placeholders`,
		`{{0}}`,
		`{0}}}`,
		``,
	} {
		require.NoErrorf(t, validateFormatString(valid), "must accept %q", valid)
	}
}

// TestFormatStringLiteralsReadsDoubledQuotes checks the literal reader against the escaping rule
// GitHub expressions use, so a quote inside a format string cannot truncate what is validated.
func TestFormatStringLiteralsReadsDoubledQuotes(t *testing.T) {
	t.Parallel()

	contents := `run: ${{ format('it''s {0} and {"x"}', github.sha) }} ${{ format( 'b{{}}' ) }}` +
		` ${{ format ('[{"init":{0}}]', inputs.init) }}`

	assert.Equal(
		t,
		[]string{`it's {0} and {"x"}`, `b{{}}`, `[{"init":{0}}]`},
		formatStringLiterals(t, contents, "fixture"),
	)
}

// formatStringLiterals returns the first-argument string literal of every format() call in
// contents. An unterminated literal fails the test rather than being skipped.
func formatStringLiterals(t *testing.T, contents, source string) []string {
	t.Helper()

	matches := formatCallOpening.FindAllStringIndex(contents, -1)
	literals := make([]string, 0, len(matches))

	for _, match := range matches {
		literal, ok := readExpressionString(contents[match[1]:])
		require.Truef(t, ok, "%s: unterminated format() string literal", source)

		literals = append(literals, literal)
	}

	return literals
}

// readExpressionString reads a GitHub expression string body up to its closing quote, decoding a
// doubled quote as one literal quote. It reports false when the closing quote is missing.
func readExpressionString(rest string) (string, bool) {
	var builder strings.Builder

	for index := 0; index < len(rest); index++ {
		if rest[index] != '\'' {
			builder.WriteByte(rest[index])

			continue
		}

		if index+1 < len(rest) && rest[index+1] == '\'' {
			builder.WriteByte('\'')

			index++

			continue
		}

		return builder.String(), true
	}

	return "", false
}

// formatStringError describes a format string GitHub would refuse to evaluate.
type formatStringError struct {
	position int
	reason   string
}

func (e formatStringError) Error() string {
	return "position " + strconv.Itoa(e.position) + ": " + e.reason
}

// validateFormatString mirrors GitHub's format() parsing: "{{" and "}}" are literal braces, "{N}"
// with a decimal N is a placeholder, and any other brace makes the string invalid.
func validateFormatString(format string) error {
	for index := 0; index < len(format); index++ {
		var err error

		switch format[index] {
		case '{':
			index, err = skipOpeningBrace(format, index)
		case '}':
			index, err = skipClosingBrace(format, index)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// skipOpeningBrace consumes the "{{" escape or "{N}" placeholder that starts at index and returns
// the index of its last byte. Any other opening brace is an error.
func skipOpeningBrace(format string, index int) (int, error) {
	if index+1 < len(format) && format[index+1] == '{' {
		return index + 1, nil
	}

	end := index + 1
	for end < len(format) && format[end] >= '0' && format[end] <= '9' {
		end++
	}

	if end == index+1 || end >= len(format) || format[end] != '}' {
		return index, formatStringError{
			position: index,
			reason:   "unescaped '{' is not a {N} placeholder",
		}
	}

	return end, nil
}

// skipClosingBrace consumes the "}}" escape that starts at index and returns the index of its
// last byte. A lone closing brace is an error.
func skipClosingBrace(format string, index int) (int, error) {
	if index+1 < len(format) && format[index+1] == '}' {
		return index + 1, nil
	}

	return index, formatStringError{position: index, reason: "unescaped '}'"}
}
