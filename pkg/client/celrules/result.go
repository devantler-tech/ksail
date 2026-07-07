package celrules

import "fmt"

// Violation is a single rule failure against a single resource.
type Violation struct {
	// Rule is the name of the rule that was violated.
	Rule string
	// Resource identifies the offending resource (e.g. "Deployment/app" or
	// "Deployment/ns/app"), or the source name when identity is unavailable.
	Resource string
	// Message is the rule's message, or the evaluation error when the rule
	// could not be evaluated against the resource.
	Message string
	// Severity is the rule's severity.
	Severity Severity
}

// String renders a violation as "severity: rule/<name> <resource>: <message>".
func (v Violation) String() string {
	return fmt.Sprintf("%s: rule/%s %s: %s", v.Severity, v.Rule, v.Resource, v.Message)
}

// Report is the outcome of evaluating a set of rules against a set of
// documents. It carries every violation found; it is not a Go error, so
// callers decide (via HasErrors) whether to fail.
type Report struct {
	// Violations are all rule failures found, in evaluation order.
	Violations []Violation
}

// HasErrors reports whether any violation is at SeverityError.
func (r Report) HasErrors() bool {
	for _, violation := range r.Violations {
		if violation.Severity == SeverityError {
			return true
		}
	}

	return false
}

// Count returns the number of violations at the given severity.
func (r Report) Count(severity Severity) int {
	count := 0

	for _, violation := range r.Violations {
		if violation.Severity == severity {
			count++
		}
	}

	return count
}
