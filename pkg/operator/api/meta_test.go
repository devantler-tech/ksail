package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metaResponse struct {
	Distributions []string            `json:"distributions"`
	Providers     map[string][]string `json:"providers"`
	Components    []struct {
		Key     string   `json:"key"`
		Values  []string `json:"values"`
		Default string   `json:"default"`
	} `json:"components"`
}

func fetchMeta(t *testing.T) metaResponse {
	t.Helper()

	server := &api.Server{}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	server.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)

	var meta metaResponse

	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &meta))

	return meta
}

func TestMeta_DistributionsAndProviderMatrix(t *testing.T) {
	t.Parallel()

	meta := fetchMeta(t)

	assert.Contains(t, meta.Distributions, "VCluster")
	assert.Contains(t, meta.Distributions, "EKS")

	// The matrix mirrors the operator's own ValidateForDistribution gate.
	assert.Equal(t, []string{"AWS"}, meta.Providers["EKS"], "EKS supports only AWS")
	assert.Contains(t, meta.Providers["VCluster"], "Kubernetes")
	assert.Contains(t, meta.Providers["VCluster"], "Docker")
	assert.NotContains(t, meta.Providers["VCluster"], "AWS")
	assert.Contains(t, meta.Providers["Talos"], "Hetzner")
}

func TestMeta_ComponentsHaveValuesAndValidDefault(t *testing.T) {
	t.Parallel()

	meta := fetchMeta(t)

	keys := make([]string, 0, len(meta.Components))
	for _, component := range meta.Components {
		keys = append(keys, component.Key)

		assert.NotEmpty(t, component.Values, "%s must list values", component.Key)
		assert.Contains(
			t,
			component.Values,
			component.Default,
			"%s default %q must be one of its values",
			component.Key,
			component.Default,
		)
	}

	for _, want := range []string{"cni", "csi", "gitOpsEngine", "policyEngine"} {
		assert.True(t, slices.Contains(keys, want), "components must include %q", want)
	}
}
