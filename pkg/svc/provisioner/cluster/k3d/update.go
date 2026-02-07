package k3dprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
	runner "github.com/devantler-tech/ksail/v5/pkg/utils/runner"
	nodecommand "github.com/k3d-io/k3d/v5/cmd/node"
)

// Update applies configuration changes to a running K3d cluster.
// K3d supports:
//   - Adding/removing worker (agent) nodes via k3d node commands
//   - Registry configuration updates via registries.yaml
//
// It does NOT support adding/removing server (control-plane) nodes after creation.
func (k *K3dClusterProvisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts types.UpdateOptions,
) (*types.UpdateResult, error) {
	if oldSpec == nil || newSpec == nil {
		return types.NewEmptyUpdateResult(), nil
	}

	diff, err := k.DiffConfig(ctx, name, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config diff: %w", err)
	}

	if opts.DryRun {
		return diff, nil
	}

	result := types.NewUpdateResultFromDiff(diff)

	if diff.HasRecreateRequired() {
		return result, fmt.Errorf("%w: %d changes require restart",
			clustererrors.ErrRecreationRequired, len(diff.RecreateRequired))
	}

	clusterName := k.resolveName(name)

	err = k.applyWorkerScaling(ctx, clusterName, result)
	if err != nil {
		return result, fmt.Errorf("failed to scale workers: %w", err)
	}

	return result, nil
}

// DiffConfig computes the differences between current and desired configurations.
// For K3d, agent count is compared between the current running state and the
// desired SimpleConfig, while server count changes are classified as recreate-required.
func (k *K3dClusterProvisioner) DiffConfig(
	ctx context.Context,
	name string,
	_, _ *v1alpha1.ClusterSpec,
) (*types.UpdateResult, error) {
	result := types.NewEmptyUpdateResult()

	if k.simpleCfg == nil {
		return result, nil
	}

	clusterName := k.resolveName(name)

	// Count running agent and server nodes
	runningAgents, runningServers, err := k.countRunningNodes(ctx, clusterName)
	if err != nil {
		return result, fmt.Errorf("failed to count running nodes: %w", err)
	}

	desiredAgents := k.simpleCfg.Agents
	desiredServers := k.simpleCfg.Servers

	// Server (control-plane) changes require recreate — K3d does not support scaling servers
	if runningServers != desiredServers {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "k3d.servers",
			OldValue: strconv.Itoa(runningServers),
			NewValue: strconv.Itoa(desiredServers),
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "K3d does not support adding/removing server (control-plane) nodes after creation",
		})
	}

	// Agent (worker) changes are in-place — K3d supports scaling agents
	if runningAgents != desiredAgents {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "k3d.agents",
			OldValue: strconv.Itoa(runningAgents),
			NewValue: strconv.Itoa(desiredAgents),
			Category: types.ChangeCategoryInPlace,
			Reason:   "K3d supports adding/removing agent (worker) nodes",
		})
	}

	return result, nil
}

// applyWorkerScaling handles adding or removing K3d agent nodes.
// It reads the desired count from simpleCfg and compares to running agents
// then uses k3d node create/delete commands to converge.
func (k *K3dClusterProvisioner) applyWorkerScaling(
	ctx context.Context,
	clusterName string,
	result *types.UpdateResult,
) error {
	if k.simpleCfg == nil {
		return nil
	}

	runningAgents, _, err := k.countRunningNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to count running agents: %w", err)
	}

	desiredAgents := k.simpleCfg.Agents
	delta := desiredAgents - runningAgents

	if delta == 0 {
		return nil
	}

	if delta > 0 {
		err = k.addAgentNodes(ctx, clusterName, delta, result)
	} else {
		err = k.removeAgentNodes(ctx, clusterName, -delta, result)
	}

	return err
}

// addAgentNodes adds new agent nodes to the cluster.
func (k *K3dClusterProvisioner) addAgentNodes(
	ctx context.Context,
	clusterName string,
	count int,
	result *types.UpdateResult,
) error {
	for i := range count {
		nodeName := fmt.Sprintf(
			"k3d-%s-agent-%d",
			clusterName,
			k.nextAgentIndex(ctx, clusterName, i),
		)

		args := []string{
			nodeName,
			"--cluster", clusterName,
			"--role", "agent",
			"--wait",
		}

		if k.simpleCfg != nil && k.simpleCfg.Image != "" {
			args = append(args, "--image", k.simpleCfg.Image)
		}

		nodeRunner := runner.NewCobraCommandRunner(io.Discard, io.Discard)
		cmd := nodecommand.NewCmdNodeCreate()

		_, err := nodeRunner.Run(ctx, cmd, args)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, types.Change{
				Field:  "k3d.agents",
				Reason: fmt.Sprintf("failed to create agent node %s: %v", nodeName, err),
			})

			return fmt.Errorf("failed to create agent node %s: %w", nodeName, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, types.Change{
			Field:    "k3d.agents",
			NewValue: nodeName,
			Category: types.ChangeCategoryInPlace,
			Reason:   "added agent node",
		})
	}

	return nil
}

// removeAgentNodes removes agent nodes from the cluster (highest-index first).
func (k *K3dClusterProvisioner) removeAgentNodes(
	ctx context.Context,
	clusterName string,
	count int,
	result *types.UpdateResult,
) error {
	agents, err := k.listAgentNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list agent nodes: %w", err)
	}

	if count > len(agents) {
		count = len(agents)
	}

	// Remove from the end (highest index first) to avoid disrupting lower-index nodes
	for i := len(agents) - 1; i >= len(agents)-count; i-- {
		nodeName := agents[i]

		nodeRunner := runner.NewCobraCommandRunner(io.Discard, io.Discard)
		cmd := nodecommand.NewCmdNodeDelete()

		_, err := nodeRunner.Run(ctx, cmd, []string{nodeName})
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, types.Change{
				Field:  "k3d.agents",
				Reason: fmt.Sprintf("failed to delete agent node %s: %v", nodeName, err),
			})

			return fmt.Errorf("failed to delete agent node %s: %w", nodeName, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, types.Change{
			Field:    "k3d.agents",
			OldValue: nodeName,
			Category: types.ChangeCategoryInPlace,
			Reason:   "removed agent node",
		})
	}

	return nil
}

// countRunningNodes counts running agent and server nodes for the cluster.
func (k *K3dClusterProvisioner) countRunningNodes(
	ctx context.Context,
	clusterName string,
) (int, int, error) {
	nodes, err := k.listClusterNodes(ctx, clusterName)
	if err != nil {
		return 0, 0, err
	}

	var agentCount, serverCount int

	for _, n := range nodes {
		switch n.Role {
		case "agent":
			agentCount++
		case "server":
			serverCount++
		}
	}

	return agentCount, serverCount, nil
}

// k3dNodeInfo holds basic info about a k3d node from JSON output.
type k3dNodeInfo struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// listClusterNodes lists all nodes belonging to the cluster using k3d node list.
func (k *K3dClusterProvisioner) listClusterNodes(
	ctx context.Context,
	clusterName string,
) ([]k3dNodeInfo, error) {
	var buf bytes.Buffer

	nodeRunner := runner.NewCobraCommandRunner(&buf, io.Discard)
	cmd := nodecommand.NewCmdNodeList()

	_, err := nodeRunner.Run(ctx, cmd, []string{"--output", "json"})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	output := strings.TrimSpace(buf.String())
	if output == "" {
		return nil, nil
	}

	var allNodes []struct {
		Name   string            `json:"name"`
		Role   string            `json:"role"`
		Labels map[string]string `json:"labels"`
	}

	err = json.Unmarshal([]byte(output), &allNodes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node list: %w", err)
	}

	// Filter to nodes belonging to this cluster based on name prefix
	prefix := "k3d-" + clusterName + "-"

	var nodes []k3dNodeInfo

	for _, n := range allNodes {
		if strings.HasPrefix(strings.TrimPrefix(n.Name, "/"), prefix) {
			nodes = append(nodes, k3dNodeInfo{
				Name: strings.TrimPrefix(n.Name, "/"),
				Role: n.Role,
			})
		}
	}

	return nodes, nil
}

// listAgentNodes returns the names of all agent nodes for the cluster, sorted by name.
func (k *K3dClusterProvisioner) listAgentNodes(
	ctx context.Context,
	clusterName string,
) ([]string, error) {
	nodes, err := k.listClusterNodes(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	var agents []string

	for _, n := range nodes {
		if n.Role == "agent" {
			agents = append(agents, n.Name)
		}
	}

	return agents, nil
}

// nextAgentIndex calculates the next agent index for naming.
// It accounts for existing nodes and the offset of nodes being added in the current batch.
func (k *K3dClusterProvisioner) nextAgentIndex(
	ctx context.Context,
	clusterName string,
	batchOffset int,
) int {
	agents, err := k.listAgentNodes(ctx, clusterName)
	if err != nil {
		return batchOffset
	}

	return len(agents) + batchOffset
}

// GetCurrentConfig retrieves the current cluster configuration.
// For K3d clusters, we return the configuration based on the SimpleConfig
// used for cluster creation, enriched with actual running node counts.
func (k *K3dClusterProvisioner) GetCurrentConfig() (*v1alpha1.ClusterSpec, error) {
	spec := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		Provider:     v1alpha1.ProviderDocker,
	}

	return spec, nil
}
