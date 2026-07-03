package gke_test

import (
	"testing"

	gke "github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/stretchr/testify/assert"
)

// TestClusterNameFromContext pins the single gcloud kubeconfig-context parser
// shared by every GKE call site (gke_<project>_<location>_<name>).
func TestClusterNameFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kubeContext string
		want        string
	}{
		{
			name:        "gcloud context extracts trailing name",
			kubeContext: "gke_my-project_europe-north1_my-cluster",
			want:        "my-cluster",
		},
		{name: "empty context", kubeContext: "", want: ""},
		{name: "non-GKE context", kubeContext: "kind-something", want: ""},
		{name: "prefix without segments", kubeContext: "gke_", want: ""},
		{name: "too few segments", kubeContext: "gke_project_name", want: ""},
		{name: "surrounding whitespace trimmed", kubeContext: "  gke_p_l_n  ", want: "n"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, gke.ClusterNameFromContext(testCase.kubeContext))
		})
	}
}
