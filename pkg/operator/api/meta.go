package api

import (
	"fmt"
	"net/http"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// metaResponse is the static cluster-configuration metadata the SPA renders its forms from. It is
// the single runtime source of truth for the distribution list, the distribution→provider matrix,
// the component option lists, and the workload browser's resource-kind allowlist — the SPA no longer
// hard-codes any of them. The form values derive from the v1alpha1 enums and the resource kinds from
// the allowlist in resources.go, so adding a distribution/provider/enum value/browsable kind in Go
// surfaces in the UI with no TypeScript change.
type metaResponse struct {
	Distributions []string            `json:"distributions"`
	Providers     map[string][]string `json:"providers"`
	Components    []componentMeta     `json:"components"`
	// ResourceKinds is the workload browser's resource-kind allowlist with per-kind verb flags, in
	// the order the kind selector presents them. Additive on the wire: older SPAs ignore it, and the
	// SPA treats its absence (an older backend) as "fall back to the hand-maintained constants".
	ResourceKinds []resourceKindMeta `json:"resourceKinds"`
}

// componentMeta describes one component field: its spec key (the JSON field under spec.cluster), the
// valid enum values, and the API default (so the form pre-selects the value equivalent to "unset").
type componentMeta struct {
	Key     string   `json:"key"`
	Values  []string `json:"values"`
	Default string   `json:"default"`
}

// resourceKindMeta describes one entry of the workload browser's resource-kind allowlist: the kind
// name, its scope, and the verbs/affordances the backend supports for it. Every flag derives from
// the allowlist and predicates in resources.go, so the SPA builds its kind selector and action gates
// from this payload instead of hand-mirroring them in TypeScript.
type resourceKindMeta struct {
	Kind         string `json:"kind"`
	Namespaced   bool   `json:"namespaced"`
	Scalable     bool   `json:"scalable"`
	Restartable  bool   `json:"restartable"`
	Reconcilable bool   `json:"reconcilable"`
	Deletable    bool   `json:"deletable"`
	Browsable    bool   `json:"browsable"`
}

// enumValuer is the subset of a v1alpha1 enum type the meta endpoint needs.
type enumValuer interface {
	ValidValues() []string
	Default() any
}

// clusterMeta assembles the metadata payload from the v1alpha1 enums and validation rules.
func clusterMeta() metaResponse {
	distributions := new(v1alpha1.Distribution).ValidValues()

	allProviders := new(v1alpha1.Provider).ValidValues()
	providers := make(map[string][]string, len(distributions))

	for _, dist := range distributions {
		var valid []string

		for _, name := range allProviders {
			provider := v1alpha1.Provider(name)
			// ValidateForDistribution is the operator's own combination gate, so the matrix can never
			// drift from what the reconciler will actually accept.
			if provider.ValidateForDistribution(v1alpha1.Distribution(dist)) == nil {
				valid = append(valid, name)
			}
		}

		providers[dist] = valid
	}

	components := []componentMeta{
		component("cni", new(v1alpha1.CNI)),
		component("csi", new(v1alpha1.CSI)),
		component("cdi", new(v1alpha1.CDI)),
		component("metricsServer", new(v1alpha1.MetricsServer)),
		component("loadBalancer", new(v1alpha1.LoadBalancer)),
		component("certManager", new(v1alpha1.CertManager)),
		component("policyEngine", new(v1alpha1.PolicyEngine)),
		component("gitOpsEngine", new(v1alpha1.GitOpsEngine)),
	}

	return metaResponse{
		Distributions: distributions,
		Providers:     providers,
		Components:    components,
		ResourceKinds: resourceKindsMeta(),
	}
}

// resourceKindsMeta assembles the resource-kind metadata from the allowlist and its predicates, in
// the curated order of resourceKindEntries. Deletability mirrors DeleteResourceWith's rule exactly:
// delete is denied for cluster-scoped kinds, so it equals the kind's scope.
func resourceKindsMeta() []resourceKindMeta {
	entries := resourceKindEntries()
	kinds := make([]resourceKindMeta, 0, len(entries))

	for _, entry := range entries {
		kinds = append(kinds, resourceKindMeta{
			Kind:         entry.name,
			Namespaced:   entry.kind.Namespaced,
			Scalable:     ResourceKindScalable(entry.name),
			Restartable:  ResourceKindRestartable(entry.name),
			Reconcilable: ResourceKindReconcilable(entry.name),
			Deletable:    entry.kind.Namespaced,
			Browsable:    resourceKindBrowsable(entry.kind),
		})
	}

	return kinds
}

func component(key string, enum enumValuer) componentMeta {
	return componentMeta{
		Key:     key,
		Values:  enum.ValidValues(),
		Default: fmt.Sprintf("%v", enum.Default()),
	}
}

// handleMeta serves the static cluster-configuration metadata. It is an open endpoint (like
// /api/v1/config): the payload is non-sensitive enum metadata the SPA needs to render forms.
func (s *Server) handleMeta(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, clusterMeta())
}
