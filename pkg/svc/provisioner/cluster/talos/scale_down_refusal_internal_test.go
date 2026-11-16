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
				nil,
			)
			require.Error(
				t,
				err,
				"automatic removal must not begin evicting or ignoring application pods",
			)
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
		nil,
	)
	require.Error(t, err)
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
			require.Error(t, err)
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
