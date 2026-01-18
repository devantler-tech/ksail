// Package talos provides configuration management for Talos cluster patches.
//
// Unlike Kind and K3d which load configuration from a single YAML file,
// Talos configuration is composed of patches from multiple directories:
//   - talos/cluster/ - patches applied to all nodes
//   - talos/control-planes/ - patches applied only to control-plane nodes
//   - talos/workers/ - patches applied only to worker nodes
//
// The ConfigManager loads these patches, validates them, and creates a
// Configs object that wraps the upstream Talos SDK's bundle.Bundle.
// This provides programmatic access to the merged machine configurations
// for both control-plane and worker nodes.
//
// Usage:
//
//	manager := talos.NewConfigManager("talos", "my-cluster", "1.32.0", "10.5.0.0/24")
//	configs, err := manager.Load(configmanager.LoadOptions{})
//	if err != nil {
//	    return err
//	}
//
//	// Access control-plane config
//	cpConfig := configs.ControlPlane()
//	cniName := cpConfig.Cluster().Network().CNI().Name()
//
//	// Access worker config
//	workerConfig := configs.Worker()
package talos
