package kubernetes_test

import (
	"testing"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

// node builds a corev1.Node with the given addresses for table-test brevity.
func node(addrs ...corev1.NodeAddress) corev1.Node {
	return corev1.Node{Status: corev1.NodeStatus{Addresses: addrs}}
}

//nolint:funlen // table test enumerating node-address selection variants
func TestFirstNodeAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodes    []corev1.Node
		addrType corev1.NodeAddressType
		expected string
	}{
		{
			name:     "no_nodes",
			nodes:    nil,
			addrType: corev1.NodeExternalIP,
			expected: "",
		},
		{
			name:     "node_without_addresses",
			nodes:    []corev1.Node{node()},
			addrType: corev1.NodeExternalIP,
			expected: "",
		},
		{
			name: "no_matching_type",
			nodes: []corev1.Node{
				node(corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.10.0.1"}),
			},
			addrType: corev1.NodeExternalIP,
			expected: "",
		},
		{
			name: "match_in_first_node",
			nodes: []corev1.Node{
				node(corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.1"}),
				node(corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.2"}),
			},
			addrType: corev1.NodeExternalIP,
			expected: "203.0.113.1",
		},
		{
			name: "match_only_in_later_node",
			nodes: []corev1.Node{
				node(corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.10.0.2"}),
				node(corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.9"}),
			},
			addrType: corev1.NodeExternalIP,
			expected: "203.0.113.9",
		},
		{
			name: "returns_first_matching_address_on_node",
			nodes: []corev1.Node{
				node(
					corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.10.0.4"},
					corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.5"},
					corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.6"},
				),
			},
			addrType: corev1.NodeExternalIP,
			expected: "203.0.113.5",
		},
		{
			name: "skips_empty_value_and_keeps_searching",
			nodes: []corev1.Node{
				node(corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: ""}),
				node(corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.7"}),
			},
			addrType: corev1.NodeExternalIP,
			expected: "203.0.113.7",
		},
		{
			name: "selects_requested_type_over_others",
			nodes: []corev1.Node{
				node(
					corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "203.0.113.8"},
					corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.10.0.3"},
				),
			},
			addrType: corev1.NodeInternalIP,
			expected: "10.10.0.3",
		},
		{
			name: "matches_hostname_type",
			nodes: []corev1.Node{
				node(corev1.NodeAddress{Type: corev1.NodeHostName, Address: "node-1.example"}),
			},
			addrType: corev1.NodeHostName,
			expected: "node-1.example",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := kubeprovider.FirstNodeAddressForTest(testCase.nodes, testCase.addrType)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestToAnyMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]any
	}{
		{
			name:     "nil_input",
			input:    nil,
			expected: map[string]any{},
		},
		{
			name:     "empty_input",
			input:    map[string]string{},
			expected: map[string]any{},
		},
		{
			name:     "single_entry",
			input:    map[string]string{"app": "demo-app"},
			expected: map[string]any{"app": "demo-app"},
		},
		{
			name: "multiple_entries_all_preserved",
			input: map[string]string{
				"app":                          "frontend",
				"app.kubernetes.io/managed-by": "helm",
				"tier":                         "backend",
			},
			expected: map[string]any{
				"app":                          "frontend",
				"app.kubernetes.io/managed-by": "helm",
				"tier":                         "backend",
			},
		},
		{
			name:     "special_characters_preserved",
			input:    map[string]string{"key/with.special_chars": "value:with/slash"},
			expected: map[string]any{"key/with.special_chars": "value:with/slash"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := kubeprovider.ToAnyMapForTest(testCase.input)
			assert.Equal(t, testCase.expected, got)
			// Result must always be non-nil so downstream unstructured builders can rely on it.
			assert.NotNil(t, got)
		})
	}
}
