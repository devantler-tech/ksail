//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package k3dprovisioner

import "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"

// ExportSimpleCfg exposes the provisioner's simpleCfg for testing.
func (p *Provisioner) ExportSimpleCfg() *v1alpha5.SimpleConfig {
	return p.simpleCfg
}

// ExportConfigPath exposes the provisioner's configPath for testing.
func (p *Provisioner) ExportConfigPath() string {
	return p.configPath
}

// ParseClusterNodesForTest exposes parseClusterNodes for unit testing.
var ParseClusterNodesForTest = parseClusterNodes
