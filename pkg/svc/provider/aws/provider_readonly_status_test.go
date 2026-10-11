package aws_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	"github.com/stretchr/testify/require"
)

var errRegionalListingDenied = errors.New("access denied: eks:ListClusters")

// namedReadRunner models a principal allowed to describe one cluster and read
// its nodegroups, but denied enumeration of every cluster in the account.
type namedReadRunner struct{}

// Run returns native named-cluster details, which have no Owned provenance field.
func (namedReadRunner) Run(
	_ context.Context,
	_ string,
	args []string,
	_ io.Reader,
) ([]byte, []byte, error) {
	if len(args) >= 2 && args[0] == "get" && args[1] == "nodegroup" {
		return []byte(`[]`), nil, nil
	}

	if slices.Contains(args, "--name") {
		return []byte(`[{"Name":"demo","Status":"ACTIVE"}]`), nil, nil
	}

	return nil, nil, errRegionalListingDenied
}

// TestGetClusterStatus_DoesNotRequireRegionalEnumeration catches ownership
// corroboration accidentally becoming a permission prerequisite for plain reads.
func TestGetClusterStatus_DoesNotRequireRegionalEnumeration(t *testing.T) {
	t.Parallel()

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary("fixture-eksctl"),
		eksctlclient.WithRunner(namedReadRunner{}),
	)
	prov, err := aws.NewProvider(client, "us-east-1", aws.WithClusterDescriber(
		&fakeDescriber{cluster: &ekstypes.Cluster{Endpoint: awssdk.String(testEndpoint)}},
	))
	require.NoError(t, err)

	status, err := prov.GetClusterStatus(t.Context(), "demo")
	require.NoError(t, err, "named reads must work without eks:ListClusters permission")
	require.Equal(t, provider.PhaseStopped, status.Phase)
	require.Equal(t, testEndpoint, status.Endpoint)

	// A successful read must not authorize ownership when regional corroboration
	// is unavailable to the same principal.
	_, err = client.GetCluster(t.Context(), "demo", "us-east-1")
	require.ErrorIs(t, err, errRegionalListingDenied)
}
