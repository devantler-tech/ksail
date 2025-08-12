package helpers

import (
	"testing"
)

func TestInputOrFallback_Primitives(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		if got := InputOrFallback("", "fallback"); got != "fallback" {
			t.Fatalf("expected 'fallback', got '%q'", got)
		}
		if got := InputOrFallback("value", "fallback"); got != "value" {
			t.Fatalf("expected 'value', got '%q'", got)
		}
	})
	t.Run("bool", func(t *testing.T) {
		if got := InputOrFallback(false, true); got != true {
			t.Fatalf("expected 'false', got '%v'", got)
		}
		if got := InputOrFallback(true, false); got != true {
			t.Fatalf("expected 'true', got '%v'", got)
		}
	})
	t.Run("int variants", func(t *testing.T) {
		if got := InputOrFallback(0, 7); got != 7 {
			t.Fatalf("int zero: expected '7', got '%d'", got)
		}
		if got := InputOrFallback(5, 7); got != 5 {
			t.Fatalf("int non-zero: expected '5', got '%d'", got)
		}
	})
	t.Run("float variants", func(t *testing.T) {
		if got := InputOrFallback(0, 1.5); got != 1.5 {
			t.Fatalf("float32 zero: expected '1.5', got '%v'", got)
		}
		if got := InputOrFallback(2.5, 1.5); got != 2.5 {
			t.Fatalf("float32 non-zero: expected '2.5', got '%v'", got)
		}
		if got := InputOrFallback(0, 3.25); got != 3.25 {
			t.Fatalf("float64 zero: expected '3.25', got '%v'", got)
		}
		if got := InputOrFallback(4.75, 3.25); got != 4.75 {
			t.Fatalf("float64 non-zero: expected '4.75', got '%v'", got)
		}
	})
	t.Run("complex variants", func(t *testing.T) {
		if got := InputOrFallback(0+0i, 1+2i); got != 1+2i {
			t.Fatalf("complex64 zero: expected '(1+2i)', got '%v'", got)
		}
		if got := InputOrFallback(3+4i, 1+2i); got != 3+4i {
			t.Fatalf("complex64 non-zero: expected '(3+4i)', got '%v'", got)
		}
		if got := InputOrFallback(0+0i, 5+6i); got != 5+6i {
			t.Fatalf("complex128 zero: expected '(5+6i)', got '%v'", got)
		}
		if got := InputOrFallback(7+8i, 5+6i); got != 7+8i {
			t.Fatalf("complex128 non-zero: expected '(7+8i)', got '%v'", got)
		}
	})
	t.Run("struct", func(t *testing.T) {
		type testStruct struct {
			Field string
		}
		if got := InputOrFallback(testStruct{}, testStruct{Field: "fallback"}); got != (testStruct{Field: "fallback"}) {
			t.Fatalf("expected 'fallback', got '%v'", got)
		}
		if got := InputOrFallback(testStruct{Field: "value"}, testStruct{Field: "fallback"}); got != (testStruct{Field: "value"}) {
			t.Fatalf("expected 'value', got '%v'", got)
		}
	})
	t.Run("enum", func(t *testing.T) {
		type testEnum string
		const (
			EnumZero testEnum = "Value1"
			EnumOne  testEnum = "Value2"
		)
		if got := InputOrFallback(EnumZero, EnumOne); got != EnumZero {
			t.Fatalf("expected '0', got '%v'", got)
		}
		if got := InputOrFallback(EnumOne, EnumZero); got != EnumOne {
			t.Fatalf("expected '1', got '%v'", got)
		}
	})
}
