package chat //nolint:testpackage // white-box tests for unexported functions

import (
	"bytes"
	"io"
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// longDiff is a diff string exceeding 200 characters, used to test truncation.
var longDiff = strings.Repeat("x", 250) //nolint:gochecknoglobals // test data

func TestIsReadOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     copilot.PermissionRequestKind
		expected bool
	}{
		{"read kind", copilot.PermissionRequestKindRead, true},
		{"url kind", copilot.PermissionRequestKindURL, true},
		{"write kind", copilot.PermissionRequestKindWrite, false},
		{"shell kind", copilot.PermissionRequestKindShell, false},
		{"extension management kind", copilot.PermissionRequestKindExtensionManagement, false},
		{
			"extension permission access kind",
			copilot.PermissionRequestKindExtensionPermissionAccess,
			false,
		},
		{"empty kind", "", false},
		{"unknown kind", "unknown", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := IsReadOperation(tc.kind)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetPermissionDescription_BasicFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  copilot.PermissionRequest
		expected string
	}{
		{
			name:    "no typed fields set",
			request: &copilot.PermissionRequestWrite{},
		},
		{
			name: "with tool name",
			request: &copilot.PermissionRequestCustomTool{
				ToolName: "ksail_cluster_create",
			},
			expected: "Tool: ksail_cluster_create",
		},
		{
			name: "with path",
			request: &copilot.PermissionRequestRead{
				Path: "/tmp/test.yaml",
			},
			expected: "Path: /tmp/test.yaml",
		},
		{
			name: "with full command text",
			request: &copilot.PermissionRequestShell{
				FullCommandText: "rm -rf /tmp/test",
			},
			expected: "$ rm -rf /tmp/test",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := getPermissionDescription(testCase.request)
			if testCase.expected == "" {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, testCase.expected)
			}
		})
	}
}

func TestGetPermissionDescription_Extension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  copilot.PermissionRequest
		expected string
	}{
		{
			name: "extension management with name",
			request: &copilot.PermissionRequestExtensionManagement{
				ExtensionName: new("my-extension"),
				Operation:     "scaffold",
			},
			expected: "Operation: scaffold\nExtension: my-extension",
		},
		{
			name: "extension management without name",
			request: &copilot.PermissionRequestExtensionManagement{
				Operation: "reload",
			},
			expected: "Operation: reload",
		},
		{
			name: "extension permission access",
			request: &copilot.PermissionRequestExtensionPermissionAccess{
				ExtensionName: "my-extension",
				Capabilities:  []string{"read"},
			},
			expected: "Extension: my-extension",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := getPermissionDescription(testCase.request)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestGetPermissionDescription_DiffPreview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  copilot.PermissionRequest
		expected string
	}{
		{
			name: "short diff",
			request: &copilot.PermissionRequestWrite{
				Diff: "- old line\n+ new line",
			},
			expected: "Diff:\n- old line\n+ new line",
		},
		{
			name: "truncated diff",
			request: &copilot.PermissionRequestWrite{
				Diff: longDiff,
			},
			expected: "Diff:\n" + longDiff[:200] + "...",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := getPermissionDescription(testCase.request)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestReadPermissionResponseFrom_Approve(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"y\n", "yes\n", " Y \n", "YES\n"} {
		t.Run(strings.TrimSpace(input), func(t *testing.T) {
			t.Parallel()

			writer := &bytes.Buffer{}

			decision, err := readPermissionResponseFrom(writer, strings.NewReader(input))
			require.NoError(t, err)
			assert.Equal(t, rpc.PermissionDecisionKindApproveOnce, decision.Kind())
		})
	}
}

func TestReadPermissionResponseFrom_DenyWithoutFeedback(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"\n", "n\n", "no\n", "  \n", "NO\n"} {
		t.Run(strings.TrimSpace(input), func(t *testing.T) {
			t.Parallel()

			writer := &bytes.Buffer{}

			decision, err := readPermissionResponseFrom(writer, strings.NewReader(input))
			require.NoError(t, err)

			reject, ok := decision.(*rpc.PermissionDecisionReject)
			require.True(t, ok, "expected a reject decision, got %T", decision)
			assert.Nil(t, reject.Feedback, "expected no feedback for a plain denial")
		})
	}
}

func TestReadPermissionResponseFrom_DenyWithFeedback(t *testing.T) {
	t.Parallel()

	writer := &bytes.Buffer{}

	decision, err := readPermissionResponseFrom(
		writer, strings.NewReader("  this command is too risky  \n"),
	)
	require.NoError(t, err)

	reject, ok := decision.(*rpc.PermissionDecisionReject)
	require.True(t, ok, "expected a reject decision, got %T", decision)
	require.NotNil(t, reject.Feedback, "expected feedback to be set for a typed reason")
	assert.Equal(t, "this command is too risky", *reject.Feedback)
}

func TestReadPermissionResponseFrom_EOF(t *testing.T) {
	t.Parallel()

	writer := &bytes.Buffer{}

	// An empty reader yields EOF immediately, which is treated as "user not available".
	decision, err := readPermissionResponseFrom(writer, strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, rpc.PermissionDecisionKindUserNotAvailable, decision.Kind())
}

// staticErrReader returns a non-EOF error on Read to exercise the I/O-error path.
type staticErrReader struct{}

func (staticErrReader) Read([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestReadPermissionResponseFrom_ReadError(t *testing.T) {
	t.Parallel()

	writer := &bytes.Buffer{}

	decision, err := readPermissionResponseFrom(writer, staticErrReader{})
	require.NoError(t, err)
	assert.Equal(t, rpc.PermissionDecisionKindUserNotAvailable, decision.Kind())
}
