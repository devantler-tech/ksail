package talosprovisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

func (p *Provisioner) proveRemovalWorkloads(
	ctx context.Context,
	clientset kubernetes.Interface,
	target replacementTarget,
	nodes []corev1.Node,
) error {
	ctx, cancel := context.WithTimeout(ctx, removalReadTimeout)
	defer cancel()

	err := proveNoRemovalNodeVolumes(nodes, target)
	if err != nil {
		return err
	}

	pods, err := clientset.CoreV1().
		Pods("").
		List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + target.ServerName})
	if err != nil {
		return fmt.Errorf("read removal workloads: %w", err)
	}

	if pods.ResourceVersion == "" {
		return fmt.Errorf(
			"%w: workload inventory has no resource version",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	for _, pod := range pods.Items {
		err = proveRemovalPod(&pod, target)
		if err != nil {
			return err
		}
	}

	err = proveNoRemovalStorageNode(ctx, clientset, target.ServerName)
	if err != nil {
		return err
	}

	return nil
}

func proveNoRemovalNodeVolumes(nodes []corev1.Node, target replacementTarget) error {
	for _, node := range nodes {
		if node.UID == target.NodeUID &&
			(len(node.Status.VolumesAttached) > 0 || len(node.Status.VolumesInUse) > 0) {
			return fmt.Errorf(
				"%w: target still uses storage volumes",
				ErrControlPlaneScaleDownUnsafe,
			)
		}
	}

	return nil
}

func removalNodeAgent(pod *corev1.Pod, target replacementTarget) bool {
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return false
	}

	if owner.Kind == "DaemonSet" {
		return true
	}

	return pod.Namespace == metav1.NamespaceSystem &&
		pod.Annotations[corev1.MirrorPodAnnotationKey] != "" &&
		owner.Kind == "Node" && owner.UID == target.NodeUID && owner.Name == target.ServerName
}

func proveRemovalPod(pod *corev1.Pod, target replacementTarget) error {
	if !removalNodeAgent(pod, target) {
		return fmt.Errorf(
			"%w: application pods must be absent before automatic removal, including terminating pods",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	for _, volume := range pod.Spec.Volumes {
		err := proveRemovalVolume(pod, volume)
		if err != nil {
			return err
		}
	}

	return nil
}

func proveRemovalVolume(pod *corev1.Pod, volume corev1.Volume) error {
	switch {
	case volume.HostPath != nil:
		if !removalSystemPath(pod, volume) {
			return fmt.Errorf(
				"%w: unqualified node-local hostPath on target",
				ErrControlPlaneScaleDownUnsafe,
			)
		}
	case volume.EmptyDir != nil, volume.Secret != nil, volume.ConfigMap != nil,
		volume.Projected != nil, volume.DownwardAPI != nil:
		// Only proven node-agent/static pods reach here. These sources are
		// rebuilt by Kubernetes and carry no application persistence claim.
	default:
		return fmt.Errorf(
			"%w: target hosts an unqualified storage volume",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

func removalSystemPath(pod *corev1.Pod, volume corev1.Volume) bool {
	clean := path.Clean(volume.HostPath.Path)

	within := func(prefix string) bool { return clean == prefix || strings.HasPrefix(clean, prefix+"/") }
	if slices.ContainsFunc([]string{"/proc", "/sys", "/run", "/var/run"}, within) {
		return true
	}

	if pod.Namespace == metav1.NamespaceSystem && slices.ContainsFunc(
		[]string{
			"/dev",
			"/opt/cni/bin",
			"/etc/cni/net.d",
			"/lib/modules",
			"/var/lib/kubelet",
			"/var/lib/cni",
			"/var/lib/cilium",
			"/system/config/kubernetes",
			"/system/secrets/kubernetes",
			"/var/etc/kubernetes",
			"/etc/kubernetes",
			"/var/log",
		},
		within,
	) {
		return true
	}

	if !within("/var/log") {
		return false
	}
	// A log forwarder may read system logs from another namespace. It may
	// not claim an arbitrary writable host directory as rebuildable state.
	mounts := []corev1.VolumeMount{}
	for _, container := range pod.Spec.Containers {
		mounts = append(mounts, container.VolumeMounts...)
	}

	for _, container := range pod.Spec.InitContainers {
		mounts = append(mounts, container.VolumeMounts...)
	}

	for _, container := range pod.Spec.EphemeralContainers {
		mounts = append(mounts, container.VolumeMounts...)
	}

	return removalReadOnlyMounts(mounts, volume.Name)
}

func removalReadOnlyMounts(mounts []corev1.VolumeMount, volumeName string) bool {
	mounted := false

	for _, mount := range mounts {
		if mount.Name != volumeName {
			continue
		}

		mounted = true

		if !mount.ReadOnly {
			return false
		}
	}

	return mounted
}

func proveNoRemovalStorageNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	// Discover the group before treating a missing resource as absence: a
	// present backend under an unsupported version is unknown, not clearance.
	groupBytes, err := clientset.CoreV1().RESTClient().Get().AbsPath("/apis/longhorn.io").DoRaw(ctx)
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("discover storage membership API: %w", err)
	}

	var group metav1.APIGroup

	err = json.Unmarshal(groupBytes, &group)
	if err != nil {
		return fmt.Errorf("decode storage membership API: %w", err)
	}

	if !slices.ContainsFunc(group.Versions, func(version metav1.GroupVersionForDiscovery) bool {
		return version.GroupVersion == "longhorn.io/v1beta2"
	}) {
		return fmt.Errorf(
			"%w: storage membership version is unproven",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return proveRemovalStorageList(ctx, clientset, nodeName)
}

func proveRemovalStorageList(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	raw, err := clientset.CoreV1().
		RESTClient().
		Get().
		AbsPath("/apis/longhorn.io/v1beta2/nodes").
		DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("read storage membership: %w", err)
	}

	var list struct {
		Kind  string `json:"kind"`
		Items []struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		} `json:"items"`
	}

	err = json.Unmarshal(raw, &list)
	if err != nil {
		return fmt.Errorf("decode storage membership: %w", err)
	}

	if list.Kind != "NodeList" || list.Items == nil {
		return fmt.Errorf(
			"%w: storage membership response is incomplete",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	for _, node := range list.Items {
		if node.Metadata.Name == nodeName {
			return fmt.Errorf(
				"%w: target is a storage backend member",
				ErrControlPlaneScaleDownUnsafe,
			)
		}
	}

	return nil
}

func (p *Provisioner) cordonEmptyControlPlane(
	ctx context.Context,
	clientset kubernetes.Interface,
	target replacementTarget,
) error {
	ctx, cancel := context.WithTimeout(ctx, removalReadTimeout)
	defer cancel()

	patch, err := json.Marshal(
		[]map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": target.NodeUID},
			{"op": "add", "path": "/spec/unschedulable", "value": true},
		},
	)
	if err != nil {
		return fmt.Errorf("encode native identity cordon: %w", err)
	}

	current, err := clientset.CoreV1().
		Nodes().
		Patch(ctx, target.ServerName, types.JSONPatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("cordon native identity: %w", err)
	}

	if current.UID != target.NodeUID || !current.Spec.Unschedulable {
		return fmt.Errorf(
			"%w: cordon did not bind the planned native identity",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}
