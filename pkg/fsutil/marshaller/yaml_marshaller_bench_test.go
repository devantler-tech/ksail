// Package marshaller_test provides benchmarks for the marshaller package.
package marshaller_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil/marshaller"
)

// BenchmarkYAMLMarshaller_Marshal benchmarks YAML marshalling performance.
func BenchmarkYAMLMarshaller_Marshal(b *testing.B) {
	tests := []struct {
		name  string
		model interface{}
	}{
		{
			name:  "simple",
			model: TestModel{Name: "test", Value: 42},
		},
		{
			name: "nested",
			model: NestedModel{
				ID:    "parent",
				Inner: &TestModel{Name: "child", Value: 10},
			},
		},
		{
			name: "slice",
			model: NestedModel{
				ID: "list",
				Items: []TestModel{
					{Name: "first", Value: 1},
					{Name: "second", Value: 2},
					{Name: "third", Value: 3},
				},
			},
		},
		{
			name: "map",
			model: NestedModel{
				ID: "with-map",
				Metadata: map[string]string{
					"key1": "val1",
					"key2": "val2",
					"key3": "val3",
				},
			},
		},
		{
			name: "large-slice",
			model: NestedModel{
				ID:    "large",
				Items: makeLargeSlice(100),
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()

			switch model := tt.model.(type) {
			case TestModel:
				m := marshaller.NewYAMLMarshaller[TestModel]()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := m.Marshal(model)
					if err != nil {
						b.Fatal(err)
					}
				}
			case NestedModel:
				m := marshaller.NewYAMLMarshaller[NestedModel]()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := m.Marshal(model)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkYAMLMarshaller_Unmarshal benchmarks YAML unmarshalling performance.
func BenchmarkYAMLMarshaller_Unmarshal(b *testing.B) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "simple",
			data: []byte("Name: test\nValue: 42\n"),
		},
		{
			name: "nested",
			data: []byte("ID: parent\nInner:\n  Name: child\n  Value: 10\n"),
		},
		{
			name: "slice",
			data: []byte("ID: list\nItems:\n  - Name: first\n    Value: 1\n  - Name: second\n    Value: 2\n  - Name: third\n    Value: 3\n"),
		},
		{
			name: "map",
			data: []byte("ID: with-map\nMetadata:\n  key1: val1\n  key2: val2\n  key3: val3\n"),
		},
		{
			name: "large-slice",
			data: makeLargeYAML(100),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()

			if tt.name == "simple" {
				m := marshaller.NewYAMLMarshaller[TestModel]()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var model TestModel
					err := m.Unmarshal(tt.data, &model)
					if err != nil {
						b.Fatal(err)
					}
				}
			} else {
				m := marshaller.NewYAMLMarshaller[NestedModel]()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var model NestedModel
					err := m.Unmarshal(tt.data, &model)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkYAMLMarshaller_UnmarshalString benchmarks string unmarshalling.
func BenchmarkYAMLMarshaller_UnmarshalString(b *testing.B) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "simple",
			data: "Name: test\nValue: 42\n",
		},
		{
			name: "multiline",
			data: "Name: |\n  multiline\n  value\nValue: 0\n",
		},
		{
			name: "whitespace",
			data: "\n\nName: test\n\nValue: 42\n\n",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			m := marshaller.NewYAMLMarshaller[TestModel]()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var model TestModel
				err := m.UnmarshalString(tt.data, &model)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkYAMLMarshaller_RoundTrip benchmarks marshal + unmarshal cycle.
func BenchmarkYAMLMarshaller_RoundTrip(b *testing.B) {
	tests := []struct {
		name  string
		model TestModel
	}{
		{
			name:  "simple",
			model: TestModel{Name: "test", Value: 42},
		},
		{
			name:  "empty",
			model: TestModel{},
		},
		{
			name:  "large-value",
			model: TestModel{Name: "test", Value: 2147483647},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			m := marshaller.NewYAMLMarshaller[TestModel]()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Marshal
				yamlStr, err := m.Marshal(tt.model)
				if err != nil {
					b.Fatal(err)
				}

				// Unmarshal
				var result TestModel
				err = m.UnmarshalString(yamlStr, &result)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkYAMLMarshaller_RoundTrip_Nested benchmarks nested structures.
func BenchmarkYAMLMarshaller_RoundTrip_Nested(b *testing.B) {
	model := NestedModel{
		ID:    "full",
		Inner: &TestModel{Name: "inner", Value: 100},
		Items: []TestModel{
			{Name: "item1", Value: 1},
			{Name: "item2", Value: 2},
			{Name: "item3", Value: 3},
		},
		Metadata: map[string]string{"a": "b", "c": "d"},
	}

	b.ReportAllocs()
	m := marshaller.NewYAMLMarshaller[NestedModel]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Marshal
		yamlStr, err := m.Marshal(model)
		if err != nil {
			b.Fatal(err)
		}

		// Unmarshal
		var result NestedModel
		err = m.UnmarshalString(yamlStr, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Helper functions for benchmark data generation
func makeLargeSlice(size int) []TestModel {
	items := make([]TestModel, size)
	for i := 0; i < size; i++ {
		items[i] = TestModel{
			Name:  "item",
			Value: i,
		}
	}
	return items
}

func makeLargeYAML(size int) []byte {
	yaml := "ID: large\nItems:\n"
	for i := 0; i < size; i++ {
		yaml += "  - Name: item\n    Value: " + string(rune('0'+i%10)) + "\n"
	}
	return []byte(yaml)
}
