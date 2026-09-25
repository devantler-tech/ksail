package talosprovisioner_test

import (
	"context"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const recoverNodeIP = "10.0.0.7"

func upgradedNode(unschedulable bool, ready corev1.ConditionStatus) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: unschedulable},
		Status: corev1.NodeStatus{
			Addresses:  []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: recoverNodeIP}},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: ready}},
		},
	}
}

type recoverCase struct {
	name         string
	node         *corev1.Node
	nodeIP       string
	noClient     bool
	cancelled    bool
	wantErr      bool
	wantCordoned bool
}

func recoverCases() []recoverCase {
	return []recoverCase{
		{
			name: "cordoned and Ready is uncordoned",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: recoverNodeIP,
		},
		{
			name: "schedulable is left alone",
			node: upgradedNode(false, corev1.ConditionTrue), nodeIP: recoverNodeIP,
		},
		{
			name: "unresolved node is left alone",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: "10.0.0.99", wantCordoned: true,
		},
		{
			name: "no Kubernetes API means nothing was cordoned",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: recoverNodeIP,
			noClient: true, wantCordoned: true,
		},
		{
			name: "never Ready fails the roll and stays cordoned",
			node: upgradedNode(true, corev1.ConditionFalse), nodeIP: recoverNodeIP,
			cancelled: true, wantErr: true, wantCordoned: true,
		},
	}
}

// TestRecoverUpgradedNode pins that a node already on the target image is not skipped
// while an earlier, interrupted attempt has left it cordoned.
func TestRecoverUpgradedNode(t *testing.T) {
	t.Parallel()

	for _, testCase := range recoverCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clientset := fake.NewClientset(testCase.node)
			prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions())

			ctx, cancel := context.WithCancel(t.Context())
			if testCase.cancelled {
				cancel()
			} else {
				defer cancel()
			}

			var client kubernetes.Interface = clientset
			if testCase.noClient {
				client = nil
			}

			err := prov.RecoverUpgradedNodeForTest(ctx, client, testCase.nodeIP)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			got, getErr := clientset.CoreV1().
				Nodes().
				Get(context.Background(), testCase.node.Name, metav1.GetOptions{})
			require.NoError(t, getErr)
			assert.Equal(t, testCase.wantCordoned, got.Spec.Unschedulable)
		})
	}
}
