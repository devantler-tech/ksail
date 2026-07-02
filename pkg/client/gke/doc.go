// Package gke provides a thin, mockable client for Google Kubernetes Engine
// cluster lifecycle operations, wrapping the official native Go SDK
// (cloud.google.com/go/container). It is the GKE counterpart to the eksctl
// client: everything above it (infra provider, provisioner, factory routing)
// talks to GKE exclusively through this package.
//
// GKE's cluster mutations are asynchronous: the API returns a long-running
// operation that must be polled to completion. CreateCluster and DeleteCluster
// hide that mechanic — they block until the operation reaches DONE (honouring
// context cancellation) and surface any operation error.
//
// Authentication uses the SDK's Application Default Credentials chain; this
// package adds no credential handling of its own.
package gke
