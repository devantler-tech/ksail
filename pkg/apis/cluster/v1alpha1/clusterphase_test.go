package v1alpha1_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestClusterPhase_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Ready", v1alpha1.ClusterPhaseReady.String())
	assert.Equal(t, "Provisioning", v1alpha1.ClusterPhaseProvisioning.String())
	assert.Equal(t, "Failed", v1alpha1.ClusterPhaseFailed.String())
}

func TestClusterPhase_ValidValues(t *testing.T) {
	t.Parallel()

	values := v1alpha1.ClusterPhasePending.ValidValues()

	assert.ElementsMatch(t, []string{
		"Pending",
		"Provisioning",
		"Ready",
		"Stopped",
		"Updating",
		"Deleting",
		"Failed",
	}, values)
}
