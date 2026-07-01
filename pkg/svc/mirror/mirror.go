package mirror

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ErrDeploymentNameEmpty is returned when ResolveTarget is called with an empty
// deployment name.
var ErrDeploymentNameEmpty = errors.New("deployment name is empty")

// ErrDeploymentNotFound is returned when the named Deployment does not exist in
// the namespace.
var ErrDeploymentNotFound = errors.New("deployment not found")

// ErrDeploymentNoContainers is returned when the Deployment's pod template
// declares no containers, so there is nothing to mirror.
var ErrDeploymentNoContainers = errors.New("deployment pod template has no containers")

// ErrNoRunningPods is returned when the Deployment has no Running pods, so there
// is no live pod for a tap to attach to.
var ErrNoRunningPods = errors.New("deployment has no running pods")

// Target describes a resolved cluster workload that a local process can mirror
// (and, in later phases, intercept) traffic for. It is the stable contract every
// bridge phase builds on, independent of the eventual traffic-tap mechanism: a
// tap attaches to one of Pods, copying traffic for one of Containers back to the
// developer's machine.
type Target struct {
	// Namespace is the namespace the Deployment lives in.
	Namespace string
	// Deployment is the name of the resolved Deployment.
	Deployment string
	// Pods are the names of the Running pods backing the Deployment, in the order
	// the API returned them. Always non-empty for a successfully resolved Target.
	Pods []string
	// Containers are the container names declared in the Deployment's pod
	// template, in declaration order. Always non-empty for a successfully
	// resolved Target; a caller selects one (e.g. via a --container flag) when
	// there is more than one.
	Containers []string
}

// ResolveTarget resolves a Deployment to the Running pods and containers a
// traffic tap would attach to. It is the Phase 0 foundation of the
// `ksail workload mirror` bridge (see the package doc): pure client-go, so it is
// fully unit-testable against a fake clientset and shared by every later phase.
//
// It returns ErrDeploymentNameEmpty for an empty name, a wrapped
// ErrDeploymentNotFound when the Deployment is absent, ErrDeploymentNoContainers
// when its pod template declares no containers, and ErrNoRunningPods when no
// backing pod is Running. The client parameter is an interface so callers can
// inject a fake clientset in tests.
func ResolveTarget(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	deployment string,
) (*Target, error) {
	if deployment == "" {
		return nil, ErrDeploymentNameEmpty
	}

	dep, err := getDeployment(ctx, client, namespace, deployment)
	if err != nil {
		return nil, err
	}

	containers := containerNames(dep)
	if len(containers) == 0 {
		return nil, fmt.Errorf("%w: %q in %s", ErrDeploymentNoContainers, deployment, namespace)
	}

	runningPods, err := listRunningPods(ctx, client, namespace, dep)
	if err != nil {
		return nil, err
	}

	if len(runningPods) == 0 {
		return nil, fmt.Errorf("%w: %q in %s", ErrNoRunningPods, deployment, namespace)
	}

	return &Target{
		Namespace:  namespace,
		Deployment: deployment,
		Pods:       runningPods,
		Containers: containers,
	}, nil
}

// getDeployment fetches the named Deployment, translating an API not-found into
// the package's ErrDeploymentNotFound sentinel.
func getDeployment(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, deployment string,
) (*appsv1.Deployment, error) {
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %q in %s", ErrDeploymentNotFound, deployment, namespace)
		}

		return nil, fmt.Errorf("getting deployment %q in %s: %w", deployment, namespace, err)
	}

	return dep, nil
}

// containerNames returns the Deployment pod template's container names in
// declaration order.
func containerNames(dep *appsv1.Deployment) []string {
	names := make([]string, 0, len(dep.Spec.Template.Spec.Containers))
	for index := range dep.Spec.Template.Spec.Containers {
		names = append(names, dep.Spec.Template.Spec.Containers[index].Name)
	}

	return names
}

// listRunningPods lists the Running pods backing the Deployment, selected by the
// Deployment's own label selector.
func listRunningPods(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	dep *appsv1.Deployment,
) ([]string, error) {
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("building pod selector for deployment %q: %w", dep.Name, err)
	}

	podList, err := client.CoreV1().Pods(namespace).List(
		ctx,
		metav1.ListOptions{LabelSelector: selector.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("listing pods for deployment %q in %s: %w", dep.Name, namespace, err)
	}

	running := make([]string, 0, len(podList.Items))
	for index := range podList.Items {
		if podList.Items[index].Status.Phase == corev1.PodRunning {
			running = append(running, podList.Items[index].Name)
		}
	}

	return running, nil
}
