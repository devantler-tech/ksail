package workload_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// experimentalInterceptCmd builds the intercept command with the --experimental
// opt-in enabled, so these tests exercise the gated command's own behaviour
// rather than the experimental.Guard rejection (which is covered in the
// experimental package's own tests). intercept ships behind the experimental
// gate (issue #5884), so a plain invocation is refused before it runs.
func experimentalInterceptCmd() *cobra.Command {
	cmd := workload.NewInterceptCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")

	return cmd
}

// interceptCall records what the stubbed steering session was invoked with, so
// tests can assert the command wired resolve → inject → session correctly
// without a live cluster.
type interceptCall struct {
	ran          bool
	point        *mirror.TapPoint
	steerCommand []string
	localPort    int
}

// newSteerPod extends the shared running test pod with a steering container so
// WaitForSteer succeeds against the fake clientset (no kubelet sets statuses
// there). withSteerSpec also puts the steering container into the pod spec,
// making InjectSteer take its reuse path.
func newSteerPod(withSteerSpec bool) *corev1.Pod {
	pod := newMirrorPod(false)

	pod.Status.EphemeralContainerStatuses = append(
		pod.Status.EphemeralContainerStatuses,
		corev1.ContainerStatus{
			Name:  mirror.SteerContainerName,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		},
	)

	if withSteerSpec {
		pod.Spec.EphemeralContainers = append(
			pod.Spec.EphemeralContainers,
			corev1.EphemeralContainer{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name: mirror.SteerContainerName,
				},
				TargetContainerName: mirrorTestContainer,
			},
		)
	}

	return pod
}

// stubInterceptSession installs intercept-command seams that serve the given
// fake clientset and record the steering session invocation, returning a
// restore function plus the recorded call.
func stubInterceptSession(client kubernetes.Interface) (func(), *interceptCall) {
	call := &interceptCall{}

	restoreClients := workload.ExportSetMirrorClients(
		func(_, _ string) (kubernetes.Interface, *rest.Config, error) {
			return client, &rest.Config{}, nil
		},
	)

	restoreSession := workload.ExportSetRunInterceptSession(
		func(
			_ context.Context,
			_ kubernetes.Interface,
			_ *rest.Config,
			point *mirror.TapPoint,
			steerCommand []string,
			localPort int,
		) error {
			call.ran = true
			call.point = point
			call.steerCommand = steerCommand
			call.localPort = localPort

			return nil
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}, call
}

func TestInterceptCmdRequiresLocalPort(t *testing.T) {
	t.Parallel()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--steer-command", "ksail-steer"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local-port")
}

func TestInterceptCmdRequiresSteerOrServicePort(t *testing.T) {
	t.Parallel()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--local-port", "8080"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrInterceptSteerUnspecified)
}

func TestInterceptCmdRejectsServicePortCollidingWithInternalPort(t *testing.T) {
	t.Parallel()

	cmd := experimentalInterceptCmd()
	// 19000 is the agent's internal listener port; steering it to itself loops.
	cmd.SetArgs([]string{mirrorTestDeploy, "--local-port", "8080", "--service-port", "19000"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrInvalidInterceptServicePort)
}

func TestInterceptCmdRejectsInvalidLocalPort(t *testing.T) {
	t.Parallel()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "70000", "--steer-command", "ksail-steer",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrInvalidInterceptLocalPort)
}

//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdFailsForMissingDeployment(t *testing.T) {
	restore, _ := stubInterceptSession(k8sfake.NewClientset())
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080", "--steer-command", "ksail-steer",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, mirror.ErrDeploymentNotFound)
}

//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdInjectsSteerAndRunsSession(t *testing.T) {
	client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(false))

	restore, call := stubInterceptSession(client)
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080",
		"--steer-command", "ksail-steer", "--steer-command", "--port=8080",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, call.ran, "the steering session must run")
	require.NotNil(t, call.point)
	assert.Equal(t, mirrorTestPod, call.point.Pod)
	assert.Equal(t, []string{"ksail-steer", "--port=8080"}, call.steerCommand)
	assert.Equal(t, 8080, call.localPort)
	assert.Contains(t, out.String(), "injected steering agent")
}

//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdDerivesSteerCommandFromServicePort(t *testing.T) {
	client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(false))

	restore, call := stubInterceptSession(client)
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080", "--service-port", "9090",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, call.ran, "the steering session must run")
	// With no --steer-command, KSail derives the default `ksail steer-agent`
	// invocation for the shipped steering image from --service-port.
	assert.Equal(t, []string{
		"ksail", "steer-agent", "--service-port=9090", "--intercept-port=19000",
	}, call.steerCommand)
	assert.Equal(t, 8080, call.localPort)
}

//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdExplicitSteerCommandOverridesServicePort(t *testing.T) {
	client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(false))

	restore, call := stubInterceptSession(client)
	defer restore()

	cmd := experimentalInterceptCmd()
	// Both flags set: the explicit --steer-command wins (the escape hatch),
	// --service-port is ignored rather than derived.
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080", "--service-port", "9090",
		"--steer-command", "custom-agent", "--steer-command", "--flag",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, call.ran, "the steering session must run")
	assert.Equal(t, []string{"custom-agent", "--flag"}, call.steerCommand)
}

//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdReusesExistingSteer(t *testing.T) {
	client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(true))

	restore, call := stubInterceptSession(client)
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080", "--steer-command", "ksail-steer",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, call.ran, "the steering session must run")
	assert.Contains(t, out.String(), "reusing the steering agent already injected")
}
