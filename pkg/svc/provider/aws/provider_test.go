package aws_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEndpoint is the control-plane endpoint the default fake describer
// returns so GetClusterStatus success paths never resolve real AWS
// credentials.
const (
	testEndpoint        = "https://ABCDEF0123456789.gr7.us-east-1.eks.amazonaws.com"
	nodegroupSubcommand = "nodegroup"
)

// fakeDescriber is a credential-free stand-in for the AWS-SDK EKS
// DescribeCluster seam. It records the requested name and returns the
// configured cluster/err.
type fakeDescriber struct {
	cluster *ekstypes.Cluster
	err     error
	gotName string
}

func (f *fakeDescriber) DescribeCluster(
	_ context.Context,
	name string,
) (*ekstypes.Cluster, error) {
	f.gotName = name

	return f.cluster, f.err
}

// errScriptedRunnerEmptyArgs is returned when the scripted runner is invoked
// with no positional arguments (should never happen in real test cases).
var errScriptedRunnerEmptyArgs = errors.New("scripted runner: empty args")

// errDescribeDenied is a static describer error used to assert that
// GetClusterStatus surfaces a DescribeCluster failure.
var errDescribeDenied = errors.New("access denied")

var errScaleDenied = errors.New("scale denied")

// scriptedRunner replays canned responses keyed by the first argument
// (`create`, `delete`, `get`, `scale`, `upgrade`). It records every call for
// assertions.
type scriptedRunner struct {
	t         *testing.T
	responses map[string][]response
	calls     [][]string
}

type response struct {
	stdout []byte
	stderr []byte
	err    error
}

func (s *scriptedRunner) Run(
	_ context.Context,
	_ string,
	args []string,
	_ io.Reader,
) ([]byte, []byte, error) {
	s.t.Helper()

	s.calls = append(s.calls, append([]string(nil), args...))

	if len(args) == 0 {
		return nil, nil, errScriptedRunnerEmptyArgs
	}

	key := args[0]
	if len(args) >= 2 {
		key = args[0] + " " + args[1]
	}

	queue, ok := s.responses[key]
	if !ok || len(queue) == 0 {
		s.t.Fatalf("scripted runner: no response configured for %q (args=%v)", key, args)
	}

	resp := queue[0]
	s.responses[key] = queue[1:]

	return resp.stdout, resp.stderr, resp.err
}

func newProvider(t *testing.T, responses map[string][]response) (*aws.Provider, *scriptedRunner) {
	t.Helper()

	runner := &scriptedRunner{t: t, responses: responses}

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary("eksctl-under-test"),
		eksctlclient.WithRunner(runner),
	)

	// Inject a happy describer so GetClusterStatus success paths populate the
	// endpoint without resolving real AWS credentials. Tests that exercise the
	// endpoint itself construct the provider with their own describer inline.
	prov, err := aws.NewProvider(client, "us-east-1", aws.WithClusterDescriber(
		&fakeDescriber{cluster: &ekstypes.Cluster{Endpoint: awssdk.String(testEndpoint)}},
	))
	require.NoError(t, err)

	return prov, runner
}

func TestNewProvider_NilClient(t *testing.T) {
	t.Parallel()

	_, err := aws.NewProvider(nil, "us-east-1")
	require.ErrorIs(t, err, aws.ErrClientRequired)
}

func TestProvider_Region(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{})
	assert.Equal(t, "us-east-1", prov.Region())
}

func TestListAllClusters_ReturnsNames(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get cluster": {{stdout: []byte(`[
			{"Name":"a","Region":"us-east-1","EksctlCreated":"True"},
			{"Name":"b","Region":"us-east-1","EksctlCreated":"True"}
		]`)}},
	})

	names, err := prov.ListAllClusters(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, names)
}

func TestListNodes_MapsNodegroupsToNodeInfo(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[
			{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE","DesiredCapacity":2,
			 "MinSize":1,"MaxSize":3,"NodeGroupType":"managed"},
			{"Cluster":"demo","Name":"ng-2","Status":"CREATING","DesiredCapacity":1,
			 "MinSize":1,"MaxSize":2,"NodeGroupType":"managed"}
		]`)}},
	})

	nodes, err := prov.ListNodes(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "ng-1", nodes[0].Name)
	assert.Equal(t, "demo", nodes[0].ClusterName)
	assert.Equal(t, "ACTIVE", nodes[0].State)
	assert.Equal(t, "worker", nodes[0].Role)
	assert.Equal(t, "CREATING", nodes[1].State)
}

func TestNodesExist_True(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"}]`)}},
	})

	exists, err := prov.NodesExist(t.Context(), "demo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestNodesExist_False(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`null`)}},
	})

	exists, err := prov.NodesExist(t.Context(), "demo")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDeleteNodes_Noop(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{})
	require.NoError(t, prov.DeleteNodes(t.Context(), "demo"))
}

func TestStopNodes_ScalesAllToZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[
			{"Cluster":"demo","Name":"ng-1","DesiredCapacity":2,"MinSize":1,"MaxSize":3},
			{"Cluster":"demo","Name":"ng-2","DesiredCapacity":1,"MinSize":1,"MaxSize":2}
		]`)}},
		"scale nodegroup": {{}, {}},
	})

	err := prov.StopNodes(t.Context(), "demo")
	require.NoError(t, err)

	var scales [][]string

	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "scale" && call[1] == nodegroupSubcommand {
			scales = append(scales, call)
		}
	}

	require.Len(t, scales, 2)
	assert.Contains(t, strings.Join(scales[0], " "), "--nodes 0")
	assert.Contains(t, strings.Join(scales[1], " "), "--nodes 0")
}

func TestStopNodes_NoNodegroups(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`null`)}},
	})

	err := prov.StopNodes(t.Context(), "demo")
	require.ErrorIs(t, err, provider.ErrNoNodes)
}

func TestStartNodes_ScalesOnlyZeroDesired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, state.SaveEKSNodegroupState("demo", &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: "demo",
		Region:      "us-east-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "ng-1", DesiredCapacity: 2, MinSize: 2, MaxSize: 5},
			{Name: "ng-2", DesiredCapacity: 3, MinSize: 1, MaxSize: 5},
		},
	}))

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {
			{stdout: []byte(`[
				{"Cluster":"demo","Name":"ng-1","DesiredCapacity":0,"MinSize":0,"MaxSize":5},
				{"Cluster":"demo","Name":"ng-2","DesiredCapacity":3,"MinSize":1,"MaxSize":5}
			]`)},
			{stdout: []byte(`[
				{"Cluster":"demo","Name":"ng-1","DesiredCapacity":2,"MinSize":2,"MaxSize":5},
				{"Cluster":"demo","Name":"ng-2","DesiredCapacity":3,"MinSize":1,"MaxSize":5}
			]`)},
		},
		"scale nodegroup": {{}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.NoError(t, err)

	scales := 0

	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "scale" && call[1] == nodegroupSubcommand {
			scales++

			joined := strings.Join(call, " ")
			assert.Contains(t, joined, "ng-1")
			assert.Contains(t, joined, "--nodes 2")
		}
	}

	assert.Equal(t, 1, scales, "should scale only nodegroups with desiredCapacity=0")
}

// TestStopThenStartRestoresNodegroupCapacities proves the lifecycle round trip does not lose the
// desired/minimum sizes when stop must temporarily set both values to zero.
func TestStopThenStartRestoresNodegroupCapacities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {
			{stdout: []byte(`[
				{"Cluster":"demo","Name":"ng-1","DesiredCapacity":3,"MinSize":2,"MaxSize":5}
			]`)},
			{stdout: []byte(`[
				{"Cluster":"demo","Name":"ng-1","DesiredCapacity":0,"MinSize":0,"MaxSize":5}
			]`)},
			{stdout: []byte(`[
				{"Cluster":"demo","Name":"ng-1","DesiredCapacity":3,"MinSize":2,"MaxSize":5}
			]`)},
		},
		"scale nodegroup": {{}, {}},
	})

	require.NoError(t, prov.StopNodes(t.Context(), "demo"))
	require.NoError(t, prov.StartNodes(t.Context(), "demo"))

	var scales []string

	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "scale" && call[1] == nodegroupSubcommand {
			scales = append(scales, strings.Join(call, " "))
		}
	}

	require.Equal(t, []string{
		"scale nodegroup --cluster demo --name ng-1 --nodes 0 --nodes-min 0 --nodes-max 5 --region us-east-1",
		"scale nodegroup --cluster demo --name ng-1 --nodes 3 --nodes-min 2 --nodes-max 5 --region us-east-1",
	}, scales)

	_, err := state.LoadEKSNodegroupState("demo")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)
}

func TestRepeatedStopPreservesOriginalNodegroupCapacities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {
			{stdout: nodegroupJSON(3, 2)},
			{stdout: nodegroupJSON(0, 0)},
			{stdout: nodegroupJSON(0, 0)},
			{stdout: nodegroupJSON(3, 2)},
		},
		"scale nodegroup": {{}, {}},
	})

	require.NoError(t, prov.StopNodes(t.Context(), "demo"))
	require.NoError(t, prov.StopNodes(t.Context(), "demo"))
	require.NoError(t, prov.StartNodes(t.Context(), "demo"))

	assert.Equal(t, []string{
		"scale nodegroup --cluster demo --name ng-1 --nodes 0 --nodes-min 0 --nodes-max 5 --region us-east-1",
		"scale nodegroup --cluster demo --name ng-1 --nodes 3 --nodes-min 2 --nodes-max 5 --region us-east-1",
	}, scaleCalls(runner.calls))
}

func TestStopNodesRejectsInvalidCapacityBeforePersisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: nodegroupJSON(1, 2)}},
	})

	err := prov.StopNodes(t.Context(), "demo")
	require.Error(t, err)
	assert.Empty(t, scaleCalls(runner.calls))

	_, loadErr := state.LoadEKSNodegroupState("demo")
	require.ErrorIs(t, loadErr, state.ErrEKSNodegroupStateNotFound)
}

func TestStartNodesRejectsMismatchedCapacityStateBeforeScaling(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveNodegroupState(t, "demo", "eu-north-1", 3, 2, 5)

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: nodegroupJSON(0, 0)}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "region")
	assert.Empty(t, scaleCalls(runner.calls))
}

func TestStartNodesRetainsCapacityStateWhenScalingFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveNodegroupState(t, "demo", "us-east-1", 3, 2, 5)

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup":   {{stdout: nodegroupJSON(0, 0)}},
		"scale nodegroup": {{err: errScaleDenied}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.ErrorIs(t, err, errScaleDenied)

	saved, loadErr := state.LoadEKSNodegroupState("demo")
	require.NoError(t, loadErr)
	assert.Equal(t, 3, saved.Nodegroups[0].DesiredCapacity)
	assert.Equal(t, 2, saved.Nodegroups[0].MinSize)
}

func TestStartNodesRequiresSnapshotWhenMinimumIsZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: nodegroupJSON(0, 0)}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-stop snapshot")
	assert.Empty(t, scaleCalls(runner.calls))
}

func TestStartNodesWithoutSnapshotPreflightsEveryNodegroup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[
			{"Cluster":"demo","Name":"a-recoverable","DesiredCapacity":0,"MinSize":2,"MaxSize":5},
			{"Cluster":"demo","Name":"z-missing-state","DesiredCapacity":0,"MinSize":0,"MaxSize":5}
		]`)}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "z-missing-state")
	assert.Empty(t, scaleCalls(runner.calls))
}

func TestStartNodesRetainsCapacityStateUntilReadbackMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveNodegroupState(t, "demo", "us-east-1", 3, 2, 5)

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {
			{stdout: nodegroupJSON(0, 0)},
			{stdout: nodegroupJSON(0, 0)},
		},
		"scale nodegroup": {{}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not restored")

	_, loadErr := state.LoadEKSNodegroupState("demo")
	require.NoError(t, loadErr)
}

func nodegroupJSON(desiredCapacity, minSize int) []byte {
	return fmt.Appendf(
		nil,
		`[{"Cluster":"demo","Name":"ng-1","DesiredCapacity":%d,"MinSize":%d,"MaxSize":5}]`,
		desiredCapacity,
		minSize,
	)
}

func saveNodegroupState(
	t *testing.T,
	clusterName, region string,
	desiredCapacity, minSize, maxSize int,
) {
	t.Helper()

	require.NoError(t, state.SaveEKSNodegroupState(clusterName, &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      region,
		Nodegroups: []state.EKSNodegroupCapacity{
			{
				Name:            "ng-1",
				DesiredCapacity: desiredCapacity,
				MinSize:         minSize,
				MaxSize:         maxSize,
			},
		},
	}))
}

func scaleCalls(calls [][]string) []string {
	scales := make([]string, 0)

	for _, call := range calls {
		if len(call) >= 2 && call[0] == "scale" && call[1] == nodegroupSubcommand {
			scales = append(scales, strings.Join(call, " "))
		}
	}

	return scales
}

func TestStartNodes_NoNodegroups(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[]`)}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.ErrorIs(t, err, provider.ErrNoNodes)
}

func TestGetClusterStatus_NotFound(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get cluster": {{stdout: []byte("null")}},
	})

	_, err := prov.GetClusterStatus(t.Context(), "missing")
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

func TestGetClusterStatus_ZeroNodegroups(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get cluster": {
			{stdout: []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`)},
		},
		"get nodegroup": {{stdout: []byte(`[]`)}},
	})

	status, err := prov.GetClusterStatus(t.Context(), "demo")
	require.NoError(t, err)
	require.NotNil(t, status, "existing cluster with zero nodegroups must yield a non-nil status")
	assert.Equal(t, 0, status.NodesTotal)
	assert.Equal(t, 0, status.NodesReady)
	assert.False(t, status.Ready)
	assert.Equal(t, "stopped", status.Phase)
}

func TestGetClusterStatus_AggregatesNodegroupStatus(t *testing.T) {
	t.Parallel()

	prov, _ := newProvider(t, map[string][]response{
		"get cluster": {
			{stdout: []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`)},
		},
		"get nodegroup": {{stdout: []byte(`[
			{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"},
			{"Cluster":"demo","Name":"ng-2","Status":"CREATING"}
		]`)}},
	})

	status, err := prov.GetClusterStatus(t.Context(), "demo")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 1, status.NodesReady)
	assert.False(t, status.Ready)
	assert.Equal(t, "degraded", status.Phase)
}

// newProviderWithDescriber builds a provider whose eksctl client reports a
// single running cluster, paired with the given describer, so the endpoint
// branch of GetClusterStatus can be exercised in isolation.
func newProviderWithDescriber(
	t *testing.T,
	describer aws.Option,
) *aws.Provider {
	t.Helper()

	runner := &scriptedRunner{t: t, responses: map[string][]response{
		"get cluster": {
			{stdout: []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`)},
		},
		"get nodegroup": {{stdout: []byte(`[]`)}},
	}}

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary("eksctl-under-test"),
		eksctlclient.WithRunner(runner),
	)

	prov, err := aws.NewProvider(client, "us-east-1", describer)
	require.NoError(t, err)

	return prov
}

func TestGetClusterStatus_PopulatesEndpoint(t *testing.T) {
	t.Parallel()

	describer := &fakeDescriber{
		cluster: &ekstypes.Cluster{Endpoint: awssdk.String(testEndpoint)},
	}
	prov := newProviderWithDescriber(t, aws.WithClusterDescriber(describer))

	status, err := prov.GetClusterStatus(t.Context(), "demo")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, testEndpoint, status.Endpoint)
	assert.Equal(t, "demo", describer.gotName, "endpoint must be read for the requested cluster")
}

func TestGetClusterStatus_EmptyEndpointWhenAbsent(t *testing.T) {
	t.Parallel()

	// A described cluster with a nil Endpoint (still provisioning) yields an
	// empty endpoint string, not a failure — cluster info omits the line.
	prov := newProviderWithDescriber(t, aws.WithClusterDescriber(
		&fakeDescriber{cluster: &ekstypes.Cluster{}},
	))

	status, err := prov.GetClusterStatus(t.Context(), "demo")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Empty(t, status.Endpoint)
}

func TestGetClusterStatus_DescribeError(t *testing.T) {
	t.Parallel()

	prov := newProviderWithDescriber(t, aws.WithClusterDescriber(
		&fakeDescriber{err: errDescribeDenied},
	))

	_, err := prov.GetClusterStatus(t.Context(), "demo")
	require.ErrorIs(t, err, errDescribeDenied)
}

// sanity check that provider.Provider interface is implemented.
var _ provider.Provider = (*aws.Provider)(nil)
