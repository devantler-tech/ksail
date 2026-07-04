package aks

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
)

// defaultPollInterval is how often long-running operations are polled. Cluster
// and agent-pool mutations take minutes, so a few seconds between polls keeps
// the request volume negligible without adding meaningful latency.
const defaultPollInterval = 5 * time.Second

// Client performs AKS cluster lifecycle operations against one Azure
// subscription, hiding the SDK's poller mechanics behind synchronous calls.
type Client struct {
	managedClusters *armcontainerservice.ManagedClustersClient
	agentPools      *armcontainerservice.AgentPoolsClient
	pollInterval    time.Duration
}

// config collects the constructor inputs Options mutate before the SDK
// sub-clients are built (the SDK clients themselves are immutable once
// constructed, so options cannot act on Client directly like gke's do).
type config struct {
	credential    azcore.TokenCredential
	clientOptions *arm.ClientOptions
	pollInterval  time.Duration
}

// Option customises a Client at construction time.
type Option func(*config)

// WithCredential injects the token credential, replacing the default
// DefaultAzureCredential chain (e.g. a fake credential in tests, or a
// workload-specific credential in callers that must not touch the ambient
// environment).
func WithCredential(credential azcore.TokenCredential) Option {
	return func(cfg *config) {
		cfg.credential = credential
	}
}

// WithClientOptions sets the ARM client options for both SDK sub-clients.
// Tests use this to route requests to the SDK's in-process fake servers via a
// custom transport.
func WithClientOptions(options *arm.ClientOptions) Option {
	return func(cfg *config) {
		cfg.clientOptions = options
	}
}

// WithPollInterval overrides how often long-running operations are polled.
// Values below one millisecond are ignored so a misconfigured caller cannot
// turn the poll loop into a busy-wait against the API.
func WithPollInterval(interval time.Duration) Option {
	return func(cfg *config) {
		if interval >= time.Millisecond {
			cfg.pollInterval = interval
		}
	}
}

// NewClient builds an AKS client for the given subscription. Without
// WithCredential it authenticates via DefaultAzureCredential, the SDK's
// standard chain (environment, workload identity, managed identity, CLI).
func NewClient(subscriptionID string, opts ...Option) (*Client, error) {
	if subscriptionID == "" {
		return nil, ErrMissingSubscriptionID
	}

	cfg := config{pollInterval: defaultPollInterval}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.credential == nil {
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("build default azure credential: %w", err)
		}

		cfg.credential = credential
	}

	managedClusters, err := armcontainerservice.NewManagedClustersClient(
		subscriptionID, cfg.credential, cfg.clientOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("build managed-clusters client: %w", err)
	}

	agentPools, err := armcontainerservice.NewAgentPoolsClient(
		subscriptionID, cfg.credential, cfg.clientOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("build agent-pools client: %w", err)
	}

	return &Client{
		managedClusters: managedClusters,
		agentPools:      agentPools,
		pollInterval:    cfg.pollInterval,
	}, nil
}

// CreateCluster creates (or updates, per ARM create-or-update semantics) the
// named managed cluster and blocks until the operation completes, returning
// the cluster as the API recorded it.
func (c *Client) CreateCluster(
	ctx context.Context,
	resourceGroup, name string,
	cluster armcontainerservice.ManagedCluster,
) (armcontainerservice.ManagedCluster, error) {
	poller, err := c.managedClusters.BeginCreateOrUpdate(ctx, resourceGroup, name, cluster, nil)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, fmt.Errorf("create cluster %q: %w", name, err)
	}

	response, err := poller.PollUntilDone(ctx, c.pollOptions())
	if err != nil {
		return armcontainerservice.ManagedCluster{},
			fmt.Errorf("wait for cluster %q creation: %w", name, err)
	}

	return response.ManagedCluster, nil
}

// DeleteCluster deletes the named managed cluster and blocks until the
// operation completes.
func (c *Client) DeleteCluster(ctx context.Context, resourceGroup, name string) error {
	poller, err := c.managedClusters.BeginDelete(ctx, resourceGroup, name, nil)
	if err != nil {
		return fmt.Errorf("delete cluster %q: %w", name, err)
	}

	_, err = poller.PollUntilDone(ctx, c.pollOptions())
	if err != nil {
		return fmt.Errorf("wait for cluster %q deletion: %w", name, err)
	}

	return nil
}

// GetCluster fetches the named managed cluster's definition.
func (c *Client) GetCluster(
	ctx context.Context,
	resourceGroup, name string,
) (armcontainerservice.ManagedCluster, error) {
	response, err := c.managedClusters.Get(ctx, resourceGroup, name, nil)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, fmt.Errorf("get cluster %q: %w", name, err)
	}

	return response.ManagedCluster, nil
}

// ListClusters lists the managed clusters in the given resource group, or —
// when resourceGroup is empty — across the whole subscription (the AKS
// counterpart to gke's all-locations wildcard).
func (c *Client) ListClusters(
	ctx context.Context,
	resourceGroup string,
) ([]*armcontainerservice.ManagedCluster, error) {
	if resourceGroup == "" {
		return collectPages(ctx, c.managedClusters.NewListPager(nil), listValue)
	}

	return collectPages(
		ctx,
		c.managedClusters.NewListByResourceGroupPager(resourceGroup, nil),
		listByResourceGroupValue,
	)
}

// listValue and listByResourceGroupValue unwrap the two list-response
// envelopes to their shared payload shape for collectPages.
func listValue(
	response armcontainerservice.ManagedClustersClientListResponse,
) []*armcontainerservice.ManagedCluster {
	return response.Value
}

func listByResourceGroupValue(
	response armcontainerservice.ManagedClustersClientListByResourceGroupResponse,
) []*armcontainerservice.ManagedCluster {
	return response.Value
}

// collectPages drains a pager, unwrapping each page's cluster slice with
// value. The two subscription/resource-group pagers share pagination
// mechanics but not a response type, so the envelope accessor is the one
// generic seam.
func collectPages[T any](
	ctx context.Context,
	pager *runtime.Pager[T],
	value func(T) []*armcontainerservice.ManagedCluster,
) ([]*armcontainerservice.ManagedCluster, error) {
	var clusters []*armcontainerservice.ManagedCluster

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list clusters: %w", err)
		}

		clusters = append(clusters, value(page)...)
	}

	return clusters, nil
}

// SetAgentPoolCount resizes the named agent pool to count nodes and blocks
// until the operation completes. It re-submits the pool's current definition
// with only the count changed, per ARM create-or-update semantics — the
// primitive Stop (count 0) and Start (restore count) build on.
func (c *Client) SetAgentPoolCount(
	ctx context.Context,
	resourceGroup, clusterName, poolName string,
	count int32,
) error {
	current, err := c.agentPools.Get(ctx, resourceGroup, clusterName, poolName, nil)
	if err != nil {
		return fmt.Errorf("get agent pool %q: %w", poolName, err)
	}

	pool := current.AgentPool
	if pool.Properties == nil {
		return fmt.Errorf("resize agent pool %q: %w", poolName, ErrAgentPoolPropertiesMissing)
	}

	pool.Properties.Count = &count

	poller, err := c.agentPools.BeginCreateOrUpdate(
		ctx, resourceGroup, clusterName, poolName, pool, nil,
	)
	if err != nil {
		return fmt.Errorf("resize agent pool %q: %w", poolName, err)
	}

	_, err = poller.PollUntilDone(ctx, c.pollOptions())
	if err != nil {
		return fmt.Errorf("wait for agent pool %q resize: %w", poolName, err)
	}

	return nil
}

// pollOptions returns the shared poll frequency for every long-running
// operation this client waits on.
func (c *Client) pollOptions() *runtime.PollUntilDoneOptions {
	return &runtime.PollUntilDoneOptions{Frequency: c.pollInterval}
}
