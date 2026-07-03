package gkeprovisioner

import "errors"

// ErrClientRequired is returned when a GKE client is not supplied.
var ErrClientRequired = errors.New("gke client is required")

// ErrProjectRequired is returned when no Google Cloud project ID is supplied —
// every GKE API call is project-scoped, so an empty project can never succeed.
var ErrProjectRequired = errors.New("google cloud project is required")

// ErrClusterSpecRequired is returned by Create when no declarative cluster
// spec is supplied. Create is driven from the same declarative source of
// truth as the rest of ksail; there is no imperative flag fallback.
var ErrClusterSpecRequired = errors.New("gke cluster spec is required")

// ErrLocationRequired is returned by Create when no concrete GKE location is
// configured. Reads can resolve a cluster's location via the all-locations
// list, but a create has nothing to resolve from — the caller must say where
// the cluster goes.
var ErrLocationRequired = errors.New("gke location is required")

// ErrClusterNotFound is returned when a cluster's location cannot be resolved
// because no cluster with that name exists in the project.
var ErrClusterNotFound = errors.New("gke cluster not found")
