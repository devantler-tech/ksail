package workload

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/celrules"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// ErrCELRuleViolation is returned when one or more error-severity CEL rules are
// violated by the validated manifests.
var ErrCELRuleViolation = errors.New("CEL rule violation")

// buildCELEngine loads and compiles the CEL rules file named by rulesPath. It
// returns (nil, nil) when rulesPath is empty (the --rules flag was not given),
// so callers can treat a nil engine as "CEL validation disabled". A malformed
// or non-compiling rules file is surfaced as an error so validate fails fast,
// before any manifest is processed, rather than silently skipping the rules.
func buildCELEngine(rulesPath string) (*celrules.Engine, error) {
	if rulesPath == "" {
		return nil, nil //nolint:nilnil // nil engine + nil error means "CEL disabled" by design.
	}

	// Canonicalize like the validate target path and --config so a relative or
	// symlinked rules path resolves to the intended file.
	canonical, err := fsutil.EvalCanonicalPath(rulesPath)
	if err != nil {
		return nil, fmt.Errorf("resolve rules file %q: %w", rulesPath, err)
	}

	rules, err := celrules.LoadRules(canonical)
	if err != nil {
		return nil, fmt.Errorf("load CEL rules: %w", err)
	}

	engine, err := celrules.NewEngine(rules)
	if err != nil {
		return nil, fmt.Errorf("compile CEL rules: %w", err)
	}

	return engine, nil
}

// evaluateCELDocuments runs the CEL engine over every document in a rendered or
// built manifest stream. Error-severity violations are aggregated and returned
// as an ErrCELRuleViolation (failing validation); warning-severity violations
// are recorded in sink for reporting after the parallel progress group so they
// do not interleave with its ANSI output. A nil engine (CEL disabled) is a
// no-op. source names the origin (kustomization dir or file) for attribution.
//
// skipKinds names the Kubernetes kinds excluded from validation via
// --skip-kinds / --skip-secrets (and the ksail.yaml equivalents). Documents
// whose kind is in that set are skipped before CEL evaluation, so CEL honors the
// same exclusions kubeconform does and a skipped Secret (or other kind) cannot
// surface a CEL failure.
func evaluateCELDocuments(
	engine *celrules.Engine,
	data []byte,
	source string,
	skipKinds []string,
	sink *celViolationSink,
) error {
	if engine == nil {
		return nil
	}

	skip := make(map[string]struct{}, len(skipKinds))
	for _, kind := range skipKinds {
		skip[kind] = struct{}{}
	}

	var errViolations []string

	for _, docBytes := range fsutil.SplitYAMLDocuments(data) {
		obj, ok := decodeDocumentObject(docBytes)
		if !ok {
			continue
		}

		if kind, _ := obj["kind"].(string); kind != "" {
			if _, skipped := skip[kind]; skipped {
				continue
			}
		}

		for _, violation := range engine.Evaluate(obj) {
			described := describeCELViolation(violation, obj, source)

			if violation.Severity == celrules.SeverityWarning {
				sink.add(described)

				continue
			}

			errViolations = append(errViolations, described)
		}
	}

	if len(errViolations) > 0 {
		return fmt.Errorf("%w:\n  %s", ErrCELRuleViolation, strings.Join(errViolations, "\n  "))
	}

	return nil
}

// decodeDocumentObject decodes one YAML document into a map for CEL evaluation.
// It returns ok=false for empty documents and for documents that are not a
// mapping (e.g. a bare list or scalar), which have no `object` to evaluate and
// are simply skipped. sigs.k8s.io/yaml (YAML→JSON) is used so numbers and
// booleans arrive as the types CEL expects, matching how the engine's callers
// decode manifests elsewhere.
func decodeDocumentObject(docBytes []byte) (map[string]any, bool) {
	if len(bytes.TrimSpace(docBytes)) == 0 {
		return nil, false
	}

	var obj map[string]any

	err := yaml.Unmarshal(docBytes, &obj)
	if err != nil || obj == nil {
		return nil, false
	}

	return obj, true
}

// describeCELViolation renders a single violation with its rule name, the
// offending document's identity (Kind/Namespace/Name when derivable), and the
// source manifest, so a failure points at the exact resource and layer.
func describeCELViolation(violation celrules.Violation, obj map[string]any, source string) string {
	identity := documentIdentityFromObject(obj)
	if identity != "" {
		return fmt.Sprintf(
			"rule %q violated by %s (in %s): %s",
			violation.Rule, identity, source, violation.Message,
		)
	}

	return fmt.Sprintf("rule %q violated (in %s): %s", violation.Rule, source, violation.Message)
}

// documentIdentityFromObject builds "Kind/Namespace/Name" (or "Kind/Name" for
// cluster-scoped resources) from a decoded document, mirroring documentIdentity
// (which reads a render.Document) so CEL and kubeconform attribution read alike.
// It returns "" when the document lacks a Kind or Name.
func documentIdentityFromObject(obj map[string]any) string {
	kind, _ := obj["kind"].(string)

	metadata, _ := obj["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)

	if kind == "" || name == "" {
		return ""
	}

	namespace, _ := metadata["namespace"].(string)
	if namespace != "" {
		return kind + "/" + namespace + "/" + name
	}

	return kind + "/" + name
}

// celViolationSink collects warning-severity CEL violations across parallel
// validation tasks so they can be reported once after the progress group
// completes — mirroring degradationSink, since emitting mid-group would
// interleave with the ANSI progress display.
type celViolationSink struct {
	mu   sync.Mutex
	list []string
}

// add records one warning-severity violation description for later reporting.
func (s *celViolationSink) add(description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.list = append(s.list, description)
}

// report emits a warning for each collected warning-severity violation.
func (s *celViolationSink) report(cmd *cobra.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, description := range s.list {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "CEL rule warning: %s",
			Args:    []any{description},
			Writer:  cmd.ErrOrStderr(),
		})
	}
}
