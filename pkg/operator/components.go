package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
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
	"metallb", "cloud-provider-kind", "hcloud-ccm", "aws-load-balancer-controller",
	"kyverno", "gatekeeper",
	"cluster-autoscaler",
	"flux", "argocd",
}

// InstallComponents installs the components declared in the Cluster spec (CNI, CSI, metrics-server,
// cert-manager, load-balancer, policy-engine, GitOps) into the provisioned child cluster, reusing
// the same installer factory the CLI uses. It satisfies controller.ComponentInstaller.
//
// Child access is obtained through the provisioner's optional clusterprovisioner.Connector
// capability: provisioners that can hand back an operator-reachable kubeconfig (the nested
// Kubernetes-provider distributions — vCluster, k3k, Talos, Kind, and KWOK — publish one) get their
// components installed; others are a no-op until they implement Connector.
//
// The returned applied is false when installation was skipped (no Connector), so the reconciler can
// report ComponentsReady=Unknown rather than a misleading True. It is true once a Connector exists,
// even if a subsequent step fails (the error then drives the status).
//
// The returned components carry the per-component install outcome (one entry per declared component,
// in install order) so the reconciler can surface per-component health in ClusterStatus. They are nil
// on the skip path and when install never reached the installer set (kubeconfig/helm/factory errors).
//
// Before converging the desired set it uninstalls components the spec previously declared but has
// since dropped (flipped to None/Default) — read from the last-applied-components baseline annotation
// the reconciler records after each successful apply — so a removed component is torn down rather than
// left orphaned. The uninstall is best-effort and folded into the returned error so a failure requeues.
func InstallComponents(
	ctx context.Context,
	provisioner clusterprovisioner.Provisioner,
	cluster *v1alpha1.Cluster,
) (bool, []v1alpha1.ComponentStatus, error) {
	log := logf.FromContext(ctx)

	connector, ok := provisioner.(clusterprovisioner.Connector)
	if !ok {
		log.Info(
			"skipping component install: distribution has no operator-reachable kubeconfig",
			"distribution", cluster.Spec.Cluster.Distribution,
			"provider", cluster.Spec.Cluster.Provider,
		)

		return false, nil, nil
	}

	raw, err := connector.Kubeconfig(ctx, controller.ProvisionedName(cluster))
	if err != nil {
		return true, nil, fmt.Errorf("get child cluster kubeconfig: %w", err)
	}

	kubeconfigPath, cleanup, err := writeTempKubeconfig(raw)
	if err != nil {
		return true, nil, err
	}
	defer cleanup()

	helmClient, err := helm.NewClient(kubeconfigPath, "")
	if err != nil {
		return true, nil, fmt.Errorf("build helm client for child cluster: %w", err)
	}

	// Several installers are distribution-dependent, so a factory is only valid
	// for the spec it was built for; the baseline uninstall set below rebuilds
	// one for the previously-applied distribution rather than reusing this one.
	newFactory := func(distribution v1alpha1.Distribution) *installer.Factory {
		return installer.NewFactory(
			helmClient,
			nil, // no Docker client: child-cluster components are Helm-based
			kubeconfigPath,
			"",
			componentInstallTimeout,
			distribution,
			// The AWS Load Balancer Controller chart requires the provisioned
			// EKS cluster name; harmless for every other distribution.
			installer.WithEKSClusterName(controller.ProvisionedName(cluster)),
		)
	}

	installers, err := newFactory(cluster.Spec.Cluster.Distribution).
		CreateInstallersForConfig(cluster)
	if err != nil {
		return true, nil, fmt.Errorf("build installers: %w", err)
	}

	uninstallErr := uninstallRemovedComponents(ctx, newFactory, cluster, installers)

	components, installErr := runInstallers(ctx, installers)

	return true, components, errors.Join(uninstallErr, installErr)
}

// uninstallRemovedComponents tears down components present in the last-applied-components baseline but
// absent from the desired set (the spec flipped them to None/Default). A malformed or unbuildable
// baseline must never block converging the desired set, so a failure to *determine* what to remove is
// logged and swallowed (the next reconcile retries); only a failure to actually *uninstall* a resolved
// component is returned, so it surfaces as ComponentsReady=False and requeues.
func uninstallRemovedComponents(
	ctx context.Context,
	newFactory func(v1alpha1.Distribution) *installer.Factory,
	cluster *v1alpha1.Cluster,
	desired map[string]installer.Installer,
) error {
	removed, err := removedComponentInstallers(newFactory, cluster, desired)
	if err != nil {
		logf.FromContext(ctx).
			Info("skip uninstall of removed components (best-effort)", "error", err.Error())

		return nil
	}

	return runUninstallers(ctx, removed)
}

// removedComponentInstallers returns the installers for components that were in the last-applied
// component set but are no longer desired. It rebuilds the previous installer set from the baseline
// spec via a factory built FOR that baseline's distribution (several installers are
// distribution-dependent, so reusing the current cluster's factory would compute the wrong uninstall
// set after a distribution change), then keeps the entries whose key is absent from the desired set.
// It returns an empty map when no baseline is recorded (first reconcile, or an unparseable annotation).
func removedComponentInstallers(
	newFactory func(v1alpha1.Distribution) *installer.Factory,
	cluster *v1alpha1.Cluster,
	desired map[string]installer.Installer,
) (map[string]installer.Installer, error) {
	previousSpec, ok := lastAppliedComponentsSpec(cluster)
	if !ok {
		return map[string]installer.Installer{}, nil
	}

	previousCluster := &v1alpha1.Cluster{Spec: *previousSpec}

	previous, err := newFactory(previousSpec.Cluster.Distribution).
		CreateInstallersForConfig(previousCluster)
	if err != nil {
		return nil, fmt.Errorf("build previous installers: %w", err)
	}

	removed := make(map[string]installer.Installer)

	for key, inst := range previous {
		_, stillDesired := desired[key]
		if !stillDesired {
			removed[key] = inst
		}
	}

	return removed, nil
}

// lastAppliedComponentsSpec parses the component-baseline annotation into a spec. The second return
// is false when no baseline is recorded or the annotation is not valid JSON (treated as "no baseline"
// so a corrupt value can never block a fresh apply).
func lastAppliedComponentsSpec(cluster *v1alpha1.Cluster) (*v1alpha1.Spec, bool) {
	raw, ok := cluster.Annotations[v1alpha1.LastAppliedComponentsAnnotation]
	if !ok || raw == "" {
		return nil, false
	}

	var spec v1alpha1.Spec

	err := json.Unmarshal([]byte(raw), &spec)
	if err != nil {
		return nil, false
	}

	return &spec, true
}

// runUninstallers uninstalls the given components in REVERSE dependency order (GitOps first, CNI last
// — the inverse of runInstallers), continuing past individual failures and returning their joined
// error so one stuck uninstall does not block the rest.
func runUninstallers(ctx context.Context, installers map[string]installer.Installer) error {
	log := logf.FromContext(ctx)

	var errs []error

	done := make(map[string]bool, len(installers))

	uninstall := func(key string, component installer.Installer) {
		log.Info("uninstalling removed component", "component", key)

		err := component.Uninstall(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}

		done[key] = true
	}

	// Reverse dependency order: tear down GitOps first, CNI last.
	for _, key := range slices.Backward(installOrder) {
		component, ok := installers[key]
		if ok {
			uninstall(key, component)
		}
	}

	// Uninstall any component not covered by the known order after the ordered ones.
	for key, component := range installers {
		if !done[key] {
			uninstall(key, component)
		}
	}

	return errors.Join(errs...)
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
// rest. It also returns a per-component status (one entry per installer, in install order) so the
// reconciler can report per-component health: a failed Install yields ComponentStateFailed with the
// error as its message, a successful one yields ComponentStateReady.
func runInstallers(
	ctx context.Context,
	installers map[string]installer.Installer,
) ([]v1alpha1.ComponentStatus, error) {
	log := logf.FromContext(ctx)

	var errs []error

	statuses := make([]v1alpha1.ComponentStatus, 0, len(installers))
	done := make(map[string]bool, len(installers))

	install := func(key string, component installer.Installer) {
		log.Info("installing component", "component", key)

		status := v1alpha1.ComponentStatus{Name: key, State: v1alpha1.ComponentStateReady}

		err := component.Install(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			status.State = v1alpha1.ComponentStateFailed
			status.Message = err.Error()
		}

		statuses = append(statuses, status)
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

	return statuses, errors.Join(errs...)
}
