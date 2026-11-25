package hetznercsiinstaller

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	controllerSampleInterval = 2 * time.Second
	controllerPodSelector    = "app.kubernetes.io/instance=hcloud-csi"
)

// controllerTerminationObserver records the CSI Pods that exist before installation
// and returns an observer that reports a driver's earliest termination in Pods
// created after that point. Pods are told apart by UID, so the installer's clock
// is never compared with the API server's. Observation is best effort: when the
// existing Pods cannot be read it returns nil, and it never changes the installer
// result or its readiness decision.
func (h *Installer) controllerTerminationObserver(ctx context.Context) func(context.Context) {
	client, err := k8s.NewClientset(h.kubeconfig, h.kubeContext)
	if err != nil {
		slog.DebugContext(ctx, "could not initialize Hetzner CSI controller diagnostics")

		return nil
	}

	pods := client.CoreV1().Pods(hetzner.Namespace)

	existing, err := listControllerPods(ctx, pods)
	if err != nil {
		slog.DebugContext(ctx, "could not read existing Hetzner CSI controller Pods")

		return nil
	}

	ignored := make(map[types.UID]struct{}, len(existing))
	for idx := range existing {
		ignored[existing[idx].UID] = struct{}{}
	}

	return func(ctx context.Context) {
		_ = wait.PollUntilContextCancel(ctx, controllerSampleInterval, true,
			func(ctx context.Context) (bool, error) {
				list, err := listControllerPods(ctx, pods)
				if err != nil {
					return false, nil //nolint:nilerr // a failed sample is retried on the next tick
				}

				for idx := range list {
					pod := &list[idx]

					_, skip := ignored[pod.UID]
					if skip || pod.UID == "" ||
						!strings.HasPrefix(pod.Name, "hcloud-csi-controller-") {
						continue
					}

					if logControllerTermination(ctx, pod) {
						ignored[pod.UID] = struct{}{}
					}
				}

				return false, nil
			})
	}
}

func listControllerPods(ctx context.Context, pods typedcorev1.PodInterface) ([]corev1.Pod, error) {
	sampleContext, cancel := context.WithTimeout(ctx, controllerSampleInterval)
	defer cancel()

	list, err := pods.List(sampleContext, metav1.ListOptions{LabelSelector: controllerPodSelector})
	if err != nil {
		return nil, err //nolint:wrapcheck // diagnostics discard the error
	}

	return list.Items, nil
}

func logControllerTermination(ctx context.Context, pod *corev1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "hcloud-csi-driver" {
			continue
		}

		termination := status.LastTerminationState.Terminated
		if termination == nil {
			termination = status.State.Terminated
		}

		if termination == nil {
			return false
		}

		slog.WarnContext(
			ctx,
			"observed Hetzner CSI controller driver termination",
			"pod", pod.Name,
			"restart_count", status.RestartCount,
			"exit_code", termination.ExitCode,
			"signal", termination.Signal,
			"reason", termination.Reason,
			"started_at", termination.StartedAt.UTC(),
			"finished_at", termination.FinishedAt.UTC(),
		)

		return true
	}

	return false
}
