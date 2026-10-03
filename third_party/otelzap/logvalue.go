package otelzap

import (
	"encoding/json"
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel/attribute"
)

// logValue retains otelutil v0.3.2's LogValue conversion using OTel's shared
// attribute values. The original converter is covered by the retained BSD license.
// Arrays are indexed directly because slicing an unaddressable array panics.
func logValue(value interface{}) attribute.Value {
	switch value := value.(type) {
	case nil:
		return attribute.StringValue("<nil>")
	case string:
		return attribute.StringValue(value)
	case int:
		return attribute.IntValue(value)
	case int64:
		return attribute.Int64Value(value)
	case uint64:
		return attribute.Int64Value(int64(value))
	case float64:
		return attribute.Float64Value(value)
	case bool:
		return attribute.BoolValue(value)
	case fmt.Stringer:
		return attribute.StringValue(value.String())
	}

	rv := reflect.ValueOf(value)

	switch rv.Kind() {
	case reflect.Array, reflect.Slice:
		values := make([]attribute.Value, rv.Len())
		for i := range values {
			values[i] = logValue(rv.Index(i).Interface())
		}
		return attribute.SliceValue(values...)
	case reflect.Bool:
		return attribute.BoolValue(rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return attribute.Int64Value(rv.Int())
	case reflect.Float64:
		return attribute.Float64Value(rv.Float())
	case reflect.String:
		return attribute.StringValue(rv.String())
	}
	if b, err := json.Marshal(value); err == nil {
		return attribute.StringValue(string(b))
	}
	return attribute.StringValue(fmt.Sprint(value))
}
