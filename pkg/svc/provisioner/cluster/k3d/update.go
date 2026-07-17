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

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	runner "github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	nodecommand "github.com/k3d-io/k3d/v5/cmd/node"
)

const (
	// fieldK3dAgents is the diff field path for K3d agent (worker) node changes.
	fieldK3dAgents = "k3d.agents"
	// roleAgent is the k3d node role for worker (agent) nodes.
	roleAgent = "agent"
)

// Update applies configuration changes to a running K3d cluster.
// K3d supports:
//   - Adding/removing worker (agent) nodes via k3d node commands
//   - Registry configuration updates via registries.yaml
//
// It does NOT support adding/removing server (control-plane) nodes after creation.
func (k *Provisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	result, proceed, prepErr := clusterupdate.BeginUpdate(
		ctx, name, oldSpec, newSpec, opts,
		clustererr.ErrRecreationRequired, k.DiffConfig,
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
func (k *Provisioner) DiffConfig(
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
			Field:    fieldK3dAgents,
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
func (k *Provisioner) applyWorkerScaling(
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

// addAgentNodes adds new agent nodes to the cluster, reclaiming the lowest
// available index for each new node so a removed agent's name is reused rather
// than always appending past the highest index (#5312).
func (k *Provisioner) addAgentNodes(
	ctx context.Context,
	clusterName string,
	count int,
	result *clusterupdate.UpdateResult,
) error {
	existing, err := k.listAgentNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list agent nodes: %w", err)
	}

	for _, nodeName := range agentNodeNames(existing, clusterName, count) {
		args := []string{
			nodeName,
			"--cluster", clusterName,
			"--role", roleAgent,
			"--wait",
		}

		if k.simpleCfg != nil && k.simpleCfg.Image != "" {
			args = append(args, "--image", k.simpleCfg.Image)
		}

		nodeRunner := runner.NewCobraCommandRunner(io.Discard, io.Discard)
		cmd := nodecommand.NewCmdNodeCreate()

		runErr := k.runK3dSafely(func() error {
			_, e := nodeRunner.Run(ctx, cmd, args)

			return e //nolint:wrapcheck // wrapped below
		})
		if runErr != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:  fieldK3dAgents,
				Reason: fmt.Sprintf("failed to create agent node %s: %v", nodeName, runErr),
			})

			return fmt.Errorf("failed to create agent node %s: %w", nodeName, runErr)
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    fieldK3dAgents,
			NewValue: nodeName,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "added agent node",
		})
	}

	return nil
}

// removeAgentNodes removes agent nodes from the cluster (highest-index first).
func (k *Provisioner) removeAgentNodes(
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

		err := k.runK3dSafely(func() error {
			_, e := nodeRunner.Run(ctx, cmd, []string{nodeName})

			return e //nolint:wrapcheck // wrapped below
		})
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:  fieldK3dAgents,
				Reason: fmt.Sprintf("failed to delete agent node %s: %v", nodeName, err),
			})

			return fmt.Errorf("failed to delete agent node %s: %w", nodeName, err)
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    fieldK3dAgents,
			OldValue: nodeName,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "removed agent node",
		})
	}

	return nil
}

// countRunningNodes counts running agent and server nodes for the cluster.
func (k *Provisioner) countRunningNodes(
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
		case roleAgent:
			agentCount++
		case "server":
			serverCount++
		}
	}

	return agentCount, serverCount, nil
}

// nodeInfo holds basic info about a k3d node from JSON output.
type nodeInfo struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// listClusterNodes lists all nodes belonging to the cluster using k3d node list.
func (k *Provisioner) listClusterNodes(
	ctx context.Context,
	clusterName string,
) ([]nodeInfo, error) {
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
		// Always close the write end so the pipe reader (io.Copy) unblocks — whether
		// the command returns, errors, or its runtime-down Fatal is recovered.
		defer func() { _ = pipeWriter.Close() }()

		errChan <- k.runK3dSafely(func() error {
			_, runErr := nodeRunner.Run(ctx, cmd, []string{"--output", "json"})

			return runErr //nolint:wrapcheck // wrapped by the caller with "failed to list nodes:"
		})
	}()

	// Read all output from the pipe (this is the JSON from k3d)
	var buf bytes.Buffer

	_, copyErr := io.Copy(&buf, pipeReader)
	_ = pipeReader.Close()

	// Wait for the command goroutine to finish BEFORE restoring os.Stdout: the
	// channel receive is the happens-before edge with the goroutine's read of
	// os.Stdout (k3d's docker client reads it via moby/term.StdStreams), so
	// restoring earlier races that read under the race detector.
	runErr := <-errChan

	// Restore stdout while still holding the lock.
	os.Stdout = origStdout

	listMutex.Unlock()

	if copyErr != nil {
		return nil, fmt.Errorf("failed to read node list output: %w", copyErr)
	}

	if runErr != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", runErr)
	}

	return parseClusterNodes(strings.TrimSpace(buf.String()), clusterName)
}

// parseClusterNodes parses JSON node list output and filters to cluster-specific nodes.
func parseClusterNodes(output, clusterName string) ([]nodeInfo, error) {
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

	var nodes []nodeInfo

	for _, n := range allNodes {
		if strings.HasPrefix(strings.TrimPrefix(n.Name, "/"), prefix) {
			nodes = append(nodes, nodeInfo{
				Name: strings.TrimPrefix(n.Name, "/"),
				Role: n.Role,
			})
		}
	}

	return nodes, nil
}

// listAgentNodes returns the names of all agent nodes for the cluster, sorted by name.
func (k *Provisioner) listAgentNodes(
	ctx context.Context,
	clusterName string,
) ([]string, error) {
	nodes, err := k.listClusterNodes(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	var agents []string

	for _, n := range nodes {
		if n.Role == roleAgent {
			agents = append(agents, n.Name)
		}
	}

	slices.Sort(agents)

	return agents, nil
}

// agentNodeNames returns the names for `count` new agent nodes, reclaiming the
// lowest-available index in the 0-based "k3d-<cluster>-agent-<n>" series before
// extending past the highest existing index (#5312). All names are computed up
// front from a single inventory snapshot: this keeps a multi-node scale-up's
// indices distinct and gap-filling, and avoids the previous per-node re-list +
// batch-offset scheme, which double-counted (and so skipped indices) once a
// freshly created node became visible to the next list call.
func agentNodeNames(existing []string, clusterName string, count int) []string {
	prefix := fmt.Sprintf("k3d-%s-agent-", clusterName)
	indices := clusterupdate.AvailableNodeIndices(existing, prefix, 0, count)

	names := make([]string, len(indices))
	for i, index := range indices {
		names[i] = fmt.Sprintf("k3d-%s-agent-%d", clusterName, index)
	}

	return names
}

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster via Helm releases and Kubernetes Deployments. Falls back to
// static defaults when no detector is available. The clusterName is used to
// merge non-introspectable persisted state (e.g. vanilla.mirrorsDir,
// localRegistry) so a configured value does not read as a false
// recreate-required diff on every update.
func (k *Provisioner) GetCurrentConfig(
	ctx context.Context,
	clusterName string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
	)

	if k.componentDetector != nil {
		detected, err := k.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionK3s,
			v1alpha1.ProviderDocker,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("detect components: %w", err)
		}

		spec = detected
	}

	err := clusterupdate.MergePersistedState(spec, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("merge persisted state: %w", err)
	}

	return spec, nil, nil
}
