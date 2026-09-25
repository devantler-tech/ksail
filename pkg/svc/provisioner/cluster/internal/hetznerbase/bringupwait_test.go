package hetznerbase_test

import (
	"bytes"
	"context"
	"net"
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
	testCloudInitStatusCmd   = "cloud-init status --long"
	testCloudInitStatusError = "status: error\ndetail: runcmd failed\n"
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
