package talosprovisioner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const recoverNodeIP = "10.0.0.7"

var errNodeListUnavailable = errors.New("node list unavailable")

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
	marked       bool
	noClient     bool
	cancelled    bool
	listFails    bool
	wantErr      bool
	wantCordoned bool
}

func recoverCases() []recoverCase {
	return []recoverCase{
		{
			name: "cordoned and Ready is uncordoned",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: recoverNodeIP, marked: true,
		},
		{
			name: "intentional cordon is preserved",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: recoverNodeIP,
			wantCordoned: true,
		},
		{
			name: "schedulable is left alone",
			node: upgradedNode(false, corev1.ConditionTrue), nodeIP: recoverNodeIP,
		},
		{
			name: "schedulable but never Ready fails the roll",
			node: upgradedNode(false, corev1.ConditionFalse), nodeIP: recoverNodeIP,
			cancelled: true, wantErr: true,
		},
		{
			name: "intentional cordon on never Ready node fails the roll",
			node: upgradedNode(true, corev1.ConditionFalse), nodeIP: recoverNodeIP,
			cancelled: true, wantErr: true, wantCordoned: true,
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
			name: "unreadable node list fails the roll and stays cordoned",
			node: upgradedNode(true, corev1.ConditionTrue), nodeIP: recoverNodeIP,
			listFails: true, wantErr: true, wantCordoned: true,
		},
		{
			name: "never Ready fails the roll and stays cordoned",
			node: upgradedNode(true, corev1.ConditionFalse), nodeIP: recoverNodeIP,
			marked: true, cancelled: true, wantErr: true, wantCordoned: true,
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

			if testCase.marked {
				testCase.node.Annotations = map[string]string{
					testImageUpgradeCordonAnnotation: "true",
				}
			}

			clientset := fake.NewClientset(testCase.node)
			if testCase.listFails {
				clientset.PrependReactor("list", "nodes",
					func(k8stesting.Action) (bool, runtime.Object, error) {
						return true, nil, errNodeListUnavailable
					})
			}

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

			if testCase.marked && !testCase.wantCordoned {
				assert.NotContains(t, got.Annotations, testImageUpgradeCordonAnnotation,
					"recovery must clear its ownership marker when it uncordons")
			}
		})
	}
}

// A previous attempt can uncordon the target-image node and then fail its
// storage gate. Retrying must keep the next stale node blocked until recovery.
func TestRecoverUpgradedNodeRepeatsStorageGateAfterUncordon(t *testing.T) {
	t.Parallel()

	node := upgradedNode(false, corev1.ConditionTrue)
	node.Annotations = map[string]string{testImageUpgradeStoragePendingAnnotation: "true"}
	clientset := fake.NewClientset(node)
	prov := talosprovisioner.NewProvisioner(
		nil,
		talosprovisioner.NewOptions().WithStorageHealthTimeout(100*time.Millisecond),
	)
	prober := talosprovisioner.StorageHealthProberForTest(
		func(context.Context) ([]string, error) {
			return []string{"longhorn-system/pvc-still-degraded"}, nil
		},
	)

	err := prov.RecoverUpgradedNodeWithStorageGateForTest(
		t.Context(), clientset, recoverNodeIP, prober,
	)
	require.ErrorIs(t, err, talosprovisioner.ErrStorageHealthTimeout)
	got, getErr := clientset.CoreV1().Nodes().Get(t.Context(), node.Name, metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.Equal(t, "true", got.Annotations[testImageUpgradeStoragePendingAnnotation],
		"a failed gate must keep recovery discoverable")

	healthy := talosprovisioner.StorageHealthProberForTest(
		func(context.Context) ([]string, error) { return nil, nil },
	)
	require.NoError(t, prov.RecoverUpgradedNodeWithStorageGateForTest(
		t.Context(), clientset, recoverNodeIP, healthy,
	))
	got, getErr = clientset.CoreV1().Nodes().Get(t.Context(), node.Name, metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.NotContains(t, got.Annotations, testImageUpgradeStoragePendingAnnotation,
		"successful recovery must stop future retries")
}
