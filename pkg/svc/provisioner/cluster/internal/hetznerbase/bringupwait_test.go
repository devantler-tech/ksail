package hetznerbase_test

import (
	"bytes"
	"context"
	"net"
	"regexp"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

const (
	testStageTimeout         = 200 * time.Millisecond
	testReadyPath            = "/var/lib/ksail/bootstrap-complete"
	testReadyProbeCommand    = "test -f '/var/lib/ksail/bootstrap-complete'"
	testCloudInitStatusCmd   = "cloud-init status --long"
	testCloudInitStatusError = "status: error\ndetail: runcmd failed\n"
	testCloudInitShortCmd    = "cloud-init status"
	testKubeadmErrorLinesCmd = "grep -hE '^[[:space:]]*(\\[ERROR |error execution phase)' " +
		"/var/log/ksail-bootstrap.log /var/log/cloud-init-output.log 2>/dev/null | tail -n 20"
	testKubeadmErrorLines = "\t[ERROR FileContent--proc-sys-net-ipv4-ip_forward]: " +
		"/proc/sys/net/ipv4/ip_forward contents are not set to 1\n" +
		"error execution phase preflight: [preflight] Some fatal errors occurred:\n"
)

// TestBringUpNodeBootstrapWaitIsBounded pins that a node whose first-boot
// bootstrap never writes the kubeconfig fails on the stage's own deadline even
// when the caller's context has none (the CLI's context never does), names the
// stage it was waiting on, carries the node's cloud-init status, and still
// cleans up.
func TestBringUpNodeBootstrapWaitIsBounded(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	neverReady := func(command string) (string, uint32) {
		switch command {
		case testProbeCommand:
			return "", errExitNotFound
		case testCloudInitStatusCmd:
			return testCloudInitStatusError, 1
		default:
			return "", errExitUnknownProbe
		}
	}

	host, port, hostKey := startBringUpSSHServer(t, pair.Signer.PublicKey(), neverReady)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.BringUpBootstrapTimeout = testStageTimeout

	done := make(chan error, 1)

	go func() {
		_, bringUpErr := base.BringUpNode(
			t.Context(), testClusterName, bringUpSpec(pair, hostKey, port),
		)
		done <- bringUpErr
	}()

	select {
	case err = <-done:
	case <-time.After(testBringUpBudget):
		t.Fatal("bring-up did not honour its own bootstrap deadline")
	}

	require.ErrorIs(t, err, hetznerbase.ErrBringUpStageTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "first-boot bootstrap")
	assert.Contains(t, err.Error(), testKubeconfigPath)
	assert.Contains(t, err.Error(), "status: error")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

// TestBringUpNodeSSHWaitIsBounded pins that a node whose sshd never comes up
// fails on the SSH stage's own deadline rather than retrying for as long as the
// caller's (deadline-free) context lives.
func TestBringUpNodeSSHWaitIsBounded(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.BringUpSSHTimeout = testStageTimeout

	spec := hetznerbase.BringUpSpec{
		Signer:          pair.Signer,
		HostKeyCallback: gossh.FixedHostKey(pair.Signer.PublicKey()),
		KubeconfigPath:  testKubeconfigPath,
		Port:            port,
	}

	done := make(chan error, 1)

	go func() {
		_, bringUpErr := base.BringUpNode(t.Context(), testClusterName, spec)
		done <- bringUpErr
	}()

	select {
	case err = <-done:
	case <-time.After(testBringUpBudget):
		t.Fatal("bring-up did not honour its own SSH deadline")
	}

	require.ErrorIs(t, err, hetznerbase.ErrBringUpStageTimeout)
	assert.Contains(t, err.Error(), "waiting for SSH")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

// TestBringUpNodeReportsStages pins that each bring-up stage prints a progress
// line, so a slow create is distinguishable from a stuck one.
func TestBringUpNodeReportsStages(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(2, testKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	var out bytes.Buffer

	base.LogWriter = &out

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	_, err = base.BringUpNode(ctx, testClusterName, bringUpSpec(pair, hostKey, port))
	require.NoError(t, err)

	for _, want := range []string{
		"Creating server",
		"Waiting for SSH",
		"SSH is up",
		"Waiting for the first-boot bootstrap to write " + testKubeconfigPath,
		"First-boot bootstrap finished",
	} {
		assert.Contains(t, out.String(), want)
	}
}

// TestBringUpNodeWithoutLogWriter pins that a Base with no LogWriter still
// brings a node up rather than panicking on its progress output.
func TestBringUpNodeWithoutLogWriter(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(0, testKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.LogWriter = nil

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	_, err = base.BringUpNode(ctx, testClusterName, bringUpSpec(pair, hostKey, port))
	require.NoError(t, err)
}

// TestBringUpNodeFailsFastOnCloudInitError pins that a node whose cloud-init
// already reports the first boot as failed ends the bootstrap wait at once
// rather than on the stage's deadline (ksail#7331: a failed kubeadm preflight
// used to surface only after 20 minutes), and that the error carries
// cloud-init's status and kubeadm's error lines.
func TestBringUpNodeFailsFastOnCloudInitError(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	failedBoot := func(command string) (string, uint32) {
		switch command {
		case testProbeCommand:
			return "", errExitNotFound
		case testCloudInitShortCmd:
			return "status: error\n", 1
		case testCloudInitStatusCmd:
			return testCloudInitStatusError, 1
		case testKubeadmErrorLinesCmd:
			return testKubeadmErrorLines, 0
		default:
			return "", errExitUnknownProbe
		}
	}

	host, port, hostKey := startBringUpSSHServer(t, pair.Signer.PublicKey(), failedBoot)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	// A deadline far beyond the test budget: only the fail-fast path can end
	// the wait in time.
	base.BringUpBootstrapTimeout = time.Hour

	done := make(chan error, 1)

	go func() {
		_, bringUpErr := base.BringUpNode(
			t.Context(), testClusterName, bringUpSpec(pair, hostKey, port),
		)
		done <- bringUpErr
	}()

	select {
	case err = <-done:
	case <-time.After(testBringUpBudget):
		t.Fatal("bring-up kept waiting after cloud-init reported the first boot as failed")
	}

	require.ErrorIs(t, err, hetznerbase.ErrBootstrapFailed)
	require.NotErrorIs(t, err, hetznerbase.ErrBringUpStageTimeout)
	assert.Contains(t, err.Error(), testKubeconfigPath)
	assert.Contains(t, err.Error(), "status: error")
	assert.Contains(t, err.Error(), "[ERROR FileContent--proc-sys-net-ipv4-ip_forward]")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

// TestBringUpNodeKeepsWaitingWhileCloudInitRuns pins that only an error
// verdict ends the wait early: a first boot cloud-init still reports as
// running is waited for until the stage's own deadline.
func TestBringUpNodeKeepsWaitingWhileCloudInitRuns(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	running := func(command string) (string, uint32) {
		switch command {
		case testProbeCommand:
			return "", errExitNotFound
		case testCloudInitShortCmd:
			return "status: running\n", 0
		default:
			return "", errExitUnknownProbe
		}
	}

	host, port, hostKey := startBringUpSSHServer(t, pair.Signer.PublicKey(), running)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.BringUpBootstrapTimeout = testStageTimeout

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	_, err = base.BringUpNode(ctx, testClusterName, bringUpSpec(pair, hostKey, port))

	require.ErrorIs(t, err, hetznerbase.ErrBringUpStageTimeout)
	require.NotErrorIs(t, err, hetznerbase.ErrBootstrapFailed)
}

// TestBootstrapErrorLinePatternSelectsOnlyKubeadmErrors pins that the pattern
// keeps kubeadm's tab-indented preflight errors and its failed-phase line (a
// column-zero anchor dropped exactly the line that names the root cause), and
// that it keeps nothing else from a transcript that can carry join tokens.
func TestBootstrapErrorLinePatternSelectsOnlyKubeadmErrors(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(hetznerbase.BootstrapErrorLinePatternForTest)

	for _, line := range []string{
		"\t[ERROR FileContent--proc-sys-net-ipv4-ip_forward]: contents are not set to 1",
		"[ERROR Swap]: running with swap on is not supported",
		"error execution phase preflight: [preflight] Some fatal errors occurred:",
	} {
		assert.True(t, pattern.MatchString(line), "should keep %q", line)
	}

	for _, line := range []string{
		"kubeadm join 10.0.0.1:6443 --token abcdef.0123456789abcdef",
		"\t[WARNING SystemVerification]: missing optional cgroups",
		"[preflight] Running pre-flight checks",
	} {
		assert.False(t, pattern.MatchString(line), "should drop %q", line)
	}
}

// TestBringUpNodeWaitsForReadyPathBeforeKubeconfig pins that a bring-up with a
// ReadyPath does not accept a kubeconfig the bootstrap wrote before failing:
// kubeadm writes admin.conf before its wait-control-plane phase, so the
// kubeconfig existing while cloud-init reports an error is a failed bring-up,
// not a running cluster.
func TestBringUpNodeWaitsForReadyPathBeforeKubeconfig(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	kubeconfigWithoutSentinel := func(command string) (string, uint32) {
		switch command {
		case testProbeCommand, testReadCommand:
			return testKubeconfig, 0
		case testReadyProbeCommand:
			return "", errExitNotFound
		case testCloudInitShortCmd:
			return "status: error\n", 1
		case testCloudInitStatusCmd:
			return testCloudInitStatusError, 1
		case testKubeadmErrorLinesCmd:
			return testKubeadmErrorLines, 0
		default:
			return "", errExitUnknownProbe
		}
	}

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigWithoutSentinel,
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.BringUpBootstrapTimeout = time.Hour

	spec := bringUpSpec(pair, hostKey, port)
	spec.ReadyPath = testReadyPath

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	result, err := base.BringUpNode(ctx, testClusterName, spec)

	require.ErrorIs(t, err, hetznerbase.ErrBootstrapFailed)
	assert.Contains(t, err.Error(), testReadyPath)
	assert.Nil(t, result.Kubeconfig)
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

// TestBringUpNodeReadsKubeconfigOnceReadyPathExists pins the success half: once
// the ReadyPath sentinel exists, the kubeconfig is read and returned.
func TestBringUpNodeReadsKubeconfigOnceReadyPathExists(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	ready := func(command string) (string, uint32) {
		switch command {
		case testProbeCommand, testReadCommand:
			return testKubeconfig, 0
		case testReadyProbeCommand:
			return "", 0
		default:
			return "", errExitUnknownProbe
		}
	}

	host, port, hostKey := startBringUpSSHServer(t, pair.Signer.PublicKey(), ready)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	spec := bringUpSpec(pair, hostKey, port)
	spec.ReadyPath = testReadyPath

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	result, err := base.BringUpNode(ctx, testClusterName, spec)
	require.NoError(t, err)
	assert.Equal(t, testKubeconfig, string(result.Kubeconfig))
	assert.Zero(t, infra.deleteNodesCalls)
}
