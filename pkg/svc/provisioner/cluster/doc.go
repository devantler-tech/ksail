// Package clusterprovisioner provides cluster provisioning for KSail distributions.
//
// # Architecture
//
// Provisioners handle distribution-specific Kubernetes configuration while
// delegating infrastructure operations to Providers (pkg/svc/provider):
//
//   - Providers: Create/manage nodes (Docker containers, cloud VMs)
//   - Provisioners: Configure distributions (Kind, K3s, Talos)
//
// # Supported Distributions
//
//   - Vanilla: Uses Kind SDK for vanilla Kubernetes on Docker
//   - K3s: Uses K3d CLI for K3s on Docker
//   - Talos: Uses Talos SDK for Talos on Docker
package clusterprovisioner
