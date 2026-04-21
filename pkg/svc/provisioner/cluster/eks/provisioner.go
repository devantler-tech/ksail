package eksprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
)

// Provisioner manages Amazon EKS clusters via the eksctl CLI.
//
// All operations delegate to pkg/client/eksctl.Client, which in turn shells
// out to the eksctl binary. The provisioner holds the declarative
// eksctl.yaml path so Create/Delete can be driven from the same source of
// truth scaffolded by ksail cluster init.
type Provisioner struct {
	// name is the cluster name derived from the ksail.yaml / eksctl.yaml.
	name string
	// region is the AWS region. Cached here so operations that accept
	// --region without a --config-file (e.g. GetCluster on a deleted
	// cluster) still work.
	region string
	// configPath is the path to the declarative eksctl ClusterConfig.
	// Required for Create; preferred over --name/--region for Delete and
	// Upgrade because it keeps CloudFormation stack naming consistent.
	configPath string
	// client is the eksctl binary wrapper.
	client *eksctl.Client
	// infraProvider is the AWS provider used for Start/Stop semantics
	// (nodegroup scale). Optional: if nil, Start/Stop return an error.
	infraProvider provider.Provider
}

// NewProvisioner builds a Provisioner. The eksctl client must be non-nil;
// configPath is required for Create but optional for inspection-only use.
func NewProvisioner(
	name, region, configPath string,
	client *eksctl.Client,
	infraProvider provider.Provider,
) (*Provisioner, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	return &Provisioner{
		name:          name,
		region:        region,
		configPath:    configPath,
		client:        client,
		infraProvider: infraProvider,
	}, nil
}

// SetProvider sets the infrastructure provider used by Start/Stop.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create provisions a new EKS cluster from the declarative eksctl.yaml.
// The eksctl binary must be on PATH.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	_ = name // name is encoded in the eksctl.yaml; CLI name flag is ignored.

	if p.configPath == "" {
		return ErrConfigPathRequired
	}

	err := p.client.CheckAvailable()
	if err != nil {
		return fmt.Errorf("eksctl unavailable: %w", err)
	}

	err = p.client.CreateCluster(ctx, p.configPath, p.region)
	if err != nil {
		return fmt.Errorf("eksctl create cluster: %w", err)
	}

	return nil
}

// Delete tears down the EKS cluster. Prefers --config-file when available so
// eksctl can unwind the same CloudFormation stacks it created; falls back to
// --name/--region for clusters imported without a local config.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	err := p.client.CheckAvailable()
	if err != nil {
		return fmt.Errorf("eksctl unavailable: %w", err)
	}

	// DeleteCluster prefers configPath over name when configPath is set.
	// Wait=true because cluster deletion must complete before ksail
	// considers the workspace clean.
	err = p.client.DeleteCluster(ctx, target, p.region, p.configPath, true)
	if err != nil {
		return fmt.Errorf("eksctl delete cluster: %w", err)
	}

	return nil
}

// Start resumes an EKS cluster by scaling all managed nodegroups back up.
// EKS control planes can't be stopped, so "start" = "scale nodes back in".
func (p *Provisioner) Start(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: start requires an AWS provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StartNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	return nil
}

// Stop scales every managed nodegroup to zero desired capacity. The EKS
// control plane continues to run (and continues to bill) because AWS does
// not expose a stop operation for the managed control plane.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: stop requires an AWS provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StopNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	return nil
}

// List returns the names of every EKS cluster in the configured region.
// When region is empty eksctl lists clusters across all regions the caller
// has credentials for.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	err := p.client.CheckAvailable()
	if err != nil {
		return nil, fmt.Errorf("eksctl unavailable: %w", err)
	}

	summaries, err := p.client.ListClusters(ctx, p.region)
	if err != nil {
		return nil, fmt.Errorf("eksctl get cluster: %w", err)
	}

	names := make([]string, 0, len(summaries))
	for _, s := range summaries {
		names = append(names, s.Name)
	}

	return names, nil
}

// Exists reports whether a cluster with the given name (or the provisioner
// default) exists in the target region. Implemented via ListClusters +
// membership check because eksctl's `get cluster --name <x>` exits non-zero
// with a stderr message when <x> is missing, which is harder to classify
// reliably than an empty list result.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)
	if target == "" {
		return false, nil
	}

	err := p.client.CheckAvailable()
	if err != nil {
		return false, fmt.Errorf("eksctl unavailable: %w", err)
	}

	summaries, err := p.client.ListClusters(ctx, p.region)
	if err != nil {
		return false, fmt.Errorf("eksctl list clusters: %w", err)
	}

	for _, s := range summaries {
		if s.Name == target {
			return true, nil
		}
	}

	return false, nil
}

// resolveName returns the caller-supplied name when set, otherwise falls
// back to the provisioner's configured name.
func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}
