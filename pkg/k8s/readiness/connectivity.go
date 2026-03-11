package readiness

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	connectivityPodName  = "ksail-api-connectivity-check"
	connectivityPodNS    = "default"
	connectivityPodImage = "busybox:stable"

	// connectivityPodTimeout is the maximum time to wait for a single
	// connectivity test pod to complete before retrying.
	connectivityPodTimeout = 30 * time.Second
)

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

	// Ensure no leftover test pod from a previous run.
	deleteConnectivityPod(ctx, clientset)

	// Always clean up the test pod when done.
	defer deleteConnectivityPod(context.WithoutCancel(ctx), clientset)

	pollErr := PollForReadiness(ctx, deadline, func(pollCtx context.Context) (bool, error) {
		return runConnectivityTestPod(pollCtx, clientset, apiServerIP)
	})
	if pollErr != nil {
		return fmt.Errorf(
			"in-cluster API connectivity check failed — "+
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
		// Pod from previous attempt may still be terminating.
		return false, nil //nolint:nilerr // transient; retry on next poll
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
			p, err := clientset.CoreV1().Pods(connectivityPodNS).Get(
				podCtx, connectivityPodName, metav1.GetOptions{},
			)
			if err != nil {
				return false, nil //nolint:nilerr // transient; retry
			}

			switch p.Status.Phase {
			case corev1.PodSucceeded:
				return true, nil
			case corev1.PodFailed:
				return false, nil
			}
			// Still Pending or Running — keep waiting.
		}
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
				Name:  "check",
				Image: connectivityPodImage,
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
