package kindprovisioner

// KubeConfigForTest returns the kubeConfig field for testing purposes.
func (k *Provisioner) KubeConfigForTest() string {
	return k.kubeConfig
}
