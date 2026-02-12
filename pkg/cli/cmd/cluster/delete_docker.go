package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// discoverDockerNodes discovers cluster node containers for Docker provider.
// Kind uses: {cluster}-control-plane, {cluster}-worker, etc.
// K3d uses: k3d-{cluster}-server-0, k3d-{cluster}-agent-0, etc.
// Talos uses: {cluster}-controlplane-*, {cluster}-worker-*.
func discoverDockerNodes(cmd *cobra.Command, clusterName string) []string {
	var nodes []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if IsClusterContainer(containerName, clusterName) {
			nodes = append(nodes, containerName)
		}

		return false // continue processing all containers
	})

	return nodes
}

// IsClusterContainer checks if a container name belongs to the given cluster.
// Exported for testing.
func IsClusterContainer(containerName, clusterName string) bool {
	// Kind pattern: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}
	// Check for exact prefixes with valid suffixes to avoid partial cluster name matches
	if matchesKindPattern(containerName, clusterName) {
		return true
	}

	// K3d pattern: k3d-{cluster}-server-*, k3d-{cluster}-agent-*
	if strings.HasPrefix(containerName, "k3d-"+clusterName+"-server-") ||
		strings.HasPrefix(containerName, "k3d-"+clusterName+"-agent-") {
		return true
	}

	// Talos pattern: {cluster}-controlplane-*, {cluster}-worker-*
	if strings.HasPrefix(containerName, clusterName+"-controlplane-") ||
		strings.HasPrefix(containerName, clusterName+"-worker-") {
		return true
	}

	return false
}

// isKindClusterFromNodes determines if a cluster is a Kind cluster by checking
// if any of its nodes match Kind's container naming convention.
// This is used as a fallback when kubeconfig-based detection fails.
func isKindClusterFromNodes(nodes []string, clusterName string) bool {
	for _, node := range nodes {
		if matchesKindPattern(node, clusterName) {
			return true
		}
	}

	return false
}

// matchesKindPattern checks if container matches Kind's naming convention.
// Kind uses: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}.
func matchesKindPattern(containerName, clusterName string) bool {
	// Check control-plane (exact suffix)
	if containerName == clusterName+"-control-plane" {
		return true
	}

	// Check worker nodes: {cluster}-worker or {cluster}-worker{N}
	workerPrefix := clusterName + "-worker"
	if containerName == workerPrefix {
		return true
	}

	// Check for numbered workers: {cluster}-worker2, {cluster}-worker3, etc.
	if strings.HasPrefix(containerName, workerPrefix) {
		suffix := containerName[len(workerPrefix):]
		// Suffix must be a number for valid worker nodes
		if suffix != "" && isNumericString(suffix) {
			return true
		}
	}

	return false
}

// isNumericString checks if a string contains only digits.
func isNumericString(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// isCloudProviderKindContainer checks if a container name belongs to cloud-provider-kind.
func isCloudProviderKindContainer(name string) bool {
	return name == "ksail-cloud-provider-kind" || strings.HasPrefix(name, "cpk-")
}

// hasRemainingKindClusters checks if there are any Kind clusters remaining in Docker.
func hasRemainingKindClusters(cmd *cobra.Command) bool {
	return countKindClusters(cmd) > 0
}

// hasCloudProviderKindContainers checks if there are any cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func hasCloudProviderKindContainers(cmd *cobra.Command) bool {
	return len(listCloudProviderKindContainerNames(cmd)) > 0
}

// listCloudProviderKindContainerNames returns the names of all cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func listCloudProviderKindContainerNames(cmd *cobra.Command) []string {
	var names []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if isCloudProviderKindContainer(containerName) {
			names = append(names, containerName)
		}

		return false // continue processing all containers
	})

	return names
}

// countKindClusters counts the number of Kind clusters currently running.
// This is determined by counting containers with the -control-plane suffix.
func countKindClusters(cmd *cobra.Command) int {
	var count int

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if strings.HasSuffix(containerName, "-control-plane") {
			count++
		}

		return false // continue processing all containers
	})

	return count
}

// cleanupCloudProviderKindIfLastCluster uninstalls cloud-provider-kind if no kind clusters remain.
// Cloud-provider-kind creates containers that can be shared across multiple kind clusters,
// so we only uninstall when the last kind cluster is deleted.
func cleanupCloudProviderKindIfLastCluster(
	cmd *cobra.Command,
	tmr timer.Timer,
) {
	// Check if there are any remaining Kind clusters by looking for Kind containers
	if hasRemainingKindClusters(cmd) {
		return
	}

	// Check if there are any cloud-provider-kind containers to clean up
	if !hasCloudProviderKindContainers(cmd) {
		return
	}

	// No kind clusters remain - proceed with cloud-provider-kind cleanup
	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Cleanup cloud-provider-kind...",
		Emoji:   "🧹",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "uninstalling cloud-provider-kind (no kind clusters remain)",
		Writer:  cmd.OutOrStdout(),
	})

	// We need to uninstall from one of the recently deleted clusters
	// Since all clusters are gone, we can't actually uninstall via Helm
	// Instead, we need to clean up any remaining cloud-provider-kind containers
	cleanupErr := cleanupCloudProviderKindContainers(cmd)
	if cleanupErr != nil {
		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: fmt.Sprintf(
				"failed to cleanup cloud-provider-kind containers: %v",
				cleanupErr,
			),
			Writer: cmd.OutOrStdout(),
		})

		return
	}

	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cloud-provider-kind cleaned up",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})
}

// cleanupCloudProviderKindContainers removes any cloud-provider-kind related containers.
// This includes:
// - The main ksail-cloud-provider-kind controller container
// - Any cpk-* containers created by cloud-provider-kind for LoadBalancer services.
func cleanupCloudProviderKindContainers(cmd *cobra.Command) error {
	return forEachContainer(
		cmd,
		func(dockerClient client.APIClient, ctr container.Summary, name string) error {
			if !isCloudProviderKindContainer(name) {
				return nil
			}

			err := dockerClient.ContainerRemove(
				cmd.Context(),
				ctr.ID,
				container.RemoveOptions{Force: true},
			)
			if err != nil {
				return fmt.Errorf("failed to remove container %s: %w", name, err)
			}

			return nil
		},
	)
}
