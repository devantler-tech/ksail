package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	// DinDDockerPort is the port the Docker daemon listens on (no TLS).
	DinDDockerPort = 2375

	// DinDAPIServerPort is the port the nested API server is exposed on (via Docker port mapping).
	DinDAPIServerPort = 6443

	// dindReadyPollInterval is the interval between Docker daemon readiness checks.
	dindReadyPollInterval = 2 * time.Second

	// dindReadyTimeout is the maximum time to wait for the Docker daemon.
	dindReadyTimeout = 120 * time.Second

	// LabelApp is the label key for pod selection by services.
	LabelApp = "app"
)

// CreateDinDPod creates a Docker-in-Docker pod and service in the cluster's namespace.
// The DinD pod runs a privileged Docker daemon that distributions like Kind
// use as their container runtime.
func (p *Provider) CreateDinDPod(ctx context.Context, clusterName, distribution string) error {
	ns := NamespaceName(clusterName)

	pod := buildDinDPod(clusterName, distribution)
	svc := buildDinDService(clusterName)

	_, err := p.client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	_, err = p.client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create DinD service: %w", err)
	}

	return nil
}

// WaitForDinD waits until the Docker daemon inside the DinD pod is ready to accept connections.
func (p *Provider) WaitForDinD(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	// Wait for pod to be Running first
	deadline := time.Now().Add(dindReadyTimeout)

	for time.Now().Before(deadline) {
		pod, err := p.client.CoreV1().Pods(ns).Get(ctx, DinDPodName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get DinD pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning {
			// Check if container is ready
			for i := range pod.Status.ContainerStatuses {
				if pod.Status.ContainerStatuses[i].Name == DinDContainerName &&
					pod.Status.ContainerStatuses[i].Ready {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for DinD: %w", ctx.Err())
		case <-time.After(dindReadyPollInterval):
		}
	}

	return fmt.Errorf("DinD pod did not become ready within %v", dindReadyTimeout)
}

// GetDockerDaemonEndpoint returns the in-cluster Docker daemon endpoint for the DinD pod.
// The returned value is suitable for use as DOCKER_HOST.
func (p *Provider) GetDockerDaemonEndpoint(clusterName string) string {
	ns := NamespaceName(clusterName)

	return fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", DinDServiceName, ns, DinDDockerPort)
}

// GetDockerDaemonPodEndpoint returns the Docker daemon endpoint using the pod IP.
// This is used when connecting from outside the cluster (via port-forward or NodePort).
func (p *Provider) GetDockerDaemonPodEndpoint(ctx context.Context, clusterName string) (string, error) {
	ns := NamespaceName(clusterName)

	pod, err := p.client.CoreV1().Pods(ns).Get(ctx, DinDPodName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get DinD pod: %w", err)
	}

	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("DinD pod has no IP assigned")
	}

	return fmt.Sprintf("tcp://%s:%d", pod.Status.PodIP, DinDDockerPort), nil
}

// DeleteDinD removes the DinD pod and service.
// These are also removed when the namespace is deleted, but this allows targeted cleanup.
func (p *Provider) DeleteDinD(ctx context.Context, clusterName string) error {
	ns := NamespaceName(clusterName)

	err := p.client.CoreV1().Pods(ns).Delete(ctx, DinDPodName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete DinD pod: %w", err)
	}

	err = p.client.CoreV1().Services(ns).Delete(ctx, DinDServiceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete DinD service: %w", err)
	}

	return nil
}

func buildDinDPod(clusterName, distribution string) *corev1.Pod {
	privileged := true
	labels := NodeLabels(clusterName, RoleControlPlane, distribution)
	labels[LabelApp] = DinDPodName

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   DinDPodName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  DinDContainerName,
					Image: DinDImage,
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
						InitialDelaySeconds: 5,
						PeriodSeconds:       2,
						FailureThreshold:    30,
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "docker",
							ContainerPort: DinDDockerPort,
						},
						{
							Name:          "apiserver",
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
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "docker-data",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "modules",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/lib/modules",
						},
					},
				},
			},
		},
	}
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
					Name:       "docker",
					Port:       DinDDockerPort,
					TargetPort: intstr.FromInt32(DinDDockerPort),
				},
				{
					Name:       "apiserver",
					Port:       DinDAPIServerPort,
					TargetPort: intstr.FromInt32(DinDAPIServerPort),
				},
			},
		},
	}
}
