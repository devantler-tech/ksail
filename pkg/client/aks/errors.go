package aks

import "errors"

// ErrMissingSubscriptionID is returned by NewClient when the Azure
// subscription ID is empty: every management-plane request is scoped to a
// subscription, so a client without one could never issue a valid call.
var ErrMissingSubscriptionID = errors.New("azure subscription ID must not be empty")

// ErrOperationFailed is returned when a long-running AKS operation (cluster
// create/delete, agent-pool resize) completes unsuccessfully, so callers can
// detect the failure path with errors.Is regardless of which operation it
// came from — mirroring the gke client's sentinel.
var ErrOperationFailed = errors.New("aks: cluster operation failed")

// ErrAgentPoolPropertiesMissing is returned by SetAgentPoolCount when the
// fetched agent pool carries no properties object to update. The management
// API always populates properties on a live pool, so this signals a malformed
// or partial API response rather than a caller mistake.
var ErrAgentPoolPropertiesMissing = errors.New("agent pool has no properties to update")

// ErrNoKubeconfig is returned by GetClusterUserCredentials when the
// credentials response carries no kubeconfig payload. ARM always returns at
// least one kubeconfig for a provisioned cluster, so this signals a malformed
// or partial API response rather than a caller mistake.
var ErrNoKubeconfig = errors.New("aks cluster credentials carry no kubeconfig")
