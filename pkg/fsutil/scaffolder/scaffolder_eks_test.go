package scaffolder_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
)

func TestDefaultEKSConfigParams_AppliesNameAndRegion(t *testing.T) {
	t.Parallel()

	params := scaffolder.DefaultEKSConfigParams("prod", "eu-central-1")

	assert.Equal(t, "prod", params.ClusterName)
	assert.Equal(t, "eu-central-1", params.Region)
	assert.NotEmpty(t, params.KubernetesVersion)
	assert.NotEmpty(t, params.InstanceType)
	assert.Positive(t, params.DesiredCapacity)
}

func TestDefaultEKSConfigParams_EmptyRegionFallsBackToDefault(t *testing.T) {
	t.Parallel()

	params := scaffolder.DefaultEKSConfigParams("prod", "")

	assert.NotEmpty(t, params.Region, "an empty region must fall back to the scaffolding default")
}

func TestRenderEKSConfig_RendersClusterNameAndRegion(t *testing.T) {
	t.Parallel()

	rendered := string(scaffolder.RenderEKSConfig(
		scaffolder.DefaultEKSConfigParams("prod", "eu-central-1"),
	))

	assert.Contains(t, rendered, "kind: ClusterConfig")
	assert.Contains(t, rendered, "name: prod")
	assert.Contains(t, rendered, "region: eu-central-1")
	assert.Contains(t, rendered, "managedNodeGroups:")
}
