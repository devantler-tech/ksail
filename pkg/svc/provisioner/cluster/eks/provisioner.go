package eksprovisioner

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/cluster"
)

// EKS Anywhere with Docker provider uses Kind for local development clusters.
// This provisioner wraps Kind with EKS-specific naming conventions.

const (
	// eksClusterPrefix is the prefix used for EKS Anywhere Docker clusters.
	eksClusterPrefix = "eksa-"

	// dockerStartTimeout is the timeout for starting Docker containers.
	dockerStartTimeout = 30 * time.Second

	// dockerStopTimeout is the timeout for stopping Docker containers.
	dockerStopTimeout = 60 * time.Second
)

// EKSConfig holds configuration for an EKS Anywhere Docker cluster.
type EKSConfig struct {
	// Name is the cluster name.
	Name string

	// ControlPlanes is the number of control plane nodes.
	ControlPlanes int32

	// Workers is the number of worker nodes.
	Workers int32

	// KubernetesVersion is the Kubernetes version.
	KubernetesVersion string
}

// KindProvider describes the subset of methods from kind's Provider used here.
type KindProvider interface {
	Create(name string, opts ...cluster.CreateOption) error
	Delete(name, kubeconfigPath string) error
	List() ([]string, error)
	ListNodes(name string) ([]string, error)
}

// EKSClusterProvisioner is an implementation of the ClusterProvisioner interface for
// provisioning EKS Anywhere clusters using the Docker provider.
//
// EKS Anywhere Docker provider is designed for local development and testing.
// Under the hood, it uses Kind for creating the cluster infrastructure.
type EKSClusterProvisioner struct {
	kubeConfig string
	eksConfig  *EKSConfig
	provider   KindProvider
	client     client.ContainerAPIClient
}

// NewEKSClusterProvisioner constructs an EKSClusterProvisioner with explicit dependencies
// for the kind provider and docker client.
func NewEKSClusterProvisioner(
	eksConfig *EKSConfig,
	kubeConfig string,
	provider KindProvider,
	client client.ContainerAPIClient,
) *EKSClusterProvisioner {
	return &EKSClusterProvisioner{
		kubeConfig: kubeConfig,
		eksConfig:  eksConfig,
		provider:   provider,
		client:     client,
	}
}

// Create creates an EKS Anywhere Docker cluster.
// This uses Kind under the hood with EKS-specific configuration.
func (e *EKSClusterProvisioner) Create(ctx context.Context, name string) error {
	target := e.getClusterName(name)

	// EKS Anywhere with Docker provider uses Kind for the cluster
	// We create a Kind cluster with EKS naming conventions
	err := e.provider.Create(target)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEKSCreateFailed, err)
	}

	return nil
}

// Delete deletes an EKS Anywhere Docker cluster.
func (e *EKSClusterProvisioner) Delete(ctx context.Context, name string) error {
	target := e.getClusterName(name)

	// Check if cluster exists before attempting to delete
	exists, err := e.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, target)
	}

	err = e.provider.Delete(target, e.kubeConfig)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEKSDeleteFailed, err)
	}

	return nil
}

// Start starts an EKS Anywhere Docker cluster.
func (e *EKSClusterProvisioner) Start(ctx context.Context, name string) error {
	target := e.getClusterName(name)

	nodes, err := e.provider.ListNodes(target)
	if err != nil {
		return fmt.Errorf("cluster '%s': %w", target, err)
	}

	if len(nodes) == 0 {
		return ErrNoEKSNodes
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, dockerStartTimeout)
	defer cancel()

	for _, nodeName := range nodes {
		err := e.client.ContainerStart(timeoutCtx, nodeName, container.StartOptions{})
		if err != nil {
			return fmt.Errorf("docker start failed for %s: %w", nodeName, err)
		}
	}

	return nil
}

// Stop stops an EKS Anywhere Docker cluster.
func (e *EKSClusterProvisioner) Stop(ctx context.Context, name string) error {
	target := e.getClusterName(name)

	nodes, err := e.provider.ListNodes(target)
	if err != nil {
		return fmt.Errorf("failed to list nodes for cluster '%s': %w", target, err)
	}

	if len(nodes) == 0 {
		return ErrNoEKSNodes
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, dockerStopTimeout)
	defer cancel()

	for _, nodeName := range nodes {
		err := e.client.ContainerStop(timeoutCtx, nodeName, container.StopOptions{})
		if err != nil {
			return fmt.Errorf("docker stop failed for %s: %w", nodeName, err)
		}
	}

	return nil
}

// List returns all EKS Anywhere Docker clusters.
func (e *EKSClusterProvisioner) List(ctx context.Context) ([]string, error) {
	clusters, err := e.provider.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list EKS clusters: %w", err)
	}

	// Filter to only return EKS Anywhere clusters (those with eksa- prefix)
	var eksClusters []string
	for _, c := range clusters {
		if len(c) > len(eksClusterPrefix) && c[:len(eksClusterPrefix)] == eksClusterPrefix {
			eksClusters = append(eksClusters, c)
		}
	}

	return eksClusters, nil
}

// Exists checks if an EKS Anywhere Docker cluster exists.
func (e *EKSClusterProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusters, err := e.provider.List()
	if err != nil {
		return false, fmt.Errorf("failed to list clusters: %w", err)
	}

	target := e.getClusterName(name)

	return slices.Contains(clusters, target), nil
}

// getClusterName returns the full cluster name with EKS prefix.
func (e *EKSClusterProvisioner) getClusterName(name string) string {
	target := name
	if target == "" {
		target = e.eksConfig.Name
	}

	// Add EKS prefix if not already present
	if len(target) < len(eksClusterPrefix) || target[:len(eksClusterPrefix)] != eksClusterPrefix {
		return eksClusterPrefix + target
	}

	return target
}

// streamLogger allows console output to be displayed in real-time.
type streamLogger struct {
	writer io.Writer
}

func (l *streamLogger) write(message string) {
	if l == nil {
		return
	}
	_, _ = io.WriteString(l.writer, message+"\n")
}

// newStreamLogger creates a new stream logger that writes to stdout.
func newStreamLogger() *streamLogger {
	return &streamLogger{writer: os.Stdout}
}
