// Package k8s provides Kubernetes utilities for resource management and readiness checking.
//
// This package offers reusable utilities for working with Kubernetes resources,
// including REST client configuration, resource readiness polling, and multi-resource
// coordination. It is designed to be used across different parts of the application
// that interact with Kubernetes clusters.
//
// Key features:
//   - REST config building from kubeconfig files (BuildRESTConfig)
//   - Deployment readiness polling (WaitForDeploymentReady)
//   - DaemonSet readiness polling (WaitForDaemonSetReady)
//   - Multi-resource coordination (WaitForMultipleResources)
//   - Flexible polling mechanism (PollForReadiness)
package k8s
