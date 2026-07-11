package workload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

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

// errInterceptTerminationIgnored reports that a documented termination signal
// did not cancel the intercept command context.
var errInterceptTerminationIgnored = errors.New(
	"termination signal did not cancel the intercept command context",
)

// skipIfInterceptSignalUnsupported skips signal-raising tests on Windows,
// where os.Interrupt cannot be sent to the running process.
func skipIfInterceptSignalUnsupported(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("raising os.Interrupt at the running process is not supported on Windows")
	}
}

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
	keepalive    bool
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
			keepalive bool,
		) error {
			call.ran = true
			call.point = point
			call.steerCommand = steerCommand
			call.localPort = localPort
			call.keepalive = keepalive

			return nil
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}, call
}

// stubTerminatingInterceptSession installs intercept-command seams whose
// steering session raises the given termination signal at the test process and
// then blocks until the signal cancels the session context — without the
// command's signal-aware context, the default disposition kills the test
// process at the raise.
func stubTerminatingInterceptSession(
	client kubernetes.Interface,
	terminationSignal os.Signal,
) func() {
	restoreClients := workload.ExportSetMirrorClients(
		func(_, _ string) (kubernetes.Interface, *rest.Config, error) {
			return client, &rest.Config{}, nil
		},
	)

	restoreSession := workload.ExportSetRunInterceptSession(
		func(
			ctx context.Context,
			_ kubernetes.Interface,
			_ *rest.Config,
			_ *mirror.TapPoint,
			_ []string,
			_ int,
			_ bool,
		) error {
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return fmt.Errorf("find own process: %w", err)
			}

			err = proc.Signal(terminationSignal)
			if err != nil {
				return fmt.Errorf("raise termination signal: %w", err)
			}

			// Block like the real tunnel session until the signal cancels
			// the context, then report the clean stop the service layer maps a
			// cancelled session to.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				return errInterceptTerminationIgnored
			}
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}
}

// stubInterruptingInterceptSetup installs intercept-command seams whose
// client factory raises the documented interrupt before pod resolution starts.
// The returned pod leaves the steering container unready, so setup can finish
// only when the signal-aware context cancels its readiness wait.
func stubInterruptingInterceptSetup() func() {
	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restoreClients := workload.ExportSetMirrorClients(
		func(_, _ string) (kubernetes.Interface, *rest.Config, error) {
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return nil, nil, fmt.Errorf("find own process: %w", err)
			}

			err = proc.Signal(os.Interrupt)
			if err != nil {
				return nil, nil, fmt.Errorf("raise interrupt: %w", err)
			}

			return client, &rest.Config{}, nil
		},
	)

	return restoreClients
}

// TestInterceptCmdHelpDocumentsCleanInterruptExit verifies that the command
// advertises the successful Ctrl-C shutdown contract.
func TestInterceptCmdHelpDocumentsCleanInterruptExit(t *testing.T) {
	t.Parallel()

	cmd := workload.NewInterceptCmd()

	assert.Contains(t, cmd.Long, "Ctrl-C stops it cleanly with exit status 0")
}

// TestInterceptCmdTerminationSignalsStopSessionCleanly verifies that SIGINT and
// SIGTERM cancel the steering session without surfacing an execution error.
//
//nolint:paralleltest // Package seams and termination signals are process-wide.
func TestInterceptCmdTerminationSignalsStopSessionCleanly(t *testing.T) {
	skipIfInterceptSignalUnsupported(t)

	tests := []struct {
		name              string
		terminationSignal os.Signal
	}{
		{name: "SIGINT", terminationSignal: os.Interrupt},
		{name: "SIGTERM", terminationSignal: syscall.SIGTERM},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(false))

			restore := stubTerminatingInterceptSession(client, test.terminationSignal)
			defer restore()

			cmd := experimentalInterceptCmd()
			cmd.SetArgs([]string{
				mirrorTestDeploy, "--local-port", "8080", "--steer-command", "ksail-steer",
			})

			var out bytes.Buffer

			cmd.SetOut(&out)
			cmd.SetErr(&out)

			require.NoError(t, cmd.Execute(), "a termination signal must end intercept cleanly")
		})
	}
}

// TestInterceptCmdInterruptDuringSetupStopsCleanly verifies that the signal
// handler is active before cluster setup starts, not only for the tunnel session.
//
//nolint:paralleltest // Package seams and SIGINT are process-wide.
func TestInterceptCmdInterruptDuringSetupStopsCleanly(t *testing.T) {
	skipIfInterceptSignalUnsupported(t)

	restore := stubInterruptingInterceptSetup()
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy,
		"--local-port", "8080",
		"--steer-command", "ksail-steer",
		"--wait-timeout", "5s",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	start := time.Now()

	require.NoError(t, cmd.Execute(), "an interrupt during setup must end as a clean stop")
	require.Less(t, time.Since(start), 2*time.Second,
		"signal cancellation must end setup before the readiness timeout")
}

// TestInterceptCmdRequiresLocalPort verifies that every intercept names the
// local process port that receives redirected traffic.
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

// TestInterceptCmdRequiresSteerOrServicePort verifies that the command has
// either an explicit steering invocation or enough input to derive one.
func TestInterceptCmdRequiresSteerOrServicePort(t *testing.T) {
	t.Parallel()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--local-port", "8080"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrInterceptSteerUnspecified)
}

// TestInterceptCmdRejectsServicePortCollidingWithInternalPort verifies that a
// workload port cannot redirect back to the steering agent's listener.
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

// TestInterceptCmdRejectsInvalidLocalPort verifies that out-of-range local TCP
// ports are rejected before cluster setup begins.
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

// TestInterceptCmdFailsForMissingDeployment verifies that target-resolution
// failures propagate without starting a steering session.
//
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

// TestInterceptCmdInjectsSteerAndRunsSession verifies the command wires the
// selected pod, steering command, and local port into the tunnel session.
//
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

// TestInterceptCmdDerivesSteerCommandFromServicePort verifies the default
// steering-agent invocation when no custom command is supplied.
//
//nolint:paralleltest // swaps package-level seams (newMirrorClients, DefaultSteerImage); unsafe with t.Parallel.
func TestInterceptCmdDerivesSteerCommandFromServicePort(t *testing.T) {
	// Pin the steer image to a release-style ref: the test binary is an
	// unstamped dev build whose :latest default deliberately never
	// negotiates keepalives, and this test covers the negotiated path.
	restoreImage := pinSteerImage("ghcr.io/devantler-tech/ksail-steer:v7.199.0")
	defer restoreImage()

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
	// invocation for the shipped steering image from --service-port — plus
	// the --expect-keepalives arming flag, since keepalives are negotiated
	// against this build's own pinned image.
	assert.Equal(t, []string{
		"ksail", "steer-agent", "--service-port=9090", "--intercept-port=19000",
		"--expect-keepalives",
	}, call.steerCommand)
	assert.Equal(t, 8080, call.localPort)
	// Fresh injection of this build's own steer image: the agent provably
	// speaks the keepalive protocol, so liveness pings are enabled.
	assert.True(t, call.keepalive, "derived command against this build's image enables keepalives")
}

// pinSteerImage swaps the package-level default steer image and returns the
// restore func. The intercept keepalive gate compares the live container
// image against this default, so tests pin it to cover both the negotiated
// (version-pinned) and refused (:latest dev fallback) states.
func pinSteerImage(image string) func() {
	previous := mirror.DefaultSteerImage
	mirror.DefaultSteerImage = image

	return func() { mirror.DefaultSteerImage = previous }
}

// TestInterceptCmdDisablesKeepalivesOnUnpinnedDevImage verifies the dev-build
// guard: an unstamped build's default steer image is the mutable :latest tag,
// where tag equality cannot prove the running agent binary speaks the
// keepalive protocol (the tag may point at an older published image, or a
// reused container may predate this build), so keepalives stay off and the
// agent command carries no --expect-keepalives flag (ksail#6061 review).
//
//nolint:paralleltest // swaps package-level seams (newMirrorClients, DefaultSteerImage); unsafe with t.Parallel.
func TestInterceptCmdDisablesKeepalivesOnUnpinnedDevImage(t *testing.T) {
	restoreImage := pinSteerImage("ghcr.io/devantler-tech/ksail-steer:latest")
	defer restoreImage()

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
	assert.Equal(t, []string{
		"ksail", "steer-agent", "--service-port=9090", "--intercept-port=19000",
	}, call.steerCommand)
	assert.False(
		t, call.keepalive,
		"a mutable :latest default must never negotiate keepalives",
	)
}

// TestInterceptCmdExplicitSteerCommandOverridesServicePort verifies that the
// custom-agent escape hatch takes precedence over command derivation.
//
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
	// A custom agent's protocol support is unknown: keepalives stay off so
	// an older decoder is never fed a frame type it would reject.
	assert.False(t, call.keepalive, "a custom --steer-command disables keepalives")
}

// TestInterceptCmdReusesExistingSteer verifies that an injected ephemeral
// steering container is reused because Kubernetes cannot remove it.
//
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

// TestInterceptCmdDisablesKeepalivesOnReusedForeignImage verifies the
// version-skew guard: a reused steering container from another release (its
// live image differs from this build's pinned default) may run a
// pre-keepalive agent, so the client must not send it keepalive frames its
// decoder would reject (ksail#6040 / ksail#6061 review).
//
//nolint:paralleltest // swaps the package-level newMirrorClients seam; unsafe with t.Parallel.
func TestInterceptCmdDisablesKeepalivesOnReusedForeignImage(t *testing.T) {
	pod := newSteerPod(true)
	pod.Spec.EphemeralContainers[0].Image = "ghcr.io/devantler-tech/ksail-steer:v0.0.1"
	client := k8sfake.NewClientset(newMirrorDeployment(), pod)

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
	assert.False(
		t, call.keepalive,
		"a reused container from another release must not receive keepalive frames",
	)
}
