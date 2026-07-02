package talosprovisioner

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// longhornNamespace is the namespace Longhorn installs into. Its presence is the
// signal that the cluster runs Longhorn replicated storage, so the between-node
// storage-health gate engages only where it is relevant (Kind/K3d/etc. have no such
// backend and the gate stays inert).
const longhornNamespace = "longhorn-system"

// longhornVolumeGVR is the GroupVersionResource for Longhorn Volume custom resources.
func longhornVolumeGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "longhorn.io",
		Version:  "v1beta2",
		Resource: "volumes",
	}
}

// isUnhealthyRobustness reports whether a Longhorn volume robustness state means the
// volume is attached but not fully replicated — "degraded" (a replica is rebuilding)
// or "faulted" (all replicas are down). The roll must not drain the next
// replica-bearing node while any attached volume is in one of these states.
// "healthy" volumes are fine, and "unknown"/"" volumes are detached (no active
// replicas to lose during the roll) so they are intentionally not gated on —
// otherwise a permanently-detached volume would hold the gate open until timeout.
func isUnhealthyRobustness(robustness string) bool {
	switch strings.ToLower(robustness) {
	case "degraded", "faulted":
		return true
	default:
		return false
	}
}

// storageHealthProber reports which replicated-storage volumes are currently not
// healthy. It is a seam so the between-node storage-health gate can be unit-tested
// without a live cluster or a real storage backend.
type storageHealthProber interface {
	// degradedVolumes returns the "<namespace>/<name>" identifiers of volumes that are
	// attached but not yet healthy. An empty result means all volumes are healthy.
	degradedVolumes(ctx context.Context) ([]string, error)
}

// storageHealthTimeout returns the configured between-node storage-health gate
// timeout. Zero (the default) means the gate is disabled.
func (p *Provisioner) storageHealthTimeout() time.Duration {
	if p.options != nil {
		return p.options.StorageHealthTimeout
	}

	return 0
}

// buildStorageHealthProberOrWarn builds the between-node storage-health prober when
// the gate is enabled (spec.cluster.talos.storageHealthTimeout > 0), degrading
// gracefully on a construction error: it warns and returns nil so a probe-setup
// failure (e.g. the storage backend briefly unreachable, or RBAC withholding the
// namespace lookup) never aborts a roll. Returns nil when the gate is disabled or no
// replicated storage backend is detected. Shared by the primary cluster-update roll
// and the autoscaler roll so both apply the gate identically.
func (p *Provisioner) buildStorageHealthProberOrWarn(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
) storageHealthProber {
	if p.storageHealthTimeout() <= 0 {
		return nil
	}

	prober, err := p.buildStorageHealthProber(ctx, clientset, clusterName)
	if err != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ storage-health gate disabled: %v\n", err)

		return nil
	}

	return prober
}

// buildStorageHealthProber composes the probers for the cluster's storage. The
// backend-agnostic generic prober (PV / PVC / VolumeAttachment stability) always
// engages when the gate is enabled, so the gate is no longer inert on clusters
// without a detected backend (#5688); a backend-specific prober is layered on top
// when one is detected — Longhorn today, by the presence of its namespace. A
// detection lookup that fails for any reason other than a clean NotFound (RBAC, API
// unreachable, transient) is propagated rather than swallowed, so an enabled gate
// surfaces the failure (and disables with a warning via
// buildStorageHealthProberOrWarn) instead of silently probing less than it should.
func (p *Provisioner) buildStorageHealthProber(
	ctx context.Context,
	clientset kubernetes.Interface,
	clusterName string,
) (storageHealthProber, error) {
	probers := []storageHealthProber{&genericStorageProber{clientset: clientset}}

	detected, err := p.longhornDetected(ctx, clientset)
	if err != nil {
		return nil, err
	}

	if detected {
		dynamicClient, err := p.newDynamicClient(clusterName)
		if err != nil {
			return nil, fmt.Errorf("create dynamic client for storage-health gate: %w", err)
		}

		probers = append(probers, &longhornVolumeProber{client: dynamicClient})
	}

	return &multiStorageProber{probers: probers}, nil
}

// longhornDetected reports whether the cluster runs Longhorn, by the presence of its
// namespace. Only a clean NotFound means "no Longhorn" (false, nil); every other
// lookup error (RBAC, API unreachable, transient) is returned so the caller does not
// silently disable an enabled gate on a cluster that may well run Longhorn.
func (p *Provisioner) longhornDetected(
	ctx context.Context,
	clientset kubernetes.Interface,
) (bool, error) {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, longhornNamespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("detect longhorn namespace: %w", err)
	}

	return true, nil
}

// newDynamicClient builds a dynamic client for the cluster, resolving the kubeconfig
// path and context the same way createK8sClient does.
func (p *Provisioner) newDynamicClient(clusterName string) (dynamic.Interface, error) {
	canonicalPath, kubeconfigContext, err := p.resolveKubeconfig(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := k8s.NewDynamicClient(canonicalPath, kubeconfigContext)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return client, nil
}

// waitForStorageHealthy blocks until the prober reports no unhealthy volumes or the
// timeout elapses. It is a no-op when the gate is disabled (timeout <= 0) or no
// storage backend was detected (prober == nil). A transient prober error (e.g. the
// API briefly unreachable just after a reboot) is treated as "keep waiting" rather
// than a hard failure; on timeout the error names the volumes still unhealthy so the
// operator can see what the roll is waiting on (and pair the gate with spare rebuild
// capacity — see the storageHealthTimeout option docs).
func (p *Provisioner) waitForStorageHealthy(
	ctx context.Context,
	prober storageHealthProber,
	timeout time.Duration,
) error {
	if prober == nil || timeout <= 0 {
		return nil
	}

	var lastUnhealthy []string

	loggedWaiting := false

	pollErr := readiness.PollForReadiness(ctx, timeout, func(ctx context.Context) (bool, error) {
		unhealthy, err := prober.degradedVolumes(ctx)
		if err != nil {
			// Transient (e.g. API not yet reachable after the reboot): keep polling
			// until the timeout rather than failing the roll on a blip.
			return false, nil //nolint:nilerr
		}

		lastUnhealthy = unhealthy
		if len(unhealthy) == 0 {
			return true, nil
		}

		if !loggedWaiting {
			_, _ = fmt.Fprintf(p.logWriter,
				"    Waiting for storage volumes to rebuild before next node: %s\n",
				strings.Join(unhealthy, ", "))

			loggedWaiting = true
		}

		return false, nil
	})
	if pollErr != nil {
		stuck := strings.Join(lastUnhealthy, ", ")
		if stuck == "" {
			stuck = "volume health could not be determined"
		}

		return fmt.Errorf("%w after %s: %s", ErrStorageHealthTimeout, timeout, stuck)
	}

	return nil
}

// multiStorageProber unions the degraded sets of its probers, so the generic
// Kubernetes signals and a backend-specific probe gate the roll together. Any
// prober error is propagated (the poll loop treats it as transient and keeps
// waiting).
type multiStorageProber struct {
	probers []storageHealthProber
}

// degradedVolumes returns the sorted, de-duplicated union of the probers' degraded
// sets.
func (m *multiStorageProber) degradedVolumes(ctx context.Context) ([]string, error) {
	var all []string

	for _, prober := range m.probers {
		unhealthy, err := prober.degradedVolumes(ctx)
		if err != nil {
			return nil, err
		}

		all = append(all, unhealthy...)
	}

	slices.Sort(all)

	return slices.Compact(all), nil
}

// genericStorageProber is the backend-agnostic storageHealthProber (#5688): it
// reports storage-stability signals every Kubernetes cluster exposes —
// PersistentVolumes in phase Failed, PersistentVolumeClaims in phase Lost, and
// VolumeAttachments whose CSI controller's attach/detach loop reports an error (the
// closest portable "storage controller unstable" signal). Benign steady states are
// deliberately not gated on: a Pending PVC (WaitForFirstConsumer) and a detached
// attachment without errors are normal, and a stale attachError on an attachment
// that has since attached is ignored — otherwise they would hold the gate open
// until timeout.
type genericStorageProber struct {
	clientset kubernetes.Interface
}

// degradedVolumes returns the identifiers of generically-unhealthy storage objects,
// prefixed by kind ("pv/", "pvc/", "volumeattachment/") to stay distinguishable
// from a backend prober's "<namespace>/<name>" volumes in logs and errors. The
// result is sorted for deterministic logs, errors, and tests.
func (g *genericStorageProber) degradedVolumes(ctx context.Context) ([]string, error) {
	unhealthy, err := g.degradedPersistentVolumes(ctx)
	if err != nil {
		return nil, err
	}

	lostClaims, err := g.lostPersistentVolumeClaims(ctx)
	if err != nil {
		return nil, err
	}

	erroredAttachments, err := g.erroredVolumeAttachments(ctx)
	if err != nil {
		return nil, err
	}

	unhealthy = append(unhealthy, lostClaims...)
	unhealthy = append(unhealthy, erroredAttachments...)

	slices.Sort(unhealthy)

	return unhealthy, nil
}

// degradedPersistentVolumes returns "pv/<name>" for every PersistentVolume in phase
// Failed (its recycle/delete failed or the backing volume errored).
func (g *genericStorageProber) degradedPersistentVolumes(ctx context.Context) ([]string, error) {
	pvs, err := g.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumes: %w", err)
	}

	var unhealthy []string

	for i := range pvs.Items {
		if pvs.Items[i].Status.Phase == corev1.VolumeFailed {
			unhealthy = append(unhealthy, "pv/"+pvs.Items[i].Name)
		}
	}

	return unhealthy, nil
}

// lostPersistentVolumeClaims returns "pvc/<namespace>/<name>" for every
// PersistentVolumeClaim in phase Lost (its bound PV is gone). Pending claims are
// normal steady state (WaitForFirstConsumer) and are not reported.
func (g *genericStorageProber) lostPersistentVolumeClaims(ctx context.Context) ([]string, error) {
	pvcs, err := g.clientset.CoreV1().
		PersistentVolumeClaims(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumeclaims: %w", err)
	}

	var unhealthy []string

	for i := range pvcs.Items {
		if pvcs.Items[i].Status.Phase == corev1.ClaimLost {
			unhealthy = append(unhealthy,
				"pvc/"+pvcs.Items[i].Namespace+"/"+pvcs.Items[i].Name)
		}
	}

	return unhealthy, nil
}

// erroredVolumeAttachments returns "volumeattachment/<name>" for every
// VolumeAttachment whose CSI attach/detach loop reports an error: an attach error
// while not attached, or any detach error. An attachment that has since attached is
// healthy even if a stale attachError remains recorded.
func (g *genericStorageProber) erroredVolumeAttachments(ctx context.Context) ([]string, error) {
	attachments, err := g.clientset.StorageV1().
		VolumeAttachments().
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumeattachments: %w", err)
	}

	var unhealthy []string

	for i := range attachments.Items {
		status := attachments.Items[i].Status
		if (status.AttachError != nil && !status.Attached) || status.DetachError != nil {
			unhealthy = append(unhealthy, "volumeattachment/"+attachments.Items[i].Name)
		}
	}

	return unhealthy, nil
}

// longhornVolumeProber is the storageHealthProber backed by Longhorn Volume custom
// resources read through a dynamic client.
type longhornVolumeProber struct {
	client dynamic.Interface
}

// degradedVolumes lists Longhorn volumes and returns the namespaced names of those
// whose status.robustness is "degraded" or "faulted" (see unhealthyRobustness). The
// result is sorted for deterministic logs, errors, and tests.
func (l *longhornVolumeProber) degradedVolumes(ctx context.Context) ([]string, error) {
	list, err := l.client.Resource(longhornVolumeGVR()).
		Namespace(longhornNamespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list longhorn volumes: %w", err)
	}

	var unhealthy []string

	for i := range list.Items {
		robustness, _, _ := unstructured.NestedString(list.Items[i].Object, "status", "robustness")
		if isUnhealthyRobustness(robustness) {
			unhealthy = append(unhealthy,
				list.Items[i].GetNamespace()+"/"+list.Items[i].GetName())
		}
	}

	slices.Sort(unhealthy)

	return unhealthy, nil
}
