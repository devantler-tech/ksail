package hostdebug

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
)

// ExportFindClusterNode exposes the unexported findClusterNode helper so the
// external test package can exercise its not-found behavior.
func ExportFindClusterNode(
	ctx context.Context,
	dockerClient docker.Client,
	scheme dockerprovider.LabelScheme,
	clusterName string,
	nodeName string,
) (provider.NodeInfo, error) {
	return findClusterNode(ctx, dockerClient, scheme, clusterName, nodeName)
}
