// Package aks provides a thin, test-injectable client for Azure Kubernetes
// Service cluster lifecycle operations, wrapping the official native Go SDK
// (github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/
// armcontainerservice). It is the AKS counterpart to the gke and eksctl
// clients: everything above it (infra provider, provisioner, factory routing)
// talks to AKS exclusively through this package.
//
// AKS cluster and agent-pool mutations are asynchronous: the API returns a
// long-running operation the SDK exposes as a poller. CreateCluster,
// DeleteCluster and SetAgentPoolCount hide that mechanic — they block until
// the operation completes (honouring context cancellation) and surface any
// operation error.
//
// Authentication uses DefaultAzureCredential (the SDK's standard environment /
// workload-identity / CLI chain) unless a credential is injected; tests drive
// the client against the SDK's in-process fake servers via WithClientOptions,
// so no bespoke poller mocks are needed.
package aks
