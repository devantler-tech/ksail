package k3dprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runner "github.com/devantler-tech/ksail/v5/pkg/runner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
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
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	if oldSpec == nil || newSpec == nil {
		return clusterupdate.NewEmptyUpdateResult(), nil
	}

	diff, diffErr := k.DiffConfig(ctx, name, oldSpec, newSpec)

	result, proceed, prepErr := clusterupdate.PrepareUpdate(
		diff, diffErr, opts, clustererr.ErrRecreationRequired,
	)
	if !proceed {
		return result, prepErr //nolint:wrapcheck // error context added in PrepareUpdate
	}

	clusterName := k.resolveName(name)

	err := k.applyWorkerScaling(ctx, clusterName, result)
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
) (*clusterupdate.UpdateResult, error) {
	result := clusterupdate.NewEmptyUpdateResult()

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

	// K3d defaults to 1 server when not explicitly set (Go zero value)
	if desiredServers == 0 {
		desiredServers = 1
	}

	// Server (control-plane) changes require recreate — K3d does not support scaling servers
	if runningServers != desiredServers {
		result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
			Field:    "k3d.servers",
			OldValue: strconv.Itoa(runningServers),
			NewValue: strconv.Itoa(desiredServers),
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "K3d does not support adding/removing server (control-plane) nodes after creation",
		})
	}

	// Agent (worker) changes are in-place — K3d supports scaling agents
	if runningAgents != desiredAgents {
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    "k3d.agents",
			OldValue: strconv.Itoa(runningAgents),
			NewValue: strconv.Itoa(desiredAgents),
			Category: clusterupdate.ChangeCategoryInPlace,
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
	result *clusterupdate.UpdateResult,
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
	result *clusterupdate.UpdateResult,
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
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:  "k3d.agents",
				Reason: fmt.Sprintf("failed to create agent node %s: %v", nodeName, err),
			})

			return fmt.Errorf("failed to create agent node %s: %w", nodeName, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    "k3d.agents",
			NewValue: nodeName,
			Category: clusterupdate.ChangeCategoryInPlace,
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
	result *clusterupdate.UpdateResult,
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
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:  "k3d.agents",
				Reason: fmt.Sprintf("failed to delete agent node %s: %v", nodeName, err),
			})

			return fmt.Errorf("failed to delete agent node %s: %w", nodeName, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    "k3d.agents",
			OldValue: nodeName,
			Category: clusterupdate.ChangeCategoryInPlace,
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
	// Temporarily redirect os.Stdout to capture K3d's direct writes.
	// K3d's node list --output json writes to os.Stdout directly,
	// bypassing Cobra's cmd.OutOrStdout().
	listMutex.Lock()

	origStdout := os.Stdout

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		listMutex.Unlock()

		return nil, fmt.Errorf("failed to create pipe: %w", err)
	}

	os.Stdout = pipeWriter

	nodeRunner := runner.NewCobraCommandRunner(pipeWriter, io.Discard)
	cmd := nodecommand.NewCmdNodeList()

	// Run the command in a goroutine since we need to read from the pipe
	// while the command is running (otherwise it may block on a full pipe buffer).
	errChan := make(chan error, 1)

	go func() {
		_, runErr := nodeRunner.Run(ctx, cmd, []string{"--output", "json"})
		// Close write end to signal EOF to the reader
		_ = pipeWriter.Close()

		errChan <- runErr
	}()

	// Read all output from the pipe (this is the JSON from k3d)
	var buf bytes.Buffer

	_, copyErr := io.Copy(&buf, pipeReader)
	_ = pipeReader.Close()

	// Restore stdout while still holding the lock
	os.Stdout = origStdout

	listMutex.Unlock()

	// Wait for command to complete and get any error
	runErr := <-errChan

	if copyErr != nil {
		return nil, fmt.Errorf("failed to read node list output: %w", copyErr)
	}

	if runErr != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", runErr)
	}

	return parseClusterNodes(strings.TrimSpace(buf.String()), clusterName)
}

// parseClusterNodes parses JSON node list output and filters to cluster-specific nodes.
func parseClusterNodes(output, clusterName string) ([]k3dNodeInfo, error) {
	if output == "" {
		return nil, nil
	}

	var allNodes []struct {
		Name   string            `json:"name"`
		Role   string            `json:"role"`
		Labels map[string]string `json:"labels"`
	}

	err := json.Unmarshal([]byte(output), &allNodes)
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

	slices.Sort(agents)

	return agents, nil
}

// nextAgentIndex calculates the next agent index for naming.
// It finds the maximum existing agent index to avoid naming collisions when there
// are gaps in the index sequence, then adds 1 plus the batch offset.
func (k *K3dClusterProvisioner) nextAgentIndex(
	ctx context.Context,
	clusterName string,
	batchOffset int,
) int {
	agents, err := k.listAgentNodes(ctx, clusterName)
	if err != nil {
		return batchOffset
	}

	if len(agents) == 0 {
		return batchOffset
	}

	maxIndex := -1
	prefix := fmt.Sprintf("k3d-%s-agent-", clusterName)

	for _, name := range agents {
		idx, found := strings.CutPrefix(name, prefix)
		if !found {
			continue
		}

		n, parseErr := strconv.Atoi(idx)
		if parseErr == nil && n > maxIndex {
			maxIndex = n
		}
	}

	return maxIndex + 1 + batchOffset
}

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster via Helm releases and Kubernetes Deployments. Falls back to
// static defaults when no detector is available.
func (k *K3dClusterProvisioner) GetCurrentConfig(
	ctx context.Context,
) (*v1alpha1.ClusterSpec, error) {
	if k.componentDetector != nil {
		spec, err := k.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionK3s,
			v1alpha1.ProviderDocker,
		)
		if err != nil {
			return nil, fmt.Errorf("detect components: %w", err)
		}

		clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

		return spec, nil
	}

	return clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionK3s, v1alpha1.ProviderDocker), nil
}
