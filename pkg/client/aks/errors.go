package aks

import "errors"

// ErrMissingSubscriptionID is returned by NewClient when the Azure
// subscription ID is empty: every management-plane request is scoped to a
// subscription, so a client without one could never issue a valid call.
var ErrMissingSubscriptionID = errors.New("azure subscription ID must not be empty")

// ErrAgentPoolPropertiesMissing is returned by SetAgentPoolCount when the
// fetched agent pool carries no properties object to update. The management
// API always populates properties on a live pool, so this signals a malformed
// or partial API response rather than a caller mistake.
var ErrAgentPoolPropertiesMissing = errors.New("agent pool has no properties to update")
