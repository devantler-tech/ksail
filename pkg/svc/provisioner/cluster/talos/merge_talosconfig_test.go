package talosprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

// talosconfigFixture is a minimal valid talosconfig with one context.
func talosconfigFixture(contextName string, endpoint string) []byte {
	return []byte(`context: ` + contextName + `
contexts:
  ` + contextName + `:
    endpoints:
      - ` + endpoint + `
    ca: dGVzdC1jYQ==
    crt: dGVzdC1jcnQ=
    key: dGVzdC1rZXk=
`)
}

// talosconfigParsed is a helper struct for asserting talosconfig contents.
type talosconfigParsed struct {
	Context  string                    `yaml:"context"`
	Contexts map[string]talosconfigCtx `yaml:"contexts"`
}

type talosconfigCtx struct {
	Endpoints []string `yaml:"endpoints"`
	CA        string   `yaml:"ca,omitempty"`
	Crt       string   `yaml:"crt,omitempty"`
	Key       string   `yaml:"key,omitempty"`
}

func loadTalosconfigForTest(t *testing.T, path string) talosconfigParsed {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)

	var parsed talosconfigParsed
	require.NoError(t, yaml.Unmarshal(data, &parsed))

	return parsed
}

func TestMergeTalosconfigBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		existingContent  []byte
		newContent       []byte
		wantContexts     []string
		wantCurrentCtx   string
		wantEndpoint     map[string]string
		useNestedDir     bool
	}{
		{
			name:            "no existing file creates new talosconfig",
			existingContent: nil,
			newContent:      talosconfigFixture("cluster-a", "10.0.0.1"),
			wantContexts:    []string{"cluster-a"},
			wantCurrentCtx:  "cluster-a",
			wantEndpoint:    map[string]string{"cluster-a": "10.0.0.1"},
		},
		{
			name:            "existing contexts from other clusters are preserved",
			existingContent: talosconfigFixture("cluster-a", "10.0.0.1"),
			newContent:      talosconfigFixture("cluster-b", "10.0.0.2"),
			wantContexts:    []string{"cluster-a", "cluster-b"},
			wantCurrentCtx:  "cluster-b",
			wantEndpoint: map[string]string{
				"cluster-a": "10.0.0.1",
				"cluster-b": "10.0.0.2",
			},
		},
		{
			name:            "same-named context gets suffix to avoid collision",
			existingContent: talosconfigFixture("cluster-a", "10.0.0.1"),
			newContent:      talosconfigFixture("cluster-a", "10.0.0.99"),
			// Talos SDK Merge renames colliding contexts with a -N suffix
			wantContexts:   []string{"cluster-a", "cluster-a-1"},
			wantCurrentCtx: "cluster-a-1",
			wantEndpoint: map[string]string{
				"cluster-a":   "10.0.0.1",
				"cluster-a-1": "10.0.0.99",
			},
		},
		{
			name:            "creates parent directory if missing",
			existingContent: nil,
			newContent:      talosconfigFixture("cluster-a", "10.0.0.1"),
			useNestedDir:    true,
			wantContexts:    []string{"cluster-a"},
			wantCurrentCtx:  "cluster-a",
			wantEndpoint:    map[string]string{"cluster-a": "10.0.0.1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runMergeTalosconfigTest(t, tc.existingContent, tc.newContent, tc.useNestedDir,
				tc.wantContexts, tc.wantCurrentCtx, tc.wantEndpoint)
		})
	}
}

func runMergeTalosconfigTest(
	t *testing.T,
	existingContent, newContent []byte,
	useNestedDir bool,
	wantContexts []string,
	wantCurrentCtx string,
	wantEndpoint map[string]string,
) {
	t.Helper()

	tmpDir := t.TempDir()

	var talosconfigPath string
	if useNestedDir {
		talosconfigPath = filepath.Join(tmpDir, "nested", "deep", "talosconfig")
	} else {
		talosconfigPath = filepath.Join(tmpDir, "talosconfig")
	}

	if existingContent != nil {
		if useNestedDir {
			require.NoError(t, os.MkdirAll(filepath.Dir(talosconfigPath), 0o700))
		}
		require.NoError(t, os.WriteFile(talosconfigPath, existingContent, 0o600))
	}

	err := talosprovisioner.MergeTalosconfigBytesForTest(talosconfigPath, newContent)
	require.NoError(t, err)

	parsed := loadTalosconfigForTest(t, talosconfigPath)

	contextNames := make([]string, 0, len(parsed.Contexts))
	for name := range parsed.Contexts {
		contextNames = append(contextNames, name)
	}
	assert.ElementsMatch(t, wantContexts, contextNames, "contexts")

	assert.Equal(t, wantCurrentCtx, parsed.Context, "current context")

	for ctxName, wantEP := range wantEndpoint {
		ctx, ok := parsed.Contexts[ctxName]
		require.True(t, ok, "context %q should exist", ctxName)
		require.NotEmpty(t, ctx.Endpoints, "context %q endpoints", ctxName)
		assert.Equal(t, wantEP, ctx.Endpoints[0], "context %q first endpoint", ctxName)
	}
}

func TestMergeTalosconfigBytes_InvalidNewData(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "talosconfig")

	err := talosprovisioner.MergeTalosconfigBytesForTest(path, []byte("not valid yaml {{{"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse new talosconfig")
}
