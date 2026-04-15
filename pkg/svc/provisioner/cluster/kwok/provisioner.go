package kwokprovisioner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	runner "github.com/devantler-tech/ksail/v6/pkg/runner"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clustererr"
	"sigs.k8s.io/kwok/pkg/config"
	createcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/create/cluster"
	deletecluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/delete/cluster"
	getclusters "sigs.k8s.io/kwok/pkg/kwokctl/cmd/get/clusters"
	startcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/start/cluster"
	stopcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/stop/cluster"
)

// Provisioner manages KWOK clusters using kwokctl's Cobra commands.
// It uses the docker (compose) runtime which runs etcd + kube-apiserver +
// kwok-controller as Docker containers.
type Provisioner struct {
	name          string
	configPath    string
	infraProvider provider.Provider
	runner        runner.CommandRunner
	stdout        io.Writer
	stderr        io.Writer
}

// NewProvisioner creates a new KWOK provisioner.
// configPath is the optional path to a kwok.yaml configuration file.
func NewProvisioner(
	name string,
	configPath string,
	infraProvider provider.Provider,
) *Provisioner {
	return &Provisioner{
		name:          name,
		configPath:    configPath,
		infraProvider: infraProvider,
		runner:        runner.NewCobraCommandRunner(os.Stdout, os.Stderr),
		stdout:        os.Stdout,
		stderr:        os.Stderr,
	}
}

// SetProvider sets the infrastructure provider for node operations.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create creates a KWOK cluster using kwokctl's create command.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	config.DefaultCluster = target
	cmd := createcluster.NewCommand(ctx)

	args := []string{"--runtime", "docker"}
	if p.configPath != "" {
		args = append(args, "--config", p.configPath)
	}

	_, err := p.runner.Run(ctx, cmd, args)
	if err != nil {
		return fmt.Errorf("failed to create KWOK cluster: %w", err)
	}

	return nil
}

// Delete deletes a KWOK cluster using kwokctl's delete command.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	exists, err := p.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	config.DefaultCluster = target
	cmd := deletecluster.NewCommand(ctx)

	_, err = p.runner.Run(ctx, cmd, nil)
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	return nil
}

// Start starts a stopped KWOK cluster.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	target := p.resolveName(name)

	config.DefaultCluster = target
	cmd := startcluster.NewCommand(ctx)

	_, err := p.runner.Run(ctx, cmd, nil)
	if err != nil {
		return fmt.Errorf("failed to start KWOK cluster: %w", err)
	}

	return nil
}

// Stop stops a running KWOK cluster.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	target := p.resolveName(name)

	config.DefaultCluster = target
	cmd := stopcluster.NewCommand(ctx)

	_, err := p.runner.Run(ctx, cmd, nil)
	if err != nil {
		return fmt.Errorf("failed to stop KWOK cluster: %w", err)
	}

	return nil
}

// List returns all KWOK cluster names.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	var outBuf bytes.Buffer
	quietRunner := runner.NewCobraCommandRunner(&outBuf, os.Stderr)

	cmd := getclusters.NewCommand(ctx)

	_, err := quietRunner.Run(ctx, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}

	return parseClusterList(outBuf.String()), nil
}

// Exists checks if a KWOK cluster exists.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)

	clusters, err := p.List(ctx)
	if err != nil {
		return false, err
	}

	for _, c := range clusters {
		if c == target {
			return true, nil
		}
	}

	return false, nil
}

func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}

func parseClusterList(output string) []string {
	var clusters []string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			clusters = append(clusters, trimmed)
		}
	}

	return clusters
}
