package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
)

// ErrKyvernoPolicyViolation is returned when a rendered document fails a Kyverno
// rule that a cluster enforcing the policy would reject at admission.
var ErrKyvernoPolicyViolation = errors.New("kyverno policy violation")

// kyvernoPoliciesFlagDescription is the --kyverno-policies help text. It states
// the scope, because a check that silently covers less than it appears to is
// worse than none.
const kyvernoPoliciesFlagDescription = "Evaluate the source's own Kyverno ClusterPolicy and " +
	"Policy validate rules against the rendered manifests, as if each document were being " +
	"created (off by default). Each kustomization is evaluated against the policies in its own " +
	"rendered output: a policy delivered by a different kustomization is not seen, loose YAML " +
	"files are not evaluated, and CEL-based policies.kyverno.io policies are not supported. " +
	"A rule a cluster would enforce fails validation; an audit-only failure or a rule that " +
	"cannot be evaluated offline is reported as a warning."

// evaluateKyvernoDocuments applies the Kyverno policies found in data to every
// other document in data. It is a no-op when enabled is false or data holds no
// policies.
//
// Policies and Namespace documents are collected from the whole stream, whatever
// the skip-kinds say, because they are the evaluation context rather than
// validation targets. Only the remaining documents whose kind is not skipped are
// evaluated, so a skipped kind cannot surface a policy failure — the same
// exclusion kubeconform and the CEL rules honour.
//
// Blocking violations are aggregated into an ErrKyvernoPolicyViolation. Everything
// else — an audit-only failure, an evaluation error the policy does not block on,
// and a rule that cannot be evaluated offline — is recorded in sink for reporting
// after the progress group. A policy that does not decode is an error: skipping it
// would report a clean run for a policy that was never applied.
func evaluateKyvernoDocuments(
	ctx context.Context,
	enabled bool,
	data []byte,
	source string,
	skipKinds []string,
	sink *celViolationSink,
	attribution map[string]string,
) error {
	if !enabled {
		return nil
	}

	docs := decodeDocuments(data)

	policies, targets, err := splitKyvernoPolicies(docs, skipKinds)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}

	if len(policies) == 0 {
		return nil
	}

	engine := kyvernopolicy.NewEngine(policies, docs)

	var blocking []string

	for _, doc := range targets {
		violations, evalErr := engine.Evaluate(ctx, doc)
		if evalErr != nil {
			return fmt.Errorf("evaluate Kyverno policies in %s: %w", source, evalErr)
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

	if len(blocking) > 0 {
		return fmt.Errorf("%w:\n  %s", ErrKyvernoPolicyViolation, strings.Join(blocking, "\n  "))
	}

	return nil
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

// splitKyvernoPolicies separates the Kyverno policies in docs from the documents
// to evaluate against them, dropping skipped kinds from the latter.
func splitKyvernoPolicies(
	docs []map[string]any,
	skipKinds []string,
) ([]kyvernov1.PolicyInterface, []map[string]any, error) {
	skip := make(map[string]struct{}, len(skipKinds))
	for _, kind := range skipKinds {
		skip[kind] = struct{}{}
	}

	var (
		policies []kyvernov1.PolicyInterface
		targets  []map[string]any
	)

	for _, doc := range docs {
		if kyvernopolicy.IsPolicy(doc) {
			policy, err := kyvernopolicy.DecodePolicy(doc)
			if err != nil {
				return nil, nil, fmt.Errorf("load Kyverno policy: %w", err)
			}

			policies = append(policies, policy)

			continue
		}

		if kind, _ := doc["kind"].(string); kind != "" {
			if _, skipped := skip[kind]; skipped {
				continue
			}
		}

		targets = append(targets, doc)
	}

	return policies, targets, nil
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

	described := fmt.Sprintf(
		"policy %q rule %q %s for %s (in %s): %s",
		violation.Policy, violation.Rule, outcome, subject, source, violation.Message,
	)

	if layer := attribution[identity]; identity != "" && layer != "" {
		described += " (from " + layer + ")"
	}

	return described
}
