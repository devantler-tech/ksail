package v1alpha1

import (
	"fmt"
	"strings"
)

// setEnum implements the shared pflag.Value Set behavior for string-based enum
// types: the input is matched case-insensitively against the canonical value
// list and, on a match, the canonical spelling is stored in target. On no
// match it returns an error wrapping errSentinel that lists the valid options,
// e.g. "invalid CNI: foo (valid options: Default, Cilium, Calico)".
func setEnum[T ~string](target *T, value string, valid []T, errSentinel error) error {
	for _, candidate := range valid {
		if strings.EqualFold(value, string(candidate)) {
			*target = candidate

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s)",
		errSentinel, value, strings.Join(validValueStrings(valid), ", "),
	)
}

// validValueStrings converts a typed enum value list to its string
// representations, preserving order. Enum ValidValues() methods use it so the
// typed ValidXxx() list stays the single canonical source of values.
func validValueStrings[T ~string](valid []T) []string {
	values := make([]string, len(valid))
	for index, value := range valid {
		values[index] = string(value)
	}

	return values
}
