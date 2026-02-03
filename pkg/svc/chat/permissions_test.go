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
		kind     string
		expected bool
	}{
		{"read kind", "read", true},
		{"url kind", "url", true},
		{"write kind", "write", false},
		{"execute kind", "execute", false},
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
			name: "nil extra",
			request: copilot.PermissionRequest{
				Kind:  "write",
				Extra: nil,
			},
			expected: "",
		},
		{
			name: "with tool name",
			request: copilot.PermissionRequest{
				Kind: "write",
				Extra: map[string]any{
					"toolName": "ksail_cluster_create",
				},
			},
			expected: "Tool: ksail_cluster_create",
		},
		{
			name: "with command",
			request: copilot.PermissionRequest{
				Kind: "execute",
				Extra: map[string]any{
					"command": "ls -la",
				},
			},
			expected: "$ ls -la",
		},
		{
			name: "with path",
			request: copilot.PermissionRequest{
				Kind: "write",
				Extra: map[string]any{
					"path": "/tmp/test.yaml",
				},
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
