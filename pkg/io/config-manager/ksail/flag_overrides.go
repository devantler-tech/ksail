package configmanager

import (
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// flagValueSetter is an interface for types that can set their value from a string.
// This is typically implemented by enum types that satisfy pflag.Value.
type flagValueSetter interface {
	Set(value string) error
}

// setFieldValueFromFlag sets a field's value from a flag string representation.
// It dispatches based on the field's concrete type.
func setFieldValueFromFlag(fieldPtr any, raw string) error {
	if setter, ok := fieldPtr.(flagValueSetter); ok {
		err := setter.Set(raw)
		if err != nil {
			return fmt.Errorf("set flag value: %w", err)
		}

		return nil
	}

	switch ptr := fieldPtr.(type) {
	case *string:
		*ptr = raw

		return nil
	case *metav1.Duration:
		return setDurationFromFlag(ptr, raw)
	case *bool:
		return setBoolFromFlag(ptr, raw)
	case *int32:
		return setInt32FromFlag(ptr, raw)
	default:
		return nil
	}
}

func setDurationFromFlag(target *metav1.Duration, raw string) error {
	if raw == "" {
		target.Duration = 0

		return nil
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}

	target.Duration = duration

	return nil
}

func setBoolFromFlag(target *bool, raw string) error {
	if raw == "" {
		*target = false

		return nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("parse bool %q: %w", raw, err)
	}

	*target = value

	return nil
}

func setInt32FromFlag(target *int32, raw string) error {
	if raw == "" {
		*target = 0

		return nil
	}

	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return fmt.Errorf("parse int32 %q: %w", raw, err)
	}

	*target = int32(value)

	return nil
}
