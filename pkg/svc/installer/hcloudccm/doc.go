// Package hcloudccminstaller provides installation of the Hetzner Cloud Controller Manager.
//
// The cloud controller manager enables LoadBalancer services on Hetzner Cloud clusters
// by automatically provisioning and managing Hetzner Load Balancers.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - The token requires read/write permissions for Load Balancers
//
// The installer creates a Kubernetes secret with the API token and deploys the
// cloud controller manager via its Helm chart. The secret is shared with the
// Hetzner CSI driver if both components are installed.
package hcloudccminstaller
