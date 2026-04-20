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
}

func (f *fakeRunner) Run(
	_ context.Context,
	name string,
	args []string,
	stdin io.Reader,
) ([]byte, []byte, error) {
	f.lastName = name

	f.lastArgs = append([]string(nil), args...)

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
