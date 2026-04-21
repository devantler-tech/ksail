package eksprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"

	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errScriptedRunnerEmptyArgs = errors.New("scripted runner: empty args")

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

// fakeProvider captures Start/Stop calls without doing real work.
type fakeProvider struct {
	provider.Provider // embedded nil — unused methods would panic

	startCalls []string
	stopCalls  []string

	startErr error
	stopErr  error
}

func (f *fakeProvider) StartNodes(_ context.Context, name string) error {
	f.startCalls = append(f.startCalls, name)

	return f.startErr
}

func (f *fakeProvider) StopNodes(_ context.Context, name string) error {
	f.stopCalls = append(f.stopCalls, name)

	return f.stopErr
}

// The eksctl binary under test must exist on PATH for CheckAvailable. In tests
// we point at /bin/ls which is guaranteed present on Darwin/Linux CI runners;
// the scripted runner intercepts the call so the binary is never executed.
const testBinary = "/bin/ls"

func newProvisioner(
	t *testing.T,
	responses map[string][]response,
	infra provider.Provider,
) (*eksprovisioner.Provisioner, *scriptedRunner) {
	t.Helper()

	runner := &scriptedRunner{t: t, responses: responses}

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(testBinary),
		eksctlclient.WithRunner(runner),
	)

	prov, err := eksprovisioner.NewProvisioner(
		"ksail-test", "us-east-1", "/tmp/eksctl.yaml",
		client, infra,
	)
	require.NoError(t, err)

	return prov, runner
}

func TestNewProvisioner_NilClient(t *testing.T) {
	t.Parallel()

	_, err := eksprovisioner.NewProvisioner("n", "r", "", nil, nil)
	require.ErrorIs(t, err, eksprovisioner.ErrClientRequired)
}

func TestCreate_ShellsOutWithConfig(t *testing.T) {
	t.Parallel()

	prov, runner := newProvisioner(t, map[string][]response{
		"create cluster": {{}},
	}, nil)

	require.NoError(t, prov.Create(context.Background(), ""))

	require.Len(t, runner.calls, 1)
	assert.Equal(
		t,
		[]string{"create", "cluster", "--config-file", "/tmp/eksctl.yaml", "--region", "us-east-1"},
		runner.calls[0],
	)
}

func TestCreate_NoConfigPath_ReturnsError(t *testing.T) {
	t.Parallel()

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(testBinary),
		eksctlclient.WithRunner(&scriptedRunner{t: t, responses: map[string][]response{}}),
	)
	prov, err := eksprovisioner.NewProvisioner("n", "us-east-1", "", client, nil)
	require.NoError(t, err)

	err = prov.Create(context.Background(), "")
	require.ErrorIs(t, err, eksprovisioner.ErrConfigPathRequired)
}

func TestDelete_PrefersConfigFile(t *testing.T) {
	t.Parallel()

	prov, runner := newProvisioner(t, map[string][]response{
		"delete cluster": {{}},
	}, nil)

	require.NoError(t, prov.Delete(context.Background(), ""))

	require.Len(t, runner.calls, 1)
	// DeleteCluster prefers --config-file over --name when configPath set.
	assert.Contains(t, runner.calls[0], "--config-file")
	assert.Contains(t, runner.calls[0], "--wait")
}

func TestStart_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeProvider{}
	prov, _ := newProvisioner(t, map[string][]response{}, fake)

	require.NoError(t, prov.Start(context.Background(), "override-name"))

	assert.Equal(t, []string{"override-name"}, fake.startCalls)
}

func TestStart_NoProvider_ReturnsError(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{}, nil)

	err := prov.Start(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start requires an AWS provider")
}

func TestStop_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeProvider{}
	prov, _ := newProvisioner(t, map[string][]response{}, fake)

	require.NoError(t, prov.Stop(context.Background(), ""))

	assert.Equal(t, []string{"ksail-test"}, fake.stopCalls)
}

func TestStop_NoProvider_ReturnsError(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{}, nil)

	err := prov.Stop(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop requires an AWS provider")
}

func TestSetProvider_Overrides(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{}, nil)
	fake := &fakeProvider{}
	prov.SetProvider(fake)

	require.NoError(t, prov.Stop(context.Background(), ""))
	assert.Equal(t, []string{"ksail-test"}, fake.stopCalls)
}

func TestList_ReturnsClusterNames(t *testing.T) {
	t.Parallel()

	prov, runner := newProvisioner(t, map[string][]response{
		"get cluster": {
			{
				stdout: []byte(
					`[{"name":"a","region":"us-east-1"},{"name":"b","region":"us-east-1"}]`,
				),
			},
		},
	}, nil)

	names, err := prov.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, names)
	require.Len(t, runner.calls, 1)
	assert.Contains(t, runner.calls[0], "--region")
}

func TestList_EmptyOutput(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{
		"get cluster": {{stdout: []byte("null")}},
	}, nil)

	names, err := prov.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestExists_True(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{
		"get cluster": {{stdout: []byte(`[{"name":"ksail-test","region":"us-east-1"}]`)}},
	}, nil)

	ok, err := prov.Exists(context.Background(), "")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestExists_False(t *testing.T) {
	t.Parallel()

	prov, _ := newProvisioner(t, map[string][]response{
		"get cluster": {{stdout: []byte(`[{"name":"other","region":"us-east-1"}]`)}},
	}, nil)

	ok, err := prov.Exists(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestExists_NoName(t *testing.T) {
	t.Parallel()

	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(testBinary),
		eksctlclient.WithRunner(&scriptedRunner{t: t, responses: map[string][]response{}}),
	)
	prov, err := eksprovisioner.NewProvisioner("", "us-east-1", "", client, nil)
	require.NoError(t, err)

	ok, err := prov.Exists(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, ok)
}
