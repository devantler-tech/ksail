package workload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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

// errInterceptInterruptIgnored reports that raising the documented interrupt
// did not cancel the steering session context.
var errInterceptInterruptIgnored = errors.New(
	"interrupt did not cancel the steering session context",
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

// stubInterruptingInterceptSession installs intercept-command seams whose
// steering session raises the documented interrupt at the test process and
// then blocks until the interrupt cancels the session context — without the
// command's signal-aware context, the default disposition kills the test
// process at the raise.
func stubInterruptingInterceptSession(client kubernetes.Interface) func() {
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
		) error {
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return fmt.Errorf("find own process: %w", err)
			}

			err = proc.Signal(os.Interrupt)
			if err != nil {
				return fmt.Errorf("raise interrupt: %w", err)
			}

			// Block like the real tunnel session until the interrupt cancels
			// the context, then report the clean stop the service layer maps a
			// cancelled session to.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				return errInterceptInterruptIgnored
			}
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}
}

// stubSetupInterruptingIntercept installs intercept-command seams that raise
// the documented interrupt while setup is still running: the client factory
// (called after the command installs its signal-aware context, before any
// Kubernetes call) raises SIGINT, and the served pod's steering container
// never reaches Running, so the steer wait can only end via context
// cancellation — without the signal-aware context covering setup, the default
// disposition kills the test process at the raise.
func stubSetupInterruptingIntercept() func() {
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

	restoreSession := workload.ExportSetRunInterceptSession(
		func(
			_ context.Context,
			_ kubernetes.Interface,
			_ *rest.Config,
			_ *mirror.TapPoint,
			_ []string,
			_ int,
		) error {
			return errInterceptInterruptIgnored
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}
}

// TestInterceptCmdInterruptDuringSetupUnwinds verifies that an interrupt
// raised before the steering agent is ready cancels setup through the
// command's signal-aware context instead of killing the process or letting
// the steer wait run to its full --wait-timeout.
//
//nolint:paralleltest // swaps package-level seams; the raised SIGINT is process-wide.
func TestInterceptCmdInterruptDuringSetupUnwinds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("raising os.Interrupt at the running process is not supported on Windows")
	}

	restore := stubSetupInterruptingIntercept()
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy,
		"--local-port", "8080",
		"--steer-command", "ksail-steer",
		"--wait-timeout", "30s",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	start := time.Now()
	err := cmd.Execute()

	require.ErrorIs(t, err, context.Canceled,
		"an interrupt during setup must cancel via the signal-aware context")
	require.Less(t, time.Since(start), 10*time.Second,
		"cancellation must end the steer wait well before --wait-timeout")
}

// TestInterceptCmdHelpDocumentsCleanInterruptExit verifies that the command
// advertises the successful Ctrl-C shutdown contract.
func TestInterceptCmdHelpDocumentsCleanInterruptExit(t *testing.T) {
	t.Parallel()

	cmd := workload.NewInterceptCmd()

	assert.Contains(t, cmd.Long, "Ctrl-C stops it cleanly with exit status 0")
}

// TestInterceptCmdInterruptStopsSessionCleanly verifies that an interrupt
// cancels the steering session without surfacing an execution error.
//
//nolint:paralleltest // swaps package-level seams; the raised SIGINT is process-wide.
func TestInterceptCmdInterruptStopsSessionCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("raising os.Interrupt at the running process is not supported on Windows")
	}

	client := k8sfake.NewClientset(newMirrorDeployment(), newSteerPod(false))

	restore := stubInterruptingInterceptSession(client)
	defer restore()

	cmd := experimentalInterceptCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--local-port", "8080", "--steer-command", "ksail-steer",
	})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute(), "an interrupted intercept must end as a clean stop")
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
