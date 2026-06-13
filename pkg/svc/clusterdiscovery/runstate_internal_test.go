package clusterdiscovery

import (
	"context"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/stretchr/testify/assert"
)

func TestDockerLabelScheme(t *testing.T) {
	t.Parallel()

	cases := []struct {
		distribution v1alpha1.Distribution
		want         dockerprovider.LabelScheme
		ok           bool
	}{
		{v1alpha1.DistributionVanilla, dockerprovider.LabelSchemeKind, true},
		{v1alpha1.DistributionK3s, dockerprovider.LabelSchemeK3d, true},
		{v1alpha1.DistributionTalos, dockerprovider.LabelSchemeTalos, true},
		{v1alpha1.DistributionVCluster, dockerprovider.LabelSchemeVCluster, true},
		{v1alpha1.DistributionKWOK, dockerprovider.LabelSchemeKWOK, true},
		{v1alpha1.DistributionEKS, "", false},
		{v1alpha1.Distribution("Bogus"), "", false},
	}

	for _, testCase := range cases {
		scheme, ok := dockerLabelScheme(testCase.distribution)
		assert.Equal(t, testCase.ok, ok, "ok for %s", testCase.distribution)
		assert.Equal(t, testCase.want, scheme, "scheme for %s", testCase.distribution)
	}
}

// TestDockerRunStateUsesSeam pins that the injected DockerStatus seam is used verbatim when present,
// so callers can drive run-state without a real Docker daemon.
func TestDockerRunStateUsesSeam(t *testing.T) {
	t.Parallel()

	discoverer := &Discoverer{
		DockerStatus: func(
			_ context.Context, _ v1alpha1.Distribution, _ string,
		) RunState {
			return RunStateStopped
		},
	}

	got := discoverer.dockerRunState(t.Context(), v1alpha1.DistributionVanilla, "x")
	assert.Equal(t, RunStateStopped, got)
}
