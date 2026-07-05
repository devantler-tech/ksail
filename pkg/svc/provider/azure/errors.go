package azure

import "errors"

// ErrClientRequired is returned when NewProvider is called without an AKS
// client, so callers fail fast instead of getting
// provider.ErrProviderUnavailable at the first call. The resource group, by
// contrast, may be empty: cluster-scoped calls then resolve it from the
// cluster's own ARM ID via a subscription-wide list (the AKS counterpart to
// the gcp provider's all-locations resolution).
var ErrClientRequired = errors.New("azure provider: aks client is required")
