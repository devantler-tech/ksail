package calicoinstaller_test

import (
	"testing"
	"time"

	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

type rolloutCondition struct {
	conditionType string
	status        string
	observed      int64
}

func rolloutInstallation(generation int64) *unstructured.Unstructured {
	installation := &unstructured.Unstructured{}
	installation.SetAPIVersion("operator.tigera.io/v1")
	installation.SetKind("Installation")
	installation.SetName("default")
	installation.SetGeneration(generation)

	return installation
}

func rolloutStatus(conditions ...rolloutCondition) *unstructured.Unstructured {
	status := &unstructured.Unstructured{}
	status.SetAPIVersion("operator.tigera.io/v1")
	status.SetKind("TigeraStatus")
	status.SetName("calico")

	items := make([]any, 0, len(conditions))
	for _, condition := range conditions {
		items = append(items, map[string]any{
			"type":               condition.conditionType,
			"status":             condition.status,
			"observedGeneration": condition.observed,
			"message":            condition.conditionType + " message",
		})
	}

	status.Object["status"] = map[string]any{"conditions": items}

	return status
}

// availableAt is a TigeraStatus that reports Calico available for generation.
func availableAt(generation int64) *unstructured.Unstructured {
	return rolloutStatus(
		rolloutCondition{"Available", "True", generation},
		rolloutCondition{"Progressing", "False", generation},
		rolloutCondition{"Degraded", "False", generation},
	)
}

// calicoNodeDesired is the number of nodes calico-node is scheduled on in these tests.
const calicoNodeDesired = 3

func calicoNodeDaemonSet(generation, observed int64, updated, available int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		Name: "calico-node", Namespace: "calico-system", Generation: generation,
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration:     observed,
			DesiredNumberScheduled: calicoNodeDesired,
			UpdatedNumberScheduled: updated,
			NumberAvailable:        available,
			NumberReady:            available,
			NumberUnavailable:      calicoNodeDesired - available,
		},
	}
}

type rolloutPendingCase struct {
	name        string
	objects     []runtime.Object
	daemonSet   *appsv1.DaemonSet
	wantPending string
}

//nolint:funlen // one entry per state the wait must tell apart
func rolloutPendingCases() []rolloutPendingCase {
	return []rolloutPendingCase{
		{
			name:        "operator still reports the previous Installation generation",
			objects:     []runtime.Object{rolloutInstallation(4), availableAt(3)},
			daemonSet:   calicoNodeDaemonSet(7, 7, 3, 3),
			wantPending: "operator has not applied Installation generation 4 yet (observed 3)",
		},
		{
			name: "calico-node is still rolling out",
			objects: []runtime.Object{rolloutInstallation(4), rolloutStatus(
				rolloutCondition{"Available", "False", 4},
				rolloutCondition{"Progressing", "True", 4},
			)},
			daemonSet:   calicoNodeDaemonSet(8, 8, 1, 2),
			wantPending: "Calico is Progressing: Progressing message",
		},
		{
			name: "calico is degraded",
			objects: []runtime.Object{rolloutInstallation(4), rolloutStatus(
				rolloutCondition{"Available", "True", 4},
				rolloutCondition{"Degraded", "True", 4},
			)},
			daemonSet:   calicoNodeDaemonSet(8, 8, 3, 3),
			wantPending: "Calico is Degraded: Degraded message",
		},
		{
			name: "calico is not available",
			objects: []runtime.Object{rolloutInstallation(4), rolloutStatus(
				rolloutCondition{"Available", "False", 4},
			)},
			daemonSet:   calicoNodeDaemonSet(8, 8, 3, 3),
			wantPending: "Calico is not Available",
		},
		{
			name:        "operator has not reported a status yet",
			objects:     []runtime.Object{rolloutInstallation(1)},
			wantPending: "TigeraStatus calico not reported yet",
		},
		{
			name:        "Installation does not exist yet",
			wantPending: "Installation default not found",
		},
		{
			name:        "DaemonSet controller has not observed the new pod spec",
			objects:     []runtime.Object{rolloutInstallation(4), availableAt(4)},
			daemonSet:   calicoNodeDaemonSet(9, 8, 3, 3),
			wantPending: "calico-node generation 9 not observed yet (observed 8)",
		},
		{
			name:        "calico-node has pods left on the old spec",
			objects:     []runtime.Object{rolloutInstallation(4), availableAt(4)},
			daemonSet:   calicoNodeDaemonSet(9, 9, 2, 3),
			wantPending: "calico-node rollout: 2 of 3 updated, 3 available, 0 unavailable",
		},
		{
			name:        "calico-node does not exist yet",
			objects:     []runtime.Object{rolloutInstallation(4), availableAt(4)},
			wantPending: "DaemonSet calico-system/calico-node not found",
		},
		{
			name:      "operator applied the current generation and calico-node rolled out",
			objects:   []runtime.Object{rolloutInstallation(4), availableAt(4)},
			daemonSet: calicoNodeDaemonSet(9, 9, 3, 3),
		},
	}
}

func TestRolloutPending(t *testing.T) {
	t.Parallel()

	for _, testCase := range rolloutPendingCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resources := dynamicfake.NewSimpleDynamicClient(
				runtime.NewScheme(),
				testCase.objects...)

			core := kubefake.NewClientset()
			if testCase.daemonSet != nil {
				core = kubefake.NewClientset(testCase.daemonSet)
			}

			pending, err := calicoinstaller.RolloutPendingForTest(t.Context(), resources, core)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantPending, pending)
		})
	}
}

func TestWaitForRolloutReportsWhatIsStillPending(t *testing.T) {
	t.Parallel()

	resources := dynamicfake.NewSimpleDynamicClient(
		runtime.NewScheme(), rolloutInstallation(4), availableAt(3),
	)
	core := kubefake.NewClientset(calicoNodeDaemonSet(7, 7, 3, 3))

	err := calicoinstaller.WaitForRolloutForTest(t.Context(), resources, core, 50*time.Millisecond)

	require.ErrorIs(t, err, calicoinstaller.ErrRolloutIncomplete)
	assert.Contains(t, err.Error(), "operator has not applied Installation generation 4 yet")
}

func TestWaitForRolloutReturnsOnceRolledOut(t *testing.T) {
	t.Parallel()

	resources := dynamicfake.NewSimpleDynamicClient(
		runtime.NewScheme(), rolloutInstallation(4), availableAt(4),
	)
	core := kubefake.NewClientset(calicoNodeDaemonSet(9, 9, 3, 3))

	require.NoError(
		t,
		calicoinstaller.WaitForRolloutForTest(t.Context(), resources, core, time.Second),
	)
}
