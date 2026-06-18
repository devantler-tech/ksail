package v1alpha1

import (
	"fmt"
	"strconv"
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

// setToggleEnum is setEnum for the Enabled/Disabled toggle enums that also
// accept a legacy boolean spelling on the CLI: "true"/"false" (any case
// strconv.ParseBool recognizes) coerce to Enabled/Disabled before the canonical
// match runs. This mirrors the config-load bool-alias decode hook so a flag like
// --node-autoscaler-enabled=true (whose field was a bool before the toggle
// migration) keeps working through the deprecation window.
func setToggleEnum[T ~string](target *T, value string, valid []T, errSentinel error) error {
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		value = BoolToToggleValue(parsed)
	}

	return setEnum(target, value, valid, errSentinel)
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
