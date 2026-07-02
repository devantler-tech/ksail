package mirror

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// TapContainerName is the fixed name of the ephemeral tap container InjectTap
// appends to the tap point's pod. One tap per pod: a second InjectTap on the
// same pod returns ErrTapAlreadyInjected.
const TapContainerName = "ksail-tap"

// DefaultTapImage is the image the tap container runs when the caller does not
// override it with WithTapImage. It matches the `workload debug` default: a
// small, ubiquitous utility image.
const DefaultTapImage = "docker.io/library/alpine:latest"

// tapPollInterval is how often WaitForTap re-reads the pod while waiting for
// the tap container to reach Running.
const tapPollInterval = 250 * time.Millisecond

// ErrTapPointNil is returned when InjectTap or WaitForTap is called with a nil
// TapPoint.
var ErrTapPointNil = errors.New("tap point is nil")

// ErrTapAlreadyInjected is returned when the tap point's pod already carries a
// tap container. Ephemeral containers cannot be removed or restarted, so the
// existing tap is the one to use (or the pod must be replaced).
var ErrTapAlreadyInjected = errors.New("tap container already injected")

// ErrTapTerminated is returned by WaitForTap when the tap container terminated
// instead of reaching Running.
var ErrTapTerminated = errors.New("tap container terminated")

// TapOption customises the ephemeral tap container InjectTap builds.
type TapOption func(*tapConfig)

// tapConfig holds the configurable parts of the tap container spec.
type tapConfig struct {
	image   string
	command []string
}

// WithTapImage overrides the tap container's image (default DefaultTapImage).
func WithTapImage(image string) TapOption {
	return func(cfg *tapConfig) { cfg.image = image }
}

// WithTapCommand overrides the tap container's command. The default is an inert
// holder (`sleep infinity`) until the later traffic increment supplies the real
// tap process.
func WithTapCommand(command ...string) TapOption {
	return func(cfg *tapConfig) { cfg.command = command }
}

// InjectTap appends the read-only tap ephemeral container to the tap point's
// pod and returns the container's name. The container targets the tap point's
// container (TargetContainerName), sharing its process namespace where the
// runtime supports it; pod network is shared regardless, which is what the
// mirror traffic path needs. Injection is one-way — ephemeral containers cannot
// be removed — so a pod that already has a tap yields ErrTapAlreadyInjected.
// Concurrent injectors converge on that same error: a 409 conflict from the
// update re-reads the pod and re-checks for the tap before retrying.
//
// The container runs an inert holder command by default; wiring the actual
// traffic tap and the reverse tunnel are the next Phase 1 increments (#4521).
func InjectTap(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	opts ...TapOption,
) (string, error) {
	if point == nil {
		return "", ErrTapPointNil
	}

	cfg := tapConfig{image: DefaultTapImage, command: []string{"sleep", "infinity"}}
	for _, opt := range opts {
		opt(&cfg)
	}

	pods := client.CoreV1().Pods(point.Namespace)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod, err := pods.Get(ctx, point.Pod, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting pod: %w", err)
		}

		for _, ephemeral := range pod.Spec.EphemeralContainers {
			if ephemeral.Name == TapContainerName {
				return fmt.Errorf(
					"%w: %q on pod %q", ErrTapAlreadyInjected, TapContainerName, point.Pod,
				)
			}
		}

		tap := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:    TapContainerName,
				Image:   cfg.image,
				Command: cfg.command,
			},
			TargetContainerName: point.Container,
		}
		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, tap)

		_, err = pods.UpdateEphemeralContainers(ctx, pod.Name, pod, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating ephemeral containers: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf(
			"injecting tap container into pod %q in %s: %w", point.Pod, point.Namespace, err,
		)
	}

	return TapContainerName, nil
}

// WaitForTap blocks until the tap container on the tap point's pod reports
// Running, the container terminates (ErrTapTerminated, with its exit code), or
// timeout elapses. A pod without a tap status yet is simply polled again — the
// kubelet adds the status asynchronously after injection.
func WaitForTap(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	timeout time.Duration,
) error {
	if point == nil {
		return ErrTapPointNil
	}

	err := wait.PollUntilContextTimeout(
		ctx, tapPollInterval, timeout, true,
		func(ctx context.Context) (bool, error) {
			pod, err := client.CoreV1().
				Pods(point.Namespace).
				Get(ctx, point.Pod, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf(
					"getting pod %q in %s: %w",
					point.Pod,
					point.Namespace,
					err,
				)
			}

			return tapRunning(pod)
		},
	)
	if err != nil {
		return fmt.Errorf(
			"waiting for tap container %q on pod %q: %w", TapContainerName, point.Pod, err,
		)
	}

	return nil
}

// tapRunning reports whether the pod's tap container is Running. A terminated
// tap is terminal (the kubelet never restarts ephemeral containers), so it
// aborts the poll with ErrTapTerminated; a missing status keeps polling.
func tapRunning(pod *corev1.Pod) (bool, error) {
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name != TapContainerName {
			continue
		}

		if status.State.Terminated != nil {
			return false, fmt.Errorf(
				"%w: exit code %d", ErrTapTerminated, status.State.Terminated.ExitCode,
			)
		}

		return status.State.Running != nil, nil
	}

	return false, nil
}
