package lifecycle_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
)

// TestResolveAWSRegion verifies the documented precedence: the env var named by
// RegionEnvVar (default AWS_REGION) overrides eks.yaml's region, which in turn
// beats an empty (defer-to-eksctl) result. This test mutates process env vars,
// so it must not run in parallel.
func TestResolveAWSRegion(t *testing.T) {
	eksDistCfg := &clusterprovisioner.DistributionConfig{
		EKS: &clusterprovisioner.EKSConfig{Region: "eu-west-1"},
	}

	t.Run("env var overrides eks.yaml", func(t *testing.T) {
		t.Setenv("AWS_REGION", "us-east-2")

		got := lifecycle.ResolveAWSRegion(v1alpha1.OptionsAWS{}, eksDistCfg)
		assert.Equal(t, "us-east-2", got)
	})

	t.Run("custom RegionEnvVar is honored", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")
		t.Setenv("KSAIL_AWS_REGION", "ap-southeast-1")

		opts := v1alpha1.OptionsAWS{RegionEnvVar: "KSAIL_AWS_REGION"}
		got := lifecycle.ResolveAWSRegion(opts, eksDistCfg)
		assert.Equal(t, "ap-southeast-1", got)
	})

	t.Run("falls back to eks.yaml region when env unset", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")

		got := lifecycle.ResolveAWSRegion(v1alpha1.OptionsAWS{}, eksDistCfg)
		assert.Equal(t, "eu-west-1", got)
	})

	t.Run("empty when no env and no eks config", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")

		got := lifecycle.ResolveAWSRegion(v1alpha1.OptionsAWS{}, nil)
		assert.Empty(t, got)
	})
}
