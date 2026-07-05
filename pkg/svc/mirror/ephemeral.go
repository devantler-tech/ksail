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

// ephemeralPollInterval is how often the wait helpers re-read the pod while
// waiting for an injected ephemeral container to reach Running.
const ephemeralPollInterval = 250 * time.Millisecond

// ErrTapPointNil is returned when an inject or wait helper is called with a
// nil TapPoint.
var ErrTapPointNil = errors.New("tap point is nil")

// ephemeralKind describes one ksail-managed ephemeral container flavour (the
// read-only tap, the steering agent) so the injection and wait mechanics can
// be shared while each flavour keeps its own name, privileges, and sentinel
// errors.
type ephemeralKind struct {
	// containerName is the flavour's fixed container name — one per pod.
	containerName string
	// label names the flavour in wrapped error messages.
	label string
	// securityContext is the flavour's hardened security context.
	securityContext *corev1.SecurityContext
	// alreadyInjected is returned when the pod already carries the container.
	alreadyInjected error
	// terminated is returned when the container terminated instead of running.
	terminated error
}

// ephemeralConfig holds the caller-configurable parts of an injected
// container's spec.
type ephemeralConfig struct {
	image   string
	command []string
}

// injectEphemeralContainer appends the kind's ephemeral container to the tap
// point's pod and returns the container's name. The container targets the tap
// point's container (TargetContainerName), sharing its process namespace where
// the runtime supports it; pod network is shared regardless. Injection is
// one-way — ephemeral containers cannot be removed — so a pod that already has
// the kind's container yields its alreadyInjected error. Concurrent injectors
// converge on that same error: a 409 conflict from the update re-reads the pod
// and re-checks before retrying.
func injectEphemeralContainer(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	kind ephemeralKind,
	cfg ephemeralConfig,
) (string, error) {
	if point == nil {
		return "", ErrTapPointNil
	}

	pods := client.CoreV1().Pods(point.Namespace)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod, err := pods.Get(ctx, point.Pod, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting pod: %w", err)
		}

		for _, ephemeral := range pod.Spec.EphemeralContainers {
			if ephemeral.Name == kind.containerName {
				return fmt.Errorf(
					"%w: %q on pod %q", kind.alreadyInjected, kind.containerName, point.Pod,
				)
			}
		}

		container := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            kind.containerName,
				Image:           cfg.image,
				Command:         cfg.command,
				SecurityContext: kind.securityContext,
			},
			TargetContainerName: point.Container,
		}
		pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, container)

		_, err = pods.UpdateEphemeralContainers(ctx, pod.Name, pod, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating ephemeral containers: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf(
			"injecting %s container into pod %q in %s: %w",
			kind.label, point.Pod, point.Namespace, err,
		)
	}

	return kind.containerName, nil
}

// waitForEphemeralContainer blocks until the kind's container on the tap
// point's pod reports Running, the container terminates (the kind's terminated
// error, with its exit code), or timeout elapses. A pod without a matching
// container status yet is simply polled again — the kubelet adds the status
// asynchronously after injection.
func waitForEphemeralContainer(
	ctx context.Context,
	client kubernetes.Interface,
	point *TapPoint,
	kind ephemeralKind,
	timeout time.Duration,
) error {
	if point == nil {
		return ErrTapPointNil
	}

	err := wait.PollUntilContextTimeout(
		ctx, ephemeralPollInterval, timeout, true,
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

			return ephemeralContainerRunning(pod, kind)
		},
	)
	if err != nil {
		return fmt.Errorf(
			"waiting for %s container %q on pod %q: %w",
			kind.label, kind.containerName, point.Pod, err,
		)
	}

	return nil
}

// ephemeralContainerRunning reports whether the pod's kind container is
// Running. A terminated container is terminal (the kubelet never restarts
// ephemeral containers), so it aborts the poll with the kind's terminated
// error; a missing status keeps polling.
func ephemeralContainerRunning(pod *corev1.Pod, kind ephemeralKind) (bool, error) {
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name != kind.containerName {
			continue
		}

		if status.State.Terminated != nil {
			return false, fmt.Errorf(
				"%w: exit code %d", kind.terminated, status.State.Terminated.ExitCode,
			)
		}

		return status.State.Running != nil, nil
	}

	return false, nil
}

// hardenedSecurityContext returns the security context every ksail-managed
// ephemeral container runs with: everything dropped except the single given
// capability, no privilege escalation, a read-only root filesystem, and the
// runtime's default seccomp profile. Callers are intentionally not allowed to
// widen it: each flavour's guarantee rests on holding exactly one capability.
func hardenedSecurityContext(capability corev1.Capability) *corev1.SecurityContext {
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true

	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  []corev1.Capability{capability},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}
