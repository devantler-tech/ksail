// Package readiness provides Kubernetes resource readiness polling utilities.
//
// This package offers reusable utilities for waiting until Kubernetes resources
// become ready. It supports deployments, daemonsets, nodes, and the API server,
// and provides a generic polling mechanism that can be extended.
//
// Key features:
//   - Generic polling mechanism (PollForReadiness)
//   - Deployment readiness polling (WaitForDeploymentReady)
//   - DaemonSet readiness polling (WaitForDaemonSetReady)
//   - Node readiness polling (WaitForNodeReady)
//   - API server readiness and stability polling (WaitForAPIServerReady, WaitForAPIServerStable)
//   - Multi-resource coordination (WaitForMultipleResources)
package readiness
