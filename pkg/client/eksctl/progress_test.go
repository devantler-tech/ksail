package eksctl_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureSecret is a credential value that must never reach progress output or errors.
const fixtureSecret = "fixture-secret-access-key-value"

// streamingRunner is a fake runner that, like the real ExecRunner, writes the command's
// output to the progress writer while it runs and also returns it buffered.
type streamingRunner struct {
	stdout []byte
	stderr []byte
	err    error

	progressCalls int
	plainCalls    int
}

// Run records a non-streaming invocation.
func (s *streamingRunner) Run(
	context.Context,
	string,
	[]string,
	io.Reader,
) ([]byte, []byte, error) {
	s.plainCalls++

	return s.stdout, s.stderr, s.err
}

// RunWithEnvironment records a non-streaming invocation with an explicit environment.
func (s *streamingRunner) RunWithEnvironment(
	context.Context,
	string,
	[]string,
	io.Reader,
	[]string,
) ([]byte, []byte, error) {
	s.plainCalls++

	return s.stdout, s.stderr, s.err
}

// RunWithProgress writes stdout and stderr to progress, the way a streaming runner would.
func (s *streamingRunner) RunWithProgress(
	_ context.Context,
	_ string,
	_ []string,
	_ io.Reader,
	_ []string,
	progress io.Writer,
) ([]byte, []byte, error) {
	s.progressCalls++

	_, _ = progress.Write(s.stdout)
	_, _ = progress.Write(s.stderr)

	return s.stdout, s.stderr, s.err
}

func newStreamingClient(runner *streamingRunner, progress io.Writer) *eksctl.Client {
	return eksctl.NewClient(
		eksctl.WithBinary("eksctl-under-test"),
		eksctl.WithRunner(runner),
		eksctl.WithEnvironment([]string{
			"AWS_ACCESS_KEY_ID=fixture-access-key-id",
			"AWS_SECRET_ACCESS_KEY=" + fixtureSecret,
		}),
		eksctl.WithProgressWriter(progress),
	)
}

// TestCreateCluster_StreamsRedactedProgress verifies eksctl's output reaches the progress
// writer while create runs, with credential values redacted.
func TestCreateCluster_StreamsRedactedProgress(t *testing.T) {
	t.Parallel()

	runner := &streamingRunner{
		stdout: []byte("[ℹ]  waiting for CloudFormation stack \"eksctl-demo-cluster\"\n" +
			"[ℹ]  using " + fixtureSecret + "\n"),
	}

	var progress bytes.Buffer

	err := newStreamingClient(runner, &progress).CreateCluster(t.Context(), "eks.yaml", "")
	require.NoError(t, err)

	assert.Equal(t, 1, runner.progressCalls)
	assert.Contains(t, progress.String(), "waiting for CloudFormation stack")
	assert.Contains(t, progress.String(), "[REDACTED]")
	assert.NotContains(t, progress.String(), fixtureSecret)
}

// TestMutatingCommands_Stream verifies every long-running mutating command streams progress.
func TestMutatingCommands_Stream(t *testing.T) {
	t.Parallel()

	commands := map[string]func(*eksctl.Client) error{
		"create cluster": func(c *eksctl.Client) error {
			return c.CreateClusterWithKubeconfig(t.Context(), "eks.yaml", "", "/tmp/kubeconfig")
		},
		"create nodegroup": func(c *eksctl.Client) error {
			return c.CreateNodegroup(t.Context(), "eks.yaml")
		},
		"delete cluster": func(c *eksctl.Client) error {
			return c.DeleteCluster(t.Context(), "demo", "eu-west-1", "", true)
		},
		"scale nodegroup": func(c *eksctl.Client) error {
			return c.ScaleNodegroup(t.Context(), "demo", "ng-1", "eu-west-1", 2, -1, -1)
		},
		"upgrade cluster": func(c *eksctl.Client) error {
			return c.UpgradeCluster(t.Context(), "eks.yaml", true)
		},
	}

	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &streamingRunner{stdout: []byte("progress line\n")}

			var progress bytes.Buffer

			require.NoError(t, run(newStreamingClient(runner, &progress)))
			assert.Equal(t, 1, runner.progressCalls)
			assert.Equal(t, 0, runner.plainCalls)
			assert.Contains(t, progress.String(), "progress line")
		})
	}
}

// TestGetCommands_DoNotStream verifies machine-readable listings never reach the progress writer.
func TestGetCommands_DoNotStream(t *testing.T) {
	t.Parallel()

	runner := &streamingRunner{stdout: []byte(`[{"Name":"demo","Region":"eu-west-1"}]`)}

	var progress bytes.Buffer

	clusters, err := newStreamingClient(runner, &progress).ListClusters(t.Context(), "eu-west-1")
	require.NoError(t, err)
	require.Len(t, clusters, 1)

	assert.Equal(t, 0, runner.progressCalls)
	assert.Empty(t, progress.String())
}

// TestCreateCluster_ErrorCarriesStdoutCause reproduces ksail#7078: eksctl logs the real
// cause to stdout and prints only a generic line on stderr, so the error must carry both.
func TestCreateCluster_ErrorCarriesStdoutCause(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		stdout: []byte("[ℹ]  waiting for CloudFormation stack \"eksctl-demo-nodegroup-ng-1\"\n" +
			"[✖]  waiter state transitioned to Failure: exceeded max wait time\n"),
		stderr: []byte("Error: failed to create cluster \"demo\"\n"),
		err:    errExitStatus1,
	}

	err := newTestClient(runner).CreateCluster(t.Context(), "eks.yaml", "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "failed to create cluster \"demo\"")
	assert.Contains(t, err.Error(), "exceeded max wait time")
}

// TestExec_ErrorOutputTailIsBoundedAndRedacted verifies only the end of a long output is kept
// and credential values never reach the error.
func TestExec_ErrorOutputTailIsBoundedAndRedacted(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	for line := range 200 {
		fmt.Fprintf(&stdout, "line-%03d\n", line)
	}

	stdout.WriteString("final cause using " + fixtureSecret + "\n")

	runner := &streamingRunner{stdout: []byte(stdout.String()), err: errExitStatus1}

	_, _, err := newStreamingClient(runner, io.Discard).Exec(t.Context(), "get", "cluster")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "final cause using [REDACTED]")
	assert.NotContains(t, err.Error(), fixtureSecret)
	assert.NotContains(t, err.Error(), "line-000")
}

// chunkedRunner writes its output to progress in the given chunks, the way a pipe can split a long
// line across several writes.
type chunkedRunner struct {
	chunks []string
}

// Run is never used by the streaming path; it satisfies the Runner interface.
func (r *chunkedRunner) Run(context.Context, string, []string, io.Reader) ([]byte, []byte, error) {
	return nil, nil, nil
}

// RunWithEnvironment is never used by the streaming path; it satisfies EnvironmentRunner.
func (r *chunkedRunner) RunWithEnvironment(
	context.Context,
	string,
	[]string,
	io.Reader,
	[]string,
) ([]byte, []byte, error) {
	return nil, nil, nil
}

// RunWithProgress writes each chunk as a separate write, then returns the joined output.
func (r *chunkedRunner) RunWithProgress(
	_ context.Context,
	_ string,
	_ []string,
	_ io.Reader,
	_ []string,
	progress io.Writer,
) ([]byte, []byte, error) {
	for _, chunk := range r.chunks {
		_, _ = progress.Write([]byte(chunk))
	}

	return []byte(strings.Join(r.chunks, "")), nil, nil
}

// TestCreateCluster_OverlongLineNeverLeaksASplitCredential reproduces a credential split across the
// pending-line cap: neither half of it may reach progress output.
func TestCreateCluster_OverlongLineNeverLeaksASplitCredential(t *testing.T) {
	t.Parallel()

	const pendingCap = 64 * 1024

	head := fixtureSecret[:15]
	tail := fixtureSecret[15:]
	runner := &chunkedRunner{chunks: []string{
		strings.Repeat("a", pendingCap-len(head)) + head,
		tail + "\n",
		"[ℹ]  next line\n",
	}}

	var progress bytes.Buffer

	client := eksctl.NewClient(
		eksctl.WithBinary("eksctl-under-test"),
		eksctl.WithRunner(runner),
		eksctl.WithEnvironment([]string{
			"AWS_ACCESS_KEY_ID=fixture-access-key-id",
			"AWS_SECRET_ACCESS_KEY=" + fixtureSecret,
		}),
		eksctl.WithProgressWriter(&progress),
	)

	require.NoError(t, client.CreateCluster(t.Context(), "eks.yaml", ""))

	assert.NotContains(t, progress.String(), head)
	assert.NotContains(t, progress.String(), tail)
	assert.Contains(t, progress.String(), "omitted")
	assert.Contains(t, progress.String(), "next line")
}

// TestExec_ErrorTailKeepsStdoutCauseDespiteLongStderr verifies a long stderr cannot push eksctl's
// stdout cause out of the bounded error tail.
func TestExec_ErrorTailKeepsStdoutCauseDespiteLongStderr(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	for line := range 30 {
		fmt.Fprintf(&stderr, "stderr-%02d\n", line)
	}

	runner := &fakeRunner{
		stdout: []byte("[✖]  exceeded max wait time for StackCreateComplete waiter\n"),
		stderr: []byte(stderr.String()),
		err:    errExitStatus1,
	}

	err := newTestClient(runner).CreateCluster(t.Context(), "eks.yaml", "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "exceeded max wait time")
	assert.Contains(t, err.Error(), "stderr-29")
}

// TestExec_ErrorTailBoundsASingleOverlongLine pins the byte bound on the failure tail. The line
// limits alone are not a size bound: eksctl can emit one very long line, and without a per-line
// cap that whole line reaches the error even though only 15 stdout lines are kept.
func TestExec_ErrorTailBoundsASingleOverlongLine(t *testing.T) {
	t.Parallel()

	const cause = "[✖]  AWS::EKS::Nodegroup CREATE_FAILED: "

	runner := &fakeRunner{
		stdout: []byte(cause + strings.Repeat("detail ", 5000) + "\n"),
		err:    errExitStatus1,
	}

	err := newTestClient(runner).CreateCluster(t.Context(), "eks.yaml", "")
	require.Error(t, err)

	// The head of the line survives, so the cause is still diagnosable.
	assert.Contains(t, err.Error(), cause)
	// The line is marked as shortened rather than silently cut.
	assert.Contains(t, err.Error(), "…[truncated]")
	// And the whole error stays small: one line can no longer dominate it.
	assert.Less(t, len(err.Error()), 2000,
		"a single overlong stdout line must not carry its full length into the error")
}

// TestExec_ErrorTailTruncatesOnARuneBoundary verifies a multi-byte character is never split by the
// byte cap, which would otherwise put invalid UTF-8 into an error string.
func TestExec_ErrorTailTruncatesOnARuneBoundary(t *testing.T) {
	t.Parallel()

	// Every rune is 3 bytes, so a naive byte cut at 512 lands mid-rune.
	runner := &fakeRunner{
		stdout: []byte(strings.Repeat("日", 1000) + "\n"),
		err:    errExitStatus1,
	}

	err := newTestClient(runner).CreateCluster(t.Context(), "eks.yaml", "")
	require.Error(t, err)

	assert.True(t, utf8.ValidString(err.Error()), "the error must remain valid UTF-8")
}
