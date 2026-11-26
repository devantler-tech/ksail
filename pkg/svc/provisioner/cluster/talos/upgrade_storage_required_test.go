package talosprovisioner_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestImageUpgradeRequiresCurrentKubernetesNodeWhenStorageHealthIsRequired(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		client kubernetes.Interface
	}{
		{name: "Kubernetes API unavailable"},
		{name: "Talos host missing from Kubernetes", client: fake.NewClientset()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options := talosprovisioner.NewOptions().WithStorageHealthTimeout(time.Minute)
			options.RequireStorageHealth = true

			provisioner := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)

			_, err := provisioner.PrepareNodeForImageUpgradeForTest(
				t.Context(),
				testCase.client,
				"192.0.2.10",
			)
			if err == nil {
				t.Fatal(
					"the storage-protected upgrade could proceed without draining a bound Kubernetes node",
				)
			}
		})
	}
}

func TestConfiguredTalosCanRequireStorageHealthForImageUpgrades(t *testing.T) {
	t.Parallel()

	var options v1alpha1.OptionsTalos

	err := json.Unmarshal(
		[]byte(`{"requireStorageHealth":true,"storageHealthTimeout":"10m"}`),
		&options,
	)
	if err != nil {
		t.Fatal(err)
	}

	provisioner, err := talosprovisioner.NewProvisionerFromOptionsForTest(options)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provisioner.PrepareNodeForImageUpgradeForTest(
		t.Context(),
		nil,
		"192.0.2.10",
	)
	if err == nil {
		t.Fatal("the configured upgrade could proceed without Kubernetes access")
	}
}

func TestImageUpgradeChecksRequiredStorageBeforeCordon(t *testing.T) {
	t.Parallel()

	options := talosprovisioner.NewOptions().WithStorageHealthTimeout(100 * time.Millisecond)
	options.RequireStorageHealth = true
	provisioner := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)

	err := provisioner.UpgradeSingleNodeForTest(
		t.Context(),
		fake.NewClientset(),
		"192.0.2.10",
		blockedStorageHealth{}.degradedVolumes,
	)
	if !errors.Is(err, talosprovisioner.ErrStorageHealthTimeout) {
		t.Fatalf(
			"the upgrade reached its node preparation before establishing healthy storage: %v",
			err,
		)
	}
}

type blockedStorageHealth struct{}

func (blockedStorageHealth) degradedVolumes(context.Context) ([]string, error) {
	return []string{"longhorn-system/pvc-v2"}, nil
}

func TestImageUpgradeChecksStorageAtTheRebootBoundary(t *testing.T) {
	t.Parallel()

	options := talosprovisioner.NewOptions().WithStorageHealthTimeout(100 * time.Millisecond)
	options.RequireStorageHealth = true
	provisioner := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)
	reboots := 0
	reboot := func() error {
		reboots++

		return nil
	}

	err := provisioner.WithImageRebootStorageGateForTest(
		t.Context(), blockedStorageHealth{}.degradedVolumes, reboot,
	)
	if !errors.Is(err, talosprovisioner.ErrStorageHealthTimeout) || reboots != 0 {
		t.Fatalf("unhealthy storage reached the reboot: reboots=%d err=%v", reboots, err)
	}

	probe := func(context.Context) ([]string, error) { return nil, nil }

	err = provisioner.WithImageRebootStorageGateForTest(t.Context(), probe, reboot)
	if err != nil || reboots != 1 {
		t.Fatalf("healthy storage did not permit the reboot: reboots=%d err=%v", reboots, err)
	}
}

func TestImageUpgradeStopsAndUncordonsWhenStorageBecomesUnhealthyAfterDrain(t *testing.T) {
	t.Parallel()

	const (
		nodeIP   = "192.0.2.10"
		nodeName = "worker-a"
	)

	clientset := fake.NewClientset(&corev1.Node{
		Name: nodeName,
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: nodeIP}},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	})
	options := talosprovisioner.NewOptions().WithStorageHealthTimeout(100 * time.Millisecond).
		WithTalosconfigPath(filepath.Join(t.TempDir(), "missing-talosconfig"))
	options.RequireStorageHealth = true
	provisioner := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	probe := drainedNodeStorageHealth{client: clientset, node: nodeName}

	err := provisioner.UpgradeSingleNodeForTest(ctx, clientset, nodeIP, probe.degradedVolumes)
	if !errors.Is(err, talosprovisioner.ErrStorageHealthTimeout) {
		t.Fatalf("the image upgrade continued after storage became unhealthy: %v", err)
	}

	node, err := clientset.CoreV1().Nodes().Get(t.Context(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if node.Spec.Unschedulable || node.Annotations[testImageUpgradeCordonAnnotation] != "" ||
		node.Annotations[testImageUpgradeStoragePendingAnnotation] != "" {
		t.Fatal("an upgrade stopped before Talos execution must release its own cordon")
	}
}

func TestImageUpgradePreservesTheCordonOfAnInterruptedAttempt(t *testing.T) {
	t.Parallel()

	const nodeIP, nodeName = "192.0.2.10", "worker-a"

	clientset := fake.NewClientset(&corev1.Node{
		Name: nodeName,
		Annotations: map[string]string{
			testImageUpgradeCordonAnnotation:         "true",
			testImageUpgradeStoragePendingAnnotation: "true",
		},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: nodeIP},
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	})
	options := talosprovisioner.NewOptions().WithStorageHealthTimeout(100 * time.Millisecond)
	options.RequireStorageHealth = true
	provisioner := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)
	first := true
	probe := func(ctx context.Context) ([]string, error) {
		if first {
			first = false

			return nil, nil
		}

		return (blockedStorageHealth{}).degradedVolumes(ctx)
	}

	err := provisioner.UpgradeSingleNodeForTest(t.Context(), clientset, nodeIP, probe)
	if !errors.Is(err, talosprovisioner.ErrStorageHealthTimeout) {
		t.Fatalf("the resumed upgrade should be held before Talos execution: %v", err)
	}

	node, err := clientset.CoreV1().Nodes().Get(t.Context(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !node.Spec.Unschedulable || node.Annotations[testImageUpgradeCordonAnnotation] != "true" ||
		node.Annotations[testImageUpgradeStoragePendingAnnotation] != "true" {
		t.Fatal("the retry released an earlier attempt's recovery markers")
	}
}

func TestImageUpgradeDoesNotReleaseACordonResumedByAnotherAttempt(t *testing.T) {
	t.Parallel()

	const nodeIP, nodeName = "192.0.2.10", "worker-a"

	clientset := fake.NewClientset(&corev1.Node{
		Name: nodeName,
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: nodeIP}},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	})
	options := talosprovisioner.NewOptions().WithStorageHealthTimeout(100 * time.Millisecond)
	options.RequireStorageHealth = true
	first := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)
	successor := talosprovisioner.NewProvisioner(nil, options).WithLogWriter(io.Discard)
	resumed := false

	probe := func(ctx context.Context) ([]string, error) {
		degraded, err := (drainedNodeStorageHealth{client: clientset, node: nodeName}).degradedVolumes(
			ctx,
		)
		if err != nil || len(degraded) == 0 {
			return degraded, err
		}

		if !resumed {
			require.NoError(t, successor.MarkImageUpgradeCordonForTest(ctx, clientset, nodeName))

			resumed = true
		}

		return degraded, nil
	}

	err := first.UpgradeSingleNodeForTest(t.Context(), clientset, nodeIP, probe)
	require.ErrorIs(t, err, talosprovisioner.ErrStorageHealthTimeout)
	require.True(t, resumed)

	node, err := clientset.CoreV1().Nodes().Get(t.Context(), nodeName, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, node.Spec.Unschedulable, "the second attempt's cordon must survive")
	require.Equal(t, "true", node.Annotations[testImageUpgradeCordonAnnotation])
	require.Equal(t, "true", node.Annotations[testImageUpgradeStoragePendingAnnotation])
}

type drainedNodeStorageHealth struct {
	client kubernetes.Interface
	node   string
}

func (probe drainedNodeStorageHealth) degradedVolumes(ctx context.Context) ([]string, error) {
	node, err := probe.client.CoreV1().Nodes().Get(ctx, probe.node, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read test node: %w", err)
	}

	if node.Spec.Unschedulable {
		return []string{"longhorn-system/pvc-v2"}, nil
	}

	return nil, nil
}
