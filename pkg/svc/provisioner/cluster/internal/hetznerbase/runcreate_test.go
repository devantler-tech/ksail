package hetznerbase_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// remoteAdminKubeconfig is what the node's bootstrap wrote: a kubeconfig whose
// endpoint is only reachable on the node itself, which RunCreate must rewrite
// to the node's public address.
const remoteAdminKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://10.9.9.9:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
users:
- name: default
  user: {}
`

// staticToken is the composePlan-visible node token the tests' generateToken
// returns.
const staticToken = "test-token"

func staticTokenGenerator() (string, error) { return staticToken, nil }

// runCreatePlan composes a single-spec BringUpPlan against the in-process SSH
// server, mirroring what a provisioner's composePlan returns once its spec
// derivation lands.
func runCreatePlan(
	pair sshbootstrap.KeyPair,
	hostKey gossh.PublicKey,
	port string,
) hetznerbase.BringUpPlan {
	return hetznerbase.BringUpPlan{
		Specs:                []hetzner.CreateServerOpts{{Name: "node-0"}},
		Signer:               pair.Signer,
		HostKeyCallback:      gossh.FixedHostKey(hostKey),
		RemoteKubeconfigPath: testKubeconfigPath,
		PollInterval:         testPollInterval,
		Port:                 port,
	}
}

func TestRunCreateBringsUpNodeAndPersistsKubeconfig(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(1, remoteAdminKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host), networkID: 11}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	var composedInfra hetznerbase.ResolvedInfra

	var composedToken string

	composePlan := func(
		_, token string,
		resolved hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		composedInfra = resolved
		composedToken = token

		return runCreatePlan(pair, hostKey, port), nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	require.NoError(t, base.RunCreate(ctx, "", composePlan, staticTokenGenerator))

	// The compose callback received the resolved infrastructure and the token.
	assert.Equal(t, int64(11), composedInfra.NetworkID)
	assert.Equal(t, staticToken, composedToken)
	assert.Equal(t, 1, infra.createServerCalls)
	assert.Equal(t, 0, infra.deleteNodesCalls)

	// The kubeconfig is persisted with its endpoint rewritten to the node's
	// public IPv4 (the in-process SSH server's loopback address).
	merged, err := os.ReadFile(base.KubeconfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(merged), "https://"+host+":6443")
	assert.NotContains(t, string(merged), "10.9.9.9")
}

func TestRunCreateMissingKubeconfigDestinationFailsFast(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		t.Fatal("composePlan must not be reached without a kubeconfig destination")

		return hetznerbase.BringUpPlan{}, nil
	}

	err := base.RunCreate(t.Context(), "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrMissingKubeconfigDestination)
	assert.Equal(t, 0, infra.ensureNetworkCalls)
	assert.Equal(t, 0, infra.createServerCalls)
}

func TestRunCreateComposePlanErrorPropagates(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return hetznerbase.BringUpPlan{}, hetznerbase.ErrLiveBringUpNotImplemented
	}

	err := base.RunCreate(t.Context(), "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrLiveBringUpNotImplemented)
	assert.Equal(t, 0, infra.createServerCalls)
}

func TestRunCreateRejectsNonSingleNodePlan(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return hetznerbase.BringUpPlan{}, nil
	}

	err := base.RunCreate(t.Context(), "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrSingleNodePlanExpected)
	assert.Equal(t, 0, infra.createServerCalls)
}

func TestRunCreateExistingClusterRejected(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{nodesExist: true}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		t.Fatal("composePlan must not be reached for an existing cluster")

		return hetznerbase.BringUpPlan{}, nil
	}

	err := base.RunCreate(t.Context(), "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrClusterAlreadyExists)
}

func TestRunCreateMultiNodeRejected(t *testing.T) {
	t.Parallel()

	infra := &fakeInfra{}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")
	base.Agents = 1

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		t.Fatal("composePlan must not be reached for a multi-node topology")

		return hetznerbase.BringUpPlan{}, nil
	}

	err := base.RunCreate(t.Context(), "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrMultiNodeNotImplemented)
}

func TestRunCreateUnparsableKubeconfigCleansUp(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(0, "\tnot a kubeconfig"),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return runCreatePlan(pair, hostKey, port), nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err = base.RunCreate(ctx, "", composePlan, staticTokenGenerator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse retrieved kubeconfig")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

func TestRunCreatePersistFailureCleansUp(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(0, remoteAdminKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host)}
	base := newBase(infra, v1alpha1.OptionsHetzner{})

	// A destination whose parent is a regular file cannot be created.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	base.KubeconfigPath = filepath.Join(blocker, "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return runCreatePlan(pair, hostKey, port), nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err = base.RunCreate(ctx, "", composePlan, staticTokenGenerator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge kubeconfig")
	assert.Equal(t, 1, infra.deleteNodesCalls)
}

// preexistingKubeconfig seeds the destination before RunCreate to pin the
// merge behaviour: existing unrelated entries must survive the persist.
const preexistingKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://other.example:6443
  name: other
contexts:
- context:
    cluster: other
    user: other
  name: other
current-context: other
users:
- name: other
  user: {}
`

func TestRunCreateMergesIntoExistingKubeconfig(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(1, remoteAdminKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host), networkID: 11}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(base.KubeconfigPath, []byte(preexistingKubeconfig), 0o600))

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return runCreatePlan(pair, hostKey, port), nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	require.NoError(t, base.RunCreate(ctx, "", composePlan, staticTokenGenerator))

	// Both the preexisting unrelated cluster and the new rewritten entry
	// coexist after the merge.
	merged, err := os.ReadFile(base.KubeconfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(merged), "https://other.example:6443")
	assert.Contains(t, string(merged), "https://"+host+":6443")
}

// clusterlessKubeconfig parses fine but has no cluster entries — the guard
// against persisting a kubeconfig read mid-write on the node.
const clusterlessKubeconfig = `apiVersion: v1
kind: Config
`

func TestRunCreateClusterlessKubeconfigFails(t *testing.T) {
	t.Parallel()

	pair, err := sshbootstrap.GenerateKeyPair()
	require.NoError(t, err)

	host, port, hostKey := startBringUpSSHServer(
		t, pair.Signer.PublicKey(), kubeconfigHandler(1, clusterlessKubeconfig),
	)

	infra := &fakeInfra{createdServer: serverWithPublicIPv4(host), networkID: 11}
	base := newBase(infra, v1alpha1.OptionsHetzner{})
	base.KubeconfigPath = filepath.Join(t.TempDir(), "kubeconfig")

	composePlan := func(
		_, _ string,
		_ hetznerbase.ResolvedInfra,
	) (hetznerbase.BringUpPlan, error) {
		return runCreatePlan(pair, hostKey, port), nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), testBringUpBudget)
	defer cancel()

	err = base.RunCreate(ctx, "", composePlan, staticTokenGenerator)
	require.ErrorIs(t, err, hetznerbase.ErrKubeconfigNoClusters)

	// Cleanup-on-failure applies to this post-bring-up guard too: the created
	// node is torn down like in the parse- and persist-failure cases.
	assert.Equal(t, 1, infra.deleteNodesCalls)

	// Nothing was persisted for the clusterless kubeconfig.
	_, statErr := os.Stat(base.KubeconfigPath)
	require.True(t, os.IsNotExist(statErr))
}
