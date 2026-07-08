package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// DinDImage is the Docker-in-Docker container image.
	DinDImage = "docker:28-dind"

	// DinDContainerName is the name of the DinD container in the pod.
	DinDContainerName = "dind"

	// DinDPodName is the name of the DinD pod.
	DinDPodName = "dind"

	// DinDServiceName is the name of the DinD ClusterIP service.
	DinDServiceName = "dind"

	// DinDDockerPort is the port the Docker daemon listens on inside the DinD pod.
	// Used for port-forwarding to the pod; not exposed via the ClusterIP Service.
	DinDDockerPort = 2375

	// DinDAPIServerPort is the port the nested API server is exposed on (via Docker port mapping).
	DinDAPIServerPort = 6443

	// dindMTU is the MTU for the DinD Docker daemon's bridge. The DinD pod sits behind
	// the host cluster's pod network (and, on CI, possibly a tunnel/NAT), so the path
	// MTU to the internet can be below the default 1500. With 1500, large packets (e.g.
	// the TLS handshake of a pull-through mirror reaching an upstream registry) are
	// silently dropped — PMTU discovery is typically blackholed — producing "TLS
	// handshake timeout". A conservative 1400 keeps egress packets within the path MTU.
	dindMTU = 1400

	// DinDPVCName is the name of the PVC for the Docker data directory.
	DinDPVCName = "docker-data"

	// defaultPVCSize is the default PVC size when persistence is enabled but no size is configured.
	defaultPVCSize = "20Gi"

	// dindReadyPollInterval is the interval between Docker daemon readiness checks.
	dindReadyPollInterval = 2 * time.Second

	// dindReadyTimeout is the maximum time to wait for the Docker daemon.
	dindReadyTimeout = 120 * time.Second

	// LabelApp is the label key for pod selection by services.
	LabelApp = "app"

	// probeInitialDelaySeconds is the readiness probe initial delay.
	probeInitialDelaySeconds = 5
	// probePeriodSeconds is the readiness probe period.
	probePeriodSeconds = 2
	// probeFailureThreshold is the readiness probe failure threshold.
	probeFailureThreshold = 30
)

// CreateDinDPod creates a Docker-in-Docker pod and service in the cluster's namespace.
// The DinD pod runs a privileged Docker daemon that distributions like Kind
// use as their container runtime. If persistence.Enabled is true, a PVC is
// created for the Docker data directory so the daemon state survives pod restarts.
func (p *Provider) CreateDinDPod(
	ctx context.Context,
	clusterName, distribution string,
	persistence v1alpha1.KubernetesPersistence,
) error {
	namespace := NamespaceName(clusterName)

	if persistence.Enabled {
		err := p.ensurePVC(ctx, namespace, clusterName, persistence)
		if err != nil {
			return fmt.Errorf("ensure PVC: %w", err)
		}
	}

	pod := buildDinDPod(clusterName, distribution, persistence)
	svc := buildDinDService(clusterName)

	_, err := p.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	_, err = p.client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create DinD service: %w", err)
	}

	return nil
}

// WaitForDefaultServiceAccount waits until the namespace's default ServiceAccount exists.
// Kubernetes' ServiceAccount controller provisions it asynchronously after the namespace is
// created; creating a pod (which references the default SA) before it exists fails with
// "error looking up service account <ns>/default: serviceaccount default not found". The lag
// is most pronounced right after a cluster restart, when the controller is still reconciling.
func (p *Provider) WaitForDefaultServiceAccount(ctx context.Context, clusterName string) error {
	namespace := NamespaceName(clusterName)

	var condErr error

	err := pollDinDReady(ctx, func(ctx context.Context) (bool, error) {
		_, getErr := p.client.CoreV1().
			ServiceAccounts(namespace).
			Get(ctx, "default", metav1.GetOptions{})
		if getErr == nil {
			return true, nil
		}

		// NotFound is the not-ready condition: the SA controller is still
		// provisioning it. Any other error is fatal.
		if !errors.IsNotFound(getErr) {
			condErr = fmt.Errorf("get default service account in %s: %w", namespace, getErr)

			return false, condErr
		}

		return false, nil
	})
	if err == nil {
		return nil
	}

	return mapWaitError(
		err,
		condErr,
		"waiting for default service account",
		fmt.Errorf("%w: %s/default", ErrDefaultServiceAccountNotReady, namespace),
	)
}

// WaitForDinD waits until the Docker daemon inside the DinD pod is ready to accept connections.
func (p *Provider) WaitForDinD(ctx context.Context, clusterName string) error {
	namespace := NamespaceName(clusterName)

	var condErr error

	err := pollDinDReady(ctx, func(ctx context.Context) (bool, error) {
		pod, getErr := p.client.CoreV1().
			Pods(namespace).
			Get(ctx, DinDPodName, metav1.GetOptions{})
		if getErr != nil {
			condErr = fmt.Errorf("get DinD pod: %w", getErr)

			return false, condErr
		}

		return dindContainerReady(pod), nil
	})
	if err == nil {
		return nil
	}

	return mapWaitError(
		err,
		condErr,
		"waiting for DinD",
		fmt.Errorf("%w: %v", ErrDinDNotReady, dindReadyTimeout),
	)
}

// pollDinDReady runs check on the DinD readiness poll/timeout cadence shared by
// WaitForDefaultServiceAccount and WaitForDinD — the two callers differ only in what they check
// each tick and how they classify a fatal condition error.
func pollDinDReady(ctx context.Context, check wait.ConditionWithContextFunc) error {
	return wait.PollUntilContextTimeout( //nolint:wrapcheck // callers pass the raw error to mapWaitError
		ctx,
		dindReadyPollInterval,
		dindReadyTimeout,
		true, // check before the first wait, matching the legacy deadline loop.
		check,
	)
}

// dindContainerReady reports whether the DinD pod is Running and its DinD
// container has reported Ready.
func dindContainerReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == DinDContainerName &&
			pod.Status.ContainerStatuses[i].Ready {
			return true
		}
	}

	return false
}

// DeleteDinD removes the DinD pod and service.
// These are also removed when the namespace is deleted, but this allows targeted cleanup.
func (p *Provider) DeleteDinD(ctx context.Context, clusterName string) error {
	namespace := NamespaceName(clusterName)

	err := p.client.CoreV1().Pods(namespace).Delete(ctx, DinDPodName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete DinD pod: %w", err)
	}

	err = p.client.CoreV1().Services(namespace).Delete(ctx, DinDServiceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete DinD service: %w", err)
	}

	return nil
}

func buildDinDPod(
	clusterName, distribution string,
	persistence v1alpha1.KubernetesPersistence,
) *corev1.Pod {
	labels := NodeLabels(clusterName, RoleControlPlane, distribution)
	labels[LabelApp] = DinDPodName

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   DinDPodName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{buildDinDContainer()},
			Volumes:    buildDinDVolumes(persistence),
		},
	}
}

func buildDinDContainer() corev1.Container {
	privileged := true

	return corev1.Container{
		Name:  DinDContainerName,
		Image: DinDImage,
		// Forwarded to dockerd by the dind entrypoint. Lower the bridge MTU so egress
		// (e.g. pull-through mirrors reaching upstream registries) fits the DinD pod's
		// path MTU instead of stalling on dropped TLS-handshake packets.
		Args: []string{fmt.Sprintf("--mtu=%d", dindMTU)},
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
		},
		Env: []corev1.EnvVar{
			{
				Name:  "DOCKER_TLS_CERTDIR",
				Value: "",
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(DinDDockerPort),
				},
			},
			InitialDelaySeconds: probeInitialDelaySeconds,
			PeriodSeconds:       probePeriodSeconds,
			FailureThreshold:    probeFailureThreshold,
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "docker",
				ContainerPort: DinDDockerPort,
			},
			{
				Name:          APIServiceName,
				ContainerPort: DinDAPIServerPort,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "docker-data",
				MountPath: "/var/lib/docker",
			},
			{
				Name:      "modules",
				MountPath: "/lib/modules",
				ReadOnly:  true,
			},
		},
	}
}

func buildDinDVolumes(persistence v1alpha1.KubernetesPersistence) []corev1.Volume {
	var dockerDataSource corev1.VolumeSource
	if persistence.Enabled {
		dockerDataSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: DinDPVCName,
			},
		}
	} else {
		dockerDataSource = corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}

	return []corev1.Volume{
		{
			Name:         "docker-data",
			VolumeSource: dockerDataSource,
		},
		{
			Name: "modules",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/lib/modules",
				},
			},
		},
	}
}

// ensurePVC creates a PVC for the DinD Docker data directory if it does not already exist.
func (p *Provider) ensurePVC(
	ctx context.Context,
	namespace, clusterName string,
	persistence v1alpha1.KubernetesPersistence,
) error {
	size := persistence.Size
	if size == "" {
		size = defaultPVCSize
	}

	storageRequest, err := resource.ParseQuantity(size)
	if err != nil {
		return fmt.Errorf("parse PVC size %q: %w", size, err)
	}

	labels := CommonLabels(clusterName)
	storageClassName := persistence.StorageClassName

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   DinDPVCName,
			Labels: labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageRequest,
				},
			},
		},
	}

	if storageClassName != "" {
		pvc.Spec.StorageClassName = &storageClassName
	}

	_, err = p.client.CoreV1().
		PersistentVolumeClaims(namespace).
		Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create PVC: %w", err)
	}

	return nil
}

func buildDinDService(clusterName string) *corev1.Service {
	labels := CommonLabels(clusterName)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   DinDServiceName,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				LabelApp: DinDPodName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       APIServiceName,
					Port:       DinDAPIServerPort,
					TargetPort: intstr.FromInt32(DinDAPIServerPort),
				},
			},
		},
	}
}
