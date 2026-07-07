package cluster_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errEksctlStubEmptyArgs is returned when the stub runner is invoked with no
// positional arguments (should never happen in real test cases).
var errEksctlStubEmptyArgs = errors.New("eksctl stub: empty args")

// eksctlStubRunner replays canned eksctl responses keyed by the first two
// positional arguments (e.g. "get cluster", "get nodegroup"), mirroring the
// scripted-runner pattern used by the aws provider tests.
type eksctlStubRunner struct {
	t         *testing.T
	responses map[string][]byte
	// gotArgs records every argument slice the runner was invoked with, so tests
	// can assert on the flags the eksctl client built (e.g. --region).
	gotArgs [][]string
}

func (s *eksctlStubRunner) Run(
	_ context.Context,
	_ string,
	args []string,
	_ io.Reader,
) ([]byte, []byte, error) {
	s.t.Helper()

	s.gotArgs = append(s.gotArgs, args)

	if len(args) == 0 {
		return nil, nil, errEksctlStubEmptyArgs
	}

	key := args[0]
	if len(args) >= 2 {
		key = args[0] + " " + args[1]
	}

	out, ok := s.responses[key]
	if !ok {
		s.t.Fatalf("eksctl stub: no response configured for %q (args=%v)", key, args)
	}

	return out, nil, nil
}

// newEksctlClient builds an eksctl client whose binary points at the test
// executable (so CheckAvailable's exec.LookPath succeeds hermetically) and
// whose runner replays the supplied canned responses instead of shelling out.
func newEksctlClient(t *testing.T, responses map[string][]byte) *eksctlclient.Client {
	t.Helper()

	return eksctlclient.NewClient(
		eksctlclient.WithBinary(os.Args[0]),
		eksctlclient.WithRunner(&eksctlStubRunner{t: t, responses: responses}),
	)
}

func TestAWSProviderStatus_AggregatesNodegroups(t *testing.T) {
	t.Parallel()

	client := newEksctlClient(t, map[string][]byte{
		"get cluster": []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`),
		"get nodegroup": []byte(
			`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"},{"Cluster":"demo","Name":"ng-2","Status":"ACTIVE"}]`,
		),
	})

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "demo", "")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 2, status.NodesReady)
	assert.True(t, status.Ready)
}

func TestAWSProviderStatus_ClusterNotFound(t *testing.T) {
	t.Parallel()

	client := newEksctlClient(t, map[string][]byte{
		"get cluster": []byte("null"),
	})

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "missing", "")
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
	assert.Nil(t, status)
}

func TestAWSProviderStatus_EksctlUnavailable(t *testing.T) {
	t.Parallel()

	// A binary that does not exist on PATH makes CheckAvailable fail, which
	// must surface as the soft errProviderNotConfigured so 'cluster info' falls
	// back to kubectl rather than erroring out.
	client := eksctlclient.NewClient(
		eksctlclient.WithBinary("ksail-nonexistent-eksctl-binary"),
	)

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "demo", "")
	require.ErrorIs(t, err, cluster.ErrProviderNotConfigured)
	assert.Nil(t, status)
}

// TestAWSProviderStatus_ForwardsRegion asserts the resolved region is threaded
// into eksctl's calls (as --region), so 'cluster info' targets the cluster's
// configured region instead of only eksctl's AWS_REGION/profile default.
func TestAWSProviderStatus_ForwardsRegion(t *testing.T) {
	t.Parallel()

	runner := &eksctlStubRunner{
		t: t,
		responses: map[string][]byte{
			"get cluster": []byte(
				`[{"Name":"demo","Region":"eu-west-1","EksctlCreated":"True"}]`,
			),
			"get nodegroup": []byte(`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"}]`),
		},
	}
	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(os.Args[0]),
		eksctlclient.WithRunner(runner),
	)

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "demo", "eu-west-1")
	require.NoError(t, err)
	require.NotNil(t, status)

	var sawRegion bool

	for _, args := range runner.gotArgs {
		for i := range len(args) - 1 {
			if args[i] == "--region" && args[i+1] == "eu-west-1" {
				sawRegion = true
			}
		}
	}

	assert.True(
		t,
		sawRegion,
		"expected eksctl to be invoked with --region eu-west-1, got %v",
		runner.gotArgs,
	)
}
