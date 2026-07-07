package celrules

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"sigs.k8s.io/yaml"
)

// objectVar is the CEL variable each rendered document is bound to.
const objectVar = "object"

// errEmptyDocument marks a blank or null document, skipped during evaluation.
var errEmptyDocument = errors.New("empty document")

// Client evaluates CEL rules against rendered Kubernetes manifests.
type Client struct{}

// NewClient creates a new CEL rules client.
func NewClient() *Client {
	return &Client{}
}

// Options tunes a validation run. A nil *Options is valid (all defaults).
type Options struct {
	// Attribution maps a resource identity (e.g. "Deployment/ns/app") to a
	// source descriptor (e.g. "HelmRelease flux-system/app"); when present it is
	// appended to the resource label in violations. Optional.
	Attribution map[string]string
}

// compiledRule pairs a rule with its compiled CEL program.
type compiledRule struct {
	rule    Rule
	program cel.Program
}

// Validate compiles the rules and evaluates each against every document,
// returning a Report of violations. A compile failure (a broken rules file)
// returns ErrRuleCompilation naming the offending rule; violations themselves
// are not Go errors — callers decide via Report.HasErrors.
func (c *Client) Validate(
	ctx context.Context,
	rules []Rule,
	documents [][]byte,
	opts *Options,
) (Report, error) {
	if len(rules) == 0 || len(documents) == 0 {
		return Report{}, nil
	}

	compiled, err := c.compile(rules)
	if err != nil {
		return Report{}, err
	}

	var report Report

	for _, document := range documents {
		violations, err := evaluateDocument(ctx, compiled, document, opts)
		if err != nil {
			return Report{}, err
		}

		report.Violations = append(report.Violations, violations...)
	}

	return report, nil
}

// evaluateDocument parses one manifest and evaluates every compiled rule against
// it. A blank/null document yields no violations; a malformed manifest or a
// cancelled context returns an error that aborts the whole run rather than
// silently passing broken input or recording a spurious violation.
func evaluateDocument(
	ctx context.Context,
	compiled []compiledRule,
	document []byte,
	opts *Options,
) ([]Violation, error) {
	// context.Cause preserves a custom cancellation cause (from WithCancelCause/
	// WithTimeoutCause) and falls back to the generic Canceled/DeadlineExceeded
	// error when none is set.
	err := context.Cause(ctx)
	if err != nil {
		return nil, fmt.Errorf("validating documents: %w", err)
	}

	object, identity, parseErr := parseDocument(document)
	if parseErr != nil {
		if errors.Is(parseErr, errEmptyDocument) {
			return nil, nil
		}

		return nil, parseErr
	}

	resource := attributedResource(identity, opts)

	var violations []Violation

	for _, rule := range compiled {
		violation, violated := evalRule(ctx, rule, object, resource)

		// A cancelled context surfaces from ContextEval as an evaluation error;
		// abort with the cancellation cause (context.Cause preserves a custom
		// cause) instead of recording it as a spurious violation and continuing.
		err := context.Cause(ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluating rules: %w", err)
		}

		if violated {
			violations = append(violations, violation)
		}
	}

	return violations, nil
}

// compile builds a CEL program for each rule, rejecting a non-boolean
// expression (or a syntax/type error) at compile time with the rule named.
func (c *Client) compile(rules []Rule) ([]compiledRule, error) {
	env, err := newEnv()
	if err != nil {
		return nil, err
	}

	compiled := make([]compiledRule, 0, len(rules))

	for _, rule := range rules {
		ast, issues := env.Compile(rule.Expression)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("%w: rule %q: %w", ErrRuleCompilation, rule.Name, issues.Err())
		}

		if outputType := ast.OutputType().String(); outputType != "bool" && outputType != "dyn" {
			return nil, fmt.Errorf(
				"%w: rule %q: expression must return bool, got %s",
				ErrRuleCompilation, rule.Name, outputType,
			)
		}

		program, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("%w: rule %q: %w", ErrRuleCompilation, rule.Name, err)
		}

		compiled = append(compiled, compiledRule{rule: rule, program: program})
	}

	return compiled, nil
}

// newEnv builds the CEL environment: "object" bound as a dynamic map plus the
// cel-go string extensions users expect from Kubernetes CEL.
func newEnv() (*cel.Env, error) {
	env, err := cel.NewEnv(
		cel.Variable(objectVar, cel.MapType(cel.StringType, cel.DynType)),
		ext.Strings(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: building CEL environment: %w", ErrRuleCompilation, err)
	}

	return env, nil
}

// evalRule evaluates one compiled rule against one object. It returns a
// violation (and true) when the rule fails, evaluates to a non-bool, or errors
// at runtime; otherwise it returns false.
func evalRule(
	ctx context.Context,
	compiled compiledRule,
	object map[string]any,
	resource string,
) (Violation, bool) {
	out, _, err := compiled.program.ContextEval(ctx, map[string]any{objectVar: object})
	if err != nil {
		return violation(compiled.rule, resource, fmt.Sprintf("evaluation error: %v", err)), true
	}

	passed, ok := out.Value().(bool)
	if !ok {
		message := fmt.Sprintf("expected bool result, got %s", out.Type())

		return violation(compiled.rule, resource, message), true
	}

	if passed {
		return Violation{}, false
	}

	message := compiled.rule.Message
	if message == "" {
		message = "rule failed"
	}

	return violation(compiled.rule, resource, message), true
}

// violation builds a Violation from a rule, resource identity, and message.
func violation(rule Rule, resource, message string) Violation {
	return Violation{
		Rule:     rule.Name,
		Resource: resource,
		Message:  message,
		Severity: rule.Severity,
	}
}

// attributedResource appends the source descriptor to the identity when the
// options carry an attribution entry for it.
func attributedResource(identity string, opts *Options) string {
	if opts == nil || opts.Attribution == nil {
		return identity
	}

	if source := opts.Attribution[identity]; source != "" {
		return fmt.Sprintf("%s (%s)", identity, source)
	}

	return identity
}

// parseDocument unmarshals one manifest into a dynamic object and derives its
// resource identity.
func parseDocument(data []byte) (map[string]any, string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, "", errEmptyDocument
	}

	var object map[string]any

	err := yaml.Unmarshal(data, &object)
	if err != nil {
		return nil, "", fmt.Errorf("unmarshalling document: %w", err)
	}

	if object == nil {
		return nil, "", errEmptyDocument
	}

	return object, documentIdentity(object), nil
}

// documentIdentity derives a "Kind/Name" or "Kind/Namespace/Name" identity from
// an object, tolerating missing fields.
func documentIdentity(object map[string]any) string {
	kind, _ := object["kind"].(string)
	if kind == "" {
		kind = "Unknown"
	}

	name, namespace := "<unnamed>", ""

	if meta, ok := object["metadata"].(map[string]any); ok {
		if value, ok := meta["name"].(string); ok && value != "" {
			name = value
		}

		namespace, _ = meta["namespace"].(string)
	}

	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	}

	return fmt.Sprintf("%s/%s", kind, name)
}
