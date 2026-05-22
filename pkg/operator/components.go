package operator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// componentInstallTimeout bounds each component's Helm install.
const componentInstallTimeout = 5 * time.Minute

// errChildKubeconfigNotReady is returned when the child cluster's kubeconfig Secret is not yet
// published, so the caller can retry on a later reconcile.
var errChildKubeconfigNotReady = errors.New("child cluster kubeconfig not ready")

// installOrder lists component installers in the order they must run: CNI first (pods cannot
// schedule without networking), then infrastructure add-ons, then GitOps last. Keys match the
// installer factory's map keys (pkg/svc/installer).
var installOrder = []string{
	"cilium", "calico",
	"cert-manager",
	"local-path-storage", "hetzner-csi",
	"metrics-server", "kubelet-csr-approver",
	"metallb", "cloud-provider-kind", "hcloud-ccm",
	"kyverno", "gatekeeper",
	"cluster-autoscaler",
	"flux", "argocd",
}

// InstallComponents installs the components declared in the Cluster spec (CNI, CSI, metrics-server,
// cert-manager, load-balancer, policy-engine, GitOps) into the provisioned child cluster, reusing
// the same installer factory the CLI uses. It satisfies controller.ComponentInstaller.
//
// Phase 1 supports the VCluster + Kubernetes-provider path only, where the child kubeconfig is
// published as a Secret in the hub. Other distributions are a no-op until the provisioner gains a
// Connector interface (see design/operator-component-lifecycle.md).
func InstallComponents(ctx context.Context, hub client.Reader, cluster *v1alpha1.Cluster) error {
	if !componentsSupported(cluster) {
		return nil
	}

	kubeconfigPath, cleanup, err := writeChildKubeconfig(ctx, hub, cluster)
	if err != nil {
		return err
	}
	defer cleanup()

	helmClient, err := helm.NewClient(kubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("build helm client for child cluster: %w", err)
	}

	factory := installer.NewFactory(
		helmClient,
		nil, // no Docker client: vcluster components are Helm-based
		kubeconfigPath,
		"",
		componentInstallTimeout,
		cluster.Spec.Cluster.Distribution,
	)

	installers, err := factory.CreateInstallersForConfig(cluster)
	if err != nil {
		return fmt.Errorf("build installers: %w", err)
	}

	return runInstallers(ctx, installers)
}

// componentsSupported reports whether the operator can install components for this cluster today:
// the VCluster + Kubernetes-provider path, which publishes a child kubeconfig Secret in the hub.
func componentsSupported(cluster *v1alpha1.Cluster) bool {
	return cluster.Spec.Cluster.Distribution == v1alpha1.DistributionVCluster &&
		cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes
}

// writeChildKubeconfig reads the vcluster kubeconfig Secret, rewrites the API server to the
// in-cluster Service address (reachable from the operator), and writes it to a temp file. The
// returned cleanup removes the file.
func writeChildKubeconfig(
	ctx context.Context,
	hub client.Reader,
	cluster *v1alpha1.Cluster,
) (string, func(), error) {
	name := controller.ProvisionedName(cluster)
	namespace := vclusterNamespacePrefix + name

	var secret corev1.Secret

	getErr := hub.Get(
		ctx,
		types.NamespacedName{Namespace: namespace, Name: vclusterSecretPrefix + name},
		&secret,
	)
	if apierrors.IsNotFound(getErr) {
		return "", nil, errChildKubeconfigNotReady
	}

	if getErr != nil {
		return "", nil, fmt.Errorf("get vcluster kubeconfig secret: %w", getErr)
	}

	raw := secret.Data[vclusterKubeconfigKey]
	if len(raw) == 0 {
		return "", nil, errMissingKubeconfig
	}

	config, err := clientcmd.Load(raw)
	if err != nil {
		return "", nil, fmt.Errorf("parse vcluster kubeconfig: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.%s.svc:%d", name, namespace, vclusterAPIPort)
	for _, cl := range config.Clusters {
		cl.Server = endpoint
		cl.TLSServerName = vclusterServerName
	}

	file, err := os.CreateTemp("", "ksail-child-*.kubeconfig")
	if err != nil {
		return "", nil, fmt.Errorf("create temp kubeconfig: %w", err)
	}

	path := file.Name()
	_ = file.Close()

	cleanup := func() { _ = os.Remove(path) }

	writeErr := clientcmd.WriteToFile(*config, path)
	if writeErr != nil {
		cleanup()

		return "", nil, fmt.Errorf("write child kubeconfig: %w", writeErr)
	}

	return path, cleanup, nil
}

// runInstallers installs the components in dependency order (CNI → infra → GitOps), continuing past
// individual failures and returning their joined error so one broken component does not block the
// rest.
func runInstallers(ctx context.Context, installers map[string]installer.Installer) error {
	log := logf.FromContext(ctx)

	var errs []error

	done := make(map[string]bool, len(installers))

	install := func(key string, component installer.Installer) {
		log.Info("installing component", "component", key)

		if err := component.Install(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}

		done[key] = true
	}

	for _, key := range installOrder {
		if component, ok := installers[key]; ok {
			install(key, component)
		}
	}

	// Run any installer not covered by the known order (future additions) after the ordered ones.
	for key, component := range installers {
		if !done[key] {
			install(key, component)
		}
	}

	return errors.Join(errs...)
}
