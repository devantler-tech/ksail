package kubernetes_test

import (
	"testing"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractGatewayPort(t *testing.T) { //nolint:funlen // table test with many Gateway status variants
	t.Parallel()

	tests := []struct {
		name     string
		status   map[string]any
		expected int32
	}{
		{
			name:     "no_status",
			status:   nil,
			expected: kubeprovider.DinDAPIServerPort,
		},
		{
			name:     "no_listeners",
			status:   map[string]any{},
			expected: kubeprovider.DinDAPIServerPort,
		},
		{
			name: "empty_listeners",
			status: map[string]any{
				"listeners": []any{},
			},
			expected: kubeprovider.DinDAPIServerPort,
		},
		{
			name: "port_as_int64",
			status: map[string]any{
				"listeners": []any{
					map[string]any{"port": int64(7001)},
				},
			},
			expected: 7001,
		},
		{
			name: "port_as_float64",
			status: map[string]any{
				"listeners": []any{
					map[string]any{"port": float64(8443)},
				},
			},
			expected: 8443,
		},
		{
			name: "port_as_string_fallback",
			status: map[string]any{
				"listeners": []any{
					map[string]any{"port": "invalid"},
				},
			},
			expected: kubeprovider.DinDAPIServerPort,
		},
		{
			name: "port_out_of_range",
			status: map[string]any{
				"listeners": []any{
					map[string]any{"port": int64(99999)},
				},
			},
			expected: kubeprovider.DinDAPIServerPort,
		},
		{
			name: "port_zero",
			status: map[string]any{
				"listeners": []any{
					map[string]any{"port": int64(0)},
				},
			},
			expected: kubeprovider.DinDAPIServerPort,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gateway := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "Gateway",
			}}
			if testCase.status != nil {
				gateway.Object["status"] = testCase.status
			}

			got := kubeprovider.ExtractGatewayPortForTest(gateway)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

//nolint:funlen // Table-driven test with multiple test cases.
func TestExtractGatewayAddressValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    map[string]any
		expected  string
		expectOK  bool
	}{
		{
			name:     "no_status",
			status:   nil,
			expected: "",
			expectOK: false,
		},
		{
			name: "no_addresses",
			status: map[string]any{
				"addresses": []any{},
			},
			expected: "",
			expectOK: false,
		},
		{
			name: "valid_ip_address",
			status: map[string]any{
				"addresses": []any{
					map[string]any{"value": "10.0.0.1"},
				},
			},
			expected: "10.0.0.1",
			expectOK: true,
		},
		{
			name: "hostname_address",
			status: map[string]any{
				"addresses": []any{
					map[string]any{"value": "gw.example.com"},
				},
			},
			expected: "gw.example.com",
			expectOK: true,
		},
		{
			name: "empty_value",
			status: map[string]any{
				"addresses": []any{
					map[string]any{"value": ""},
				},
			},
			expected: "",
			expectOK: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gateway := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "Gateway",
			}}
			if testCase.status != nil {
				gateway.Object["status"] = testCase.status
			}

			val, ok := kubeprovider.ExtractGatewayAddressValueForTest(gateway)
			assert.Equal(t, testCase.expected, val)
			assert.Equal(t, testCase.expectOK, ok)
		})
	}
}
