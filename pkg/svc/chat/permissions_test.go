package chat //nolint:testpackage // white-box tests for unexported functions

import (
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
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
		{"read kind", copilot.Read, true},
		{"url kind", copilot.URL, true},
		{"write kind", copilot.Write, false},
		{"shell kind", copilot.KindShell, false},
		{"empty kind", "", false},
		{"unknown kind", "unknown", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := isReadOperation(tc.kind)
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
			name: "no typed fields set",
			request: copilot.PermissionRequest{
				Kind: copilot.Write,
			},
		},
		{
			name: "with tool name",
			request: copilot.PermissionRequest{
				Kind:     copilot.Write,
				ToolName: new("ksail_cluster_create"),
			},
			expected: "Tool: ksail_cluster_create",
		},
		{
			name: "with path",
			request: copilot.PermissionRequest{
				Kind: copilot.Write,
				Path: new("/tmp/test.yaml"),
			},
			expected: "Path: /tmp/test.yaml",
		},
		{
			name: "with full command text",
			request: copilot.PermissionRequest{
				Kind:            copilot.KindShell,
				FullCommandText: new("rm -rf /tmp/test"),
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

func TestGetPermissionDescription_DiffPreview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  copilot.PermissionRequest
		expected string
	}{
		{
			name: "short diff",
			request: copilot.PermissionRequest{
				Kind: copilot.Write,
				Diff: new("- old line\n+ new line"),
			},
			expected: "Diff:\n- old line\n+ new line",
		},
		{
			name: "truncated diff",
			request: copilot.PermissionRequest{
				Kind: copilot.Write,
				Diff: new(longDiff),
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
