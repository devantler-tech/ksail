package workload

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	policiesv1beta1 "github.com/kyverno/api/api/policies.kyverno.io/v1beta1"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ErrKyvernoPolicyViolation is returned when a rendered document fails a Kyverno
// rule that a cluster enforcing the policy would reject at admission.
var ErrKyvernoPolicyViolation = errors.New("kyverno policy violation")

// kyvernoPoliciesFlagDescription is the --kyverno-policies help text. It states
// the scope, because a check that silently covers less than it appears to is
// worse than none.
const kyvernoPoliciesFlagDescription = "Evaluate the source's own Kyverno ClusterPolicy and " +
	"Policy validate rules, and its CEL-based ValidatingPolicy and NamespacedValidatingPolicy " +
	"validations, against the rendered manifests, as if each document were being " +
	"created (off by default). Each kustomization is evaluated against the policies in its own " +
	"rendered output: a policy delivered by a different kustomization is not seen, loose YAML " +
	"files are not evaluated, a document is evaluated in the namespace it declares (a Flux " +
	"targetNamespace is not applied), and other policies.kyverno.io kinds are not " +
	"supported. A ValidatingPolicy that calls the http library, or a CEL lookup of cluster, " +
	"registry or global-context data, cannot be evaluated offline. Namespace labels come " +
	"from the kustomization's own Namespaces first, then from " +
	"a Namespace another kustomization renders, unless two render it with different labels. " +
	"A rule a cluster would enforce fails validation; an audit-only failure or a rule that " +
	"cannot be evaluated offline is reported as a warning."

// evaluateKyvernoDocuments applies the Kyverno policies found in data to every
// other document in data. It is a no-op when enabled is false or data holds no
// policies.
//
// Policies and Namespace documents are collected from the whole stream, whatever
// the skip-kinds say, because they are evaluation context: policies are what is
// applied, and Namespaces supply namespace metadata to other documents' rules.
// A Namespace is also an object the cluster admits, so it is evaluated like any
// other document unless its kind is skipped. Only documents whose kind is not
// skipped are evaluated, so a skipped kind cannot surface a policy failure — the
// same exclusion kubeconform and the CEL rules honour.
//
// Blocking violations are aggregated into an ErrKyvernoPolicyViolation. Everything
// else — an audit-only failure, an evaluation error the policy does not block on,
// and a rule that cannot be evaluated offline — is recorded in sink for reporting
// after the progress group. A policy that does not decode is an error: skipping it
// would report a clean run for a policy that was never applied.
//
// shared holds Namespace documents rendered elsewhere in the validated tree (see
// sharedNamespaces). They only supply labels for a Namespace data does not render
// itself: a kustomization's own Namespace always wins, and shared Namespaces are
// never evaluated as targets here — each is evaluated by the kustomization that
// renders it.
func evaluateKyvernoDocuments(
	ctx context.Context,
	enabled bool,
	data []byte,
	source string,
	skipKinds []string,
	sink *celViolationSink,
	attribution map[string]string,
	shared []map[string]any,
) error {
	if !enabled {
		return nil
	}

	docs := decodeDocuments(data)

	split, err := splitKyvernoPolicies(docs, skipKinds)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}

	if len(split.policies) == 0 && len(split.celPolicies) == 0 {
		return nil
	}

	namespaces := withSharedNamespaces(docs, shared)
	engine := kyvernopolicy.NewEngine(split.policies, namespaces)

	celEngine, err := kyvernopolicy.NewCELEngine(split.celPolicies, namespaces)
	if err != nil {
		return fmt.Errorf("load Kyverno ValidatingPolicies in %s: %w", source, err)
	}

	blocking, err := evaluateKyvernoTargets(
		ctx, engine, celEngine, split.targets, source, sink, attribution,
	)
	if err != nil {
		return err
	}

	if len(blocking) > 0 {
		return fmt.Errorf("%w:\n  %s", ErrKyvernoPolicyViolation, strings.Join(blocking, "\n  "))
	}

	return nil
}

// evaluateKyvernoTargets evaluates every target document and returns the
// described blocking violations. Non-blocking violations are recorded in sink.
func evaluateKyvernoTargets(
	ctx context.Context,
	engine *kyvernopolicy.Engine,
	celEngine *kyvernopolicy.CELEngine,
	targets []map[string]any,
	source string,
	sink *celViolationSink,
	attribution map[string]string,
) ([]string, error) {
	var blocking []string

	for _, doc := range targets {
		violations, err := evaluateKyvernoDocument(ctx, engine, celEngine, doc, source)
		if err != nil {
			return nil, err
		}

		for _, violation := range violations {
			described := describeKyvernoViolation(violation, doc, source, attribution)

			if violation.Blocking {
				blocking = append(blocking, described)

				continue
			}

			sink.add(described)
		}
	}

	return blocking, nil
}

// evaluateKyvernoDocument applies both the kyverno.io policies and the CEL
// ValidatingPolicies to one document and returns their combined violations.
func evaluateKyvernoDocument(
	ctx context.Context,
	engine *kyvernopolicy.Engine,
	celEngine *kyvernopolicy.CELEngine,
	doc map[string]any,
	source string,
) ([]kyvernopolicy.Violation, error) {
	violations, err := engine.Evaluate(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("evaluate Kyverno policies in %s: %w", source, err)
	}

	celViolations, err := celEngine.Evaluate(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("evaluate Kyverno ValidatingPolicies in %s: %w", source, err)
	}

	return append(violations, celViolations...), nil
}

// decodeDocuments decodes every mapping document in data, dropping empty and
// non-mapping documents.
func decodeDocuments(data []byte) []map[string]any {
	var docs []map[string]any

	for _, docBytes := range fsutil.SplitYAMLDocuments(data) {
		obj, ok := decodeDocumentObject(docBytes)
		if ok {
			docs = append(docs, obj)
		}
	}

	return docs
}

// sharedNamespaces collects the Namespace documents rendered across every
// kustomization's output, so a namespaced document in one kustomization can be
// evaluated against a namespaceSelector whose Namespace another renders. A
// Namespace is cluster-scoped, so its labels do not depend on which layer renders
// it. When two outputs render the same Namespace with different labels, or any
// output renders it with labels that cannot be read, that Namespace is left out:
// its labels are unknown, and a rule selecting on them is reported as not
// evaluable rather than guessed.
func sharedNamespaces(outputs [][]byte) []map[string]any {
	type seen struct {
		doc        map[string]any
		labels     map[string]string
		conflicted bool
	}

	byName := map[string]*seen{}

	var order []string

	for _, data := range outputs {
		for _, doc := range decodeDocuments(data) {
			name, ok := kyvernopolicy.NamespaceDocumentName(doc)
			if !ok {
				continue
			}

			// Labels that cannot be read are unknown, exactly like conflicting ones.
			labels, _, err := unstructured.NestedStringMap(doc, "metadata", "labels")
			malformed := err != nil

			existing, found := byName[name]
			if !found {
				byName[name] = &seen{doc: doc, labels: labels, conflicted: malformed}
				order = append(order, name)

				continue
			}

			if malformed || !maps.Equal(existing.labels, labels) {
				existing.conflicted = true
			}
		}
	}

	var shared []map[string]any

	for _, name := range order {
		if entry := byName[name]; !entry.conflicted {
			shared = append(shared, entry.doc)
		}
	}

	return shared
}

// withSharedNamespaces returns docs plus every shared Namespace docs does not
// render itself, as the policy engine's namespace context. docs is not modified.
func withSharedNamespaces(docs, shared []map[string]any) []map[string]any {
	if len(shared) == 0 {
		return docs
	}

	own := map[string]struct{}{}

	for _, doc := range docs {
		if name, ok := kyvernopolicy.NamespaceDocumentName(doc); ok {
			own[name] = struct{}{}
		}
	}

	combined := slices.Clone(docs)

	for _, doc := range shared {
		name, _ := kyvernopolicy.NamespaceDocumentName(doc)
		if _, rendered := own[name]; !rendered {
			combined = append(combined, doc)
		}
	}

	return combined
}

// kyvernoSplit is a document stream separated into the Kyverno policies it
// carries and the documents to evaluate against them.
type kyvernoSplit struct {
	policies    []kyvernov1.PolicyInterface
	celPolicies []policiesv1beta1.ValidatingPolicyLike
	targets     []map[string]any
}

// splitKyvernoPolicies separates the Kyverno policies in docs, classic and CEL,
// from the documents to evaluate against them, dropping skipped kinds from the
// latter.
func splitKyvernoPolicies(docs []map[string]any, skipKinds []string) (kyvernoSplit, error) {
	skip := make(map[string]struct{}, len(skipKinds))
	for _, kind := range skipKinds {
		skip[kind] = struct{}{}
	}

	var split kyvernoSplit

	for _, doc := range docs {
		if kyvernopolicy.IsPolicy(doc) {
			policy, err := kyvernopolicy.DecodePolicy(doc)
			if err != nil {
				return kyvernoSplit{}, fmt.Errorf("load Kyverno policy: %w", err)
			}

			split.policies = append(split.policies, policy)

			continue
		}

		if kyvernopolicy.IsValidatingPolicy(doc) {
			policy, err := kyvernopolicy.DecodeValidatingPolicy(doc)
			if err != nil {
				return kyvernoSplit{}, fmt.Errorf("load Kyverno ValidatingPolicy: %w", err)
			}

			split.celPolicies = append(split.celPolicies, policy)

			continue
		}

		if kind, _ := doc["kind"].(string); kind != "" {
			if _, skipped := skip[kind]; skipped {
				continue
			}
		}

		split.targets = append(split.targets, doc)
	}

	return split, nil
}

// describeKyvernoViolation renders a violation with its policy, rule, the
// offending document's identity and the source, plus the HelmRelease layer when
// attribution knows it — the same shape CEL and kubeconform failures use. The
// outcome is named so a warning says why it did not fail validation.
func describeKyvernoViolation(
	violation kyvernopolicy.Violation,
	doc map[string]any,
	source string,
	attribution map[string]string,
) string {
	outcome := "failed"

	switch {
	case violation.Unsupported:
		outcome = "not evaluable offline"
	case violation.Error:
		outcome = "errored"
	case !violation.Blocking:
		outcome = "failed (audit)"
	}

	subject := "document"

	identity := documentIdentityFromObject(doc)
	if identity != "" {
		subject = identity
	}

	// A CEL ValidatingPolicy's validations carry no rule name.
	rule := ""
	if violation.Rule != "" {
		rule = fmt.Sprintf(" rule %q", violation.Rule)
	}

	described := fmt.Sprintf(
		"policy %q%s %s for %s (in %s): %s",
		violation.Policy, rule, outcome, subject, source, violation.Message,
	)

	if layer := attribution[identity]; identity != "" && layer != "" {
		described += " (from " + layer + ")"
	}

	return described
}
