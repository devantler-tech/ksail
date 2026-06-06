package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
// template's RolloutRestartAnnotation with the current time via a strategic-merge
// patch. The patch merges the annotation into any existing pod-template
// annotations rather than replacing them, and is atomic — so, unlike a
// get-modify-update, it needs no optimistic-concurrency retry.
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

	patch, err := rolloutRestartPatch()
	if err != nil {
		return 0, err
	}

	restarted := 0

	for index := range list.Items {
		name := list.Items[index].Name

		_, err = deploymentsClient.Patch(
			ctx,
			name,
			types.StrategicMergePatchType,
			patch,
			metav1.PatchOptions{},
		)
		if err != nil {
			return restarted, fmt.Errorf("restarting deployment %s: %w", name, err)
		}

		restarted++
	}

	return restarted, nil
}

// rolloutRestartPatch builds the strategic-merge patch body that stamps the pod
// template's RolloutRestartAnnotation with the current time, the same mutation
// `kubectl rollout restart` applies.
func rolloutRestartPatch() ([]byte, error) {
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						RolloutRestartAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
		},
	}

	data, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("building rollout-restart patch: %w", err)
	}

	return data, nil
}
