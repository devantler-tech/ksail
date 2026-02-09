// Package detector detects installed Kubernetes components by querying the
// cluster via Helm release history and Kubernetes API. This is used by the
// update command to build an accurate baseline of the running cluster state,
// replacing client-side state persistence.
package detector

// Helm release names and namespaces for components installed by KSail.
// These must stay in sync with the installer implementations in pkg/svc/installer/.
const (
	// ReleaseCilium is the Helm release name for the Cilium CNI.
	ReleaseCilium = "cilium"
	// NamespaceCilium is the namespace where Cilium is installed.
	NamespaceCilium = "kube-system"
	// ReleaseCalico is the Helm release name for the Calico CNI.
	ReleaseCalico = "calico"
	// NamespaceCalico is the namespace where Calico is installed.
	NamespaceCalico = "tigera-operator"

	// ReleaseHCloudCSI is the Helm release name for the Hetzner Cloud CSI driver.
	ReleaseHCloudCSI = "hcloud-csi"
	// NamespaceHCloudCSI is the namespace where the Hetzner Cloud CSI driver is installed.
	NamespaceHCloudCSI = "kube-system"

	// ReleaseMetricsServer is the Helm release name for the metrics-server.
	ReleaseMetricsServer = "metrics-server"
	// NamespaceMetricsServer is the namespace where the metrics-server is installed.
	NamespaceMetricsServer = "kube-system"

	// ReleaseCertManager is the Helm release name for cert-manager.
	ReleaseCertManager = "cert-manager"
	// NamespaceCertManager is the namespace where cert-manager is installed.
	NamespaceCertManager = "cert-manager"

	// ReleaseKyverno is the Helm release name for the Kyverno policy engine.
	ReleaseKyverno = "kyverno"
	// NamespaceKyverno is the namespace where Kyverno is installed.
	NamespaceKyverno = "kyverno"
	// ReleaseGatekeeper is the Helm release name for the Gatekeeper policy engine.
	ReleaseGatekeeper = "gatekeeper"
	// NamespaceGatekeeper is the namespace where Gatekeeper is installed.
	NamespaceGatekeeper = "gatekeeper-system"

	// ReleaseFluxOperator is the Helm release name for the Flux operator.
	ReleaseFluxOperator = "flux-operator"
	// NamespaceFluxOperator is the namespace where the Flux operator is installed.
	NamespaceFluxOperator = "flux-system"
	// ReleaseArgoCD is the Helm release name for ArgoCD.
	ReleaseArgoCD = "argocd"
	// NamespaceArgoCD is the namespace where ArgoCD is installed.
	NamespaceArgoCD = "argocd"

	// DeploymentLocalPathProvisioner is the Deployment name for local-path-provisioner CSI.
	DeploymentLocalPathProvisioner = "local-path-provisioner"
	// NamespaceLocalPathStorage is the namespace where local-path-provisioner is installed.
	NamespaceLocalPathStorage = "local-path-storage"

	// ContainerCloudProviderKind is the Docker container name for cloud-provider-kind.
	ContainerCloudProviderKind = "ksail-cloud-provider-kind"

	// K3s-specific component detection constants.
	// K3s installs its built-in components in kube-system, not in dedicated namespaces.

	// DeploymentMetricsServerK3s is the Deployment name for the K3s built-in metrics-server.
	DeploymentMetricsServerK3s = "metrics-server"
	// NamespaceKubeSystem is the kube-system namespace used by K3s built-in components.
	NamespaceKubeSystem = "kube-system"
	// DeploymentLocalPathProvisionerK3s is the Deployment name for the K3s built-in local-path-provisioner.
	DeploymentLocalPathProvisionerK3s = "local-path-provisioner"
	// LabelServiceLBK3s is the label applied to DaemonSets created by K3s ServiceLB.
	LabelServiceLBK3s = "svccontroller.k3s.cattle.io/svcname"
)
