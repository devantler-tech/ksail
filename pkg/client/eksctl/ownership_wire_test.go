package eksctl_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pinned eksctl v0.229.0 named command prints an EKS Cluster, whereas its
// regional listing prints Description{Name, Region, Owned}. Keep those wire
// formats separate so invented provenance in a named-response fixture cannot
// hide a production ownership failure.
type ownershipWireRunner struct {
	named, regional string
	regionalErr     error
	calls           [][]string
}

var errRegionalOwnershipQuery = errors.New("regional query refused")

// Run keeps native named details separate from the regional provenance response.
func (r *ownershipWireRunner) Run(
	_ context.Context,
	_ string,
	args []string,
	_ io.Reader,
) ([]byte, []byte, error) {
	r.calls = append(r.calls, slices.Clone(args))
	if slices.Contains(args, "--name") {
		return []byte(r.named), nil, nil
	}

	return []byte(r.regional), nil, r.regionalErr
}

// TestGetClusterUsesRegionalOwnershipForDetailedNamedResponse catches reliance on nonexistent named provenance.
func TestGetClusterUsesRegionalOwnershipForDetailedNamedResponse(t *testing.T) {
	t.Parallel()

	runner := &ownershipWireRunner{
		named: `[{"Name":"demo","Status":"ACTIVE","Version":"1.36",
			"Arn":"arn:aws:eks:eu-north-1:123456789012:cluster/demo","CreatedAt":"2026-10-01T12:00:00Z"}]`,
		regional: `[
			{"Name":"another","Region":"eu-north-1","Owned":"False"},
			{"Name":"demo","Region":"eu-north-1","Owned":"True"}
		]`,
	}
	client := eksctl.NewClient(eksctl.WithBinary("fixture-eksctl"), eksctl.WithRunner(runner))

	got, err := client.GetCluster(t.Context(), "demo", "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, "demo", got.Name)
	assert.Equal(t, "eu-north-1", got.Region)
	assert.Equal(t, "True", got.EKSCTLCreated)
	assert.Equal(t, [][]string{
		{"get", "cluster", "--name", "demo", "--output", "json", "--region", "eu-north-1"},
		{"get", "cluster", "--output", "json", "--region", "eu-north-1"},
	}, runner.calls)
}

// TestListClustersReadsEksctlOwnedField pins the published regional wire field rather than a synthetic tag.
func TestListClustersReadsEksctlOwnedField(t *testing.T) {
	t.Parallel()

	runner := &ownershipWireRunner{
		regional: `[{"Name":"demo","Region":"eu-north-1","Owned":"False"}]`,
	}
	client := eksctl.NewClient(eksctl.WithBinary("fixture-eksctl"), eksctl.WithRunner(runner))
	got, err := client.ListClusters(t.Context(), "eu-north-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "False", got[0].EKSCTLCreated)
}

// TestGetClusterRejectsUncorroboratedRegionalOwnership rejects missing, ambiguous, malformed, and refused evidence.
func TestGetClusterRejectsUncorroboratedRegionalOwnership(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, regional string
		err            error
	}{
		{name: "absent", regional: `[]`},
		{name: "another cluster", regional: `[{"Name":"other","Region":"eu-north-1","Owned":"True"}]`},
		{name: "duplicate target", regional: `[
			{"Name":"demo","Region":"eu-north-1","Owned":"True"},
			{"Name":"demo","Region":"eu-north-1","Owned":"True"}
		]`},
		{name: "invalid json", regional: `{`},
		{name: "failed query", err: errRegionalOwnershipQuery},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runner := &ownershipWireRunner{
				named:       `[{"Name":"demo","Status":"ACTIVE"}]`,
				regional:    testCase.regional,
				regionalErr: testCase.err,
			}
			client := eksctl.NewClient(
				eksctl.WithBinary("fixture-eksctl"),
				eksctl.WithRunner(runner),
			)
			_, err := client.GetCluster(t.Context(), "demo", "eu-north-1")
			require.Error(t, err)
		})
	}
}

// TestGetClusterPreservesNegativeRegionalOwnership prevents false or unknown provenance becoming ownership.
func TestGetClusterPreservesNegativeRegionalOwnership(t *testing.T) {
	t.Parallel()

	for _, owned := range []string{"False", "Unknown", ""} {
		t.Run(owned, func(t *testing.T) {
			t.Parallel()

			runner := &ownershipWireRunner{
				named:    `[{"Name":"demo","Status":"ACTIVE"}]`,
				regional: `[{"Name":"demo","Region":"eu-north-1","Owned":"` + owned + `"}]`,
			}
			client := eksctl.NewClient(
				eksctl.WithBinary("fixture-eksctl"),
				eksctl.WithRunner(runner),
			)
			got, err := client.GetCluster(t.Context(), "demo", "eu-north-1")
			require.NoError(t, err)
			assert.Equal(t, owned, got.EKSCTLCreated)
		})
	}
}

// TestGetClusterRejectsMultipleNamedResults prevents one arbitrarily selected cluster from satisfying a named query.
func TestGetClusterRejectsMultipleNamedResults(t *testing.T) {
	t.Parallel()

	runner := &ownershipWireRunner{named: `[{"Name":"demo"},{"Name":"demo"}]`}
	client := eksctl.NewClient(eksctl.WithBinary("fixture-eksctl"), eksctl.WithRunner(runner))
	_, err := client.GetCluster(t.Context(), "demo", "eu-north-1")
	require.Error(t, err)
}
