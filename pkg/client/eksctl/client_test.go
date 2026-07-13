package eksctl_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errExitStatus1   = errors.New("exit status 1")
	errExitStatus127 = errors.New("exit status 127")
)

// fakeRunner captures calls and returns canned responses.
type fakeRunner struct {
	stdout []byte
	stderr []byte
	err    error

	lastName  string
	lastArgs  []string
	lastStdin []byte
	lastEnv   []string

	mutateEnvironment bool
}

type legacyRunner struct{}

func (legacyRunner) Run(
	context.Context,
	string,
	[]string,
	io.Reader,
) ([]byte, []byte, error) {
	return nil, nil, nil
}

func (f *fakeRunner) Run(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
) ([]byte, []byte, error) {
	return f.run(ctx, name, args, stdin, nil)
}

func (f *fakeRunner) RunWithEnvironment(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
	environment []string,
) ([]byte, []byte, error) {
	return f.run(ctx, name, args, stdin, environment)
}

func (f *fakeRunner) run(
	_ context.Context,
	name string,
	args []string,
	stdin io.Reader,
	environment []string,
) ([]byte, []byte, error) {
	f.lastName = name

	f.lastArgs = append([]string(nil), args...)
	f.lastEnv = append([]string(nil), environment...)

	if f.mutateEnvironment && len(environment) > 0 {
		environment[0] = "MUTATED_BY_RUNNER"
	}

	if stdin != nil {
		buf, _ := io.ReadAll(stdin)
		f.lastStdin = buf
	}

	return f.stdout, f.stderr, f.err
}

func newTestClient(runner *fakeRunner) *eksctl.Client {
	return eksctl.NewClient(
		eksctl.WithBinary("eksctl-under-test"),
		eksctl.WithRunner(runner),
	)
}

func TestNewClient_Defaults(t *testing.T) {
	t.Parallel()

	client := eksctl.NewClient()
	assert.Equal(t, eksctl.DefaultBinary, client.Binary())
}

func TestWithBinary_Override(t *testing.T) {
	t.Parallel()

	client := eksctl.NewClient(eksctl.WithBinary("/usr/local/bin/eksctl-custom"))
	assert.Equal(t, "/usr/local/bin/eksctl-custom", client.Binary())
}

func TestWithBinary_EmptyIgnored(t *testing.T) {
	t.Parallel()

	client := eksctl.NewClient(eksctl.WithBinary(""))
	assert.Equal(t, eksctl.DefaultBinary, client.Binary())
}

func TestWithEnvironment_ForwardsAnIsolatedSnapshot(t *testing.T) {
	t.Parallel()

	environment := []string{"HOME=/tmp/ksail", "AWS_PROFILE=custom-profile"}
	runner := &fakeRunner{mutateEnvironment: true}
	client := eksctl.NewClient(
		eksctl.WithRunner(runner),
		eksctl.WithEnvironment(environment),
	)

	// Construction must snapshot the caller's slice.
	environment[0] = "HOME=/mutated-by-caller"

	_, _, err := client.Exec(t.Context(), "get", "cluster")
	require.NoError(t, err)
	assert.Equal(t, []string{"HOME=/tmp/ksail", "AWS_PROFILE=custom-profile"}, runner.lastEnv)

	// A runner must not be able to mutate the environment reused by the next
	// command on the same client.
	_, _, err = client.ExecWithStdin(
		t.Context(),
		bytes.NewBufferString("config"),
		"create",
		"cluster",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"HOME=/tmp/ksail", "AWS_PROFILE=custom-profile"}, runner.lastEnv)
}

func TestNewClient_DefaultEnvironmentInheritsParent(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := eksctl.NewClient(eksctl.WithRunner(runner))

	_, _, err := client.Exec(t.Context(), "get", "cluster")
	require.NoError(t, err)
	assert.Nil(t, runner.lastEnv)
}

func TestWithEnvironment_FailsClosedForLegacyRunner(t *testing.T) {
	t.Parallel()

	client := eksctl.NewClient(
		eksctl.WithRunner(legacyRunner{}),
		eksctl.WithEnvironment([]string{"PATH=/usr/bin", "AWS_PROFILE=selected"}),
	)

	_, _, err := client.Exec(t.Context(), "get", "cluster")
	require.ErrorIs(t, err, eksctl.ErrRunnerEnvironmentUnsupported)
}

func TestExec_RedactsCredentialValuesFromStderrErrors(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stderr: []byte("provider rejected fixture-secret-value"),
		err:    errExitStatus1,
	}
	client := eksctl.NewClient(
		eksctl.WithRunner(runner),
		eksctl.WithEnvironment([]string{
			"PATH=/usr/bin",
			"AWS_SECRET_ACCESS_KEY=fixture-secret-value",
		}),
	)

	_, stderr, err := client.Exec(t.Context(), "get", "cluster")
	require.Error(t, err)
	assert.NotContains(t, string(stderr), "fixture-secret-value")
	assert.NotContains(t, err.Error(), "fixture-secret-value")
	assert.Contains(t, err.Error(), "[REDACTED]")
}

func TestExec_RedactsOverlappingCredentialValuesLongestFirst(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stderr: []byte("provider rejected fixture-secret-long"),
		err:    errExitStatus1,
	}
	client := eksctl.NewClient(
		eksctl.WithRunner(runner),
		eksctl.WithEnvironment([]string{
			"AWS_PROFILE=fixture-secret",
			"AWS_SECRET_ACCESS_KEY=fixture-secret-long",
		}),
	)

	_, stderr, err := client.Exec(t.Context(), "get", "cluster")
	require.Error(t, err)
	assert.Equal(t, "provider rejected [REDACTED]", string(stderr))
	assert.NotContains(t, err.Error(), "-long")
}

func TestRequireCredentialValuesRejectsMissingAndPartialSelections(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		environment []string
		expectedErr error
	}{
		"missing": {
			environment: []string{"PATH=/usr/bin"},
			expectedErr: eksctl.ErrExplicitCredentialsUnavailable,
		},
		"access without secret": {
			environment: []string{"AWS_ACCESS_KEY_ID=fixture-access"},
			expectedErr: eksctl.ErrIncompleteStaticCredentials,
		},
		"secret without access": {
			environment: []string{"AWS_SECRET_ACCESS_KEY=fixture-secret"},
			expectedErr: eksctl.ErrIncompleteStaticCredentials,
		},
		"session without static pair": {
			environment: []string{"AWS_SESSION_TOKEN=fixture-session"},
			expectedErr: eksctl.ErrIncompleteStaticCredentials,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{}
			client := eksctl.NewClient(
				eksctl.WithRunner(runner),
				eksctl.WithEnvironment(test.environment),
				eksctl.RequireCredentialValues(),
			)

			_, _, err := client.Exec(t.Context(), "get", "cluster")
			require.ErrorIs(t, err, test.expectedErr)
			assert.Empty(t, runner.lastArgs, "invalid credentials must fail before invoking eksctl")
		})
	}
}

func TestRequireCredentialValuesAcceptsProfileOrStaticPair(t *testing.T) {
	t.Parallel()

	for name, environment := range map[string][]string{
		"profile": {"AWS_PROFILE=selected-profile"},
		"static pair": {
			"AWS_ACCESS_KEY_ID=fixture-access",
			"AWS_SECRET_ACCESS_KEY=fixture-secret",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{}
			client := eksctl.NewClient(
				eksctl.WithRunner(runner),
				eksctl.WithEnvironment(environment),
				eksctl.RequireCredentialValues(),
			)

			_, _, err := client.Exec(t.Context(), "get", "cluster")
			require.NoError(t, err)
			assert.Equal(t, []string{"get", "cluster"}, runner.lastArgs)
		})
	}
}

func TestCreateCluster_InvokesCorrectArgs(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.CreateCluster(t.Context(), "eks.yaml", "us-east-1")
	require.NoError(t, err)

	assert.Equal(t, "eksctl-under-test", runner.lastName)
	assert.Equal(t,
		[]string{"create", "cluster", "--config-file", "eks.yaml", "--region", "us-east-1"},
		runner.lastArgs,
	)
}

func TestCreateCluster_NoRegionOmitsFlag(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.CreateCluster(t.Context(), "eks.yaml", "")
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"create", "cluster", "--config-file", "eks.yaml"},
		runner.lastArgs,
	)
}

func TestCreateCluster_WithKubeconfigPinsOutputPath(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.CreateClusterWithKubeconfig(
		t.Context(),
		"eks.yaml",
		"eu-west-1",
		"/tmp/ksail-kubeconfig",
	)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{
			"create", "cluster",
			"--config-file", "eks.yaml",
			"--region", "eu-west-1",
			"--kubeconfig", "/tmp/ksail-kubeconfig",
		},
		runner.lastArgs,
	)
}

func TestCreateCluster_EmptyConfigPath_ReturnsError(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	err := client.CreateCluster(t.Context(), "  ", "us-east-1")
	require.ErrorIs(t, err, eksctl.ErrEmptyConfigPath)
}

func TestDeleteCluster_ByName(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.DeleteCluster(t.Context(), "my-cluster", "eu-west-1", "", true)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{
			"delete", "cluster",
			"--name", "my-cluster",
			"--region", "eu-west-1",
			"--wait",
		},
		runner.lastArgs,
	)
}

func TestDeleteCluster_ByConfigFile(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.DeleteCluster(t.Context(), "", "", "eks.yaml", false)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"delete", "cluster", "--config-file", "eks.yaml"},
		runner.lastArgs,
	)
}

func TestDeleteCluster_NoNameOrConfig(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	err := client.DeleteCluster(t.Context(), "", "", "", false)
	require.ErrorIs(t, err, eksctl.ErrEmptyClusterName)
}

func TestGetCluster_NotFound(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte("null")}
	client := newTestClient(runner)

	_, err := client.GetCluster(t.Context(), "missing", "us-east-1")
	require.ErrorIs(t, err, eksctl.ErrClusterNotFound)
}

func TestGetCluster_HappyPath(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stdout: []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`),
	}
	client := newTestClient(runner)

	cluster, err := client.GetCluster(t.Context(), "demo", "us-east-1")
	require.NoError(t, err)
	assert.Equal(t, "demo", cluster.Name)
	assert.Equal(t, "us-east-1", cluster.Region)
	assert.Equal(t, "True", cluster.EKSCTLCreated)
}

func TestGetCluster_EmptyName(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	_, err := client.GetCluster(t.Context(), "", "us-east-1")
	require.ErrorIs(t, err, eksctl.ErrEmptyClusterName)
}

func TestListClusters_EmptyOutput(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte(" \n")}
	client := newTestClient(runner)

	clusters, err := client.ListClusters(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, clusters)
}

func TestListClusters_HappyPath(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stdout: []byte(`[
			{"Name":"a","Region":"us-east-1","EksctlCreated":"True"},
			{"Name":"b","Region":"us-east-1","EksctlCreated":"False"}
		]`),
	}
	client := newTestClient(runner)

	clusters, err := client.ListClusters(t.Context(), "us-east-1")
	require.NoError(t, err)
	require.Len(t, clusters, 2)
	assert.Equal(t, "a", clusters[0].Name)
	assert.Equal(t, "b", clusters[1].Name)
}

func TestListNodegroups_HappyPath(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stdout: []byte(`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE",
			"DesiredCapacity":2,"MinSize":1,"MaxSize":3,
			"InstanceType":"t3.medium","NodeGroupType":"managed","Version":"1.31"}]`),
	}
	client := newTestClient(runner)

	groups, err := client.ListNodegroups(t.Context(), "demo", "us-east-1")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "ng-1", groups[0].Name)
	assert.Equal(t, 2, groups[0].DesiredCap)
	assert.Equal(t, "managed", groups[0].NodeGroupType)
}

func TestListNodegroups_EmptyOutput(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte("")}
	client := newTestClient(runner)

	groups, err := client.ListNodegroups(t.Context(), "demo", "")
	require.NoError(t, err)
	assert.Nil(t, groups)
}

func TestListNodegroups_EmptyClusterName(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	_, err := client.ListNodegroups(t.Context(), "", "us-east-1")
	require.ErrorIs(t, err, eksctl.ErrEmptyClusterName)
}

func TestScaleNodegroup_AllFlags(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.ScaleNodegroup(t.Context(), "demo", "ng-1", "us-east-1", 5, 2, 10)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{
			"scale", "nodegroup",
			"--cluster", "demo",
			"--name", "ng-1",
			"--nodes", "5",
			"--nodes-min", "2",
			"--nodes-max", "10",
			"--region", "us-east-1",
		},
		runner.lastArgs,
	)
}

func TestScaleNodegroup_NegativeMinMaxOmitted(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.ScaleNodegroup(t.Context(), "demo", "ng-1", "", 3, -1, -1)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{
			"scale", "nodegroup",
			"--cluster", "demo",
			"--name", "ng-1",
			"--nodes", "3",
		},
		runner.lastArgs,
	)
}

func TestScaleNodegroup_EmptyNames(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	err := client.ScaleNodegroup(t.Context(), "", "ng", "", 1, -1, -1)
	require.ErrorIs(t, err, eksctl.ErrEmptyClusterName)

	err = client.ScaleNodegroup(t.Context(), "c", "", "", 1, -1, -1)
	require.ErrorIs(t, err, eksctl.ErrEmptyNodegroupName)
}

func TestUpgradeCluster_WithApprove(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.UpgradeCluster(t.Context(), "eks.yaml", true)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"upgrade", "cluster", "--config-file", "eks.yaml", "--approve"},
		runner.lastArgs,
	)
}

func TestUpgradeCluster_DryRunOmitsApprove(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	err := client.UpgradeCluster(t.Context(), "eks.yaml", false)
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"upgrade", "cluster", "--config-file", "eks.yaml"},
		runner.lastArgs,
	)
}

func TestUpgradeCluster_EmptyConfigPath(t *testing.T) {
	t.Parallel()

	client := newTestClient(&fakeRunner{})
	err := client.UpgradeCluster(t.Context(), "", true)
	require.ErrorIs(t, err, eksctl.ErrEmptyConfigPath)
}

func TestExec_WrapsErrorWithFirstStderrLine(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stderr: []byte("Error: cluster \"missing\" not found\n\nstacktrace..."),
		err:    errExitStatus1,
	}
	client := newTestClient(runner)

	_, _, err := client.Exec(t.Context(), "get", "cluster")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster \"missing\" not found")
	assert.Contains(t, err.Error(), "get cluster")
}

func TestExec_WrapsErrorWithoutStderr(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{err: errExitStatus127}
	client := newTestClient(runner)

	_, _, err := client.Exec(t.Context(), "version")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
	assert.Contains(t, err.Error(), "exit status 127")
}

func TestExecWithStdin_ForwardsStdin(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	client := newTestClient(runner)

	payload := bytes.NewBufferString("apiVersion: eksctl.io/v1alpha5\n")

	_, _, err := client.ExecWithStdin(t.Context(), payload, "create", "cluster", "-f", "-")
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: eksctl.io/v1alpha5\n", string(runner.lastStdin))
}

func TestCheckAvailable_MissingBinary(t *testing.T) {
	t.Parallel()

	client := eksctl.NewClient(
		eksctl.WithBinary("/nonexistent/eksctl-does-not-exist"),
	)

	err := client.CheckAvailable()
	require.ErrorIs(t, err, eksctl.ErrBinaryNotFound)
}
