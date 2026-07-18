package cluster

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
)

// deleteTimeout is the maximum duration for the auto-delete operation.
const deleteTimeout = 10 * time.Minute

// waitForTTLAndDelete blocks until the TTL duration elapses and then auto-deletes the cluster.
// The wait can be cancelled with SIGINT/SIGTERM, in which case the cluster is left running.
// This implements the ephemeral cluster pattern: after creation, the process stays alive
// and automatically tears down the cluster when the TTL expires.
func waitForTTLAndDelete(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
	ttl time.Duration,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"cluster will auto-destroy in %s (press Ctrl+C to cancel)", ttl)

	// Create a context that is cancelled on SIGINT/SIGTERM and also respects cmd.Context().
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	timer := time.NewTimer(ttl)
	defer timer.Stop()

	select {
	case <-timer.C:
		return autoDeleteCluster(cmd, clusterName, clusterCfg, eksConfig)
	case <-ctx.Done():
		notify.Infof(cmd.OutOrStdout(),
			"TTL wait cancelled; cluster %q will remain running", clusterName)

		return nil
	}
}

// autoDeleteCluster performs an automatic cluster deletion after TTL expiry.
// It creates a minimal provisioner based on distribution and provider info
// from the original cluster config and deletes the cluster.
func autoDeleteCluster(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) error {
	clusterName, err := ttlAutoDeleteTargetName(clusterName, clusterCfg, eksConfig)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: resolve target: %w", err)
	}

	notify.Infof(cmd.OutOrStdout(),
		"TTL expired; auto-destroying cluster %q...", clusterName)

	info, options, err := ttlDeleteProvisionerInputs(
		cmd.Context(), clusterName, clusterCfg, eksConfig,
	)
	if err != nil {
		return err
	}

	provisioner, err := createDeleteProvisioner(cmd.Context(), info, options)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: failed to create provisioner: %w", err)
	}

	deleteCtx, cancel := context.WithTimeout(cmd.Context(), deleteTimeout)
	defer cancel()

	err = lifecycle.VerifyAWSOwnershipBeforeMutation(
		deleteCtx,
		options.AWSOwnershipVerifier,
	)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: %w", err)
	}

	err = provisioner.Delete(deleteCtx, clusterName)
	if err != nil {
		return fmt.Errorf("TTL auto-delete failed: %w", err)
	}

	// Clean up persisted state (spec + TTL).
	// Best-effort: warn on failure rather than blocking success.
	stateErr := state.DeleteClusterState(clusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to clean up cluster state: %v", stateErr)
	}

	notify.Successf(cmd.OutOrStdout(),
		"cluster %q auto-destroyed after TTL expiry", clusterName)

	return nil
}

func ttlDeleteProvisionerInputs(
	ctx context.Context,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) (*clusterdetector.Info, lifecycle.MinimalProvisionerOptions, error) {
	info := &clusterdetector.Info{
		ClusterName:    clusterName,
		Distribution:   clusterCfg.Spec.Cluster.Distribution,
		Provider:       clusterCfg.Spec.Cluster.Provider,
		KubeconfigPath: clusterCfg.Spec.Cluster.Connection.Kubeconfig,
	}
	options := lifecycle.MinimalProvisionerOptions{
		OmniOpts:       clusterCfg.Spec.Provider.Omni,
		KubernetesOpts: clusterCfg.Spec.Provider.Kubernetes,
		AWSOpts:        clusterCfg.Spec.Provider.AWS,
	}

	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS {
		if strings.TrimSpace(eksConfig.KubeconfigPath) != "" {
			info.KubeconfigPath = strings.TrimSpace(eksConfig.KubeconfigPath)
		}

		resolved := &lifecycle.ResolvedClusterInfo{
			ClusterName:       clusterName,
			ConfigClusterName: clusterName,
			EKSConfigSource: eksConfig.NameFromConfig &&
				strings.TrimSpace(eksConfig.ConfigPath) != "",
			Provider:       v1alpha1.ProviderAWS,
			KubeconfigPath: info.KubeconfigPath,
			AWSOpts:        clusterCfg.Spec.Provider.AWS,
			// EKSConfig.Region was pinned before creation. Do not re-read a mutable region
			// environment variable after the TTL wait.
			AWSRegion: strings.TrimSpace(eksConfig.Region),
		}

		err := ensureAWSClusterManaged(ctx, resolved)
		if err != nil {
			return nil, lifecycle.MinimalProvisionerOptions{}, fmt.Errorf(
				"TTL auto-delete: verify EKS ownership: %w",
				err,
			)
		}

		eksConfig.Region = resolved.AWSRegion
		options.AWSRegion = resolved.AWSRegion
		options.AWSResolution = resolved.AWSResolution
		options.AWSOwnershipVerifier = resolved.AWSOwnershipVerifier
	}

	return info, options, nil
}

// ttlAutoDeleteTargetName returns the exact target the provisioner created. EKS creation consumes
// EKSConfig.Name rather than the lifecycle action's name argument, so TTL cleanup must use that same
// immutable identity and fail closed when it is unavailable.
func ttlAutoDeleteTargetName(
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) (string, error) {
	if clusterCfg == nil || clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return clusterName, nil
	}

	if eksConfig == nil {
		return "", errEKSConfigurationUnavailable
	}

	actualName := strings.TrimSpace(eksConfig.Name)
	if actualName == "" {
		return "", errEKSClusterNameRequired
	}

	return actualName, nil
}
