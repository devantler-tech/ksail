package workload

import (
	"fmt"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
)

type namespaceWarningKey struct {
	policy, rule, namespace, reason string
}

type namespaceWarningGroup struct {
	count     int
	example   string
	resources map[string]struct{}
	sources   map[string]struct{}
}

// addKyverno groups only warnings caused by unavailable namespace context.
// Counts retain the scope of skipped evaluations, with a deterministic example
// retaining the resource, source and HelmRelease attribution. Audit failures,
// evaluation errors and other offline limitations keep individual diagnostics.
func (s *celViolationSink) addKyverno(
	violation kyvernopolicy.Violation,
	doc map[string]any,
	source, description string,
) {
	if !violation.Unsupported || violation.UnknownNamespace == "" {
		s.add(description)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.namespaceWarnings == nil {
		s.namespaceWarnings = map[namespaceWarningKey]*namespaceWarningGroup{}
	}

	key := namespaceWarningKey{
		policy: violation.Policy, rule: violation.Rule,
		namespace: violation.UnknownNamespace, reason: violation.Message,
	}

	group := s.namespaceWarnings[key]
	if group == nil {
		group = &namespaceWarningGroup{
			example: description, resources: map[string]struct{}{}, sources: map[string]struct{}{},
		}
		s.namespaceWarnings[key] = group
	}

	group.count++
	group.example = min(group.example, description)
	apiVersion, _ := doc["apiVersion"].(string)
	group.resources[apiVersion+"/"+documentIdentityFromObject(doc)] = struct{}{}
	group.sources[source] = struct{}{}
}

// namespaceDescriptions is called under the sink's mutex, after validation.
func (s *celViolationSink) namespaceDescriptions() []string {
	descriptions := make([]string, 0, len(s.namespaceWarnings))
	for _, group := range s.namespaceWarnings {
		description := group.example
		if group.count > 1 {
			description += fmt.Sprintf(
				" (grouped: %d evaluations, %d resources, %d sources; representative example above)",
				group.count,
				len(group.resources),
				len(group.sources),
			)
		}

		descriptions = append(descriptions, description)
	}

	slices.Sort(descriptions)

	return descriptions
}
