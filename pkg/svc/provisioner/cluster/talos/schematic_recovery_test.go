package talosprovisioner

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDistributionImageChangedFindsInterruptedCordon(t *testing.T) {
	t.Parallel()

	nodes := []nodeWithRole{{IP: "10.0.0.2", Role: RoleWorker}}
	read := func(context.Context, string) (string, error) { return "target", nil }
	newClient := func() (kubernetes.Interface, error) {
		return fake.NewClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Spec:       corev1.NodeSpec{Unschedulable: true},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
			}},
		}), nil
	}

	changed, err := distributionImageChanged(t.Context(), nodes, "target", read, newClient)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a node left cordoned after the image roll must re-enter recovery")
	}
}

func TestDistributionImageChangedIgnoresUnrelatedCordon(t *testing.T) {
	t.Parallel()

	nodes := []nodeWithRole{{IP: "10.0.0.2", Role: RoleWorker}}
	read := func(context.Context, string) (string, error) { return "target", nil }
	newClient := func() (kubernetes.Interface, error) {
		return fake.NewClientset(
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
				Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
				}},
			},
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "other-cluster"},
				Spec:       corev1.NodeSpec{Unschedulable: true},
				Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "10.0.0.3"},
				}},
			},
		), nil
	}

	changed, err := distributionImageChanged(t.Context(), nodes, "target", read, newClient)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a cordon outside the managed node inventory must not start a rollout")
	}
}

func TestDistributionImageChangedFailsWhenCordonStateIsUnknown(t *testing.T) {
	t.Parallel()

	nodes := []nodeWithRole{{IP: "10.0.0.2", Role: RoleWorker}}
	read := func(context.Context, string) (string, error) { return "target", nil }
	client := fake.NewClientset()
	readErr := errors.New("node list unavailable")
	client.PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, readErr
	})

	_, err := distributionImageChanged(t.Context(), nodes, "target", read,
		func() (kubernetes.Interface, error) { return client, nil })
	if !errors.Is(err, readErr) {
		t.Fatalf("wanted node-list failure, got %v", err)
	}
}
