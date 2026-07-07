package celrules

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// Rule is a single CEL validation rule. The Expression is evaluated with the
// rendered document bound to the variable "object" and must return a bool:
// true passes, false is a violation reported with Message at Severity.
type Rule struct {
	// Name uniquely identifies the rule in violation output and compile errors.
	Name string
	// Expression is the CEL expression evaluated against "object".
	Expression string
	// Message is the human-readable text reported when the rule is violated.
	Message string
	// Severity governs whether a violation fails validation (error) or is only
	// advisory (warning/info).
	Severity Severity
}

// ruleSpec is the on-disk shape of a rule; severity is a string that
// ParseSeverity resolves into the typed Severity.
type ruleSpec struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Message    string `json:"message"`
	Severity   string `json:"severity"`
}

// rulesFile is the on-disk shape of a rules document.
type rulesFile struct {
	Rules []ruleSpec `json:"rules"`
}

// ParseRules decodes a rules document (YAML or JSON) into typed Rules. A rule
// missing a name or expression, or declaring an unknown severity, is an error.
func ParseRules(data []byte) ([]Rule, error) {
	var file rulesFile

	err := yaml.Unmarshal(data, &file)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRulesParse, err)
	}

	rules := make([]Rule, 0, len(file.Rules))

	for index, spec := range file.Rules {
		rule, err := spec.toRule()
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", index, err)
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// LoadRules reads and parses a rules file from disk.
func LoadRules(path string) ([]Rule, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- caller-supplied rules path, read-only
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRulesParse, err)
	}

	return ParseRules(data)
}

// toRule validates a ruleSpec and converts it into a typed Rule.
func (s ruleSpec) toRule() (Rule, error) {
	if s.Name == "" {
		return Rule{}, fmt.Errorf("%w: missing name", ErrInvalidRule)
	}

	if s.Expression == "" {
		return Rule{}, fmt.Errorf("%w: rule %q missing expression", ErrInvalidRule, s.Name)
	}

	severity, err := ParseSeverity(s.Severity)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %q: %w", s.Name, err)
	}

	return Rule{
		Name:       s.Name,
		Expression: s.Expression,
		Message:    s.Message,
		Severity:   severity,
	}, nil
}
