package celrules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/celrules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validRulesYAML = `rules:
  - name: no-latest-tag
    expression: "!object.metadata.name.endsWith('latest')"
    message: "avoid the latest suffix"
    severity: warning
  - name: is-deployment
    expression: "object.kind == 'Deployment'"
    message: "must be a Deployment"
`

func TestParseRules_Valid(t *testing.T) {
	t.Parallel()

	rules, err := celrules.ParseRules([]byte(validRulesYAML))

	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "no-latest-tag", rules[0].Name)
	assert.Equal(t, celrules.SeverityWarning, rules[0].Severity)
	assert.Equal(t, "is-deployment", rules[1].Name)
	// omitted severity defaults to error.
	assert.Equal(t, celrules.SeverityError, rules[1].Severity)
}

func TestParseRules_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr error
	}{
		{
			name:    "missing name",
			yaml:    "rules:\n  - expression: \"true\"\n",
			wantErr: celrules.ErrInvalidRule,
		},
		{
			name:    "missing expression",
			yaml:    "rules:\n  - name: r\n",
			wantErr: celrules.ErrInvalidRule,
		},
		{
			name:    "unknown severity",
			yaml:    "rules:\n  - name: r\n    expression: \"true\"\n    severity: fatal\n",
			wantErr: celrules.ErrUnknownSeverity,
		},
		{
			name:    "malformed yaml",
			yaml:    "rules: : :\n  bad",
			wantErr: celrules.ErrRulesParse,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := celrules.ParseRules([]byte(testCase.yaml))

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

func TestParseRules_Empty(t *testing.T) {
	t.Parallel()

	rules, err := celrules.ParseRules([]byte("rules: []\n"))

	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestLoadRules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validRulesYAML), 0o600))

	rules, err := celrules.LoadRules(path)

	require.NoError(t, err)
	assert.Len(t, rules, 2)
}

func TestLoadRules_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := celrules.LoadRules(filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	require.ErrorIs(t, err, celrules.ErrRulesParse)
}
