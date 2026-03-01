package marshaller_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil/marshaller"
)

// BenchmarkYAMLMarshaller_Marshal_Simple benchmarks simple model marshalling.
func BenchmarkYAMLMarshaller_Marshal_Simple(b *testing.B) {
	b.ReportAllocs()

	yamlMarshaller := marshaller.NewYAMLMarshaller[TestModel]()
	model := TestModel{Name: "test", Value: 42}

	b.ResetTimer()

	for range b.N {
		_, err := yamlMarshaller.Marshal(model)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkYAMLMarshaller_Marshal_Nested benchmarks nested model marshalling.
func BenchmarkYAMLMarshaller_Marshal_Nested(b *testing.B) {
	tests := []struct {
		name  string
		model NestedModel
	}{
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

	for _, testCase := range tests {
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()

			yamlMarshaller := marshaller.NewYAMLMarshaller[NestedModel]()

			b.ResetTimer()

			for range b.N {
				_, err := yamlMarshaller.Marshal(testCase.model)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkYAMLMarshaller_Unmarshal_Simple benchmarks simple YAML unmarshalling.
func BenchmarkYAMLMarshaller_Unmarshal_Simple(b *testing.B) {
	b.ReportAllocs()

	yamlMarshaller := marshaller.NewYAMLMarshaller[TestModel]()
	data := []byte("Name: test\nValue: 42\n")

	b.ResetTimer()

	for range b.N {
		var model TestModel

		err := yamlMarshaller.Unmarshal(data, &model)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkYAMLMarshaller_Unmarshal_Nested benchmarks nested YAML unmarshalling.
func BenchmarkYAMLMarshaller_Unmarshal_Nested(b *testing.B) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "nested",
			data: []byte("ID: parent\nInner:\n  Name: child\n  Value: 10\n"),
		},
		{
			name: "slice",
			data: []byte(
				"ID: list\nItems:\n  - Name: first\n    Value: 1\n" +
					"  - Name: second\n    Value: 2\n  - Name: third\n    Value: 3\n",
			),
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

	for _, testCase := range tests {
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()

			yamlMarshaller := marshaller.NewYAMLMarshaller[NestedModel]()

			b.ResetTimer()

			for range b.N {
				var model NestedModel

				err := yamlMarshaller.Unmarshal(testCase.data, &model)
				if err != nil {
					b.Fatal(err)
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

	for _, testCase := range tests {
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()

			yamlMarshaller := marshaller.NewYAMLMarshaller[TestModel]()

			b.ResetTimer()

			for range b.N {
				var model TestModel

				err := yamlMarshaller.UnmarshalString(testCase.data, &model)
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

	for _, testCase := range tests {
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()

			yamlMarshaller := marshaller.NewYAMLMarshaller[TestModel]()

			b.ResetTimer()

			for range b.N {
				yamlStr, err := yamlMarshaller.Marshal(testCase.model)
				if err != nil {
					b.Fatal(err)
				}

				var result TestModel

				err = yamlMarshaller.UnmarshalString(yamlStr, &result)
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

	yamlMarshaller := marshaller.NewYAMLMarshaller[NestedModel]()

	b.ResetTimer()

	for range b.N {
		yamlStr, err := yamlMarshaller.Marshal(model)
		if err != nil {
			b.Fatal(err)
		}

		var result NestedModel

		err = yamlMarshaller.UnmarshalString(yamlStr, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Helper functions for benchmark data generation.
func makeLargeSlice(size int) []TestModel {
	items := make([]TestModel, size)

	for i := range size {
		items[i] = TestModel{
			Name:  "item",
			Value: i,
		}
	}

	return items
}

func makeLargeYAML(size int) []byte {
	var builder strings.Builder

	builder.WriteString("ID: large\nItems:\n")

	for i := range size {
		fmt.Fprintf(&builder, "  - Name: item\n    Value: %d\n", i%10)
	}

	return []byte(builder.String())
}
