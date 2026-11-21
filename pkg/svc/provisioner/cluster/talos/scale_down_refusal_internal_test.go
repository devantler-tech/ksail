package talosprovisioner

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func removalApplicationPod() corev1.Pod {
	controller := true

	return corev1.Pod{
		Name:      "application",
		Namespace: "default",
		UID:       "app-original",
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "application-rs",
				UID:        "replicaset",
				Controller: &controller,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "fixture-control-plane-1",
			Containers: []corev1.Container{
				{Name: "application", Image: "example.invalid/application:fixture"},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestControlPlaneRemovalRequiresNoApplicationPods(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"running", "already terminating", "completed with local data"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)
			pod := removalApplicationPod()

			if state == "already terminating" {
				stamp := metav1.NewTime(time.Now().Add(-2 * time.Minute))
				pod.DeletionTimestamp = &stamp
			}

			if state == "completed with local data" {
				pod.Status.Phase = corev1.PodSucceeded
				pod.Spec.Volumes = []corev1.Volume{
					{
						Name:     "data",
						HostPath: &corev1.HostPathVolumeSource{Path: "/application-data"},
					},
				}
			}

			fixture.pods = []corev1.Pod{pod}
			clientset, err := fixture.provisioner.createK8sClient("fixture")
			require.NoError(t, err)
			err = fixture.provisioner.proveRemovalWorkloads(
				t.Context(),
				clientset,
				replacementTarget{ServerName: pod.Spec.NodeName, NodeUID: "native-1"},
				[]corev1.Node{*fixture.nodes[pod.Spec.NodeName]},
			)
			require.ErrorIs(
				t,
				err,
				ErrControlPlaneScaleDownUnsafe,
				"automatic removal must not begin evicting or ignoring application pods",
			)
			require.ErrorContains(t, err, "application pods must be absent")
			require.Empty(t, fixture.apiWrites)
		})
	}
}

func TestControlPlaneRemovalRefusesUnknownSystemHostPath(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	pod := removalApplicationPod()
	pod.OwnerReferences[0].Kind = "DaemonSet"
	pod.Spec.Volumes = []corev1.Volume{
		{Name: "data", HostPath: &corev1.HostPathVolumeSource{Path: "/application-data"}},
	}
	fixture.pods = []corev1.Pod{pod}
	clientset, err := fixture.provisioner.createK8sClient("fixture")
	require.NoError(t, err)
	err = fixture.provisioner.proveRemovalWorkloads(
		t.Context(),
		clientset,
		replacementTarget{ServerName: pod.Spec.NodeName, NodeUID: "native-1"},
		[]corev1.Node{*fixture.nodes[pod.Spec.NodeName]},
	)
	require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
	require.ErrorContains(t, err, "unqualified node-local hostPath")
	require.Empty(t, fixture.apiWrites)
}

func TestControlPlaneScaleDownRebindsFinalProviderResponse(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"role changed", "floating owner changed", "wrong immutable response ID", "provider volume attached",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)

			switch scenario {
			case "provider volume attached":
				fixture.lateProviderVolume = true
			case "role changed":
				fixture.lateRoleChange = true
			case "floating owner changed":
				fixture.lateFloatingOwner = true
			case "wrong immutable response ID":
				fixture.wrongResponseID = true
			}

			err := fixture.provisioner.removeHetznerNodes(
				t.Context(),
				"fixture",
				RoleControlPlane,
				2,
				clusterupdate.NewEmptyUpdateResult(),
			)
			if scenario == "wrong immutable response ID" {
				require.ErrorContains(t, err, "does not match requested immutable identity")
			} else {
				require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
			}

			require.Equal(
				t,
				[]int64{1},
				fixture.leaves,
				"the scenario must reach the final provider lookup after membership removal",
			)
			require.Empty(
				t,
				fixture.deleted,
				"contradictory final identity/route must retain every server",
			)
		})
	}
}

func TestControlPlaneScaleDownCordonIsBoundToNativeUID(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.cordonReplacesNode = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Empty(t, fixture.leaves)
	require.Empty(t, fixture.deleted)
	require.False(
		t,
		fixture.nodes["fixture-control-plane-1"].Spec.Unschedulable,
		"the replacement UID must not inherit the old node's cordon",
	)
}

func TestControlPlaneScaleDownAllowsOnlyRebuildableSystemPods(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	agent := removalApplicationPod()
	agent.Namespace = metav1.NamespaceSystem
	agent.Name = "kernel-agent"
	agent.OwnerReferences[0].Kind = "DaemonSet"
	agent.Spec.Volumes = []corev1.Volume{
		{Name: "bpf", HostPath: &corev1.HostPathVolumeSource{Path: "/sys/fs/bpf"}},
	}
	mirror := agent.DeepCopy()
	mirror.Name = "static-control-plane"
	mirror.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "mirror-hash"}
	mirror.OwnerReferences[0].Kind = "Node"
	mirror.OwnerReferences[0].Name = mirror.Spec.NodeName
	mirror.OwnerReferences[0].UID = "native-1"
	mirror.Spec.Volumes = []corev1.Volume{
		{
			Name: "config",
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/system/config/kubernetes/kube-apiserver",
			},
		},
	}
	reader := agent.DeepCopy()
	reader.Namespace = "observability"
	reader.Name = "system-log-reader"
	reader.Spec.Volumes = []corev1.Volume{
		{Name: "logs", HostPath: &corev1.HostPathVolumeSource{Path: "/var/log/audit/kube"}},
	}
	reader.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: "logs", MountPath: "/logs", ReadOnly: true},
	}
	fixture.pods = []corev1.Pod{agent, *mirror, *reader}
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, fixture.deleted)

	for _, write := range fixture.apiWrites {
		require.NotContains(t, write, "/pods/", "removal never evicts/deletes even system pods")
	}
}

func TestControlPlaneRemovalRefusesProviderVolume(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.servers[1].Volumes = []*hcloud.Volume{{ID: 71}}
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(), "fixture", RoleControlPlane, 2, clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Empty(t, fixture.leaves)
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneRemovalRechecksProviderStorageBeforeMembership(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.attachVolumeOnRefresh = true

	err := fixture.provisioner.removeHetznerNodes(
		t.Context(), "fixture", RoleControlPlane, 2, clusterupdate.NewEmptyUpdateResult(),
	)

	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	require.Error(t, err)
	require.Empty(t, fixture.leaves, "fresh provider storage must stop before a membership RPC")
	require.Empty(t, fixture.deleted)
}

func runRemovalScaleDown(fixture *removalFixture) error {
	return fixture.provisioner.removeHetznerNodes(
		fixture.t.Context(), "fixture", RoleControlPlane, 2, clusterupdate.NewEmptyUpdateResult(),
	)
}

func TestControlPlaneRemovalRefusesDeleteProtectedTarget(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.servers[1].Protection.Delete = true

	err := runRemovalScaleDown(fixture)
	require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
	require.ErrorContains(t, err, "delete protection")
	require.Empty(t, fixture.leaves, "a protected target must stop before any membership change")
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneRemovalAllowsStorageOnRetainedServer(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.servers[fixture.floatingHolder].Volumes = []*hcloud.Volume{{ID: 71}}
	fixture.servers[fixture.floatingHolder].Protection.Delete = true

	err := runRemovalScaleDown(fixture)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, fixture.deleted)
}

func TestControlPlaneRemovalRefusesUnprovenPersistentVolumeLocality(t *testing.T) {
	t.Parallel()

	for scenario, prepare := range map[string]func(*removalFixture) corev1.PersistentVolume{
		"hostname label differs from node name": func(fixture *removalFixture) corev1.PersistentVolume {
			fixture.nodes["fixture-control-plane-1"].Labels[corev1.LabelHostname] = "rack-a-01"

			return removalLocalVolume("rack-a-01")
		},
		"unrecognized CSI driver with topology-only affinity": func(*removalFixture) corev1.PersistentVolume {
			volume := removalLocalVolume("unused")
			volume.Spec.Local = nil
			volume.Spec.CSI = &corev1.CSIPersistentVolumeSource{
				Driver:       "zfs.csi.openebs.io",
				VolumeHandle: "data",
			}
			volume.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0] = corev1.NodeSelectorRequirement{
				Key:      "openebs.io/nodename",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"fixture-control-plane-1"},
			}

			return volume
		},
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)
			fixture.volumes = []corev1.PersistentVolume{prepare(fixture)}

			err := runRemovalScaleDown(fixture)
			require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
			require.ErrorContains(t, err, "may keep data on the target")
			require.Empty(t, fixture.leaves)
			require.Empty(t, fixture.deleted)
		})
	}
}

func TestControlPlaneRemovalRefusesWritableSystemLogPath(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	pod := removalApplicationPod()
	pod.Namespace = metav1.NamespaceSystem
	pod.OwnerReferences[0].Kind = "DaemonSet"
	pod.Spec.Volumes = []corev1.Volume{
		{Name: "state", HostPath: &corev1.HostPathVolumeSource{Path: "/var/log/app-state"}},
	}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: "state", MountPath: "/state"},
	}
	fixture.pods = []corev1.Pod{pod}

	err := runRemovalScaleDown(fixture)
	require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
	require.ErrorContains(t, err, "unqualified node-local hostPath")
	require.Empty(t, fixture.leaves)
	require.Empty(t, fixture.deleted)
}
