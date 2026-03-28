package ciliuminstaller

// SetGatewayAPICRDInstaller overrides the Gateway API CRD installer function for testing.
func (c *Installer) SetGatewayAPICRDInstaller(fn GatewayAPICRDInstallerFunc) {
	c.gatewayAPICRDInstaller = fn
}
