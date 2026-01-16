package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/cilium"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// ErrUnsupportedCNI is returned when an unsupported CNI type is encountered.
var ErrUnsupportedCNI = errors.New("unsupported CNI type")

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
	tmr.NewStage()

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

	ciliumInst := ciliuminstaller.NewCiliumInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		clusterCfg.Spec.Cluster.Distribution,
	)

	return runCNIInstallation(cmd, ciliumInst, "cilium", tmr)
}

func installCalicoCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	setup, err := prepareCNISetup(cmd, clusterCfg, "Calico")
	if err != nil {
		return err
	}

	setup.timeout = installer.MaxTimeout(setup.timeout, installer.CalicoInstallTimeout)

	calicoInst := calicoinstaller.NewCalicoInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		clusterCfg.Spec.Cluster.Distribution,
	)

	return runCNIInstallation(cmd, calicoInst, "calico", tmr)
}

func runCNIInstallation(
	cmd *cobra.Command,
	inst cniInstaller,
	cniName string,
	tmr timer.Timer,
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

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cni installed",
		Timer:   helpers.MaybeTimer(cmd, tmr),
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
