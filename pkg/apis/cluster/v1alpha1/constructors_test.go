package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCluster(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()

	require.NotNil(t, cluster)
	assert.Equal(t, v1alpha1.Kind, cluster.Kind)
	assert.Equal(t, v1alpha1.APIVersion, cluster.APIVersion)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, cluster.Spec.Cluster.GitOpsEngine)
}

func TestNewOCIRegistry(t *testing.T) {
	t.Parallel()

	registry := v1alpha1.NewOCIRegistry()

	assert.Equal(t, v1alpha1.OCIRegistryStatusNotProvisioned, registry.Status)
	assert.Empty(t, registry.Endpoint)
}
