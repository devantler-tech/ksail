package k8s_test

import (
	"context"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestDumpNamespaceDiagnostics_ReportsPodStateEventsAndLogs(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "server-0", Namespace: "k3k-nested"},
			Spec: corev1.PodSpec{
				NodeName:   "talos-cp-1",
				Containers: []corev1.Container{{Name: "k3s"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:         "k3s",
					Ready:        false,
					RestartCount: 3,
					Image:        "rancher/k3s:v1.36.1-k3s1",
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ImagePullBackOff",
						Message: "back-off pulling image",
					}},
				}},
				Conditions: []corev1.PodCondition{{
					Type:    corev1.PodScheduled,
					Status:  corev1.ConditionFalse,
					Reason:  "Unschedulable",
					Message: "no nodes available",
				}},
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "evt-1", Namespace: "k3k-nested"},
			Type:           corev1.EventTypeWarning,
			Reason:         "Failed",
			Message:        "failed to pull image",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "server-0"},
		},
	)

	report := k8s.DumpNamespaceDiagnostics(context.Background(), clientset, "k3k-nested")

	for _, want := range []string{
		"namespace \"k3k-nested\" pods",
		"pod server-0: phase=Pending",
		"restarts=3",
		"ImagePullBackOff",
		"condition PodScheduled=False",
		"Unschedulable",
		"Warning Failed Pod/server-0: failed to pull image",
		"--- server-0/k3s ---",
	} {
		assert.Contains(t, report, want)
	}
}

func TestDumpNamespaceDiagnostics_PendingPodUsesSpecContainerCount(t *testing.T) {
	t.Parallel()

	// A pod still scheduling/pulling has no ContainerStatuses yet; the expected total
	// must come from the spec so the output is "ready=0/2", not a misleading "ready=0/0".
	clientset := k8sfake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "server-0", Namespace: "k3k-nested"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "k3s"}, {Name: "sidecar"}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
	)

	report := k8s.DumpNamespaceDiagnostics(context.Background(), clientset, "k3k-nested")

	assert.Contains(t, report, "pod server-0: phase=Pending ready=0/2")
}

func TestDumpNamespaceDiagnostics_EmptyNamespace(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	report := k8s.DumpNamespaceDiagnostics(context.Background(), clientset, "missing")

	assert.Contains(t, report, "(no pods)")
	assert.Contains(t, report, "(no events)")
}

func TestDumpNamespaceDiagnostics_MultipleNamespaces(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "k3k-system"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "k3k"}}},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Name: "k3k", Ready: true}},
			},
		},
	)

	report := k8s.DumpNamespaceDiagnostics(
		context.Background(),
		clientset,
		"k3k-nested",
		"k3k-system",
	)

	assert.Equal(t, 2, strings.Count(report, "pods ==="))
	assert.Contains(t, report, "pod operator-0: phase=Running")
}
