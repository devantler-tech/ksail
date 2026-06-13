package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	testNameMyCluster = "ctxtest-cluster"
	testCtxK3kNested  = "k3k-nested"
	testNameFoo       = "ctxtest-foo"
)

// TestStripContextPrefix verifies the inverse mapping recognizes every standard
// distribution prefix plus the nested-on-Kubernetes k3k- alias.
func TestStripContextPrefix(t *testing.T) {
	t.Parallel()

	type want struct {
		dist    v1alpha1.Distribution
		cluster string
		ok      bool
	}

	clusterNm := testNameMyCluster

	tests := []struct {
		name        string
		contextName string
		want        want
	}{
		{"kind", "kind-" + clusterNm, want{v1alpha1.DistributionVanilla, clusterNm, true}},
		{"k3d", "k3d-" + clusterNm, want{v1alpha1.DistributionK3s, clusterNm, true}},
		{"k3k alias", testCtxK3kNested, want{v1alpha1.DistributionK3s, "nested", true}},
		{"talos", "admin@talos-box", want{v1alpha1.DistributionTalos, "talos-box", true}},
		{"vcluster", "vcluster-docker_vc", want{v1alpha1.DistributionVCluster, "vc", true}},
		{"kwok", "kwok-sim", want{v1alpha1.DistributionKWOK, "sim", true}},
		{"bare prefix is empty name", "kind-", want{"", "", false}},
		{"unknown prefix", "eks-cluster", want{"", "", false}},
		{"no prefix", "some-random-context", want{"", "", false}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dist, clusterName, ok := cluster.StripContextPrefix(testCase.contextName)

			assert.Equal(t, testCase.want.ok, ok)
			assert.Equal(t, testCase.want.dist, dist)
			assert.Equal(t, testCase.want.cluster, clusterName)
		})
	}
}

// TestStripContextPrefixForDistribution verifies distribution-scoped stripping,
// including the k3k alias being accepted only for K3s.
func TestStripContextPrefixForDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contextName string
		dist        v1alpha1.Distribution
		want        string
	}{
		{"kind match", "kind-" + testNameFoo, v1alpha1.DistributionVanilla, testNameFoo},
		{"k3d match", "k3d-" + testNameFoo, v1alpha1.DistributionK3s, testNameFoo},
		{"k3k alias for k3s", "k3k-" + testNameFoo, v1alpha1.DistributionK3s, testNameFoo},
		{"k3k not accepted for vanilla", "k3k-" + testNameFoo, v1alpha1.DistributionVanilla, ""},
		{"wrong prefix", "kind-" + testNameFoo, v1alpha1.DistributionK3s, ""},
		{"unsupported distribution", "kind-" + testNameFoo, "made-up", ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.StripContextPrefixForDistribution(testCase.contextName, testCase.dist)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestContextPrefixes verifies the prefix list is single-sourced and includes
// Talos (admin@) and the k3k- alias that cluster delete now relies on.
func TestContextPrefixes(t *testing.T) {
	t.Parallel()

	prefixes := cluster.ContextPrefixes()

	assert.Contains(t, prefixes, "kind-")
	assert.Contains(t, prefixes, "k3d-")
	assert.Contains(t, prefixes, "admin@")
	assert.Contains(t, prefixes, "vcluster-docker_")
	assert.Contains(t, prefixes, "kwok-")
	assert.Contains(t, prefixes, "k3k-")
}

// TestMatchContexts verifies forward-candidate matching, the k3k alias, and the
// Omni substring fallback.
func TestMatchContexts(t *testing.T) {
	t.Parallel()

	config := &clientcmdapi.Config{
		Contexts: map[string]*clientcmdapi.Context{
			"kind-app":           {},
			testCtxK3kNested:     {},
			"org-omnicluster-sa": {},
		},
	}

	t.Run("standard prefix candidate", func(t *testing.T) {
		t.Parallel()

		matches := cluster.MatchContexts(config, "app")
		assert.Equal(t, []string{"kind-app"}, matches)
	})

	t.Run("k3k alias candidate", func(t *testing.T) {
		t.Parallel()

		matches := cluster.MatchContexts(config, "nested")
		assert.Equal(t, []string{testCtxK3kNested}, matches)
	})

	t.Run("substring fallback for omni", func(t *testing.T) {
		t.Parallel()

		matches := cluster.MatchContexts(config, "omnicluster")
		assert.Equal(t, []string{"org-omnicluster-sa"}, matches)
	})

	t.Run("nil config or empty name", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, cluster.MatchContexts(nil, "app"))
		assert.Empty(t, cluster.MatchContexts(config, ""))
	})
}

// TestDetectDistributionFromContext_K3kNotRecognized verifies the k3k alias is
// intentionally NOT recognized by DetectDistributionFromContext (only by the
// inverse-mapping helpers), so provider detection is not misled.
func TestDetectDistributionFromContext_K3kNotRecognized(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectDistributionFromContext(testCtxK3kNested)
	assert.ErrorIs(t, err, cluster.ErrUnknownContextPattern)
}
