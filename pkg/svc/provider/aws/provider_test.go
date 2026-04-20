package aws_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errScriptedRunnerEmptyArgs is returned when the scripted runner is invoked
// with no positional arguments (should never happen in real test cases).
var errScriptedRunnerEmptyArgs = errors.New("scripted runner: empty args")

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

	prov, err := aws.NewProvider(client, "us-east-1")
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
	t.Parallel()

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
		if len(call) >= 2 && call[0] == "scale" && call[1] == "nodegroup" {
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
	t.Parallel()

	prov, runner := newProvider(t, map[string][]response{
		"get nodegroup": {{stdout: []byte(`[
			{"Cluster":"demo","Name":"ng-1","DesiredCapacity":0,"MinSize":2,"MaxSize":5},
			{"Cluster":"demo","Name":"ng-2","DesiredCapacity":3,"MinSize":1,"MaxSize":5}
		]`)}},
		"scale nodegroup": {{}},
	})

	err := prov.StartNodes(t.Context(), "demo")
	require.NoError(t, err)

	scales := 0

	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "scale" && call[1] == "nodegroup" {
			scales++

			joined := strings.Join(call, " ")
			assert.Contains(t, joined, "ng-1")
			assert.Contains(t, joined, "--nodes 2")
		}
	}

	assert.Equal(t, 1, scales, "should scale only nodegroups with desiredCapacity=0")
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

// sanity check that provider.Provider interface is implemented.
var _ provider.Provider = (*aws.Provider)(nil)
