package readiness

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// isContextError returns true when the error wraps context.DeadlineExceeded or
// context.Canceled. These errors typically originate from the client-go rate
// limiter when the per-attempt context expires while an API request is queued.
// They should be treated as transient so the outer polling loop can retry
// rather than aborting prematurely.
func isContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// errImagePullFailure is returned when the connectivity check pod cannot
// pull its container image, which is distinct from actual API connectivity
// failures and requires different remediation.
var errImagePullFailure = errors.New("connectivity check pod image pull failed")

const (
	connectivityPodName  = "ksail-api-connectivity-check"
	connectivityPodNS    = "default"
	connectivityPodImage = "busybox:1.36.1"

	// connectivityPodTimeout is the maximum time to wait for a single
	// connectivity test pod to complete before retrying.
	connectivityPodTimeout = 30 * time.Second

	// cleanupTimeout bounds how long the deferred pod cleanup may block.
	cleanupTimeout = 10 * time.Second
)

// errInvalidClusterIP is returned when the kubernetes service ClusterIP
// is not a valid IP address.
var errInvalidClusterIP = errors.New("invalid kubernetes service ClusterIP")

// WaitForInClusterAPIConnectivity verifies that pods can reach the Kubernetes
// API server ClusterIP from within the cluster. This catches race conditions
// where the CNI (e.g., Cilium) DaemonSet pods report Ready but the eBPF
// dataplane hasn't fully programmed pod-to-service routing paths.
//
// The function creates a short-lived busybox pod that tests TCP connectivity
// to the API server ClusterIP on port 443. It retries until the connection
// succeeds or the deadline is reached.
//
// Parameters:
//   - ctx: Context for cancellation
//   - clientset: Kubernetes client interface
//   - deadline: Maximum time to wait for connectivity verification
//
// Returns an error if connectivity cannot be verified within the deadline.
func WaitForInClusterAPIConnectivity(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	svc, err := clientset.CoreV1().Services(connectivityPodNS).Get(
		ctx, "kubernetes", metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("get kubernetes service ClusterIP: %w", err)
	}

	apiServerIP := svc.Spec.ClusterIP

	// Validate that the ClusterIP is a valid IP address to prevent injection
	// into the shell command executed inside the test pod.
	if ip := net.ParseIP(apiServerIP); ip == nil {
		return fmt.Errorf("%w: %q", errInvalidClusterIP, apiServerIP)
	}

	// Ensure no leftover test pod from a previous run.
	deleteConnectivityPod(ctx, clientset)

	// Always clean up the test pod when done, bounded by a short timeout
	// so cleanup is best-effort and never blocks the caller indefinitely.
	//nolint:contextcheck // intentionally uses a fresh context for best-effort cleanup
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()

		deleteConnectivityPod(cleanupCtx, clientset)
	}()

	// Track the last image pull error so we can surface it specifically if
	// all retries are exhausted. Image pull failures (e.g. Docker Hub
	// rate-limiting, transient registry outages) are retried automatically
	// instead of aborting immediately.
	var lastImagePullErr error

	pollErr := PollForReadiness(ctx, deadline, func(pollCtx context.Context) (bool, error) {
		succeeded, err := runConnectivityTestPod(pollCtx, clientset, apiServerIP)
		if err != nil && errors.Is(err, errImagePullFailure) {
			lastImagePullErr = err

			return false, nil // treat as transient; retry with a new pod
		}

		return succeeded, err
	})
	if pollErr != nil {
		// If polling timed out and image pull errors were observed, surface
		// the image pull error specifically instead of the misleading "not
		// reachable" message. When a different fatal error stopped polling
		// (e.g. Forbidden), prefer that error over a stale image pull error.
		if lastImagePullErr != nil &&
			(errors.Is(pollErr, context.DeadlineExceeded) || errors.Is(pollErr, context.Canceled)) {
			return fmt.Errorf(
				"in-cluster API connectivity pre-flight check aborted: %w",
				lastImagePullErr,
			)
		}

		return fmt.Errorf(
			"in-cluster API connectivity check failed: "+
				"API server ClusterIP %s:443 not reachable from pods: %w",
			apiServerIP, pollErr,
		)
	}

	return nil
}

// runConnectivityTestPod creates a short-lived pod that tests TCP connectivity
// to the API server ClusterIP. Returns (true, nil) if the connection succeeded,
// (false, nil) if the connection failed (should retry), or (false, error) on
// non-recoverable errors.
func runConnectivityTestPod(
	ctx context.Context,
	clientset kubernetes.Interface,
	apiServerIP string,
) (bool, error) {
	deleteConnectivityPod(ctx, clientset)

	pod := connectivityCheckPod(apiServerIP)

	_, err := clientset.CoreV1().Pods(connectivityPodNS).Create(
		ctx, pod, metav1.CreateOptions{},
	)
	if err != nil {
		if isTransientPodError(err) {
			return false, nil
		}

		return false, fmt.Errorf("create connectivity check pod: %w", err)
	}

	return waitForConnectivityPodCompletion(ctx, clientset)
}

// waitForConnectivityPodCompletion polls the test pod until it reaches a
// terminal state or the per-attempt timeout expires.
func waitForConnectivityPodCompletion(
	ctx context.Context,
	clientset kubernetes.Interface,
) (bool, error) {
	podCtx, cancel := context.WithTimeout(ctx, connectivityPodTimeout)
	defer cancel()

	ticker := time.NewTicker(readinessPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-podCtx.Done():
			return false, nil // per-attempt timeout; retry on next poll
		case <-ticker.C:
			succeeded, done, err := pollConnectivityPod(podCtx, clientset)
			if done || err != nil {
				return succeeded, err
			}
		}
	}
}

// pollConnectivityPod fetches the connectivity check pod and evaluates its
// status. Returns:
//   - (true, true, nil) — pod succeeded; connectivity confirmed
//   - (false, true, nil) — pod reached terminal failure; caller should exit
//     and let the outer PollForReadiness retry
//   - (false, true, err) — non-recoverable error; caller should propagate
//   - (false, false, nil) — pod still in progress; caller should keep polling
func pollConnectivityPod(
	ctx context.Context,
	clientset kubernetes.Interface,
) (bool, bool, error) {
	pod, getErr := clientset.CoreV1().Pods(connectivityPodNS).Get(
		ctx, connectivityPodName, metav1.GetOptions{},
	)
	if getErr != nil {
		if isTransientPodError(getErr) || isContextError(getErr) {
			return false, true, nil
		}

		return false, true, fmt.Errorf("get connectivity check pod: %w", getErr)
	}

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return true, true, nil
	case corev1.PodFailed:
		return false, true, nil
	case corev1.PodPending:
		pullErr := checkPendingPodImagePull(pod)
		if pullErr != nil {
			return false, true, pullErr
		}
	case corev1.PodRunning, corev1.PodUnknown:
		// Still Running or Unknown — keep waiting.
	}

	return false, false, nil
}

// isTransientPodError returns true for Kubernetes API errors that are expected
// to be transient and should be retried (e.g. AlreadyExists from a previous
// pod still terminating, server timeouts, rate limiting). Permanent errors
// like Forbidden or Invalid are surfaced immediately so the caller gets
// actionable failure output.
func isTransientPodError(err error) bool {
	return apierrors.IsAlreadyExists(err) ||
		apierrors.IsConflict(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsNotFound(err)
}

// checkPendingPodImagePull checks a Pending pod for image pull failures and
// returns a descriptive error if one is detected. Returns nil if the pod is
// still pulling normally.
func checkPendingPodImagePull(pod *corev1.Pod) error {
	reason, message, failed := containerImagePullFailure(pod)
	if !failed {
		return nil
	}

	if message != "" {
		return fmt.Errorf(
			"%w: %s: %s (image %q)",
			errImagePullFailure, reason, message, connectivityPodImage,
		)
	}

	return fmt.Errorf(
		"%w: %s (image %q)",
		errImagePullFailure, reason, connectivityPodImage,
	)
}

// containerImagePullFailure inspects a pod's container statuses for image pull
// failures. Returns the waiting reason, message, and true if an image pull
// failure is detected, or ("", "", false) if no image pull failure is found.
func containerImagePullFailure(pod *corev1.Pod) (string, string, bool) {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && isImagePullFailureReason(cs.State.Waiting.Reason) {
			return cs.State.Waiting.Reason, cs.State.Waiting.Message, true
		}
	}

	return "", "", false
}

// isImagePullFailureReason returns true if the given container waiting reason
// indicates that the container image could not be pulled. These are
// non-recoverable within the scope of a single pod attempt and should be
// surfaced immediately.
func isImagePullFailureReason(reason string) bool {
	switch reason {
	case "ImagePullBackOff", "ErrImagePull", "ImageInspectError", "ErrImageNeverPull":
		return true
	default:
		return false
	}
}

// connectivityCheckPod builds the spec for the short-lived connectivity test pod.
func connectivityCheckPod(apiServerIP string) *corev1.Pod {
	gracePeriod := int64(0)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      connectivityPodName,
			Namespace: connectivityPodNS,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:            "check",
				Image:           connectivityPodImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"sh", "-c",
					fmt.Sprintf("nc -w 5 %s 443 </dev/null", apiServerIP),
				},
			}},
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &gracePeriod,
			// Tolerate all taints so the pod can schedule on control-plane
			// nodes in single-node clusters.
			Tolerations: []corev1.Toleration{{
				Operator: corev1.TolerationOpExists,
			}},
		},
	}
}

// deleteConnectivityPod force-deletes the connectivity test pod.
func deleteConnectivityPod(ctx context.Context, clientset kubernetes.Interface) {
	gracePeriod := int64(0)
	_ = clientset.CoreV1().Pods(connectivityPodNS).Delete(
		ctx,
		connectivityPodName,
		metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod},
	)
}
