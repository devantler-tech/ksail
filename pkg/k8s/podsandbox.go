package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

const (
	reservedPodSandboxMessage             = "failed to reserve sandbox name"
	reservedPodSandboxThreshold           = int32(3)
	defaultReservedPodSandboxPollInterval = 5 * time.Second
	reservedPodSandboxListWarningInterval = 30 * time.Second
)

// ErrRepeatedReservedPodSandbox identifies a nested-containerd pod sandbox
// reservation that has repeated often enough to require bounded recovery.
var ErrRepeatedReservedPodSandbox = errors.New("repeated reserved pod sandbox failures")

// RepeatedReservedPodSandboxError describes the Kubernetes event that caused
// KSail to stop waiting for a nested K3s cluster to recover by itself.
type RepeatedReservedPodSandboxError struct {
	Namespace string
	Pod       string
	Count     int32
	Message   string
}

// Error returns a stable marker plus the original Kubernetes event details so
// CI can classify the failure without discarding its diagnostic evidence.
func (e *RepeatedReservedPodSandboxError) Error() string {
	return fmt.Sprintf(
		"%s: namespace=%q pod=%q count=%d: %s",
		ErrRepeatedReservedPodSandbox,
		e.Namespace,
		e.Pod,
		e.Count,
		e.Message,
	)
}

// Unwrap exposes the stable reservation sentinel for errors.Is checks.
func (e *RepeatedReservedPodSandboxError) Unwrap() error {
	return ErrRepeatedReservedPodSandbox
}

// WatchRepeatedReservedPodSandboxes polls Kubernetes warning events until the
// context is cancelled or a current pod reports the reserved-sandbox signature
// at least three times. Transient list failures are reported at a bounded
// cadence but cannot abort an otherwise healthy cluster setup.
func WatchRepeatedReservedPodSandboxes(
	ctx context.Context,
	clientset kubernetes.Interface,
	pollInterval time.Duration,
) error {
	return watchRepeatedReservedPodSandboxes(
		ctx,
		clientset,
		pollInterval,
		func(ctx context.Context, err error) {
			slog.WarnContext(
				ctx,
				"reserved pod sandbox monitor could not list Kubernetes events",
				"error",
				err,
			)
		},
	)
}

func watchRepeatedReservedPodSandboxes(
	ctx context.Context,
	clientset kubernetes.Interface,
	pollInterval time.Duration,
	warn func(context.Context, error),
) error {
	if pollInterval <= 0 {
		pollInterval = defaultReservedPodSandboxPollInterval
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastListWarning time.Time

	started := time.Now()

	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("reason", "FailedCreatePodSandBox"),
		fields.OneTermEqualSelector("type", corev1.EventTypeWarning),
	).String()

	for {
		events, err := clientset.CoreV1().Events(metav1.NamespaceAll).List(
			ctx,
			metav1.ListOptions{FieldSelector: selector},
		)
		if err == nil {
			reservationErr := repeatedReservedPodSandboxFailure(events.Items, started)
			if reservationErr != nil {
				return reservationErr
			}
		} else {
			if ctx.Err() != nil {
				return nil
			}

			now := time.Now()

			shouldWarn := lastListWarning.IsZero() ||
				now.Sub(lastListWarning) >= reservedPodSandboxListWarningInterval
			if shouldWarn {
				lastListWarning = now

				warn(ctx, fmt.Errorf("list Kubernetes warning events: %w", err))
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func repeatedReservedPodSandboxFailure(events []corev1.Event, started time.Time) error {
	for idx := range events {
		event := &events[idx]
		if !isRepeatedReservedPodSandboxEvent(event, started) {
			continue
		}

		return &RepeatedReservedPodSandboxError{
			Namespace: event.InvolvedObject.Namespace,
			Pod:       event.InvolvedObject.Name,
			Count:     podSandboxEventCount(event),
			Message:   event.Message,
		}
	}

	return nil
}

func isRepeatedReservedPodSandboxEvent(event *corev1.Event, started time.Time) bool {
	if event.Type != corev1.EventTypeWarning ||
		event.Reason != "FailedCreatePodSandBox" ||
		event.InvolvedObject.Kind != "Pod" ||
		!strings.Contains(strings.ToLower(event.Message), reservedPodSandboxMessage) ||
		podSandboxEventCount(event) < reservedPodSandboxThreshold {
		return false
	}

	observedAt := podSandboxEventTime(event)

	return !observedAt.IsZero() && !observedAt.Before(started)
}

func podSandboxEventCount(event *corev1.Event) int32 {
	count := event.Count
	if event.Series != nil {
		count = max(count, event.Series.Count)
	}

	return count
}

func podSandboxEventTime(event *corev1.Event) time.Time {
	observedAt := event.CreationTimestamp.Time
	if event.LastTimestamp.After(observedAt) {
		observedAt = event.LastTimestamp.Time
	}

	if event.EventTime.After(observedAt) {
		observedAt = event.EventTime.Time
	}

	if event.Series != nil && event.Series.LastObservedTime.After(observedAt) {
		observedAt = event.Series.LastObservedTime.Time
	}

	return observedAt
}
