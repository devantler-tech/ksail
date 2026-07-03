package clusterprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	gkeprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/gke"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeADCPath writes a minimal authorized_user credentials file so the GKE SDK
// client constructs without real Google credentials or network access (token
// exchange is lazy). Callers point GOOGLE_APPLICATION_CREDENTIALS at it.
func fakeADCPath(t *testing.T) string {
	t.Helper()

	credPath := filepath.Join(t.TempDir(), "adc.json")
	err := os.WriteFile(credPath, []byte(
		`{"type":"authorized_user","client_id":"test","client_secret":"test","refresh_token":"test"}`,
	), 0o600)
	require.NoError(t, err)

	return credPath
}

// gkeTestCluster returns a cluster shaped for the GKE factory path.
func gkeTestCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionGKE

	return cluster
}

// TestCreateGKEProvisionerWithConfig asserts a populated GKEConfig yields a GKE
// provisioner. No t.Parallel(): t.Setenv is incompatible with parallel tests.
func TestCreateGKEProvisionerWithConfig(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", fakeADCPath(t))

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			GKE: &clusterprovisioner.GKEConfig{
				Name:     "test-gke",
				Project:  "test-project",
				Location: "europe-north1",
			},
		},
	}

	provisioner, config, err := factory.Create(context.Background(), gkeTestCluster())
	require.NoError(t, err)
	assert.IsType(t, &gkeprovisioner.Provisioner{}, provisioner)

	gkeConfig, isGKEConfig := config.(*clusterprovisioner.GKEConfig)
	require.True(t, isGKEConfig)
	assert.Equal(t, "test-gke", gkeConfig.GetClusterName())
}

// TestCreateGKEProvisionerWithoutProject asserts a missing project surfaces the
// provisioner's clear error. No t.Parallel(): t.Setenv is incompatible with
// parallel tests.
func TestCreateGKEProvisionerWithoutProject(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", fakeADCPath(t))

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			GKE: &clusterprovisioner.GKEConfig{Name: "test-gke"},
		},
	}

	provisioner, _, err := factory.Create(context.Background(), gkeTestCluster())
	require.Error(t, err)
	assert.Nil(t, provisioner)
}
