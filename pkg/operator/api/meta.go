package api

import (
	"fmt"
	"net/http"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// metaResponse is the static cluster-configuration metadata the SPA renders its forms from. It is
// the single runtime source of truth for the distribution list, the distribution→provider matrix,
// and the component option lists — the SPA no longer hard-codes any of them. Every value derives
// from the v1alpha1 enums, so adding a distribution/provider/enum value in Go surfaces in the UI
// with no TypeScript change.
type metaResponse struct {
	Distributions []string            `json:"distributions"`
	Providers     map[string][]string `json:"providers"`
	Components    []componentMeta     `json:"components"`
}

// componentMeta describes one component field: its spec key (the JSON field under spec.cluster), the
// valid enum values, and the API default (so the form pre-selects the value equivalent to "unset").
type componentMeta struct {
	Key     string   `json:"key"`
	Values  []string `json:"values"`
	Default string   `json:"default"`
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
	}
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
