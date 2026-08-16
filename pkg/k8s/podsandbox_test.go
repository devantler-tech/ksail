//nolint:testpackage // White-box coverage locks the event-classification boundary.
package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

//nolint:funlen // The table documents every event field that guards recovery.
func TestRepeatedReservedPodSandboxFailure(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	recent := metav1.NewTime(started.Add(time.Second))
	stale := metav1.NewTime(started.Add(-time.Second))

	tests := []struct {
		name    string
		event   corev1.Event
		wantErr bool
	}{
		{
			name:    "repeated count",
			event:   reservedPodSandboxEvent(3, nil, recent),
			wantErr: true,
		},
		{
			name: "repeated series count",
			event: reservedPodSandboxEvent(1, &corev1.EventSeries{
				Count:            4,
				LastObservedTime: metav1.NewMicroTime(recent.Time),
			}, recent),
			wantErr: true,
		},
		{
			name:  "below threshold",
			event: reservedPodSandboxEvent(2, nil, recent),
		},
		{
			name:  "stale event",
			event: reservedPodSandboxEvent(9, nil, stale),
		},
		{
			name: "wrong reason",
			event: func() corev1.Event {
				event := reservedPodSandboxEvent(9, nil, recent)
				event.Reason = "FailedScheduling"

				return event
			}(),
		},
		{
			name: "normal event",
			event: func() corev1.Event {
				event := reservedPodSandboxEvent(9, nil, recent)
				event.Type = corev1.EventTypeNormal

				return event
			}(),
		},
		{
			name: "non-pod event",
			event: func() corev1.Event {
				event := reservedPodSandboxEvent(9, nil, recent)
				event.InvolvedObject.Kind = "Node"

				return event
			}(),
		},
		{
			name: "different pod sandbox failure",
			event: func() corev1.Event {
				event := reservedPodSandboxEvent(9, nil, recent)
				event.Message = "failed to setup network for sandbox: flannel subnet.env missing"

				return event
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := repeatedReservedPodSandboxFailure([]corev1.Event{test.event}, started)
			if !test.wantErr {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrRepeatedReservedPodSandbox)

			var reservationErr *RepeatedReservedPodSandboxError
			require.ErrorAs(t, err, &reservationErr)
			assert.Equal(t, "argocd", reservationErr.Namespace)
			assert.Equal(t, "argocd-server-0", reservationErr.Pod)
			assert.GreaterOrEqual(t, reservationErr.Count, int32(3))
		})
	}
}

func TestWatchRepeatedReservedPodSandboxesStopsOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WatchRepeatedReservedPodSandboxes(ctx, fake.NewSimpleClientset(), time.Millisecond)
	require.NoError(t, err)
}

func TestWatchRepeatedReservedPodSandboxesReportsListErrorsWithoutSpamming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientset := fake.NewSimpleClientset()
	attempts := 0

	clientset.PrependReactor(
		"list",
		"events",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			attempts++
			if attempts == 3 {
				cancel()
			}

			return true, nil, assert.AnError
		},
	)

	reported := make([]error, 0, 1)
	err := watchRepeatedReservedPodSandboxes(
		ctx,
		clientset,
		time.Millisecond,
		func(_ context.Context, err error) {
			reported = append(reported, err)
		},
	)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, attempts, 3)
	require.Len(t, reported, 1)
	assert.ErrorIs(t, reported[0], assert.AnError)
}

func TestWatchRepeatedReservedPodSandboxesReturnsTypedCurrentEvent(t *testing.T) {
	t.Parallel()

	recent := metav1.NewTime(time.Now().Add(time.Minute))
	event := reservedPodSandboxEvent(3, nil, recent)
	clientset := fake.NewSimpleClientset(&event)

	err := WatchRepeatedReservedPodSandboxes(
		context.Background(),
		clientset,
		time.Millisecond,
	)

	require.ErrorIs(t, err, ErrRepeatedReservedPodSandbox)
	assert.Contains(t, err.Error(), `namespace="argocd"`)
	assert.Contains(t, err.Error(), `pod="argocd-server-0"`)
}

func reservedPodSandboxEvent(
	count int32,
	series *corev1.EventSeries,
	lastTimestamp metav1.Time,
) corev1.Event {
	return corev1.Event{
		Type:   corev1.EventTypeWarning,
		Reason: "FailedCreatePodSandBox",
		Message: "Failed to create pod sandbox: failed to reserve sandbox name " +
			"k8s_argocd-server_argocd_123_0: name is reserved",
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "argocd",
			Name:      "argocd-server-0",
		},
		Count:         count,
		Series:        series,
		LastTimestamp: lastTimestamp,
	}
}
