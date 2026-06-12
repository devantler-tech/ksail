package k8s_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var errConnectionRefused = errors.New("connection refused")

//nolint:funlen,maintidx // Table-driven cases are verbose; keep assertions straightforward.
func TestDiagnosePodFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pods        []corev1.Pod
		namespaces  []string
		wantEmpty   bool
		wantContain []string
	}{
		{
			name:       "no namespaces returns empty",
			pods:       nil,
			namespaces: []string{},
			wantEmpty:  true,
		},
		{
			name:       "all pods healthy returns empty",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("healthy-pod", "default", corev1.PodRunning, []corev1.ContainerStatus{
					{Ready: true},
				}, nil),
			},
			wantEmpty: true,
		},
		{
			name:       "succeeded pod is healthy",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("succeeded-pod", "default", corev1.PodSucceeded, nil, nil),
			},
			wantEmpty: true,
		},
		{
			name:       "pending pod is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("pending-pod", "default", corev1.PodPending, nil, nil),
			},
			wantContain: []string{"pending-pod", "Pending"},
		},
		{
			name:       "failed pod is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("failed-pod", "default", corev1.PodFailed, nil, nil),
			},
			wantContain: []string{"failed-pod", "Failed"},
		},
		{
			name:       "unknown phase pod is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("unknown-pod", "default", corev1.PodUnknown, nil, nil),
			},
			wantContain: []string{"unknown-pod", "Unknown"},
		},
		{
			name:       "pod with ImagePullBackOff is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod(
					"pull-fail-pod",
					"default",
					corev1.PodPending,
					nil,
					[]corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ImagePullBackOff",
								},
							},
							Image: "ghcr.io/org/app:latest",
						},
					},
				),
			},
			wantContain: []string{"pull-fail-pod", "ImagePullBackOff", "ghcr.io/org/app:latest"},
		},
		{
			name:       "pod with CrashLoopBackOff is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("crash-pod", "default", corev1.PodRunning, []corev1.ContainerStatus{
					{
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason: "CrashLoopBackOff",
							},
						},
						Image: "myapp:v1",
					},
				}, nil),
			},
			wantContain: []string{"crash-pod", "CrashLoopBackOff", "myapp:v1"},
		},
		{
			name:       "pod with CrashLoopBackOff includes restart count (plural)",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod(
					"crash-pod-restarts",
					"default",
					corev1.PodRunning,
					[]corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
							Image:        "ghcr.io/fluxcd/notification-controller:v1.8.3",
							RestartCount: 7,
						},
					},
					nil,
				),
			},
			wantContain: []string{
				"crash-pod-restarts",
				"CrashLoopBackOff",
				"notification-controller:v1.8.3",
				"7 restarts",
			},
		},
		{
			name:       "pod with CrashLoopBackOff uses singular for exactly 1 restart",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod(
					"crash-pod-one-restart",
					"default",
					corev1.PodRunning,
					[]corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
							Image:        "myapp:v1",
							RestartCount: 1,
						},
					},
					nil,
				),
			},
			wantContain: []string{"crash-pod-one-restart", "CrashLoopBackOff", "1 restart"},
		},
		{
			name:       "pod terminated with non-zero exit code is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("terminated-pod", "default", corev1.PodFailed, []corev1.ContainerStatus{
					{
						Ready: false,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 1,
								Reason:   "Error",
							},
						},
					},
				}, nil),
			},
			wantContain: []string{"terminated-pod", "exit code 1"},
		},
		{
			name:       "terminated container includes restart count (plural)",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod(
					"terminated-restarts",
					"default",
					corev1.PodFailed,
					[]corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 2,
									Reason:   "Error",
								},
							},
							RestartCount: 3,
						},
					},
					nil,
				),
			},
			wantContain: []string{"terminated-restarts", "exit code 2", "3 restarts"},
		},
		{
			name:       "terminated container uses singular for exactly 1 restart",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod(
					"terminated-one-restart",
					"default",
					corev1.PodFailed,
					[]corev1.ContainerStatus{
						{
							Ready: false,
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
								},
							},
							RestartCount: 1,
						},
					},
					nil,
				),
			},
			wantContain: []string{"terminated-one-restart", "exit code 1", "1 restart"},
		},
		{
			name:       "pod with failing init container is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePodWithInit("init-fail-pod", "default", corev1.PodPending,
					nil, // no regular container statuses
					[]corev1.ContainerStatus{
						{
							Name: "init-setup",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "PodInitializing",
								},
							},
							Image: "busybox:latest",
						},
					},
				),
			},
			wantContain: []string{
				"init-fail-pod",
				"init container",
				"init-setup",
				"PodInitializing",
			},
		},
		{
			name:       "pod with reason falls back to phase and reason",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePodWithReason("evicted-pod", "default", corev1.PodFailed, "Evicted"),
			},
			wantContain: []string{"evicted-pod", "Failed", "Evicted"},
		},
		{
			name:       "running pod with unready container is reported",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("unready-pod", "default", corev1.PodRunning, []corev1.ContainerStatus{
					{Ready: false},
				}, nil),
			},
			wantContain: []string{"unready-pod", "Running"},
		},
		{
			name:       "reports pods across multiple namespaces",
			namespaces: []string{"ns-a", "ns-b"},
			pods: []corev1.Pod{
				makePod("pod-a", "ns-a", corev1.PodFailed, nil, nil),
				makePod("pod-b", "ns-b", corev1.PodFailed, nil, nil),
			},
			wantContain: []string{"pod-a", "ns-a", "pod-b", "ns-b"},
		},
		{
			name:       "ignores pods in unlisted namespaces",
			namespaces: []string{"default"},
			pods: []corev1.Pod{
				makePod("other-ns-pod", "kube-system", corev1.PodFailed, nil, nil),
			},
			wantEmpty: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clientset := k8sfake.NewClientset(objectsToRuntimeObjects(testCase.pods)...)

			result := k8s.DiagnosePodFailures(context.Background(), clientset, testCase.namespaces)

			if testCase.wantEmpty {
				assert.Empty(t, result, "expected empty diagnostic output")
			} else {
				assert.NotEmpty(t, result, "expected non-empty diagnostic output")

				for _, want := range testCase.wantContain {
					assert.Contains(t, result, want)
				}
			}
		})
	}
}

func TestDiagnosePodFailures_ListError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor(
		"list",
		"pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errConnectionRefused
		},
	)

	result := k8s.DiagnosePodFailures(context.Background(), clientset, []string{"default"})

	assert.Contains(t, result, "failed to list pods")
	assert.Contains(t, result, "default")
	assert.Contains(t, result, "connection refused")
}

// makePod creates a test pod with the given phase and regular container statuses.
func makePod(
	name, namespace string,
	phase corev1.PodPhase,
	containerStatuses []corev1.ContainerStatus,
	extraStatuses []corev1.ContainerStatus,
) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}

	if containerStatuses != nil {
		pod.Status.ContainerStatuses = containerStatuses
	}

	if extraStatuses != nil {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, extraStatuses...)
	}

	return pod
}

// makePodWithInit creates a test pod with init container statuses.
func makePodWithInit(
	name, namespace string,
	phase corev1.PodPhase,
	containerStatuses []corev1.ContainerStatus,
	initStatuses []corev1.ContainerStatus,
) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: corev1.PodStatus{
			Phase:                 phase,
			ContainerStatuses:     containerStatuses,
			InitContainerStatuses: initStatuses,
		},
	}

	return pod
}

// makePodWithReason creates a test pod with a status reason.
func makePodWithReason(name, namespace string, phase corev1.PodPhase, reason string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: corev1.PodStatus{
			Phase:  phase,
			Reason: reason,
		},
	}
}

// objectsToRuntimeObjects converts a slice of pods to runtime.Objects for the fake clientset.
func objectsToRuntimeObjects(pods []corev1.Pod) []runtime.Object {
	objects := make([]runtime.Object, len(pods))
	for i := range pods {
		objects[i] = &pods[i]
	}

	return objects
}

func TestDiagnoseClusterReport_NamespaceListErrorIsSurfaced(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor(
		"list",
		"namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errConnectionRefused
		},
	)

	_, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list namespaces")
}

func TestDiagnoseClusterReport_HealthyClusterReturns100(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy", Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
			},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "my-cluster")

	require.NoError(t, err)
	assert.Equal(t, "my-cluster", report.ClusterName)
	assert.Equal(t, 100, report.HealthScore)
	assert.Empty(t, report.Findings)
}

func TestDiagnoseClusterReport_FailingPodReducesScore(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-ok"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "crasher", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	assert.Equal(t, 75, report.HealthScore)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, k8s.DiagnoseSeverityCritical, report.Findings[0].Severity)
	assert.Contains(t, report.Findings[0].Resource, "crasher")
}

func TestDiagnoseClusterReport_NotReadyNodeReducesScore(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "broken-node"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "disk pressure"},
			}},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	assert.Equal(t, 75, report.HealthScore)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, k8s.DiagnoseSeverityCritical, report.Findings[0].Severity)
	assert.Equal(t, "node/broken-node", report.Findings[0].Resource)
}

func TestDiagnoseClusterReport_ScoreFloorsAtZero(t *testing.T) {
	t.Parallel()

	pods := make([]runtime.Object, 0, 5)
	pods = append(pods,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)

	for i := range 5 {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("fail-%d", i), Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		})
	}

	clientset := k8sfake.NewClientset(pods...)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "overloaded")

	require.NoError(t, err)
	assert.Equal(t, 0, report.HealthScore)
}

func TestDiagnoseClusterReport_NodeListErrorIsSurfaced(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor(
		"list",
		"nodes",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errConnectionRefused
		},
	)

	_, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list nodes")
}

func TestDiagnoseClusterReport_PodListErrorCreatesWarningFinding(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "broken-ns"}},
	)
	clientset.PrependReactor(
		"list",
		"pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errConnectionRefused
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, k8s.DiagnoseSeverityWarning, report.Findings[0].Severity)
	assert.Equal(t, "namespace/broken-ns", report.Findings[0].Resource)
	assert.Contains(t, report.Findings[0].Reason, "failed to list pods")
	assert.Equal(t, 90, report.HealthScore)
}

func TestDiagnoseClusterReport_PVCListErrorCreatesWarningFinding(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "broken-ns"}},
	)
	clientset.PrependReactor(
		"list",
		"persistentvolumeclaims",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errConnectionRefused
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, k8s.DiagnoseSeverityWarning, report.Findings[0].Severity)
	assert.Equal(t, "namespace/broken-ns", report.Findings[0].Resource)
	assert.Contains(t, report.Findings[0].Reason, "failed to list PVCs")
	assert.Equal(t, 90, report.HealthScore)
}

func TestDiagnoseClusterReport_CrashLoopBackOffIncludesRemediation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-ok"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: "default"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Ready: false,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
						},
						Image: "myapp:v1",
					},
				},
			},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Contains(t, report.Findings[0].Remediation, "logs")
	assert.Contains(t, report.Findings[0].Remediation, "crash")
}

func TestDiagnoseClusterReport_ImagePullBackOffIncludesRemediation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-ok"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pull-fail", Namespace: "default"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
						},
						Image: "ghcr.io/missing:latest",
					},
				},
			},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Contains(t, report.Findings[0].Remediation, "registry")
}

func TestDiagnoseClusterReport_OOMKilledIncludesRemediation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-ok"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "oom-pod", Namespace: "default"},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Ready: false,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
								Reason:   "OOMKilled",
							},
						},
					},
				},
			},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Contains(t, report.Findings[0].Remediation, "memory")
}

func TestDiagnoseClusterReport_NotReadyNodeIncludesRemediation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-node"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "disk pressure"},
			}},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Contains(t, report.Findings[0].Remediation, "kubelet")
}

func TestDiagnoseClusterReport_UnknownReasonHasEmptyRemediation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "mystery", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Empty(t, report.Findings[0].Remediation)
}

func TestDiagnoseClusterReport_PendingPVCCreatesWarningFinding(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "stuck-pvc", Namespace: "default"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, k8s.DiagnoseSeverityWarning, report.Findings[0].Severity)
	assert.Equal(t, "pvc/stuck-pvc (default)", report.Findings[0].Resource)
	assert.Contains(t, report.Findings[0].Reason, "Pending")
	assert.Contains(t, report.Findings[0].Remediation, "StorageClass")
	assert.Equal(t, 90, report.HealthScore)
}

func TestDiagnoseClusterReport_BoundPVCIsIgnored(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "bound-pvc", Namespace: "default"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "test")

	require.NoError(t, err)
	assert.Empty(t, report.Findings)
	assert.Equal(t, 100, report.HealthScore)
}

func TestDiagnoseClusterReport_MixedFindingsComputeCorrectScore(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-node"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "not ready"},
			}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "fail-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pending-pvc", Namespace: "default"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
	)

	report, err := k8s.DiagnoseClusterReport(context.Background(), clientset, "mixed")

	require.NoError(t, err)
	// 100 - 25 (node critical) - 25 (pod critical) - 10 (pvc warning) = 40
	assert.Equal(t, 40, report.HealthScore)
	assert.Len(t, report.Findings, 3)
}
