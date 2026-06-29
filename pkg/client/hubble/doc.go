// Package hubble provides a thin client for Cilium Hubble's flow-observation
// API. It exposes a small [FlowObserver] seam over the Hubble Relay gRPC
// service together with pure helpers to filter and render the observed flows,
// so that command code stays testable without a live cluster.
package hubble
