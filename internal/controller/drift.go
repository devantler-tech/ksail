package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// reconcileDrift detects configuration drift against the last-applied spec and applies an
// in-place update when the provisioner supports it. Provisioners that do not implement Updater
// (or clusters without a recorded baseline) are reconciled to a baseline without changes.
func (r *ClusterReconciler) reconcileDrift(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	provisioner clusterprovisioner.Provisioner,
) error {
	updater, ok := provisioner.(clusterprovisioner.Updater)
	if !ok {
		return nil
	}

	oldSpec, hasBaseline := lastAppliedSpec(cluster)
	if !hasBaseline {
		// No baseline yet (e.g. a cluster adopted by the operator): record the current spec.
		return r.recordAppliedSpec(ctx, cluster)
	}

	newSpec := cluster.Spec.Cluster.DeepCopy()

	diff, err := updater.DiffConfig(ctx, ProvisionedName(cluster), oldSpec, newSpec)
	if err != nil {
		return fmt.Errorf("diff cluster config: %w", err)
	}

	if diff.TotalChanges() == 0 {
		return nil
	}

	r.markProgressing(
		cluster,
		v1alpha1.ClusterPhaseUpdating,
		"Updating",
		"Applying configuration changes",
	)
	_ = r.updateStatus(ctx, cluster)

	_, err = updater.Update(
		ctx,
		ProvisionedName(cluster),
		oldSpec,
		newSpec,
		// Operator reconciles are non-interactive automation: authorize both
		// partition wipes (Force) and rolling node replacement.
		clusterupdate.UpdateOptions{Force: true, AllowRollingRecreate: true},
	)
	if err != nil {
		return fmt.Errorf("update cluster: %w", err)
	}

	return r.recordAppliedSpec(ctx, cluster)
}

// lastAppliedSpec returns the cluster spec the operator last provisioned, parsed from the
// last-applied annotation. The second return is false when no valid baseline is recorded.
func lastAppliedSpec(cluster *v1alpha1.Cluster) (*v1alpha1.ClusterSpec, bool) {
	raw, ok := cluster.Annotations[LastAppliedSpecAnnotation]
	if !ok || raw == "" {
		return nil, false
	}

	var spec v1alpha1.ClusterSpec

	err := json.Unmarshal([]byte(raw), &spec)
	if err != nil {
		return nil, false
	}

	return &spec, true
}

// recordAppliedSpec stores the current cluster spec as the drift-detection baseline.
func (r *ClusterReconciler) recordAppliedSpec(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) error {
	data, err := json.Marshal(cluster.Spec.Cluster)
	if err != nil {
		return fmt.Errorf("marshal cluster spec: %w", err)
	}

	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}

	cluster.Annotations[LastAppliedSpecAnnotation] = string(data)

	updateErr := r.Update(ctx, cluster)
	if updateErr != nil {
		return fmt.Errorf("record last-applied spec: %w", updateErr)
	}

	return nil
}

// recordAppliedComponents stores the full cluster spec as the baseline for component-removal
// detection, so a later spec change that drops a component (e.g. policyEngine Kyverno→None) can be
// detected and the component uninstalled. It persists the whole Spec (the installer factory reads
// cluster, workload, and provider fields), under a distinct annotation from recordAppliedSpec so it
// is owned by the component reconciler and decoupled from drift-detection timing.
//
// Unlike recordAppliedSpec it runs after runtime status has been observed this reconcile, so it must
// not clobber the pending status. A plain Update would overwrite cluster.Status with the server's
// stored status (the status subresource ignores the write but the response is decoded back into the
// object). It therefore writes through a deep copy and propagates only the new metadata (annotations
// + resourceVersion) back to cluster, leaving the conditions and observed status this reconcile set
// intact for the single status update at the end of reconcileNormal. It is a no-op when the baseline
// already matches, avoiding a needless write + watch event.
func (r *ClusterReconciler) recordAppliedComponents(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) error {
	data, err := json.Marshal(cluster.Spec)
	if err != nil {
		return fmt.Errorf("marshal cluster spec for components baseline: %w", err)
	}

	if cluster.Annotations[v1alpha1.LastAppliedComponentsAnnotation] == string(data) {
		return nil
	}

	updated := cluster.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}

	updated.Annotations[v1alpha1.LastAppliedComponentsAnnotation] = string(data)

	updateErr := r.Update(ctx, updated)
	if updateErr != nil {
		return fmt.Errorf("record last-applied components: %w", updateErr)
	}

	cluster.Annotations = updated.Annotations
	cluster.ResourceVersion = updated.ResourceVersion

	return nil
}

// recordComponentOwnershipProgress persists only a controller release-identity change made before a
// later component failed. The full component baseline deliberately remains unchanged so the next
// reconcile retries the failed pass, while the narrow ownership marker survives the status update.
func (r *ClusterReconciler) recordComponentOwnershipProgress(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	previousAnnotations map[string]string,
) error {
	previousValue, previousPresent := previousAnnotations[v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation]
	currentValue, currentPresent := cluster.Annotations[v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation]

	if currentPresent == previousPresent && currentValue == previousValue {
		return nil
	}

	updated := cluster.DeepCopy()

	updated.Annotations = maps.Clone(previousAnnotations)
	if currentPresent {
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}

		updated.Annotations[v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation] = currentValue
	} else {
		delete(updated.Annotations, v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation)
	}

	updateErr := r.Update(ctx, updated)
	if updateErr != nil {
		return fmt.Errorf("record partial component ownership: %w", updateErr)
	}

	cluster.Annotations = updated.Annotations
	cluster.ResourceVersion = updated.ResourceVersion

	return nil
}
