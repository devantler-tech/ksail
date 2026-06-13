package controller

import (
	"context"
	"encoding/json"
	"fmt"

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
