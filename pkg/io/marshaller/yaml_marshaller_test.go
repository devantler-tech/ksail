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
