// Package vcluster provides default configuration for vCluster clusters.
//
// This package embeds a Dockerfile containing the default Kubernetes image
// reference for vCluster. The Dockerfile is scanned by Dependabot for
// automated version updates. The parsed version is exported as
// DefaultKubernetesVersion for use by the provisioner and scaffolder.
package vcluster
