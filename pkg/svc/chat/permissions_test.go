package chat //nolint:testpackage // white-box tests for unexported functions

import (
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
)

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

func TestGetPermissionDescription(t *testing.T) {
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
			expected: "",
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := getPermissionDescription(tc.request)
			assert.Contains(t, result, tc.expected)
		})
	}
}
