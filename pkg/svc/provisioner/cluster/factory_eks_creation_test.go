package clusterprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type effectiveEKSMetadata struct {
	Region string            `json:"region"`
	Tags   map[string]string `json:"tags"`
}

// createWithCreationID runs the factory-built EKS provisioner's Create against the eksctl stand-in
// and returns the create error and the metadata of the config eksctl received.
func createWithCreationID(
	t *testing.T,
	region, creationID, sourceConfig string,
) (effectiveEKSMetadata, error) {
	t.Helper()

	_, effectiveConfigPath := configureEKSCreateFixture(t)

	sourceConfigPath := filepath.Join(t.TempDir(), "eks.yaml")
	require.NoError(t, os.WriteFile(sourceConfigPath, []byte(sourceConfig), 0o600))

	cluster := eksTestCluster()
	cluster.Spec.Provider.AWS = v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Name:           "test-eks",
				Region:         region,
				ConfigPath:     sourceConfigPath,
				KubeconfigPath: "/tmp/ksail-kubeconfig",
				CreationID:     creationID,
			},
		},
	}

	provisioner, _, err := factory.Create(t.Context(), cluster)
	require.NoError(t, err)

	createErr := provisioner.Create(t.Context(), "test-eks")
	if createErr != nil {
		return effectiveEKSMetadata{}, createErr //nolint:wrapcheck // returned to the test as-is.
	}

	//nolint:gosec // effectiveConfigPath is a private test capture path.
	data, err := os.ReadFile(effectiveConfigPath)
	require.NoError(t, err)

	var config struct {
		Metadata effectiveEKSMetadata `json:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal(data, &config))

	return config.Metadata, nil
}

// TestEKSCreateTagsClusterWithCreationID pins ksail#7591: the create attempt's identifier reaches
// eksctl as a cluster tag, alongside the user's own tags, so a failed create can later recognise the
// cluster it began building. The user's source config is never modified.
//
//nolint:paralleltest // exercises explicit process environment isolation for the child eksctl binary.
func TestEKSCreateTagsClusterWithCreationID(t *testing.T) {
	const source = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: test-eks
  region: eu-central-1
  tags:
    team: platform
`

	testCases := []struct {
		name           string
		region         string
		expectedRegion string
	}{
		{name: "resolved region", region: "eu-west-1", expectedRegion: "eu-west-1"},
		{name: "region left to the config", region: "", expectedRegion: "eu-central-1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			metadata, err := createWithCreationID(t, testCase.region, "attempt-7591", source)
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedRegion, metadata.Region)
			assert.Equal(t, map[string]string{
				"team":                            "platform",
				eksprovisioner.ClusterCreationTag: "attempt-7591",
			}, metadata.Tags)
		})
	}
}

// TestEKSCreateWithoutCreationIDAddsNoTag keeps creates that carry no attempt identifier unchanged.
//
//nolint:paralleltest // exercises explicit process environment isolation for the child eksctl binary.
func TestEKSCreateWithoutCreationIDAddsNoTag(t *testing.T) {
	_, source := writeEKSCreateConfigFixture(t)

	metadata, err := createWithCreationID(t, "eu-west-1", "", string(source))
	require.NoError(t, err)
	assert.Empty(t, metadata.Tags)
}

// TestEKSCreateRejectsMalformedTagsWithCreationID refuses to guess at a tags value that is not a
// mapping instead of silently dropping the user's tags or the creation tag.
//
//nolint:paralleltest // exercises explicit process environment isolation for the child eksctl binary.
func TestEKSCreateRejectsMalformedTagsWithCreationID(t *testing.T) {
	const source = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: test-eks
  tags:
    - team
`

	_, err := createWithCreationID(t, "eu-west-1", "attempt-7591", source)
	require.ErrorContains(t, err, "metadata.tags must be a mapping")
}
