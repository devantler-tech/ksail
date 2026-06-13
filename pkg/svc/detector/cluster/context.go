package cluster

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// k3kContextPrefix is the kubeconfig context prefix written by K3s clusters run
// via the k3k operator on the Kubernetes provider. It is an alias for the
// standalone k3d "k3d-" prefix: both map back to the K3s distribution. Modeling
// it here, once, keeps the k3k special case out of every inverse-mapping call
// site (mirrors resolveCreatedContextName, which sets this context on create).
const k3kContextPrefix = "k3k-"

// contextNameSentinel is a placeholder substituted into ContextName so the
// cluster-name slot can be stripped, leaving just the prefix.
const contextNameSentinel = "\x00"

// contextPrefix is a single inverse-mapping entry: the prefix written to a
// kubeconfig context name and the distribution it resolves back to.
type contextPrefix struct {
	prefix       string
	distribution v1alpha1.Distribution
}

// standardContextPrefixes returns the per-distribution kubeconfig-context
// prefixes derived from the forward mapping
// (v1alpha1.Distribution.ContextName), so prefix knowledge has a single source.
//
// Distributions whose context format is not a simple prefix (e.g. EKS, whose
// eksctl contexts are "<iam>@<name>.<region>.eksctl.io") have no entry: they
// cannot be reverse-derived from a prefix and are intentionally omitted. The
// nested-on-Kubernetes "k3k-" alias is NOT included here; see contextPrefixes.
func standardContextPrefixes() []contextPrefix {
	prefixes := make([]contextPrefix, 0, len(v1alpha1.ValidDistributions()))

	for _, dist := range v1alpha1.ValidDistributions() {
		contextName := dist.ContextName(contextNameSentinel)

		prefix, found := strings.CutSuffix(contextName, contextNameSentinel)
		if !found || prefix == "" {
			// No simple prefix convention (e.g. EKS has a suffix instead).
			continue
		}

		prefixes = append(prefixes, contextPrefix{prefix: prefix, distribution: dist})
	}

	return prefixes
}

// contextPrefixes returns the standard per-distribution prefixes plus the
// modeled nested-on-Kubernetes "k3k-" alias (mapping to K3s). The alias is
// appended last so the standalone "k3d-" prefix is preferred when both could
// match. This is the full inverse-mapping set used by name-resolution call
// sites (switch, delete, lifecycle) that must recognize nested K3s contexts.
func contextPrefixes() []contextPrefix {
	prefixes := standardContextPrefixes()

	return append(prefixes, contextPrefix{
		prefix:       k3kContextPrefix,
		distribution: v1alpha1.DistributionK3s,
	})
}

// ContextPrefixes returns the ordered list of kubeconfig-context prefixes used
// by KSail's known distributions, including the nested-on-Kubernetes "k3k-"
// alias. Callers that probe a kubeconfig by constructing "<prefix><name>"
// candidates (e.g. cluster delete) use this to cover every convention without
// re-tabulating the prefixes.
func ContextPrefixes() []string {
	entries := contextPrefixes()
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		out = append(out, entry.prefix)
	}

	return out
}

// StripContextPrefix is the inverse of v1alpha1.Distribution.ContextName: given a
// kubeconfig context name, it returns the distribution and cluster name encoded
// in it. It recognizes every standard distribution prefix plus the
// nested-on-Kubernetes "k3k-" alias (which maps to K3s), each handled once.
//
// ok is false when the context matches no known prefix or when the stripped
// cluster name is empty (e.g. a bare "kind-").
func StripContextPrefix(contextName string) (v1alpha1.Distribution, string, bool) {
	for _, entry := range contextPrefixes() {
		if clusterName, found := strings.CutPrefix(contextName, entry.prefix); found {
			if clusterName == "" {
				return "", "", false
			}

			return entry.distribution, clusterName, true
		}
	}

	return "", "", false
}

// StripContextPrefixForDistribution extracts the cluster name from a context
// name when it matches the prefix convention for the given distribution. For
// K3s it also accepts the nested-on-Kubernetes "k3k-" alias. Returns an empty
// string when the context does not match the distribution's convention.
//
// This is the distribution-scoped variant used by callers that already know the
// distribution and only want to validate/strip the matching prefix (e.g.
// deriving a display name from a configured context).
func StripContextPrefixForDistribution(
	contextName string,
	distribution v1alpha1.Distribution,
) string {
	prefix := strings.TrimSuffix(distribution.ContextName(contextNameSentinel), contextNameSentinel)
	if prefix != "" {
		if clusterName, found := strings.CutPrefix(contextName, prefix); found {
			return clusterName
		}
	}

	// K3s additionally accepts the nested-on-Kubernetes "k3k-" alias.
	if distribution == v1alpha1.DistributionK3s {
		if clusterName, found := strings.CutPrefix(contextName, k3kContextPrefix); found {
			return clusterName
		}
	}

	return ""
}

// MatchContexts returns the kubeconfig context names that correspond to the
// given cluster name. It first builds candidates from every known distribution
// prefix (plus the "k3k-" alias) and keeps those present in the kubeconfig; if
// none match, it falls back to substring matching so providers whose context
// format does not follow the standard prefix conventions (e.g. Omni's
// "<org>-<cluster>-<sa>") still resolve.
func MatchContexts(config *clientcmdapi.Config, clusterName string) []string {
	var matches []string

	if config == nil || clusterName == "" {
		return matches
	}

	for _, dist := range v1alpha1.ValidDistributions() {
		candidate := dist.ContextName(clusterName)
		if candidate == "" {
			continue
		}

		if _, exists := config.Contexts[candidate]; exists {
			matches = append(matches, candidate)
		}
	}

	// K3s run via the k3k operator writes a "k3k-" context rather than the
	// standalone "k3d-"; add it as an explicit candidate so nested K3s clusters
	// resolve deterministically instead of falling through to substring matching.
	k3kCandidate := k3kContextPrefix + clusterName
	if _, exists := config.Contexts[k3kCandidate]; exists {
		matches = append(matches, k3kCandidate)
	}

	if len(matches) == 0 {
		for ctxName := range config.Contexts {
			if strings.Contains(ctxName, clusterName) {
				matches = append(matches, ctxName)
			}
		}
	}

	return matches
}
