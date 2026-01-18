// Package marshaller_test provides unit tests for the marshaller package.
//
//nolint:funlen // Table-driven tests are naturally long
package marshaller_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestModel struct {
	Name  string `yaml:"name"`
	Value int    `yaml:"value"`
}

// NestedModel tests marshalling nested structures.
type NestedModel struct {
	ID       string            `yaml:"id"`
	Inner    *TestModel        `yaml:"inner,omitempty"`
	Items    []TestModel       `yaml:"items,omitempty"`
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

func TestYAMLMarshaller_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    TestModel
		expected string
		wantErr  bool
	}{
		{
			name:     "marshal simple model",
			model:    TestModel{Name: "test", Value: 42},
			expected: "Name: test\nValue: 42\n",
			wantErr:  false,
		},
		{
			name:     "marshal empty model",
			model:    TestModel{},
			expected: "Name: \"\"\nValue: 0\n",
			wantErr:  false,
		},
		{
			name:     "marshal model with special characters",
			model:    TestModel{Name: "test: value", Value: 0},
			expected: "Name: 'test: value'\nValue: 0\n",
			wantErr:  false,
		},
		{
			name:     "marshal model with unicode",
			model:    TestModel{Name: "tëst™", Value: 100},
			expected: "Name: tëst™\nValue: 100\n",
			wantErr:  false,
		},
		{
			name:     "marshal model with negative value",
			model:    TestModel{Name: "negative", Value: -42},
			expected: "Name: negative\nValue: -42\n",
			wantErr:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := marshaller.NewYAMLMarshaller[TestModel]()
			got, err := m.Marshal(testCase.model)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestYAMLMarshaller_Marshal_Nested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    NestedModel
		contains []string
		wantErr  bool
	}{
		{
			name: "marshal nested model with pointer",
			model: NestedModel{
				ID:    "parent",
				Inner: &TestModel{Name: "child", Value: 10},
			},
			contains: []string{"ID: parent", "Inner:", "Name: child", "Value: 10"},
			wantErr:  false,
		},
		{
			name: "marshal nested model with nil pointer",
			model: NestedModel{
				ID:    "alone",
				Inner: nil,
			},
			contains: []string{"ID: alone"},
			wantErr:  false,
		},
		{
			name: "marshal model with slice",
			model: NestedModel{
				ID: "list",
				Items: []TestModel{
					{Name: "first", Value: 1},
					{Name: "second", Value: 2},
				},
			},
			contains: []string{"ID: list", "Items:", "Name: first", "Name: second"},
			wantErr:  false,
		},
		{
			name: "marshal model with empty slice",
			model: NestedModel{
				ID:    "empty-list",
				Items: []TestModel{},
			},
			contains: []string{"ID: empty-list"},
			wantErr:  false,
		},
		{
			name: "marshal model with map",
			model: NestedModel{
				ID:       "with-map",
				Metadata: map[string]string{"key1": "val1", "key2": "val2"},
			},
			contains: []string{"ID: with-map", "Metadata:", "key1: val1", "key2: val2"},
			wantErr:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := marshaller.NewYAMLMarshaller[NestedModel]()
			got, err := m.Marshal(testCase.model)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			for _, substr := range testCase.contains {
				assert.Contains(t, got, substr)
			}
		})
	}
}

func TestYAMLMarshaller_Unmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     []byte
		expected TestModel
		wantErr  bool
	}{
		{
			name:     "unmarshal simple YAML",
			data:     []byte("Name: test\nValue: 42\n"),
			expected: TestModel{Name: "test", Value: 42},
			wantErr:  false,
		},
		{
			name:     "unmarshal empty YAML",
			data:     []byte(""),
			expected: TestModel{},
			wantErr:  false,
		},
		{
			name:     "unmarshal invalid YAML",
			data:     []byte("invalid: [unclosed"),
			expected: TestModel{},
			wantErr:  true,
		},
		{
			name:     "unmarshal YAML with extra fields ignored",
			data:     []byte("Name: test\nValue: 42\nextra: ignored\n"),
			expected: TestModel{Name: "test", Value: 42},
			wantErr:  false,
		},
		{
			name:     "unmarshal YAML with whitespace",
			data:     []byte("\n\nName: test\n\nValue: 42\n\n"),
			expected: TestModel{Name: "test", Value: 42},
			wantErr:  false,
		},
		{
			name:     "unmarshal quoted values",
			data:     []byte("Name: \"test: value\"\nValue: 0\n"),
			expected: TestModel{Name: "test: value", Value: 0},
			wantErr:  false,
		},
		{
			name:     "unmarshal nil data",
			data:     nil,
			expected: TestModel{},
			wantErr:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := marshaller.NewYAMLMarshaller[TestModel]()

			var got TestModel

			err := m.Unmarshal(testCase.data, &got)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestYAMLMarshaller_Unmarshal_Nested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     []byte
		expected NestedModel
		wantErr  bool
	}{
		{
			name: "unmarshal nested structure",
			data: []byte("ID: parent\nInner:\n  Name: child\n  Value: 10\n"),
			expected: NestedModel{
				ID:    "parent",
				Inner: &TestModel{Name: "child", Value: 10},
			},
			wantErr: false,
		},
		{
			name: "unmarshal with slice",
			data: []byte(
				"ID: list\nItems:\n  - Name: first\n    Value: 1\n  - Name: second\n    Value: 2\n",
			),
			expected: NestedModel{
				ID: "list",
				Items: []TestModel{
					{Name: "first", Value: 1},
					{Name: "second", Value: 2},
				},
			},
			wantErr: false,
		},
		{
			name: "unmarshal with map",
			data: []byte("ID: with-map\nMetadata:\n  key1: val1\n  key2: val2\n"),
			expected: NestedModel{
				ID:       "with-map",
				Metadata: map[string]string{"key1": "val1", "key2": "val2"},
			},
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := marshaller.NewYAMLMarshaller[NestedModel]()

			var got NestedModel

			err := m.Unmarshal(testCase.data, &got)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestYAMLMarshaller_UnmarshalString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     string
		expected TestModel
		wantErr  bool
	}{
		{
			name:     "unmarshal simple YAML string",
			data:     "Name: test\nValue: 42\n",
			expected: TestModel{Name: "test", Value: 42},
			wantErr:  false,
		},
		{
			name:     "unmarshal empty string",
			data:     "",
			expected: TestModel{},
			wantErr:  false,
		},
		{
			name:     "unmarshal invalid YAML string",
			data:     "invalid: [unclosed",
			expected: TestModel{},
			wantErr:  true,
		},
		{
			name:     "unmarshal multiline string values",
			data:     "Name: |\n  multiline\n  value\nValue: 0\n",
			expected: TestModel{Name: "multiline\nvalue\n", Value: 0},
			wantErr:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := marshaller.NewYAMLMarshaller[TestModel]()

			var got TestModel

			err := m.UnmarshalString(testCase.data, &got)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestYAMLMarshaller_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model TestModel
	}{
		{
			name:  "simple model round trip",
			model: TestModel{Name: "test", Value: 42},
		},
		{
			name:  "empty model round trip",
			model: TestModel{},
		},
		{
			name:  "model with max int value",
			model: TestModel{Name: "max", Value: 2147483647},
		},
		{
			name:  "model with min int value",
			model: TestModel{Name: "min", Value: -2147483648},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			yamlMarshaller := marshaller.NewYAMLMarshaller[TestModel]()

			// Marshal to string
			yamlStr, err := yamlMarshaller.Marshal(testCase.model)
			require.NoError(t, err)

			// Unmarshal back
			var result TestModel

			err = yamlMarshaller.UnmarshalString(yamlStr, &result)
			require.NoError(t, err)

			// Verify round-trip
			assert.Equal(t, testCase.model, result)
		})
	}
}

func TestYAMLMarshaller_RoundTrip_Nested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model NestedModel
	}{
		{
			name: "nested model with all fields",
			model: NestedModel{
				ID:    "full",
				Inner: &TestModel{Name: "inner", Value: 100},
				Items: []TestModel{
					{Name: "item1", Value: 1},
					{Name: "item2", Value: 2},
				},
				Metadata: map[string]string{"a": "b", "c": "d"},
			},
		},
		{
			name: "nested model with nil pointer",
			model: NestedModel{
				ID:    "partial",
				Inner: nil,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			yamlMarshaller := marshaller.NewYAMLMarshaller[NestedModel]()

			// Marshal to string
			yamlStr, err := yamlMarshaller.Marshal(testCase.model)
			require.NoError(t, err)

			// Unmarshal back
			var result NestedModel

			err = yamlMarshaller.UnmarshalString(yamlStr, &result)
			require.NoError(t, err)

			// Verify round-trip
			assert.Equal(t, testCase.model, result)
		})
	}
}

func TestNewYAMLMarshaller_Interface(t *testing.T) {
	t.Parallel()

	// Verify NewYAMLMarshaller returns the Marshaller interface
	m := marshaller.NewYAMLMarshaller[TestModel]()
	require.NotNil(t, m)

	// Verify it can marshal
	output, err := m.Marshal(TestModel{Name: "interface-test", Value: 123})
	require.NoError(t, err)
	assert.Contains(t, output, "interface-test")
}
