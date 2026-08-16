package gke

import (
	"context"
	"fmt"
	"time"

	container "cloud.google.com/go/container/apiv1"
	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/googleapis/gax-go/v2"
)

// defaultPollInterval is how often Create/Delete poll their long-running
// operation. Cluster mutations take minutes, so a few seconds between polls
// keeps the request volume negligible without adding meaningful latency.
const defaultPollInterval = 5 * time.Second

// clusterManager is the narrow seam over the GKE ClusterManagerClient
// operations this package uses, so tests can inject a fake without GCP
// credentials. *container.ClusterManagerClient satisfies it.
type clusterManager interface {
	CreateCluster(
		ctx context.Context,
		req *containerpb.CreateClusterRequest,
		opts ...gax.CallOption,
	) (*containerpb.Operation, error)
	DeleteCluster(
		ctx context.Context,
		req *containerpb.DeleteClusterRequest,
		opts ...gax.CallOption,
	) (*containerpb.Operation, error)
	GetCluster(
		ctx context.Context,
		req *containerpb.GetClusterRequest,
		opts ...gax.CallOption,
	) (*containerpb.Cluster, error)
	ListClusters(
		ctx context.Context,
		req *containerpb.ListClustersRequest,
		opts ...gax.CallOption,
	) (*containerpb.ListClustersResponse, error)
	SetNodePoolSize(
		ctx context.Context,
		req *containerpb.SetNodePoolSizeRequest,
		opts ...gax.CallOption,
	) (*containerpb.Operation, error)
	GetOperation(
		ctx context.Context,
		req *containerpb.GetOperationRequest,
		opts ...gax.CallOption,
	) (*containerpb.Operation, error)
	Close() error
}

// Client performs GKE cluster lifecycle operations against one Google Cloud
// project, hiding the SDK's request shapes and long-running-operation polling.
type Client struct {
	manager      clusterManager
	pollInterval time.Duration
}

// Option customises a Client.
type Option func(*Client)

// WithClusterManager injects the cluster-manager implementation, letting tests
// substitute a fake for the real SDK client.
func WithClusterManager(manager clusterManager) Option {
	return func(client *Client) {
		client.manager = manager
	}
}

// WithPollInterval overrides how often long-running operations are polled.
// Values below one millisecond are ignored so a misconfigured caller cannot
// turn the poll loop into a busy-wait against the API.
func WithPollInterval(interval time.Duration) Option {
	return func(client *Client) {
		if interval >= time.Millisecond {
			client.pollInterval = interval
		}
	}
}

// NewClient constructs a Client. Unless a cluster manager is injected, it
// creates the real SDK client using Application Default Credentials.
func NewClient(ctx context.Context, opts ...Option) (*Client, error) {
	client := &Client{
		manager:      nil,
		pollInterval: defaultPollInterval,
	}

	for _, opt := range opts {
		opt(client)
	}

	if client.manager == nil {
		manager, err := container.NewClusterManagerClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating GKE cluster manager client: %w", err)
		}

		client.manager = manager
	}

	return client, nil
}

// Close releases the underlying SDK client's connections.
func (c *Client) Close() error {
	err := c.manager.Close()
	if err != nil {
		return fmt.Errorf("closing GKE cluster manager client: %w", err)
	}

	return nil
}

// CreateCluster creates the given cluster in the project and location and
// blocks until the create operation completes.
func (c *Client) CreateCluster(
	ctx context.Context,
	project string,
	location string,
	cluster *containerpb.Cluster,
) error {
	if cluster == nil {
		return ErrNilCluster
	}

	request := &containerpb.CreateClusterRequest{
		Parent:  locationName(project, location),
		Cluster: cluster,
	}

	operation, err := c.manager.CreateCluster(ctx, request)
	if err != nil {
		return fmt.Errorf("creating GKE cluster %q: %w", cluster.GetName(), err)
	}

	return c.waitForOperation(ctx, project, location, operation)
}

// DeleteCluster deletes the named cluster in the project and location and
// blocks until the delete operation completes.
func (c *Client) DeleteCluster(
	ctx context.Context,
	project string,
	location string,
	name string,
) error {
	request := &containerpb.DeleteClusterRequest{
		Name: clusterName(project, location, name),
	}

	operation, err := c.manager.DeleteCluster(ctx, request)
	if err != nil {
		return fmt.Errorf("deleting GKE cluster %q: %w", name, err)
	}

	return c.waitForOperation(ctx, project, location, operation)
}

// GetCluster returns the named cluster in the project and location.
func (c *Client) GetCluster(
	ctx context.Context,
	project string,
	location string,
	name string,
) (*containerpb.Cluster, error) {
	request := &containerpb.GetClusterRequest{
		Name: clusterName(project, location, name),
	}

	cluster, err := c.manager.GetCluster(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("getting GKE cluster %q: %w", name, err)
	}

	return cluster, nil
}

// ListClusters returns the clusters in the project and location. Location "-"
// lists across all locations, per the GKE API convention.
func (c *Client) ListClusters(
	ctx context.Context,
	project string,
	location string,
) ([]*containerpb.Cluster, error) {
	request := &containerpb.ListClustersRequest{
		Parent: locationName(project, location),
	}

	response, err := c.manager.ListClusters(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("listing GKE clusters: %w", err)
	}

	return response.GetClusters(), nil
}

// SetNodePoolSize resizes the named node pool to the given node count and
// blocks until the resize operation completes. Resizing to the pool's current
// size is a server-side no-op, so callers may re-assert a size idempotently.
func (c *Client) SetNodePoolSize(
	ctx context.Context,
	project string,
	location string,
	cluster string,
	nodePool string,
	nodeCount int32,
) error {
	request := &containerpb.SetNodePoolSizeRequest{
		Name:      nodePoolName(project, location, cluster, nodePool),
		NodeCount: nodeCount,
	}

	operation, err := c.manager.SetNodePoolSize(ctx, request)
	if err != nil {
		return fmt.Errorf("resizing GKE node pool %q to %d: %w", nodePool, nodeCount, err)
	}

	return c.waitForOperation(ctx, project, location, operation)
}

// waitForOperation polls the long-running operation until it reaches DONE,
// honouring context cancellation. A DONE operation that carries an error is
// surfaced as ErrOperationFailed.
func (c *Client) waitForOperation(
	ctx context.Context,
	project string,
	location string,
	operation *containerpb.Operation,
) error {
	for {
		if operation.GetStatus() == containerpb.Operation_DONE {
			return operationResult(operation)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for GKE operation %q: %w", operation.GetName(), ctx.Err())
		case <-time.After(c.pollInterval):
		}

		request := &containerpb.GetOperationRequest{
			Name: operationName(project, location, operation.GetName()),
		}

		refreshed, err := c.manager.GetOperation(ctx, request)
		if err != nil {
			return fmt.Errorf("polling GKE operation %q: %w", operation.GetName(), err)
		}

		operation = refreshed
	}
}

// operationResult maps a DONE operation to its outcome. The API reports
// failures via the operation's structured error field.
func operationResult(operation *containerpb.Operation) error {
	opErr := operation.GetError()
	if opErr != nil {
		return fmt.Errorf(
			"%w: operation %q: %s", ErrOperationFailed, operation.GetName(), opErr.GetMessage(),
		)
	}

	return nil
}

// locationName renders the "projects/*/locations/*" resource name that
// parents cluster requests.
func locationName(project string, location string) string {
	return fmt.Sprintf("projects/%s/locations/%s", project, location)
}

// clusterName renders the fully-qualified cluster resource name.
func clusterName(project string, location string, name string) string {
	return fmt.Sprintf("%s/clusters/%s", locationName(project, location), name)
}

// nodePoolName renders the fully-qualified node-pool resource name.
func nodePoolName(project string, location string, cluster string, nodePool string) string {
	return fmt.Sprintf("%s/nodePools/%s", clusterName(project, location, cluster), nodePool)
}

// operationName renders the fully-qualified operation resource name.
func operationName(project string, location string, name string) string {
	return fmt.Sprintf("%s/operations/%s", locationName(project, location), name)
}
