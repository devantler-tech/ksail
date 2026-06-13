package hostdebug_test

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/hostdebug"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/mock"
)

func TestDistributionToLabelScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		distribution v1alpha1.Distribution
		want         dockerprovider.LabelScheme
	}{
		{v1alpha1.DistributionVanilla, dockerprovider.LabelSchemeKind},
		{v1alpha1.DistributionK3s, dockerprovider.LabelSchemeK3d},
		{v1alpha1.DistributionTalos, dockerprovider.LabelSchemeTalos},
		{v1alpha1.DistributionVCluster, dockerprovider.LabelSchemeVCluster},
		{v1alpha1.DistributionKWOK, dockerprovider.LabelSchemeKWOK},
		{v1alpha1.DistributionEKS, dockerprovider.LabelSchemeKind},
	}

	for _, test := range tests {
		got := hostdebug.DistributionToLabelScheme(test.distribution)
		if got != test.want {
			t.Errorf(
				"DistributionToLabelScheme(%v) = %v, want %v",
				test.distribution,
				got,
				test.want,
			)
		}
	}
}

func TestRunUnsupportedDistribution(t *testing.T) {
	t.Parallel()

	err := hostdebug.Run(context.Background(), hostdebug.Options{
		Info: &clusterdetector.Info{Distribution: v1alpha1.DistributionEKS},
	})
	if !errors.Is(err, hostdebug.ErrUnsupportedHostDebug) {
		t.Fatalf("expected ErrUnsupportedHostDebug, got %v", err)
	}
}

func TestRunNonDockerProviderForDockerDistribution(t *testing.T) {
	t.Parallel()

	err := hostdebug.Run(context.Background(), hostdebug.Options{
		Info: &clusterdetector.Info{
			Distribution: v1alpha1.DistributionVanilla,
			Provider:     v1alpha1.ProviderHetzner,
		},
	})
	if !errors.Is(err, hostdebug.ErrUnsupportedHostDebug) {
		t.Fatalf("expected ErrUnsupportedHostDebug for non-Docker provider, got %v", err)
	}
}

func TestFindClusterNodeNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := dockerclient.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil)

	_, err := hostdebug.ExportFindClusterNode(
		ctx,
		mockClient,
		dockerprovider.LabelSchemeKind,
		"my-cluster",
		"missing-node",
	)
	if !errors.Is(err, hostdebug.ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}
