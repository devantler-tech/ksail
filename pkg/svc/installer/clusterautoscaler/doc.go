// Package clusterautoscalerinstaller provides installation of the Kubernetes Cluster Autoscaler
// on Talos clusters running on Hetzner Cloud.
//
// The Cluster Autoscaler automatically adjusts the number of worker nodes in
// Hetzner node pools based on pending pod demand and idle node utilization.
// It communicates with Hetzner Cloud via the hcloud API token (from the
// shared "hcloud" secret in kube-system) and uses a cloud-init configuration
// stored in the "cluster-autoscaler-config" secret (created by the Talos
// provisioner) to provision new nodes.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set (used by the hcloud-ccm installer
//     which creates the shared "hcloud" secret)
//   - The "cluster-autoscaler-config" secret must exist in kube-system (created by
//     the Talos provisioner's ApplyAutoscalerConfigSecret)
//   - spec.cluster.autoscaler.node.enabled must be Enabled
//   - Distribution must be Talos and Provider must be Hetzner
package clusterautoscalerinstaller
