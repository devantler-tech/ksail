package gcp

import "errors"

var (
	// ErrClientRequired is returned when NewProvider is called without a GKE
	// client, so callers fail fast instead of getting
	// provider.ErrProviderUnavailable at the first call.
	ErrClientRequired = errors.New("gcp provider: gke client is required")

	// ErrProjectRequired is returned when NewProvider is called without a
	// Google Cloud project ID — every GKE API call is project-scoped, so an
	// empty project can never succeed.
	ErrProjectRequired = errors.New("gcp provider: google cloud project is required")
)
