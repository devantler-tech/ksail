// Package celrules provides the CEL rule engine backing native semantic
// validation of rendered GitOps manifests (issue #5693, epic #5344): a YAML
// rules-file schema, per-rule expression compilation, and an evaluation loop
// producing attributable violations. It is deliberately decoupled from the
// render pipeline and CLI — callers hand it decoded documents — so the engine
// stays unit-testable and the validate wiring can evolve independently.
package celrules

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
	"sigs.k8s.io/yaml"
)

// Severity classifies how a violated rule is reported.
type Severity string

const (
	// SeverityError marks a violation that should fail validation.
	SeverityError Severity = "error"
	// SeverityWarning marks a violation that should be reported without
	// failing validation.
	SeverityWarning Severity = "warning"
)

// objectVarName is the CEL variable each rendered document is bound to,
// matching the convention users know from Kubernetes ValidatingAdmissionPolicy.
const objectVarName = "object"

var (
	errNoRules            = errors.New("rules file declares no rules")
	errRuleNameEmpty      = errors.New("rule has no name")
	errRuleNameDuplicate  = errors.New("duplicate rule name")
	errRuleExpressionOnly = errors.New("rule has no expression")
	errRuleSeverity       = errors.New("rule has invalid severity (want error or warning)")
	errRuleNotBool        = errors.New("rule expression must evaluate to a boolean")
)

// Rule is one semantic check evaluated against every rendered document.
type Rule struct {
	// Name uniquely identifies the rule in reports.
	Name string `json:"name"`
	// Expression is a CEL expression over `object` that must evaluate to
	// true for a document to pass.
	Expression string `json:"expression"`
	// Message optionally overrides the violation text.
	Message string `json:"message,omitempty"`
	// Severity is error (default) or warning.
	Severity Severity `json:"severity,omitempty"`
}

// RulesFile is the on-disk YAML schema of a rules file.
type RulesFile struct {
	Rules []Rule `json:"rules"`
}

// Violation reports one rule failing against one document.
type Violation struct {
	// Rule is the violated rule's name.
	Rule string
	// Message is the rule's message, or a synthesized description when the
	// rule declares none or its evaluation errored.
	Message string
	// Severity mirrors the rule's severity.
	Severity Severity
}

// LoadRules reads and validates a YAML rules file. The severity of rules that
// omit one defaults to error.
func LoadRules(path string) ([]Rule, error) {
	//nolint:gosec // the rules file is user-project input by design
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules file: %w", err)
	}

	var file RulesFile

	err = yaml.UnmarshalStrict(content, &file)
	if err != nil {
		return nil, fmt.Errorf("parse rules file %s: %w", path, err)
	}

	err = validateRules(file.Rules)
	if err != nil {
		return nil, fmt.Errorf("invalid rules file %s: %w", path, err)
	}

	for index := range file.Rules {
		if file.Rules[index].Severity == "" {
			file.Rules[index].Severity = SeverityError
		}
	}

	return file.Rules, nil
}

// validateRules enforces the rules-file invariants: at least one rule, unique
// non-empty names, non-empty expressions, and a known severity.
func validateRules(rules []Rule) error {
	if len(rules) == 0 {
		return errNoRules
	}

	seen := make(map[string]struct{}, len(rules))

	for index, rule := range rules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			return fmt.Errorf("%w (index %d)", errRuleNameEmpty, index)
		}

		_, duplicate := seen[name]
		if duplicate {
			return fmt.Errorf("%w: %s", errRuleNameDuplicate, name)
		}

		seen[name] = struct{}{}

		if strings.TrimSpace(rule.Expression) == "" {
			return fmt.Errorf("%w: %s", errRuleExpressionOnly, name)
		}

		switch rule.Severity {
		case "", SeverityError, SeverityWarning:
		default:
			return fmt.Errorf("%w: %s has %q", errRuleSeverity, name, rule.Severity)
		}
	}

	return nil
}

// compiledRule pairs a rule with its compiled CEL program.
type compiledRule struct {
	rule    Rule
	program cel.Program
}

// Engine evaluates a compiled rule set against rendered documents.
type Engine struct {
	rules []compiledRule
}

// NewEngine compiles every rule's expression against a CEL environment that
// binds each document as `object` (dynamic type, ValidatingAdmissionPolicy
// style) with the strings extension library. Compilation errors name the
// offending rule so a broken rules file is diagnosable from the error alone.
func NewEngine(rules []Rule) (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Variable(objectVarName, cel.DynType),
		ext.Strings(),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL environment: %w", err)
	}

	compiled := make([]compiledRule, 0, len(rules))

	for _, rule := range rules {
		program, err := compileRule(env, rule)
		if err != nil {
			return nil, err
		}

		compiled = append(compiled, compiledRule{rule: rule, program: program})
	}

	return &Engine{rules: compiled}, nil
}

// compileRule compiles one rule and enforces a boolean output type.
func compileRule(env *cel.Env, rule Rule) (cel.Program, error) {
	ast, issues := env.Compile(rule.Expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile rule %s: %w", rule.Name, issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf(
			"%w: %s returns %s", errRuleNotBool, rule.Name, ast.OutputType(),
		)
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("build program for rule %s: %w", rule.Name, err)
	}

	return program, nil
}

// Evaluate runs every rule against one decoded document and returns the
// violations. An expression evaluating to false violates its rule; an
// evaluation error (e.g. selecting a missing field without has()) also
// violates it — fail-closed, matching ValidatingAdmissionPolicy semantics —
// with the evaluation error carried in the message.
func (e *Engine) Evaluate(doc map[string]any) []Violation {
	violations := make([]Violation, 0)

	for _, compiled := range e.rules {
		result, _, err := compiled.program.Eval(map[string]any{objectVarName: doc})
		if err != nil {
			violations = append(violations, Violation{
				Rule:     compiled.rule.Name,
				Message:  fmt.Sprintf("rule evaluation failed: %v", err),
				Severity: compiled.rule.Severity,
			})

			continue
		}

		if result == types.True {
			continue
		}

		violations = append(violations, newViolation(compiled.rule))
	}

	return violations
}

// newViolation builds the violation for a rule whose expression returned false.
func newViolation(rule Rule) Violation {
	message := rule.Message
	if message == "" {
		message = "expression evaluated to false: " + rule.Expression
	}

	return Violation{
		Rule:     rule.Name,
		Message:  message,
		Severity: rule.Severity,
	}
}
