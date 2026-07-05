package aksprovisioner

import "errors"

// ErrClientRequired is returned when an AKS client is not supplied.
var ErrClientRequired = errors.New("aks client is required")

// ErrNameRequired is returned by Create when neither the call nor the
// provisioner configuration carries a cluster name — ARM addresses a managed
// cluster by name, so an empty name can never succeed.
var ErrNameRequired = errors.New("aks cluster name is required")

// ErrClusterSpecRequired is returned by Create when no declarative cluster
// spec is supplied. Create is driven from the same declarative source of
// truth as the rest of ksail; there is no imperative flag fallback.
var ErrClusterSpecRequired = errors.New("aks cluster spec is required")

// ErrResourceGroupRequired is returned by Create when no resource group is
// configured. Reads can resolve a cluster's resource group from its ARM ID
// via a subscription-wide list, but a create has nothing to resolve from —
// the caller must say where the cluster goes.
var ErrResourceGroupRequired = errors.New("azure resource group is required")

// ErrClusterNotFound is returned when a cluster's resource group cannot be
// resolved because no cluster with that name exists in the subscription.
var ErrClusterNotFound = errors.New("aks cluster not found")
