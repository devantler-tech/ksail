package v1alpha1

// validDistributions returns supported distribution values.
func validDistributions() []Distribution {
	return []Distribution{DistributionK3d, DistributionKind}
}

// validGitOpsEngines enumerates supported GitOps engine values.
func validGitOpsEngines() []GitOpsEngine {
	return []GitOpsEngine{
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	}
}

// validCNIs returns supported CNI values.
func validCNIs() []CNI {
	return []CNI{CNIDefault, CNICilium, CNICalico}
}

// validCSIs returns supported CSI values.
func validCSIs() []CSI {
	return []CSI{CSIDefault, CSILocalPathStorage}
}

// validMetricsServers returns supported metrics server values.
func validMetricsServers() []MetricsServer {
	return []MetricsServer{
		MetricsServerEnabled,
		MetricsServerDisabled,
	}
}

// validCertManagers returns supported cert-manager values.
func validCertManagers() []CertManager {
	return []CertManager{
		CertManagerEnabled,
		CertManagerDisabled,
	}
}

// validLocalRegistryModes returns supported local registry configuration modes.
func validLocalRegistryModes() []LocalRegistry {
	return []LocalRegistry{LocalRegistryEnabled, LocalRegistryDisabled}
}

// validValidateWorkloadOnCreate returns supported validate workload on create values.
func validValidateWorkloadOnCreate() []ValidateWorkloadOnCreate {
	return []ValidateWorkloadOnCreate{
		ValidateWorkloadOnCreateEnabled,
		ValidateWorkloadOnCreateDisabled,
	}
}
