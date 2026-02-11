// Package k8s provides Kubernetes client configuration and general-purpose utilities.
//
// This package offers reusable utilities for working with Kubernetes clusters,
// including REST client configuration, kubeconfig management, DNS label
// sanitization, and label extraction.
//
// For resource readiness polling, see the [readiness] sub-package.
//
// Key features:
//   - REST config building from kubeconfig files (BuildRESTConfig, GetRESTConfig)
//   - Clientset creation (NewClientset)
//   - Kubeconfig cleanup (CleanupKubeconfig)
//   - DNS label sanitization (SanitizeToDNSLabel)
//   - Label value extraction (UniqueLabelValues)
package k8s
