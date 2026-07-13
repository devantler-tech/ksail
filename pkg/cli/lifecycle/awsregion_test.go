package lifecycle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestResolveClusterInfoRetainsAWSCredentialMappings verifies lifecycle
// resolution preserves immutable AWS credential-name mappings.
func TestResolveClusterInfoRetainsAWSCredentialMappings(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	t.Setenv("KSAIL_AWS_REGION", "ap-southeast-1")

	require.NoError(
		t,
		os.WriteFile(filepath.Join(workingDir, "ksail.yaml"), []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: mapped-eks
spec:
  cluster:
    distribution: EKS
    provider: AWS
    distributionConfig: eks.yaml
    connection:
      kubeconfig: kubeconfig
  provider:
    aws:
      profileEnvVar: KSAIL_PROFILE
      regionEnvVar: KSAIL_AWS_REGION
      accessKeyIdEnvVar: KSAIL_ACCESS
      secretAccessKeyEnvVar: KSAIL_SECRET
      sessionTokenEnvVar: KSAIL_SESSION
`), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(workingDir, "eks.yaml"), []byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: mapped-eks
  region: eu-west-1
`), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workingDir, "kubeconfig"),
			[]byte("apiVersion: v1\nkind: Config\n"),
			0o600,
		),
	)

	resolved, err := lifecycle.ResolveClusterInfo(nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "mapped-eks", resolved.ClusterName)
	assert.Equal(t, "KSAIL_PROFILE", resolved.AWSOpts.ProfileEnvVar)
	assert.Equal(t, "KSAIL_ACCESS", resolved.AWSOpts.AccessKeyIDEnvVar)
	assert.Equal(t, "KSAIL_SECRET", resolved.AWSOpts.SecretAccessKeyEnvVar)
	assert.Equal(t, "KSAIL_SESSION", resolved.AWSOpts.SessionTokenEnvVar)
	assert.Equal(t, "ap-southeast-1", resolved.AWSRegion)
}
