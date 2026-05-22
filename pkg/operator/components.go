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
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// componentInstallTimeout bounds each component's Helm install.
const componentInstallTimeout = 5 * time.Minute

// installOrder lists component installers in the order they must run: CNI first (pods cannot
// schedule without networking), then infrastructure add-ons, then GitOps last. Keys match the
// installer factory's map keys (pkg/svc/installer).
//
//nolint:gochecknoglobals // immutable ordered lookup table for component install ordering
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
// Child access is obtained through the provisioner's optional clusterprovisioner.Connector
// capability: provisioners that can hand back an operator-reachable kubeconfig (today: VCluster on
// the Kubernetes provider) get their components installed; others are a no-op until they implement
// Connector (see design/operator-component-lifecycle.md).
func InstallComponents(
	ctx context.Context,
	provisioner clusterprovisioner.Provisioner,
	cluster *v1alpha1.Cluster,
) error {
	log := logf.FromContext(ctx)

	connector, ok := provisioner.(clusterprovisioner.Connector)
	if !ok {
		log.Info(
			"skipping component install: distribution has no operator-reachable kubeconfig",
			"distribution", cluster.Spec.Cluster.Distribution,
			"provider", cluster.Spec.Cluster.Provider,
		)

		return nil
	}

	raw, err := connector.Kubeconfig(ctx, controller.ProvisionedName(cluster))
	if err != nil {
		return fmt.Errorf("get child cluster kubeconfig: %w", err)
	}

	kubeconfigPath, cleanup, err := writeTempKubeconfig(raw)
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
		nil, // no Docker client: child-cluster components are Helm-based
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

// writeTempKubeconfig writes the child kubeconfig bytes to a temp file and returns its path plus a
// cleanup that removes it.
func writeTempKubeconfig(raw []byte) (string, func(), error) {
	file, err := os.CreateTemp("", "ksail-child-*.kubeconfig")
	if err != nil {
		return "", nil, fmt.Errorf("create temp kubeconfig: %w", err)
	}

	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }

	_, writeErr := file.Write(raw)
	if writeErr != nil {
		_ = file.Close()

		cleanup()

		return "", nil, fmt.Errorf("write child kubeconfig: %w", writeErr)
	}

	closeErr := file.Close()
	if closeErr != nil {
		cleanup()

		return "", nil, fmt.Errorf("close child kubeconfig: %w", closeErr)
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

		err := component.Install(ctx)
		if err != nil {
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
