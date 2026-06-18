package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// statusPollInterval is how often the desktop polls cluster status to drive native notifications and
// the dock badge. The local backend's List enumerates providers, so this is deliberately unaggressive.
const statusPollInterval = 8 * time.Second

// clusterSnapshot is the minimal cluster state the status watcher diffs between polls.
type clusterSnapshot struct {
	Name  string
	Phase v1alpha1.ClusterPhase
}

// clusterTransition is a cluster that just entered a notable terminal phase (Ready or Failed).
type clusterTransition struct {
	Name  string
	Phase v1alpha1.ClusterPhase
}

// notableTerminalPhase reports whether a phase is one worth notifying on when newly reached.
func notableTerminalPhase(phase v1alpha1.ClusterPhase) bool {
	return phase == v1alpha1.ClusterPhaseReady || phase == v1alpha1.ClusterPhaseFailed
}

// clusterTransitions returns the clusters whose phase CHANGED since the previous snapshot into a
// notable terminal phase (Ready/Failed). A cluster absent from prev (a first observation) is never
// reported, so launching the app with already-settled clusters does not spam notifications.
func clusterTransitions(prev, curr map[string]clusterSnapshot) []clusterTransition {
	var transitions []clusterTransition

	for key, now := range curr {
		before, seen := prev[key]
		if !seen || before.Phase == now.Phase {
			continue
		}

		if notableTerminalPhase(now.Phase) {
			transitions = append(transitions, clusterTransition(now))
		}
	}

	return transitions
}

// busyCount counts clusters in a transient (in-progress) phase, for the dock badge.
func busyCount(curr map[string]clusterSnapshot) int {
	count := 0

	for _, snap := range curr {
		if snap.Phase == v1alpha1.ClusterPhaseProvisioning ||
			snap.Phase == v1alpha1.ClusterPhaseUpdating ||
			snap.Phase == v1alpha1.ClusterPhaseDeleting {
			count++
		}
	}

	return count
}

// transitionNotification builds the native notification for a terminal cluster transition.
func transitionNotification(transition clusterTransition) notifications.NotificationOptions {
	title := "Cluster ready"
	body := fmt.Sprintf("%q is ready.", transition.Name)

	if transition.Phase == v1alpha1.ClusterPhaseFailed {
		title = "Cluster failed"
		body = fmt.Sprintf("%q failed to reconcile.", transition.Name)
	}

	return notifications.NotificationOptions{
		ID:    fmt.Sprintf("ksail-%s-%s", transition.Name, transition.Phase),
		Title: title,
		Body:  body,
	}
}

// snapshotClusters lists clusters and reduces them to the watcher's snapshot map. The bool is false
// when the list call fails, so the caller keeps the previous snapshot rather than treating a transient
// error as "every cluster vanished" (which would mis-fire transitions on recovery).
func snapshotClusters(
	ctx context.Context,
	service api.ClusterService,
) (map[string]clusterSnapshot, bool) {
	list, err := service.List(ctx)
	if err != nil {
		slog.Debug("cluster status poll failed", "error", err)

		return nil, false
	}

	snapshots := make(map[string]clusterSnapshot, len(list.Items))
	for index := range list.Items {
		cluster := &list.Items[index]
		key := cluster.Namespace + "/" + cluster.Name
		snapshots[key] = clusterSnapshot{Name: cluster.Name, Phase: cluster.Status.Phase}
	}

	return snapshots, true
}

// updateBadge reflects the in-progress cluster count on the dock badge (cleared at zero). Best-effort:
// errors are ignored (the dock badge is unavailable on some platforms).
func updateBadge(dockSvc *dock.DockService, count int) {
	if dockSvc == nil {
		return
	}

	if count == 0 {
		_ = dockSvc.RemoveBadge()

		return
	}

	_ = dockSvc.SetBadge(strconv.Itoa(count))
}

// watchClusterStatus polls cluster status until ctx is cancelled, firing a native notification when a
// cluster reaches Ready/Failed and reflecting in-progress operations on the dock badge. It is the only
// part of this file that touches the Wails services; the diff logic above is pure and unit-tested.
func watchClusterStatus(
	ctx context.Context,
	service api.ClusterService,
	notifSvc *notifications.NotificationService,
	dockSvc *dock.DockService,
) {
	if service == nil {
		return
	}

	if notifSvc != nil {
		// Best-effort: prompts for permission on macOS, no-op/denied elsewhere. A denial just means no
		// notifications are shown — never fatal.
		_, _ = notifSvc.RequestNotificationAuthorization()
	}

	ticker := time.NewTicker(statusPollInterval)
	defer ticker.Stop()

	// prev starts empty, so the first poll establishes a baseline without firing any notification —
	// transitions are only reported for clusters seen in a previous poll.
	prev := map[string]clusterSnapshot{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curr, ok := snapshotClusters(ctx, service)
			if !ok {
				continue
			}

			if notifSvc != nil {
				for _, transition := range clusterTransitions(prev, curr) {
					_ = notifSvc.SendNotification(transitionNotification(transition))
				}
			}

			updateBadge(dockSvc, busyCount(curr))
			prev = curr
		}
	}
}
