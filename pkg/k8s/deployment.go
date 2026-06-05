package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/retry"
)

// RolloutRestartAnnotation is the pod-template annotation that triggers a rolling
// restart, matching the key `kubectl rollout restart` stamps. Changing its value
// mutates the pod template, so the Deployment controller recreates the pods — the
// only way pods that source environment variables from a Secret or ConfigMap pick
// up data changes, since Kubernetes does not live-reload env vars.
const RolloutRestartAnnotation = "kubectl.kubernetes.io/restartedAt"

// RolloutRestartDeploymentsByLabel triggers a rolling restart of every Deployment
// in namespace whose labels match labelSelector, the same way
// `kubectl rollout restart deployment -l <selector>` does: it stamps each pod
// template's RolloutRestartAnnotation with the current time.
//
// Callers that rewrite a Secret or ConfigMap consumed via env-var valueFrom must
// call this for the change to reach the running pods. A selector that matches no
// Deployment is not an error (returns 0) — the workload may not be installed yet.
// Returns the number of Deployments restarted.
func RolloutRestartDeploymentsByLabel(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	labelSelector string,
) (int, error) {
	deploymentsClient := client.AppsV1().Deployments(namespace)

	list, err := deploymentsClient.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return 0, fmt.Errorf("listing deployments %q in %s: %w", labelSelector, namespace, err)
	}

	timestamp := time.Now().Format(time.RFC3339)
	restarted := 0

	for index := range list.Items {
		err = restartDeployment(ctx, deploymentsClient, list.Items[index].Name, timestamp)
		if err != nil {
			return restarted, err
		}

		restarted++
	}

	return restarted, nil
}

// restartDeployment stamps a single Deployment's pod-template restart annotation,
// retrying on optimistic-concurrency conflicts.
func restartDeployment(
	ctx context.Context,
	deploymentsClient appsv1client.DeploymentInterface,
	name, timestamp string,
) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("getting deployment %s: %w", name, getErr)
		}

		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}

		deployment.Spec.Template.Annotations[RolloutRestartAnnotation] = timestamp

		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("restarting deployment %s: %w", name, updateErr)
		}

		return nil
	})
	if retryErr != nil {
		return fmt.Errorf("rolling restart of deployment %s: %w", name, retryErr)
	}

	return nil
}
