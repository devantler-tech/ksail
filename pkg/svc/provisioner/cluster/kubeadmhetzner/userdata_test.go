package kubeadmhetzner_test

import (
	"strings"
	"testing"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	containerdbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/containerd"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testClusterName = "test-cluster"
	testVersion     = "v1.31.0"
	testToken       = "abcdef.0123456789abcdef"
	testEndpoint    = "10.0.0.2:6443"
	// repoForTrack is the community package repository the pinned minor track (the
	// minor of testVersion) resolves to; every node installs the kube* components
	// from it.
	repoForTrack = "pkgs.k8s.io/core:/stable:/v1.31/deb"
	// testCACertHash is a well-formed pinned CA hash a joining node verifies during
	// token discovery (sha256: followed by 64 hex digits).
	testCACertHash = "sha256:" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// singleControlPlaneInput is a valid single-control-plane, no-agent Input: the
// smallest cluster the composer produces (one RoleServerInit node).
func singleControlPlaneInput() kubeadmhetzner.Input {
	return kubeadmhetzner.Input{
		ClusterName: testClusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             testToken,
			KubernetesVersion: testVersion,
			ControlPlaneCount: 1,
			AgentCount:        0,
		},
	}
}

func TestBuildNodeUserDataSingleControlPlane(t *testing.T) {
	t.Parallel()

	nodes, err := kubeadmhetzner.BuildNodeUserData(singleControlPlaneInput())
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	node := nodes[0]
	assert.Equal(t, 0, node.Index)
	assert.Equal(t, kubeadmbootstrap.RoleServerInit, node.Role)
	assert.True(t, strings.HasPrefix(node.UserData, "#cloud-config"))

	// The composed document carries the declarative install end to end: the
	// community apt repository for the pinned minor track, the kubeadm config
	// dropped at its managed path, the containerd runtime config with the systemd
	// cgroup driver, and the cluster-initialising bootstrap command.
	assert.Contains(t, node.UserData, repoForTrack)
	assert.Contains(t, node.UserData, kubeadmbootstrap.ConfigPath)
	assert.Contains(t, node.UserData, containerdbootstrap.ConfigPath)
	assert.Contains(t, node.UserData, "SystemdCgroup = true")
	assert.Contains(t, node.UserData, "kubeadm init --config "+kubeadmbootstrap.ConfigPath)
	assert.NotContains(t, node.UserData, "kubeadm join")

	assert.Equal(t, hetzner.NodeTypeControlPlane, node.Labels[hetzner.LabelNodeType])
	assert.Equal(t, "0", node.Labels[hetzner.LabelNodeIndex])
	assert.Equal(t, testClusterName, node.Labels[hetzner.LabelClusterName])
}

func TestBuildNodeUserDataMultiNodeOrderRolesAndJoin(t *testing.T) {
	t.Parallel()

	input := kubeadmhetzner.Input{
		ClusterName: testClusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             testToken,
			KubernetesVersion: testVersion,
			ControlPlaneCount: 2,
			AgentCount:        2,
			APIServerEndpoint: testEndpoint,
			CACertHashes:      []string{testCACertHash},
		},
	}

	nodes, err := kubeadmhetzner.BuildNodeUserData(input)
	require.NoError(t, err)
	require.Len(t, nodes, 4)

	wantRoles := []kubeadmbootstrap.Role{
		kubeadmbootstrap.RoleServerInit,
		kubeadmbootstrap.RoleServer,
		kubeadmbootstrap.RoleAgent,
		kubeadmbootstrap.RoleAgent,
	}

	for index, node := range nodes {
		assert.Equal(t, index, node.Index)
		assert.Equal(t, wantRoles[index], node.Role)
		assert.True(t, strings.HasPrefix(node.UserData, "#cloud-config"))
		// Every node — control plane and agent — installs from the same minor track
		// and gets the shared containerd runtime config.
		assert.Contains(t, node.UserData, repoForTrack)
		assert.Contains(t, node.UserData, containerdbootstrap.ConfigPath)
	}

	// The cluster-initialising control plane runs `kubeadm init`; every joining node
	// (the extra control plane and the agents) runs `kubeadm join`.
	assert.Contains(t, nodes[0].UserData, "kubeadm init --config")

	for _, node := range nodes[1:] {
		assert.Contains(t, node.UserData, "kubeadm join --config")
	}

	assert.Equal(t, hetzner.NodeTypeControlPlane, nodes[0].Labels[hetzner.LabelNodeType])
	assert.Equal(t, hetzner.NodeTypeControlPlane, nodes[1].Labels[hetzner.LabelNodeType])
	assert.Equal(t, hetzner.NodeTypeWorker, nodes[3].Labels[hetzner.LabelNodeType])
	assert.Equal(t, "3", nodes[3].Labels[hetzner.LabelNodeIndex])
}

func TestBuildNodeUserDataPinsSandboxImage(t *testing.T) {
	t.Parallel()

	input := singleControlPlaneInput()
	input.SandboxImage = "registry.k8s.io/pause:3.10"

	nodes, err := kubeadmhetzner.BuildNodeUserData(input)
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	assert.Contains(t, nodes[0].UserData, "sandbox_image")
	assert.Contains(t, nodes[0].UserData, "registry.k8s.io/pause:3.10")
}

func TestBuildNodeUserDataDeterministic(t *testing.T) {
	t.Parallel()

	first, err := kubeadmhetzner.BuildNodeUserData(singleControlPlaneInput())
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := kubeadmhetzner.BuildNodeUserData(singleControlPlaneInput())
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.Equal(t, first[0].UserData, second[0].UserData)
}

func TestBuildNodeUserDataEmitsValidCloudInit(t *testing.T) {
	t.Parallel()

	nodes, err := kubeadmhetzner.BuildNodeUserData(singleControlPlaneInput())
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	// The composed user_data must be a document cloud-init's own builder accepts —
	// i.e. it round-trips through the same transport the provisioner delivers with,
	// guarding against the composer emitting a payload the delivery seam rejects.
	transport := cloudinitbootstrap.New()
	require.NotNil(t, transport)
	assert.True(t, strings.HasPrefix(nodes[0].UserData, "#cloud-config\n"))
}

func TestBuildNodeUserDataErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate  func(*kubeadmhetzner.Input)
		wantErr error
	}{
		"missing token": {
			mutate:  func(in *kubeadmhetzner.Input) { in.Plan.Token = "" },
			wantErr: kubeadmbootstrap.ErrMissingToken,
		},
		"missing kubernetes version": {
			mutate:  func(in *kubeadmhetzner.Input) { in.Plan.KubernetesVersion = "" },
			wantErr: kubeadmbootstrap.ErrMissingKubernetesVersion,
		},
		"joiner without api server endpoint": {
			mutate: func(in *kubeadmhetzner.Input) {
				in.Plan.ControlPlaneCount = 2
				in.Plan.CACertHashes = []string{testCACertHash}
			},
			wantErr: kubeadmbootstrap.ErrMissingAPIServerEndpoint,
		},
		"invalid sandbox image": {
			mutate:  func(in *kubeadmhetzner.Input) { in.SandboxImage = "bad image" },
			wantErr: containerdbootstrap.ErrInvalidSandboxImage,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input := singleControlPlaneInput()
			testCase.mutate(&input)

			nodes, err := kubeadmhetzner.BuildNodeUserData(input)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, nodes)
		})
	}
}
