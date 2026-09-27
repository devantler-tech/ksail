package talosprovisioner_test

import (
	"context"
	"errors"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

const testSchematic = "target"
const testImageUpgradeCordonAnnotation = "ksail.devantler.tech/image-upgrade-cordon"
const testImageUpgradeStoragePendingAnnotation = "ksail.devantler.tech/image-upgrade-storage-pending"

var errSchematicNodeListUnavailable = errors.New("node list unavailable")

func TestDistributionImageChangedFindsInterruptedCordon(t *testing.T) {
	t.Parallel()

	nodes := []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.0.0.2", Role: talosprovisioner.RoleWorker},
	}
	read := func(context.Context, string) (string, error) { return testSchematic, nil }
	newClient := func() (kubernetes.Interface, error) {
		return fake.NewClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "worker-1",
				Annotations: map[string]string{
					testImageUpgradeCordonAnnotation: "true",
				},
			},
			Spec: corev1.NodeSpec{Unschedulable: true},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
			}},
		}), nil
	}

	changed, err := talosprovisioner.DistributionImageChangedForTest(
		t.Context(), nodes, testSchematic, read, newClient)
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("a node left cordoned after the image roll must re-enter recovery")
	}
}

func TestDistributionImageChangedIgnoresIntentionalManagedNodeCordon(t *testing.T) {
	t.Parallel()

	nodes := []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.0.0.2", Role: talosprovisioner.RoleWorker},
	}
	read := func(context.Context, string) (string, error) { return testSchematic, nil }
	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
			{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
		}},
	})

	changed, err := talosprovisioner.DistributionImageChangedForTest(
		t.Context(), nodes, testSchematic, read,
		func() (kubernetes.Interface, error) { return clientset, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("an administrator's cordon must not start an image recovery")
	}
}

func TestDistributionImageChangedFindsPendingStorageGateAfterUncordon(t *testing.T) {
	t.Parallel()

	nodes := []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.0.0.2", Role: talosprovisioner.RoleWorker},
	}
	read := func(context.Context, string) (string, error) { return testSchematic, nil }
	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
			Annotations: map[string]string{
				testImageUpgradeStoragePendingAnnotation: "true",
			},
		},
		Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
			{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
		}},
	})

	changed, err := talosprovisioner.DistributionImageChangedForTest(
		t.Context(), nodes, testSchematic, read,
		func() (kubernetes.Interface, error) { return clientset, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a failed storage gate after uncordon must trigger recovery on retry")
	}
}

func TestDistributionImageChangedIgnoresUnrelatedCordon(t *testing.T) {
	t.Parallel()

	nodes := []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.0.0.2", Role: talosprovisioner.RoleWorker},
	}
	read := func(context.Context, string) (string, error) { return testSchematic, nil }
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

	changed, err := talosprovisioner.DistributionImageChangedForTest(
		t.Context(), nodes, testSchematic, read, newClient)
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Fatal("a cordon outside the managed node inventory must not start a rollout")
	}
}

func TestDistributionImageChangedFailsWhenCordonStateIsUnknown(t *testing.T) {
	t.Parallel()

	nodes := []talosprovisioner.NodeWithRoleForTest{
		{IP: "10.0.0.2", Role: talosprovisioner.RoleWorker},
	}
	read := func(context.Context, string) (string, error) { return testSchematic, nil }
	client := fake.NewClientset()
	client.PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errSchematicNodeListUnavailable
	})

	_, err := talosprovisioner.DistributionImageChangedForTest(
		t.Context(),
		nodes,
		testSchematic,
		read,
		func() (kubernetes.Interface, error) { return client, nil },
	)
	if !errors.Is(err, errSchematicNodeListUnavailable) {
		t.Fatalf("wanted node-list failure, got %v", err)
	}
}
