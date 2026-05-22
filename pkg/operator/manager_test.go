package operator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNewSchemeRegistersClusterKinds(t *testing.T) {
	t.Parallel()

	scheme, err := operator.NewScheme()
	require.NoError(t, err)

	for _, kind := range []string{"Cluster", "ClusterList"} {
		gvk := schema.GroupVersionKind{Group: v1alpha1.Group, Version: v1alpha1.Version, Kind: kind}
		assert.True(t, scheme.Recognizes(gvk), "scheme should recognize %s", kind)
	}
}

func TestManagerOptionsDefaultsLeaderElectionID(t *testing.T) {
	t.Parallel()

	scheme, err := operator.NewScheme()
	require.NoError(t, err)

	opts := operator.ManagerOptions(scheme, operator.Options{
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: ":8081",
		LeaderElection:         true,
	})

	assert.Equal(t, operator.DefaultLeaderElectionID, opts.LeaderElectionID)
	assert.True(t, opts.LeaderElection)
	assert.Same(t, scheme, opts.Scheme)
}

func TestManagerOptionsHonorsCustomLeaderID(t *testing.T) {
	t.Parallel()

	scheme, err := operator.NewScheme()
	require.NoError(t, err)

	opts := operator.ManagerOptions(scheme, operator.Options{LeaderElectionID: "custom-id"})

	assert.Equal(t, "custom-id", opts.LeaderElectionID)
}
