package eksctl

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ClusterSummary represents a single cluster entry returned by
// `eksctl get cluster -o json`.
type ClusterSummary struct {
	Name   string `json:"Name"`
	Region string `json:"Region"`
	// EKSCTLCreated reports whether the cluster was created by eksctl.
	// eksctl prints the string "True"/"False"; we keep it as-is.
	EKSCTLCreated string `json:"EksctlCreated"`
}

// NodegroupSummary represents a single managed nodegroup returned by
// `eksctl get nodegroup --cluster <name> -o json`.
type NodegroupSummary struct {
	Cluster       string `json:"Cluster"`
	Name          string `json:"Name"`
	Status        string `json:"Status"`
	DesiredCap    int    `json:"DesiredCapacity"`
	MinSize       int    `json:"MinSize"`
	MaxSize       int    `json:"MaxSize"`
	InstanceType  string `json:"InstanceType"`
	ImageID       string `json:"ImageID"`
	CreationTime  string `json:"CreationTime"`
	NodeGroupType string `json:"NodeGroupType"`
	Version       string `json:"Version"`
}

// CreateCluster invokes `eksctl create cluster -f <configPath>`.
// Pass "" for region unless overriding what the config file specifies.
func (c *Client) CreateCluster(ctx context.Context, configPath, region string) error {
	if strings.TrimSpace(configPath) == "" {
		return ErrEmptyConfigPath
	}

	args := []string{"create", "cluster", "--config-file", configPath}
	if region != "" {
		args = append(args, "--region", region)
	}

	_, _, err := c.Exec(ctx, args...)

	return err
}

// DeleteCluster invokes `eksctl delete cluster --name <name> [--region <region>]`.
// When configPath is non-empty, `--config-file` is used instead and takes
// precedence (matching eksctl's own flag precedence).
func (c *Client) DeleteCluster(
	ctx context.Context,
	name, region, configPath string,
	wait bool,
) error {
	args := []string{"delete", "cluster"}

	switch {
	case strings.TrimSpace(configPath) != "":
		args = append(args, "--config-file", configPath)
	case strings.TrimSpace(name) != "":
		args = append(args, "--name", name)
		if region != "" {
			args = append(args, "--region", region)
		}
	default:
		return ErrEmptyClusterName
	}

	if wait {
		args = append(args, "--wait")
	}

	_, _, err := c.Exec(ctx, args...)

	return err
}

// GetCluster returns the summary for a named cluster. Returns
// ErrClusterNotFound when no cluster with the given name exists.
func (c *Client) GetCluster(
	ctx context.Context,
	name, region string,
) (*ClusterSummary, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrEmptyClusterName
	}

	args := []string{"get", "cluster", "--name", name, "--output", "json"}
	if region != "" {
		args = append(args, "--region", region)
	}

	stdout, _, err := c.Exec(ctx, args...)
	if err != nil {
		return nil, err
	}

	clusters, err := parseClusterSummaries(stdout)
	if err != nil {
		return nil, err
	}

	if len(clusters) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrClusterNotFound, name)
	}

	return &clusters[0], nil
}

// ListClusters returns the summary of all EKS clusters in the given region.
// Pass "" for region to rely on eksctl's default (AWS_REGION env or
// AWS profile).
func (c *Client) ListClusters(ctx context.Context, region string) ([]ClusterSummary, error) {
	args := []string{"get", "cluster", "--output", "json"}
	if region != "" {
		args = append(args, "--region", region)
	}

	stdout, _, err := c.Exec(ctx, args...)
	if err != nil {
		return nil, err
	}

	return parseClusterSummaries(stdout)
}

// ListNodegroups returns the nodegroup summaries for a named cluster.
func (c *Client) ListNodegroups(
	ctx context.Context,
	clusterName, region string,
) ([]NodegroupSummary, error) {
	if strings.TrimSpace(clusterName) == "" {
		return nil, ErrEmptyClusterName
	}

	args := []string{
		"get", "nodegroup",
		"--cluster", clusterName,
		"--output", "json",
	}
	if region != "" {
		args = append(args, "--region", region)
	}

	stdout, _, err := c.Exec(ctx, args...)
	if err != nil {
		return nil, err
	}

	if len(stdout) == 0 || strings.TrimSpace(string(stdout)) == "" {
		return nil, nil
	}

	var nodegroups []NodegroupSummary

	err = json.Unmarshal(stdout, &nodegroups)
	if err != nil {
		return nil, fmt.Errorf("eksctl: parse nodegroup json: %w", err)
	}

	return nodegroups, nil
}

// ScaleNodegroup invokes `eksctl scale nodegroup` with the provided sizes.
// minSize and maxSize are optional; pass a negative value to skip.
func (c *Client) ScaleNodegroup(
	ctx context.Context,
	clusterName, nodegroupName, region string,
	desiredCapacity int,
	minSize, maxSize int,
) error {
	if strings.TrimSpace(clusterName) == "" {
		return ErrEmptyClusterName
	}

	if strings.TrimSpace(nodegroupName) == "" {
		return ErrEmptyNodegroupName
	}

	args := []string{
		"scale", "nodegroup",
		"--cluster", clusterName,
		"--name", nodegroupName,
		"--nodes", strconv.Itoa(desiredCapacity),
	}

	if minSize >= 0 {
		args = append(args, "--nodes-min", strconv.Itoa(minSize))
	}

	if maxSize >= 0 {
		args = append(args, "--nodes-max", strconv.Itoa(maxSize))
	}

	if region != "" {
		args = append(args, "--region", region)
	}

	_, _, err := c.Exec(ctx, args...)

	return err
}

// UpgradeCluster invokes `eksctl upgrade cluster -f <configPath>`.
// When approve is true, `--approve` is appended, which eksctl requires to
// perform (rather than preview) the upgrade.
func (c *Client) UpgradeCluster(
	ctx context.Context,
	configPath string,
	approve bool,
) error {
	if strings.TrimSpace(configPath) == "" {
		return ErrEmptyConfigPath
	}

	args := []string{"upgrade", "cluster", "--config-file", configPath}
	if approve {
		args = append(args, "--approve")
	}

	_, _, err := c.Exec(ctx, args...)

	return err
}

// parseClusterSummaries unmarshals the JSON output of `eksctl get cluster`.
// eksctl returns `null` (not `[]`) when no clusters exist, so we handle both.
func parseClusterSummaries(data []byte) ([]ClusterSummary, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var clusters []ClusterSummary

	err := json.Unmarshal(data, &clusters)
	if err != nil {
		return nil, fmt.Errorf("eksctl: parse cluster json: %w", err)
	}

	return clusters, nil
}
