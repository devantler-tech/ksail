package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create/registrystage"
	"github.com/devantler-tech/ksail/v5/pkg/cli/docker"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/cilium"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
	fluxResourcesActivity       = "applying custom resources"
	argoCDResourcesActivity     = "configuring argocd resources"
)

// registryStageInfo contains display information for a registry stage.
// Used by both local registry and mirror registry stages.
type registryStageInfo struct {
	title         string
	emoji         string
	activity      string
	success       string
	failurePrefix string
}

// ErrUnsupportedCNI is returned when an unsupported CNI type is encountered.
var ErrUnsupportedCNI = errors.New("unsupported CNI type")

// Test injection for installer factories, docker client invoker, and cluster provisioner factory.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverrideMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverride *create.InstallerFactories
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvokerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryOverride clusterprovisioner.Factory
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvoker = docker.WithClient
)

// getInstallerFactories returns the installer factories to use, allowing test override.
func getInstallerFactories() *create.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return create.DefaultInstallerFactories()
}

// newCreateLifecycleConfig creates the lifecycle configuration for cluster creation.
func newCreateLifecycleConfig() lifecycle.Config {
	return lifecycle.Config{
		TitleEmoji:         "🚀",
		TitleContent:       "Create cluster...",
		ActivityContent:    "creating cluster",
		SuccessContent:     "cluster created",
		ErrorMessagePrefix: "failed to create cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.ClusterProvisioner, clusterName string) error {
			return provisioner.Create(ctx, clusterName)
		},
	}
}

// NewCreateCmd wires the cluster create command using the shared runtime container.
func NewCreateCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a cluster",
		Long:         `Create a Kubernetes cluster as defined by configuration.`,
		SilenceUsage: true,
	}

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCSIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.ControlPlanesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.WorkersFieldSelector())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io)")
	_ = cfgManager.Viper.BindPFlag("mirror-registry", cmd.Flags().Lookup("mirror-registry"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleCreateRunE)

	return cmd
}

// handleCreateRunE executes cluster creation with mirror registry setup and CNI installation.
//
//nolint:funlen // Orchestrates full cluster creation lifecycle with multiple stages.
func handleCreateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	clusterCfg, kindConfig, k3dConfig, talosConfig, err := loadClusterConfiguration(
		cfgManager,
		outputTimer,
	)
	if err != nil {
		return err
	}

	firstActivityShown := false

	err = ensureLocalRegistriesReady(
		cmd,
		clusterCfg,
		deps,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		&firstActivityShown,
	)
	if err != nil {
		return err
	}

	setupK3dMetricsServer(clusterCfg, k3dConfig)

	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		deps.Factory = factoryOverride
	} else {
		deps.Factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: &clusterprovisioner.DistributionConfig{
				Kind:  kindConfig,
				K3d:   k3dConfig,
				Talos: talosConfig,
			},
		}
	}

	err = executeClusterLifecycle(cmd, clusterCfg, deps, &firstActivityShown)
	if err != nil {
		return err
	}

	connectMirrorRegistriesWithWarning(
		cmd,
		clusterCfg,
		deps,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		&firstActivityShown,
	)

	err = executeLocalRegistryStage(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		talosConfig,
		localRegistryStageConnect,
		&firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to connect local registry: %w", err)
	}

	return handlePostCreationSetup(cmd, clusterCfg, deps.Timer, &firstActivityShown)
}

func loadClusterConfiguration(
	cfgManager *ksailconfigmanager.ConfigManager,
	tmr timer.Timer,
) (*v1alpha1.Cluster, *v1alpha4.Cluster, *v1alpha5.SimpleConfig, *talosconfigmanager.Configs, error) {
	clusterCfg, err := cfgManager.LoadConfig(tmr)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	distConfig := cfgManager.DistributionConfig

	return clusterCfg, distConfig.Kind, distConfig.K3d, distConfig.Talos, nil
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	firstActivityShown *bool,
) error {
	err := executeLocalRegistryStage(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		talosConfig,
		localRegistryStageProvision,
		firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to provision local registry: %w", err)
	}

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err = registrystage.SetupMirrorRegistries(registrystage.StageParams{
		Cmd:                cmd,
		ClusterCfg:         clusterCfg,
		Deps:               deps,
		CfgManager:         cfgManager,
		KindConfig:         kindConfig,
		K3dConfig:          k3dConfig,
		TalosConfig:        talosConfig,
		FirstActivityShown: firstActivityShown,
		DockerInvoker:      invoker,
	})
	if err != nil {
		return fmt.Errorf("failed to setup mirror registries: %w", err)
	}

	return nil
}

func executeClusterLifecycle(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	firstActivityShown *bool,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

	err := lifecycle.RunWithConfig(cmd, deps, newCreateLifecycleConfig(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to execute cluster lifecycle: %w", err)
	}

	return nil
}

func connectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	firstActivityShown *bool,
) {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := registrystage.ConnectRegistriesToClusterNetwork(registrystage.StageParams{
		Cmd:                cmd,
		ClusterCfg:         clusterCfg,
		Deps:               deps,
		CfgManager:         cfgManager,
		KindConfig:         kindConfig,
		K3dConfig:          k3dConfig,
		TalosConfig:        talosConfig,
		FirstActivityShown: firstActivityShown,
		DockerInvoker:      invoker,
	})
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to connect registries to cluster network: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// handlePostCreationSetup installs CNI, CSI, cert-manager, metrics-server, and GitOps engines.
func handlePostCreationSetup(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	switch clusterCfg.Spec.Cluster.CNI {
	case v1alpha1.CNICilium:
		err := installCNIOnly(cmd, clusterCfg, tmr, installCiliumCNI, firstActivityShown)
		if err != nil {
			return err
		}
	case v1alpha1.CNICalico:
		err := installCNIOnly(cmd, clusterCfg, tmr, installCalicoCNI, firstActivityShown)
		if err != nil {
			return err
		}
	case v1alpha1.CNIDefault, "":
		// No custom CNI to install
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedCNI, clusterCfg.Spec.Cluster.CNI)
	}

	return installPostCNIComponentsParallel(cmd, clusterCfg, tmr, firstActivityShown)
}

func installCNIOnly(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	installFunc func(*cobra.Command, *v1alpha1.Cluster, timer.Timer) error,
	firstActivityShown *bool,
) error {
	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	tmr.NewStage()

	return installFunc(cmd, clusterCfg, tmr)
}

// installPostCNIComponentsParallel installs all post-CNI components in parallel.
//
//nolint:funlen,cyclop,gocognit // Orchestrates multiple parallel installations with proper error handling
func installPostCNIComponentsParallel(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	needsMetricsServer := create.NeedsMetricsServerInstall(clusterCfg)
	needsCSI := clusterCfg.Spec.Cluster.CSI == v1alpha1.CSILocalPathStorage
	needsCertManager := clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled
	needsArgoCD := clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD
	needsFlux := clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux

	componentCount := 0
	if needsMetricsServer {
		componentCount++
	}

	if needsCSI {
		componentCount++
	}

	if needsCertManager {
		componentCount++
	}

	if needsArgoCD {
		componentCount++
	}

	if needsFlux {
		componentCount++
	}

	if componentCount == 0 {
		return nil
	}

	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		gitOpsKubeconfig    string
		gitOpsKubeconfigErr error
	)

	factories := getInstallerFactories()

	if needsArgoCD || needsFlux {
		_, gitOpsKubeconfig, gitOpsKubeconfigErr = factories.HelmClientFactory(clusterCfg)
		if gitOpsKubeconfigErr != nil {
			return fmt.Errorf("failed to create helm client for gitops: %w", gitOpsKubeconfigErr)
		}
	}

	var tasks []notify.ProgressTask

	if needsMetricsServer {
		tasks = append(tasks, notify.ProgressTask{
			Name: "metrics-server",
			Fn: func(taskCtx context.Context) error {
				return create.InstallMetricsServerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsCSI {
		tasks = append(tasks, notify.ProgressTask{
			Name: "csi",
			Fn: func(taskCtx context.Context) error {
				return create.InstallCSISilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsCertManager {
		tasks = append(tasks, notify.ProgressTask{
			Name: "cert-manager",
			Fn: func(taskCtx context.Context) error {
				return create.InstallCertManagerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsArgoCD {
		tasks = append(tasks, notify.ProgressTask{
			Name: "argocd",
			Fn: func(taskCtx context.Context) error {
				return create.InstallArgoCDSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsFlux {
		tasks = append(tasks, notify.ProgressTask{
			Name: "flux",
			Fn: func(taskCtx context.Context) error {
				return create.InstallFluxSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	progressGroup := notify.NewProgressGroup(
		"Installing components",
		"📦",
		cmd.OutOrStdout(),
		notify.WithLabels(notify.InstallingLabels()),
		notify.WithTimer(flags.MaybeTimer(cmd, tmr)),
	)

	executeErr := progressGroup.Run(ctx, tasks...)
	if executeErr != nil {
		return fmt.Errorf("failed to execute parallel component installation: %w", executeErr)
	}

	// Post-install GitOps configuration
	if needsArgoCD {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: argoCDResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureArgoCDResources(ctx, gitOpsKubeconfig, clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to configure Argo CD resources: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
			Writer:  cmd.OutOrStdout(),
		})
	}

	if needsFlux {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: fluxResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureFluxResources(ctx, gitOpsKubeconfig, clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to configure Flux resources: %w", err)
		}
	}

	return nil
}

func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3d || k3dConfig == nil {
		return
	}

	if clusterCfg.Spec.Cluster.MetricsServer != v1alpha1.MetricsServerDisabled {
		return
	}

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == k3sDisableMetricsServerFlag {
			return
		}
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         k3sDisableMetricsServerFlag,
			NodeFilters: []string{"server:*"},
		},
	)
}

// CNI installation functions

type cniInstaller interface {
	Install(ctx context.Context) error
	WaitForReadiness(ctx context.Context) error
}

// cniSetupResult contains common resources prepared for CNI installation.
type cniSetupResult struct {
	helmClient helm.Interface
	kubeconfig string
	timeout    time.Duration
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

	helmClient, kubeconfig, err := create.HelmClientForCluster(clusterCfg)
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

	err = setup.helmClient.AddRepository(cmd.Context(), &helm.RepositoryEntry{
		Name: "cilium",
		URL:  "https://helm.cilium.io/",
	})
	if err != nil {
		return fmt.Errorf("failed to add Cilium Helm repository: %w", err)
	}

	var distribution ciliuminstaller.Distribution

	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionTalos:
		distribution = ciliuminstaller.DistributionTalos
	case v1alpha1.DistributionKind:
		distribution = ciliuminstaller.DistributionKind
	case v1alpha1.DistributionK3d:
		distribution = ciliuminstaller.DistributionK3d
	}

	ciliumInst := ciliuminstaller.NewCiliumInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		distribution,
	)

	return runCNIInstallation(cmd, ciliumInst, "cilium", tmr)
}

func installCalicoCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	setup, err := prepareCNISetup(cmd, clusterCfg, "Calico")
	if err != nil {
		return err
	}

	var distribution calicoinstaller.Distribution

	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionTalos:
		distribution = calicoinstaller.DistributionTalos
	case v1alpha1.DistributionKind:
		distribution = calicoinstaller.DistributionKind
	case v1alpha1.DistributionK3d:
		distribution = calicoinstaller.DistributionK3d
	}

	calicoInst := calicoinstaller.NewCalicoInstallerWithDistribution(
		setup.helmClient,
		setup.kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		setup.timeout,
		distribution,
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
		Type:    notify.ActivityType,
		Content: "awaiting " + strings.ToLower(cniName) + " to be ready",
		Writer:  cmd.OutOrStdout(),
	})

	err = inst.WaitForReadiness(cmd.Context())
	if err != nil {
		return fmt.Errorf("%s readiness check failed: %w", cniName, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cni installed",
		Timer:   flags.MaybeTimer(cmd, tmr),
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// Test injection functions

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	override.CertManager = factory
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()
		installerFactoriesOverride = previous
		installerFactoriesOverrideMu.Unlock()
	}
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	override.CSI = factory
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()
		installerFactoriesOverride = previous
		installerFactoriesOverrideMu.Unlock()
	}
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	override.ArgoCD = factory
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()
		installerFactoriesOverride = previous
		installerFactoriesOverrideMu.Unlock()
	}
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	override.EnsureArgoCDResources = fn
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()
		installerFactoriesOverride = previous
		installerFactoriesOverrideMu.Unlock()
	}
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
func SetDockerClientInvokerForTests(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	dockerClientInvokerMu.Lock()

	previous := dockerClientInvoker
	dockerClientInvoker = invoker

	dockerClientInvokerMu.Unlock()

	return func() {
		dockerClientInvokerMu.Lock()

		dockerClientInvoker = previous

		dockerClientInvokerMu.Unlock()
	}
}

// SetClusterProvisionerFactoryForTests overrides the cluster provisioner factory for testing.
func SetClusterProvisionerFactoryForTests(factory clusterprovisioner.Factory) func() {
	clusterProvisionerFactoryMu.Lock()

	previous := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryOverride = factory

	clusterProvisionerFactoryMu.Unlock()

	return func() {
		clusterProvisionerFactoryMu.Lock()

		clusterProvisionerFactoryOverride = previous

		clusterProvisionerFactoryMu.Unlock()
	}
}

// runRegistryStage executes a registry stage with proper lifecycle management.
// Used by local registry and mirror registry stages.
func runRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info registryStageInfo,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: info.title,
		Emoji:   info.emoji,
		Writer:  cmd.OutOrStdout(),
	})

	if info.activity != "" {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: info.activity,
			Writer:  cmd.OutOrStdout(),
		})
	}

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := invoker(cmd, func(dockerClient client.APIClient) error {
		err := action(cmd.Context(), dockerClient)
		if err != nil {
			return fmt.Errorf("%s: %w", info.failurePrefix, err)
		}

		outputTimer := flags.MaybeTimer(cmd, deps.Timer)

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: info.success,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to execute registry stage: %w", err)
	}

	return nil
}
