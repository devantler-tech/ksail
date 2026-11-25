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
)

const controllerSampleInterval = 2 * time.Second

// observeControllerTerminations records a driver's earliest observed termination
// before Helm can time out or replace the Pod. Observation is best effort so
// it cannot change the installer result or its readiness decision.
func (h *Installer) observeControllerTerminations(ctx context.Context, startedAt time.Time) {
	client, err := k8s.NewClientset(h.kubeconfig, h.kubeContext)
	if err != nil {
		slog.DebugContext(ctx, "could not initialize Hetzner CSI controller diagnostics")

		return
	}

	pods := client.CoreV1().Pods(hetzner.Namespace)
	observed := make(map[types.UID]struct{})

	_ = wait.PollUntilContextCancel(ctx, controllerSampleInterval, true,
		func(ctx context.Context) (bool, error) {
			sampleContext, cancel := context.WithTimeout(ctx, controllerSampleInterval)
			defer cancel()

			list, err := pods.List(sampleContext, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/instance=hcloud-csi",
			})
			if err != nil {
				return false, nil
			}

			for idx := range list.Items {
				pod := &list.Items[idx]

				_, seen := observed[pod.UID]
				if seen || pod.UID == "" || pod.CreationTimestamp.Time.Before(startedAt) ||
					!strings.HasPrefix(pod.Name, "hcloud-csi-controller-") {
					continue
				}

				if logControllerTermination(ctx, pod) {
					observed[pod.UID] = struct{}{}
				}
			}

			return false, nil
		})
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
