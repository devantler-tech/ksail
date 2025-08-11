package helpers

import (
	"reflect"
)

// InputOrFallback returns input if not zero value, otherwise InputOrFallback.
func InputOrFallback[T comparable](input, fallback T) T {
	if !reflect.DeepEqual(input, *new(T)) {
		return input
	}
	return fallback
}