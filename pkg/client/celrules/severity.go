package celrules

import (
	"fmt"
	"strings"
)

// Severity classifies how a rule violation is treated.
type Severity int

const (
	// SeverityError marks a violation that fails validation. It is the default
	// when a rule omits an explicit severity.
	SeverityError Severity = iota
	// SeverityWarning marks an advisory violation that is reported but does not
	// fail validation.
	SeverityWarning
	// SeverityInfo marks an informational violation, reported but never fatal.
	SeverityInfo
)

// String returns the lower-case name of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// ParseSeverity converts a case-insensitive severity name into a Severity. An
// empty string defaults to SeverityError; any other unknown value is an error.
func ParseSeverity(name string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "error":
		return SeverityError, nil
	case "warning", "warn":
		return SeverityWarning, nil
	case "info":
		return SeverityInfo, nil
	default:
		return SeverityError, fmt.Errorf("%w: %q", ErrUnknownSeverity, name)
	}
}
