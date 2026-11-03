package operator_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// identityNode builds a Node carrying only the facts node-identity detection reads.
type identityNode struct {
	osImage        string
	kubeletVersion string
	providerID     string
	labels         map[string]string
	annotations    map[string]string
	ready          bool
}

func (n identityNode) build(name string) corev1.Node {
	status := corev1.ConditionFalse
	if n.ready {
		status = corev1.ConditionTrue
	}

	return corev1.Node{
		Name: name, Labels: n.labels, Annotations: n.annotations,
		Spec: corev1.NodeSpec{ProviderID: n.providerID},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				OSImage:        n.osImage,
				KubeletVersion: n.kubeletVersion,
			},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}},
		},
	}
}

func buildNodes(specs ...identityNode) []corev1.Node {
	nodes := make([]corev1.Node, 0, len(specs))
	for i, spec := range specs {
		nodes = append(nodes, spec.build("node-"+string(rune('a'+i))))
	}

	return nodes
}

//nolint:funlen // table of every distribution rule plus the consensus cases
func TestDetectDistribution(t *testing.T) {
	t.Parallel()

	talos := identityNode{osImage: "Talos (v1.13.12)", kubeletVersion: "v1.36.4"}

	tests := []struct {
		name  string
		nodes []corev1.Node
		want  v1alpha1.Distribution
	}{
		{name: "no nodes is unknown", nodes: nil, want: ""},
		{
			name:  "talos on every node",
			nodes: buildNodes(talos, talos),
			want:  v1alpha1.DistributionTalos,
		},
		{
			name: "k3s kubelet",
			nodes: buildNodes(
				identityNode{osImage: "K3s v1.36.4+k3s1", kubeletVersion: "v1.36.4+k3s1"},
			),
			want: v1alpha1.DistributionK3s,
		},
		{
			name:  "eks kubelet version",
			nodes: buildNodes(identityNode{kubeletVersion: "v1.36.2-eks-1a2b3c4"}),
			want:  v1alpha1.DistributionEKS,
		},
		{
			name: "eks node-group label",
			nodes: buildNodes(identityNode{
				kubeletVersion: "v1.36.2",
				labels:         map[string]string{"eks.amazonaws.com/nodegroup": "workers"},
			}),
			want: v1alpha1.DistributionEKS,
		},
		{
			name:  "gke kubelet version",
			nodes: buildNodes(identityNode{kubeletVersion: "v1.36.1-gke.1200000"}),
			want:  v1alpha1.DistributionGKE,
		},
		{
			name: "aks cluster label",
			nodes: buildNodes(identityNode{
				kubeletVersion: "v1.36.1",
				labels:         map[string]string{"kubernetes.azure.com/cluster": "mc_rg"},
			}),
			want: v1alpha1.DistributionAKS,
		},
		{
			name: "kwok simulated node",
			nodes: buildNodes(identityNode{
				kubeletVersion: "fake",
				annotations:    map[string]string{"kwok.x-k8s.io/node": "fake"},
			}),
			want: v1alpha1.DistributionKWOK,
		},
		{
			name: "kind provider id is vanilla",
			nodes: buildNodes(
				identityNode{
					kubeletVersion: "v1.36.4",
					providerID:     "kind://docker/dev/dev-control-plane",
				},
			),
			want: v1alpha1.DistributionVanilla,
		},
		{
			name:  "kubeadm without a marker is unknown",
			nodes: buildNodes(identityNode{osImage: "Ubuntu 24.04", kubeletVersion: "v1.36.4"}),
			want:  "",
		},
		{
			name: "mixed cluster is unknown rather than a guess",
			nodes: buildNodes(talos, identityNode{
				osImage:        "K3s v1.36.4+k3s1",
				kubeletVersion: "v1.36.4+k3s1",
			}),
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, operator.DetectDistribution(test.nodes))
		})
	}
}

//nolint:funlen // table of every provider scheme plus the consensus cases
func TestDetectProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		nodes []corev1.Node
		want  v1alpha1.Provider
	}{
		{name: "no nodes is unknown", nodes: nil, want: ""},
		{
			name:  "no provider id on any node is unknown",
			nodes: buildNodes(identityNode{}, identityNode{}),
			want:  "",
		},
		{
			name: "hetzner",
			nodes: buildNodes(
				identityNode{providerID: "hcloud://101"},
				identityNode{providerID: "hcloud://102"},
			),
			want: v1alpha1.ProviderHetzner,
		},
		{
			name:  "aws",
			nodes: buildNodes(identityNode{providerID: "aws:///eu-west-1a/i-0123"}),
			want:  v1alpha1.ProviderAWS,
		},
		{
			name:  "gcp",
			nodes: buildNodes(identityNode{providerID: "gce://project/zone/vm"}),
			want:  v1alpha1.ProviderGCP,
		},
		{
			name:  "azure",
			nodes: buildNodes(identityNode{providerID: "azure:///subscriptions/x/vm"}),
			want:  v1alpha1.ProviderAzure,
		},
		{
			name:  "kind is docker",
			nodes: buildNodes(identityNode{providerID: "kind://docker/dev/dev-control-plane"}),
			want:  v1alpha1.ProviderDocker,
		},
		{
			name:  "a node not yet initialised supplies no evidence",
			nodes: buildNodes(identityNode{providerID: "hcloud://101"}, identityNode{}),
			want:  v1alpha1.ProviderHetzner,
		},
		{
			name: "disagreeing providers are unknown",
			nodes: buildNodes(
				identityNode{providerID: "hcloud://101"},
				identityNode{providerID: "aws:///zone/i-1"},
			),
			want: "",
		},
		{
			name: "an unknown scheme is unknown",
			nodes: buildNodes(
				identityNode{providerID: "hcloud://101"},
				identityNode{providerID: "openstack:///x"},
			),
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, operator.DetectProvider(test.nodes))
		})
	}
}

func TestObserveHostNodesReportsCountsAndIdentity(t *testing.T) {
	t.Parallel()

	ready := identityNode{osImage: "Talos (v1.13.12)", providerID: "hcloud://101", ready: true}
	notReady := identityNode{osImage: "Talos (v1.13.12)", providerID: "hcloud://102"}
	nodes := buildNodes(ready, notReady)
	clientset := kubefake.NewClientset(&nodes[0], &nodes[1])

	observed, err := operator.ObserveHostNodes(context.Background(), clientset)
	require.NoError(t, err)
	assert.True(t, observed.NodesObserved)
	assert.Equal(t, int32(1), observed.NodesReady)
	assert.Equal(t, int32(2), observed.NodesTotal)
	assert.Equal(t, v1alpha1.DistributionTalos, observed.Distribution)
	assert.Equal(t, v1alpha1.ProviderHetzner, observed.Provider)
}

func TestObserveHostNodesWithoutMarkersReportsNoIdentity(t *testing.T) {
	t.Parallel()

	nodes := buildNodes(
		identityNode{osImage: "Ubuntu 24.04", kubeletVersion: "v1.36.4", ready: true},
	)
	clientset := kubefake.NewClientset(&nodes[0])

	observed, err := operator.ObserveHostNodes(context.Background(), clientset)
	require.NoError(t, err)
	assert.True(t, observed.NodesObserved)
	assert.Empty(t, observed.Distribution)
	assert.Empty(t, observed.Provider)
}
