package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/cilium"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
)

// ErrUnsupportedCNI is returned when an unsupported CNI type is encountered.
var ErrUnsupportedCNI = errors.New("unsupported CNI type")

// ErrCNIReadinessTimeout is returned when nodes fail to become ready after CNI installation.
var ErrCNIReadinessTimeout = errors.New("CNI node readiness timed out")

// cniInstaller provides methods for installing CNI components.
type cniInstaller interface {
	Install(ctx context.Context) error
}

// cniSetupResult contains common resources prepared for CNI installation.
type cniSetupResult struct {
	helmClient helm.Interface
	kubeconfig string
	timeout    time.Duration
}

// InstallCNI installs the configured CNI for the cluster.
// Returns true if a CNI was installed, false if using default/none.
func InstallCNI(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
) (bool, error) {
	switch clusterCfg.Spec.Cluster.CNI {
	case v1alpha1.CNICilium:
		err := installCNIOnly(cmd, clusterCfg, tmr, installCiliumCNI)

		return true, err
	case v1alpha1.CNICalico:
		err := installCNIOnly(cmd, clusterCfg, tmr, installCalicoCNI)

		return true, err
	case v1alpha1.CNIDefault, "":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %s", ErrUnsupportedCNI, clusterCfg.Spec.Cluster.CNI)
	}
}

func installCNIOnly(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	installFunc func(*cobra.Command, *v1alpha1.Cluster, timer.Timer) error,
) error {
	if tmr != nil {
		tmr.NewStage()
	}

	return installFunc(cmd, clusterCfg, tmr)
}

// prepareCNISetup shows the CNI title and prepares common resources for installation.
func prepareCNISetup(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	cniName string,
) (*cniSetupResult, error) {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Install CNI...",
		Emoji:   "🌐",
		Writer:  cmd.OutOrStdout(),
	})

	helmClient, kubeconfig, err := HelmClientForCluster(clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm client for %s: %w", cniName, err)
	}

	return &cniSetupResult{
		helmClient: helmClient,
		kubeconfig: kubeconfig,
		timeout:    installer.GetInstallTimeout(clusterCfg),
	}, nil
}

func installCiliumCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	setup, err := prepareCNISetup(cmd, clusterCfg, "Cilium")
	if err != nil {
		return err
	}

	ciliumInst := ciliuminstaller.NewInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		clusterCfg.Spec.Cluster.Distribution,
	)

	return runCNIInstallation(
		cmd, ciliumInst, "cilium", tmr, setup, clusterCfg, []string{"kube-system"},
	)
}

func installCalicoCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	setup, err := prepareCNISetup(cmd, clusterCfg, "Calico")
	if err != nil {
		return err
	}

	setup.timeout = max(setup.timeout, installer.CalicoInstallTimeout)

	calicoInst := calicoinstaller.NewInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		clusterCfg.Spec.Cluster.Distribution,
	)

	return runCNIInstallation(
		cmd, calicoInst, "calico", tmr, setup, clusterCfg,
		[]string{"tigera-operator", "calico-system"},
	)
}

func runCNIInstallation(
	cmd *cobra.Command,
	inst cniInstaller,
	cniName string,
	tmr timer.Timer,
	setup *cniSetupResult,
	clusterCfg *v1alpha1.Cluster,
	cniNamespaces []string,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "installing " + strings.ToLower(cniName),
		Writer:  cmd.OutOrStdout(),
	})

	err := inst.Install(cmd.Context())
	if err != nil {
		return fmt.Errorf("%s installation failed: %w", cniName, err)
	}

	// Wait for at least one node to become Ready before declaring success.
	// This is critical for CNIs like Calico that use SkipWait (Helm returns
	// before pods are ready), ensuring the network layer is functional
	// before post-CNI components begin installing.
	err = waitForCNIReadiness(cmd.Context(), setup, clusterCfg, cniNamespaces)
	if err != nil {
		return fmt.Errorf("node readiness check after %s install failed: %w", cniName, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cni installed",
		Timer:   flags.MaybeTimer(cmd, tmr),
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// waitForCNIReadiness waits for at least one node to become Ready after CNI installation.
// On timeout, it diagnoses pod failures in the CNI namespaces to provide actionable errors.
func waitForCNIReadiness(
	ctx context.Context,
	setup *cniSetupResult,
	clusterCfg *v1alpha1.Cluster,
	cniNamespaces []string,
) error {
	clientset, err := k8s.NewClientset(
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	err = readiness.WaitForNodeReady(ctx, clientset, setup.timeout)
	if err != nil {
		diag := k8s.DiagnosePodFailures(ctx, clientset, cniNamespaces)
		if diag != "" {
			return fmt.Errorf(
				"%w after %s\n%s\n\nTip: check registry availability and rate limits",
				ErrCNIReadinessTimeout, setup.timeout, diag,
			)
		}

		return fmt.Errorf("wait for node readiness after CNI install: %w", err)
	}

	return nil
}
