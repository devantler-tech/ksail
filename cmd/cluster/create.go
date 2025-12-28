package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	argocdgitops "github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	cmdhelpers "github.com/devantler-tech/ksail/v5/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	argocdinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/argocd"
	certmanagerinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cert-manager"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/cilium"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metrics-server"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const (
	// k3sDisableMetricsServerFlag is the K3s flag to disable metrics-server.
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
	fluxResourcesActivity       = "applying custom resources"
	fluxResourcesSuccess        = "flux installed"
	argoCDResourcesActivity     = "configuring argocd resources"
	argoCDResourcesSuccess      = "argocd installed"
)

// ErrUnsupportedCNI is returned when an unsupported CNI type is encountered.
var ErrUnsupportedCNI = errors.New("unsupported CNI type")

var (
	errCertManagerInstallerFactoryNil = errors.New("cert-manager installer factory is nil")
	errArgoCDInstallerFactoryNil      = errors.New("argocd installer factory is nil")
	errClusterConfigNil               = errors.New("cluster config is nil")
)

// newCreateLifecycleConfig creates the lifecycle configuration for cluster creation.
func newCreateLifecycleConfig() cmdhelpers.LifecycleConfig {
	return cmdhelpers.LifecycleConfig{
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

	// Create field selectors including metrics-server
	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCSIFieldSelector())
	// Unified node count selectors for all distributions (runtime overrides)
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.ControlPlanesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.WorkersFieldSelector())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		fieldSelectors,
	)

	cmd.Flags().
		StringSlice("mirror-registry", []string{},
			"Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io)")
	_ = cfgManager.Viper.BindPFlag("mirror-registry", cmd.Flags().Lookup("mirror-registry"))

	cmd.RunE = cmdhelpers.WrapLifecycleHandler(runtimeContainer, cfgManager, handleCreateRunE)

	return cmd
}

// handleCreateRunE executes cluster creation with mirror registry setup and CNI installation.
//
//nolint:funlen // Function orchestrates multiple lifecycle stages
func handleCreateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps cmdhelpers.LifecycleDeps,
) error {
	deps.Timer.Start()

	outputTimer := cmdhelpers.MaybeTimer(cmd, deps.Timer)

	clusterCfg, kindConfig, k3dConfig, talosConfig, err := loadClusterConfiguration(
		cfgManager,
		outputTimer,
	)
	if err != nil {
		return err
	}

	// Track whether first activity has been shown to manage blank line spacing
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

	// Configure metrics-server for K3d before cluster creation
	setupK3dMetricsServer(clusterCfg, k3dConfig)

	// Configure kubelet cert rotation for Talos when metrics-server is enabled
	setupTalosKubeletCertRotation(cmd.OutOrStdout(), clusterCfg, talosConfig)

	// Check if a test override is set for the factory
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		// Use the test override factory
		deps.Factory = factoryOverride
	} else {
		// Pass pre-loaded distribution configs to the factory to preserve in-memory modifications
		// (e.g., mirror registries, metrics-server flags). This avoids double-loading from disk.
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

	// Use cached distribution config from ConfigManager
	// These are pointers so in-memory modifications will be preserved
	distConfig := cfgManager.DistributionConfig

	return clusterCfg, distConfig.Kind, distConfig.K3d, distConfig.Talos, nil
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
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

	err = setupMirrorRegistries(
		cmd,
		clusterCfg,
		deps,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to setup mirror registries: %w", err)
	}

	return nil
}

func executeClusterLifecycle(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	firstActivityShown *bool,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

	err := cmdhelpers.RunLifecycleWithConfig(cmd, deps, newCreateLifecycleConfig(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to execute cluster lifecycle: %w", err)
	}

	return nil
}

func connectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	firstActivityShown *bool,
) {
	err := connectRegistriesToClusterNetwork(
		cmd,
		clusterCfg,
		deps,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		firstActivityShown,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to connect registries to cluster network: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// handlePostCreationSetup installs CNI, CSI, and metrics-server after cluster creation.
// Order depends on CNI configuration to resolve dependencies.
func handlePostCreationSetup(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	var err error

	// For custom CNI (Cilium or Calico), install CNI first as metrics-server needs networking
	// For default CNI, install metrics-server first as it's independent
	switch clusterCfg.Spec.Cluster.CNI {
	case v1alpha1.CNICilium:
		err = installCustomCNIAndMetrics(cmd, clusterCfg, tmr, installCiliumCNI, firstActivityShown)
	case v1alpha1.CNICalico:
		err = installCustomCNIAndMetrics(cmd, clusterCfg, tmr, installCalicoCNI, firstActivityShown)
	case v1alpha1.CNIDefault, "":
		err = handleMetricsServer(cmd, clusterCfg, tmr, firstActivityShown)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedCNI, clusterCfg.Spec.Cluster.CNI)
	}

	if err != nil {
		return err
	}

	err = installCSIIfConfigured(cmd, clusterCfg, tmr, firstActivityShown)
	if err != nil {
		return err
	}

	err = installCertManagerIfConfigured(cmd, clusterCfg, tmr, firstActivityShown)
	if err != nil {
		return err
	}

	err = installArgoCDIfConfigured(cmd, clusterCfg, tmr, firstActivityShown)
	if err != nil {
		return err
	}

	return installFluxIfConfigured(cmd, clusterCfg, tmr, firstActivityShown)
}

// installCustomCNIAndMetrics installs a custom CNI and then metrics-server.
func installCustomCNIAndMetrics(
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

	err := installFunc(cmd, clusterCfg, tmr)
	if err != nil {
		return err
	}

	// Install metrics-server after CNI is ready
	return handleMetricsServer(cmd, clusterCfg, tmr, firstActivityShown)
}

// setupK3dMetricsServer configures metrics-server for K3d clusters by adding K3s flags.
// K3s includes metrics-server by default, so we add --disable=metrics-server flag when disabled.
// This function is called during cluster creation to handle cases where:
// 1. The user overrides --metrics-server flag at create time (different from init-time config).
// 2. The k3d.yaml was manually edited and the flag needs to be added.
// 3. Ensures consistency even if the scaffolder-generated config was modified.
func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	// Only apply to K3d distribution
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3d || k3dConfig == nil {
		return
	}

	// Only add disable flag if explicitly disabled
	if clusterCfg.Spec.Cluster.MetricsServer != v1alpha1.MetricsServerDisabled {
		return
	}

	// Check if --disable=metrics-server is already present
	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == k3sDisableMetricsServerFlag {
			// Already configured, no action needed
			return
		}
	}

	// Add --disable=metrics-server flag
	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         k3sDisableMetricsServerFlag,
			NodeFilters: []string{"server:*"},
		},
	)
}

// setupTalosKubeletCertRotation configures kubelet server certificate rotation for Talos clusters.
// This is required for metrics-server to scrape kubelet metrics over HTTPS without TLS errors.
// When metrics-server is enabled on a Talos cluster, the kubelet needs to rotate its serving
// certificates so that metrics-server can verify them against the cluster's CA.
// This function is called during cluster creation to:
//  1. Respect any overrides to the --metrics-server flag at create time (different from init-time config).
//  2. Programmatically apply kubelet cert rotation on the in-memory Talos configuration used for cluster creation,
//     ensuring consistent metrics-server support regardless of any Talos patches that may or may not exist on disk.
func setupTalosKubeletCertRotation(
	writer io.Writer,
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
) {
	// Only apply to Talos distribution
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos || talosConfig == nil {
		return
	}

	// Only enable cert rotation if metrics-server is enabled
	if clusterCfg.Spec.Cluster.MetricsServer != v1alpha1.MetricsServerEnabled {
		return
	}

	// Apply kubelet cert rotation to the Talos config
	// This modifies the in-memory config that will be used for cluster creation
	// Error is logged but not fatal - cluster can still function without cert rotation
	err := talosConfig.ApplyKubeletCertRotation()
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to apply kubelet cert rotation: %v", err),
			Writer:  writer,
		})
	}
}

const (
	mirrorStageTitle    = "Create mirror registry..."
	mirrorStageEmoji    = "🪞"
	mirrorStageActivity = "creating mirror registries"
	mirrorStageSuccess  = "mirror registries created"
	mirrorStageFailure  = "failed to setup registries"

	connectStageTitle    = "Connect registry..."
	connectStageEmoji    = "🔗"
	connectStageActivity = "connecting registries"
	connectStageSuccess  = "registries connected"
	connectStageFailure  = "failed to connect registries"

	certManagerStageTitle    = "Install Cert-Manager..."
	certManagerStageEmoji    = "🔐"
	certManagerStageActivity = "installing cert-manager"
	certManagerStageSuccess  = "cert-manager installed"

	fluxStageTitle    = "Install Flux..."
	fluxStageEmoji    = "☸️"
	fluxStageActivity = "installing controllers"

	argoCDStageTitle    = "Install Argo CD..."
	argoCDStageEmoji    = "🦑"
	argoCDStageActivity = "installing argocd"
)

var (
	//nolint:gochecknoglobals // Shared stage configuration used by lifecycle helpers.
	mirrorRegistryStageInfo = registryStageInfo{
		title:         mirrorStageTitle,
		emoji:         mirrorStageEmoji,
		activity:      mirrorStageActivity,
		success:       mirrorStageSuccess,
		failurePrefix: mirrorStageFailure,
	}
	//nolint:gochecknoglobals // Shared stage configuration used by lifecycle helpers.
	connectRegistryStageInfo = registryStageInfo{
		title:         connectStageTitle,
		emoji:         connectStageEmoji,
		activity:      connectStageActivity,
		success:       connectStageSuccess,
		failurePrefix: connectStageFailure,
	}
	//nolint:gochecknoglobals // Stage action definitions reused across lifecycle flows.
	registryStageDefinitions = map[registryStageRole]registryStageDefinition{
		registryStageRoleMirror: {
			info:        mirrorRegistryStageInfo,
			kindAction:  kindRegistryActionFor(registryStageRoleMirror),
			k3dAction:   k3dRegistryActionFor(registryStageRoleMirror),
			talosAction: talosRegistryActionFor(registryStageRoleMirror),
		},
		registryStageRoleConnect: {
			info:        connectRegistryStageInfo,
			kindAction:  kindRegistryActionFor(registryStageRoleConnect),
			k3dAction:   k3dRegistryActionFor(registryStageRoleConnect),
			talosAction: talosRegistryActionFor(registryStageRoleConnect),
		},
	}
	// setupMirrorRegistries configures mirror registries before cluster creation.
	//nolint:gochecknoglobals // Function reused by tests and runtime flow.
	setupMirrorRegistries = makeRegistryStageRunner(registryStageRoleMirror)
	// connectRegistriesToClusterNetwork attaches mirror registries to the cluster network after creation.
	//nolint:gochecknoglobals // Function reused by tests and runtime flow.
	connectRegistriesToClusterNetwork = makeRegistryStageRunner(registryStageRoleConnect)
	// fluxInstallerFactory is overridden in tests to stub Flux installer creation.
	//nolint:gochecknoglobals // dependency injection for tests
	fluxInstallerFactory = func(client helm.Interface, timeout time.Duration) installer.Installer {
		return fluxinstaller.NewFluxInstaller(client, timeout)
	}
	// certManagerInstallerFactory is overridden in tests to stub cert-manager installer creation.
	//nolint:gochecknoglobals // dependency injection for tests
	certManagerInstallerFactory = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := createHelmClientForCluster(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		return certmanagerinstaller.NewCertManagerInstaller(helmClient, timeout), nil
	}
	// certManagerInstallerFactoryMu protects concurrent access to certManagerInstallerFactory in tests.
	//nolint:gochecknoglobals // protects certManagerInstallerFactory global variable
	certManagerInstallerFactoryMu sync.RWMutex
	// csiInstallerFactory is overridden in tests to stub CSI installer creation.
	//nolint:gochecknoglobals // dependency injection for tests
	csiInstallerFactory = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		_, kubeconfig, err := createHelmClientForCluster(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		return localpathstorageinstaller.NewLocalPathStorageInstaller(
			kubeconfig,
			clusterCfg.Spec.Cluster.Connection.Context,
			timeout,
			clusterCfg.Spec.Cluster.Distribution,
		), nil
	}
	// csiInstallerFactoryMu protects concurrent access to csiInstallerFactory in tests.
	//nolint:gochecknoglobals // protects csiInstallerFactory global variable
	csiInstallerFactoryMu sync.RWMutex
	// argocdInstallerFactory is overridden in tests to stub Argo CD installer creation.
	//nolint:gochecknoglobals // dependency injection for tests
	argocdInstallerFactory = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := createHelmClientForCluster(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		return argocdinstaller.NewArgoCDInstaller(helmClient, timeout), nil
	}
	// argocdInstallerFactoryMu protects concurrent access to argocdInstallerFactory in tests.
	//nolint:gochecknoglobals // protects argocdInstallerFactory global variable
	argocdInstallerFactoryMu sync.RWMutex
	// ensureArgoCDResourcesFunc configures default Argo CD resources post-install.
	//nolint:gochecknoglobals // dependency injection for tests
	ensureArgoCDResourcesFunc = ensureArgoCDResources
	// ensureArgoCDResourcesMu protects concurrent access to ensureArgoCDResourcesFunc in tests.
	//nolint:gochecknoglobals // protects ensureArgoCDResourcesFunc global variable
	ensureArgoCDResourcesMu sync.RWMutex
	// ensureFluxResourcesFunc enforces default Flux resources post-install.
	//nolint:gochecknoglobals // dependency injection for tests
	ensureFluxResourcesFunc = fluxinstaller.EnsureDefaultResources
	// dockerClientInvoker can be overridden in tests to avoid real Docker connections.
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvoker = cmdhelpers.WithDockerClient
	// dockerClientInvokerMu protects concurrent access to dockerClientInvoker in tests.
	//nolint:gochecknoglobals // protects dockerClientInvoker global variable
	dockerClientInvokerMu sync.RWMutex
	// clusterProvisionerFactoryOverride allows tests to override the factory creation.
	// When set, this factory is used instead of creating a new DefaultFactory.
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryOverride clusterprovisioner.Factory
	// clusterProvisionerFactoryMu protects concurrent access to clusterProvisionerFactoryOverride.
	//nolint:gochecknoglobals // protects clusterProvisionerFactoryOverride global variable
	clusterProvisionerFactoryMu sync.RWMutex
)

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
//
// It returns a restore function that resets the factory to its previous value.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	certManagerInstallerFactoryMu.Lock()

	previous := certManagerInstallerFactory
	certManagerInstallerFactory = factory

	certManagerInstallerFactoryMu.Unlock()

	return func() {
		certManagerInstallerFactoryMu.Lock()

		certManagerInstallerFactory = previous

		certManagerInstallerFactoryMu.Unlock()
	}
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
//
// It returns a restore function that resets the factory to its previous value.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	csiInstallerFactoryMu.Lock()

	previous := csiInstallerFactory
	csiInstallerFactory = factory

	csiInstallerFactoryMu.Unlock()

	return func() {
		csiInstallerFactoryMu.Lock()

		csiInstallerFactory = previous

		csiInstallerFactoryMu.Unlock()
	}
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
//
// It returns a restore function that resets the factory to its previous value.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	argocdInstallerFactoryMu.Lock()

	previous := argocdInstallerFactory
	argocdInstallerFactory = factory

	argocdInstallerFactoryMu.Unlock()

	return func() {
		argocdInstallerFactoryMu.Lock()

		argocdInstallerFactory = previous

		argocdInstallerFactoryMu.Unlock()
	}
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
//
// It returns a restore function that resets the function to its previous value.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	ensureArgoCDResourcesMu.Lock()

	previous := ensureArgoCDResourcesFunc
	ensureArgoCDResourcesFunc = fn

	ensureArgoCDResourcesMu.Unlock()

	return func() {
		ensureArgoCDResourcesMu.Lock()

		ensureArgoCDResourcesFunc = previous

		ensureArgoCDResourcesMu.Unlock()
	}
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
//
// It returns a restore function that resets the invoker to its previous value.
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
//
// When set, this factory is used instead of creating a new DefaultFactory in handleCreateRunE.
// It returns a restore function that resets the factory to nil.
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

type registryStageRole int

const (
	registryStageRoleMirror registryStageRole = iota
	registryStageRoleConnect
)

type registryStageInfo struct {
	title         string
	emoji         string
	activity      string
	success       string
	failurePrefix string
}

type registryStageHandler struct {
	prepare func() bool
	action  func(context.Context, client.APIClient) error
}

type registryStageContext struct {
	cmd         *cobra.Command
	clusterCfg  *v1alpha1.Cluster
	kindConfig  *v1alpha4.Cluster
	k3dConfig   *v1alpha5.SimpleConfig
	talosConfig *talosconfigmanager.Configs
	mirrorSpecs []registry.MirrorSpec
}

type registryStageDefinition struct {
	info        registryStageInfo
	kindAction  func(*registryStageContext) func(context.Context, client.APIClient) error
	k3dAction   func(*registryStageContext) func(context.Context, client.APIClient) error
	talosAction func(*registryStageContext) func(context.Context, client.APIClient) error
}

type registryAction func(context.Context, *registryStageContext, client.APIClient) error

func registryActionFor(
	role registryStageRole,
	selectAction func(registryStageRole) registryAction,
) func(*registryStageContext) func(context.Context, client.APIClient) error {
	return func(ctx *registryStageContext) func(context.Context, client.APIClient) error {
		action := selectAction(role)

		if action == nil {
			return func(context.Context, client.APIClient) error {
				return nil
			}
		}

		return func(execCtx context.Context, dockerClient client.APIClient) error {
			return action(execCtx, ctx, dockerClient)
		}
	}
}

func makeRegistryStageRunner(role registryStageRole) func(
	*cobra.Command,
	*v1alpha1.Cluster,
	cmdhelpers.LifecycleDeps,
	*ksailconfigmanager.ConfigManager,
	*v1alpha4.Cluster,
	*v1alpha5.SimpleConfig,
	*talosconfigmanager.Configs,
	*bool,
) error {
	return func(
		cmd *cobra.Command,
		clusterCfg *v1alpha1.Cluster,
		deps cmdhelpers.LifecycleDeps,
		cfgManager *ksailconfigmanager.ConfigManager,
		kindConfig *v1alpha4.Cluster,
		k3dConfig *v1alpha5.SimpleConfig,
		talosConfig *talosconfigmanager.Configs,
		firstActivityShown *bool,
	) error {
		return runRegistryStageWithRole(
			cmd,
			clusterCfg,
			deps,
			cfgManager,
			kindConfig,
			k3dConfig,
			talosConfig,
			role,
			firstActivityShown,
		)
	}
}

func kindRegistryActionFor(
	role registryStageRole,
) func(*registryStageContext) func(context.Context, client.APIClient) error {
	return registryActionFor(role, func(currentRole registryStageRole) registryAction {
		switch currentRole {
		case registryStageRoleMirror:
			return runKindMirrorAction
		case registryStageRoleConnect:
			return runKindConnectAction
		default:
			return nil
		}
	})
}

func runKindMirrorAction(
	execCtx context.Context,
	ctx *registryStageContext,
	dockerClient client.APIClient,
) error {
	writer := ctx.cmd.OutOrStdout()
	clusterName := ctx.kindConfig.Name

	// Kind always uses a network named "kind"
	networkName := "kind"

	// Pre-create the Docker network so registries can be connected before Kind nodes start.
	// This ensures registry containers are reachable via Docker DNS when nodes pull images.
	// For Kind, we don't need to specify a CIDR as Kind manages its own network settings.
	err := ensureDockerNetworkExists(execCtx, dockerClient, networkName, "", writer)
	if err != nil {
		return fmt.Errorf("failed to create docker network: %w", err)
	}

	// Setup registry containers
	err = kindprovisioner.SetupRegistries(
		execCtx,
		ctx.kindConfig,
		clusterName,
		dockerClient,
		ctx.mirrorSpecs,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup kind registries: %w", err)
	}

	// Connect registries to the network immediately for Docker DNS resolution
	err = kindprovisioner.ConnectRegistriesToNetwork(
		execCtx,
		ctx.mirrorSpecs,
		dockerClient,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to connect kind registries to network: %w", err)
	}

	return nil
}

func runKindConnectAction(
	execCtx context.Context,
	ctx *registryStageContext,
	dockerClient client.APIClient,
) error {
	// Registries are already connected to the network in runKindMirrorAction.
	// This function only configures containerd inside Kind nodes to use the registry mirrors.
	// This injects hosts.toml files directly into the running nodes.
	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		execCtx,
		ctx.kindConfig,
		ctx.mirrorSpecs,
		dockerClient,
		ctx.cmd.OutOrStdout(),
	)
	if err != nil {
		return fmt.Errorf("failed to configure containerd registry mirrors: %w", err)
	}

	return nil
}

type k3dRegistryAction func(context.Context, *v1alpha5.SimpleConfig, string, client.APIClient, io.Writer) error

func k3dRegistryActionFor(
	role registryStageRole,
) func(*registryStageContext) func(context.Context, client.APIClient) error {
	return registryActionFor(role, func(currentRole registryStageRole) registryAction {
		switch currentRole {
		case registryStageRoleMirror:
			return func(execCtx context.Context, ctx *registryStageContext, dockerClient client.APIClient) error {
				// Setup registries first
				err := runK3DRegistryAction(
					execCtx,
					ctx,
					dockerClient,
					"setup k3d registries",
					k3dprovisioner.SetupRegistries,
				)
				if err != nil {
					return err
				}

				// Pre-create Docker network and connect registries before cluster creation.
				// This ensures registries are reachable via Docker DNS when K3d nodes pull images.
				clusterName := k3dconfigmanager.ResolveClusterName(ctx.clusterCfg, ctx.k3dConfig)
				networkName := "k3d-" + clusterName
				writer := ctx.cmd.OutOrStdout()

				// For K3d, we don't need to specify a CIDR as K3d manages its own network settings.
				errNetwork := ensureDockerNetworkExists(
					execCtx,
					dockerClient,
					networkName,
					"",
					writer,
				)
				if errNetwork != nil {
					return fmt.Errorf("failed to create k3d network: %w", errNetwork)
				}

				return runK3DRegistryAction(
					execCtx,
					ctx,
					dockerClient,
					"connect k3d registries to network",
					k3dprovisioner.ConnectRegistriesToNetwork,
				)
			}
		case registryStageRoleConnect:
			// Registries are already connected to the network in the mirror action.
			// No additional work needed after cluster creation.
			return func(_ context.Context, _ *registryStageContext, _ client.APIClient) error {
				return nil
			}
		default:
			return nil
		}
	})
}

func runK3DRegistryAction(
	execCtx context.Context,
	ctx *registryStageContext,
	dockerClient client.APIClient,
	description string,
	action k3dRegistryAction,
) error {
	if action == nil {
		return nil
	}

	targetName := k3dconfigmanager.ResolveClusterName(ctx.clusterCfg, ctx.k3dConfig)
	writer := ctx.cmd.OutOrStdout()

	err := action(execCtx, ctx.k3dConfig, targetName, dockerClient, writer)
	if err != nil {
		return fmt.Errorf("failed to %s: %w", description, err)
	}

	return nil
}

func talosRegistryActionFor(
	role registryStageRole,
) func(*registryStageContext) func(context.Context, client.APIClient) error {
	return registryActionFor(role, func(currentRole registryStageRole) registryAction {
		switch currentRole {
		case registryStageRoleMirror:
			return runTalosMirrorAction
		case registryStageRoleConnect:
			return runTalosConnectAction
		default:
			return nil
		}
	})
}

func runTalosMirrorAction(
	execCtx context.Context,
	ctx *registryStageContext,
	dockerAPIClient client.APIClient,
) error {
	if len(ctx.mirrorSpecs) == 0 {
		return nil
	}

	clusterName := resolveTalosClusterName(ctx.talosConfig)
	networkName := clusterName // Talos uses cluster name as network name
	networkCIDR := resolveTalosNetworkCIDR(ctx.talosConfig)
	writer := ctx.cmd.OutOrStdout()

	// Build registry infos from mirror specs
	upstreams := registry.BuildUpstreamLookup(ctx.mirrorSpecs)
	registryInfos := registry.BuildRegistryInfosFromSpecs(ctx.mirrorSpecs, upstreams, nil)

	if len(registryInfos) == 0 {
		return nil
	}

	// Pre-create Docker network and setup registries before Talos nodes boot.
	// This ensures registries are reachable via Docker DNS when nodes pull images.
	return setupTalosMirrorRegistries(
		execCtx,
		dockerAPIClient,
		clusterName,
		networkName,
		networkCIDR,
		registryInfos,
		writer,
	)
}

// resolveTalosClusterName extracts the cluster name from Talos config or returns the default.
func resolveTalosClusterName(talosConfig *talosconfigmanager.Configs) string {
	if talosConfig != nil && talosConfig.Name != "" {
		return talosConfig.Name
	}

	return talosconfigmanager.DefaultClusterName
}

// resolveTalosNetworkCIDR returns the Docker network CIDR for Talos.
// This is always DefaultNetworkCIDR (10.5.0.0/24) - NOT the pod CIDR from cluster config.
// The Talos SDK uses this CIDR for the Docker bridge network that nodes connect to.
func resolveTalosNetworkCIDR(_ *talosconfigmanager.Configs) string {
	return talosconfigmanager.DefaultNetworkCIDR
}

// setupTalosMirrorRegistries creates network, registry containers, and connects them.
func setupTalosMirrorRegistries(
	ctx context.Context,
	dockerAPIClient client.APIClient,
	clusterName string,
	networkName string,
	networkCIDR string,
	registryInfos []registry.Info,
	writer io.Writer,
) error {
	// Pre-create the Docker network with Talos-compatible labels and CIDR.
	// This allows the Talos SDK to recognize and reuse the network when creating the cluster.
	err := ensureDockerNetworkExists(ctx, dockerAPIClient, networkName, networkCIDR, writer)
	if err != nil {
		return fmt.Errorf("failed to create docker network: %w", err)
	}

	// Create registry manager and setup containers
	registryMgr, err := dockerclient.NewRegistryManager(dockerAPIClient)
	if err != nil {
		return fmt.Errorf("failed to create registry manager: %w", err)
	}

	err = registry.SetupRegistries(
		ctx,
		registryMgr,
		registryInfos,
		clusterName,
		networkName,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup talos registries: %w", err)
	}

	// Connect registries to the network with static IPs from the high end of the subnet
	// to avoid conflicts with Talos node IPs that start from .2
	err = registry.ConnectRegistriesToNetworkWithStaticIPs(
		ctx,
		dockerAPIClient,
		registryInfos,
		networkName,
		networkCIDR,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to connect talos registries to network: %w", err)
	}

	return nil
}

func runTalosConnectAction(
	_ context.Context,
	_ *registryStageContext,
	_ client.APIClient,
) error {
	// For Talos, registries are already connected to the network in runTalosMirrorAction.
	// This function is a no-op but kept for consistency with the Kind/K3d flow.
	// The early connection in runTalosMirrorAction ensures registries are available
	// when Talos nodes start pulling images during boot.
	return nil
}

// ensureDockerNetworkExists creates a Docker network if it doesn't already exist.
// This is used by Talos to pre-create the cluster network before registry setup,
// allowing registry containers to be connected and accessible via Docker DNS when
// Talos nodes start pulling images during boot.
//
// The network is created with Talos-compatible labels and CIDR so that the Talos SDK
// will recognize and reuse it when creating the cluster.
//
//nolint:funlen // Network creation requires multiple configuration steps
func ensureDockerNetworkExists(
	ctx context.Context,
	dockerClient client.APIClient,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	// Check if network already exists
	networks, err := dockerClient.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Network with exact name match
	for _, nw := range networks {
		if nw.Name == networkName {
			notify.WriteMessage(notify.Message{
				Type:    notify.ActivityType,
				Content: "network '%s' already exists",
				Writer:  writer,
				Args:    []any{networkName},
			})

			return nil
		}
	}

	// Create the network with Talos-compatible labels and CIDR
	// This ensures the Talos SDK will recognize and reuse the network
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "creating network '%s'",
		Writer:  writer,
		Args:    []any{networkName},
	})

	// Default MTU for Docker bridge networks - required by Talos SDK's Reflect() function
	const defaultNetworkMTU = "1500"

	createOptions := network.CreateOptions{
		Driver: "bridge",
		// Use Talos labels so the SDK recognizes this as a Talos network
		Labels: map[string]string{
			"talos.owned":        "true",
			"talos.cluster.name": networkName,
		},
		// Enable container name DNS resolution and set MTU
		// The MTU option is required by the Talos SDK's Reflect() function which
		// reads com.docker.network.driver.mtu to parse network state. Without it,
		// strconv.Atoi("") fails with "invalid syntax" during cluster deletion.
		Options: map[string]string{
			"com.docker.network.bridge.enable_icc":           "true",
			"com.docker.network.bridge.enable_ip_masquerade": "true",
			"com.docker.network.driver.mtu":                  defaultNetworkMTU,
		},
	}

	// Add IPAM config if CIDR is provided
	if networkCIDR != "" {
		createOptions.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet: networkCIDR,
				},
			},
		}
	}

	_, err = dockerClient.NetworkCreate(ctx, networkName, createOptions)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}

func newRegistryHandlers(
	clusterCfg *v1alpha1.Cluster,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	mirrorSpecs []registry.MirrorSpec,
	kindAction func(context.Context, client.APIClient) error,
	k3dAction func(context.Context, client.APIClient) error,
	talosAction func(context.Context, client.APIClient) error,
) map[v1alpha1.Distribution]registryStageHandler {
	return map[v1alpha1.Distribution]registryStageHandler{
		v1alpha1.DistributionKind: {
			prepare: func() bool { return prepareKindConfigWithMirrors(clusterCfg, cfgManager, kindConfig) },
			action:  kindAction,
		},
		v1alpha1.DistributionK3d: {
			prepare: func() bool { return prepareK3dConfigWithMirrors(clusterCfg, k3dConfig, mirrorSpecs) },
			action:  k3dAction,
		},
		v1alpha1.DistributionTalos: {
			prepare: func() bool { return prepareTalosConfigWithMirrors(clusterCfg, talosConfig, mirrorSpecs) },
			action:  talosAction,
		},
	}
}

func handleRegistryStage(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	info registryStageInfo,
	mirrorSpecs []registry.MirrorSpec,
	kindAction func(context.Context, client.APIClient) error,
	k3dAction func(context.Context, client.APIClient) error,
	talosAction func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
) error {
	handlers := newRegistryHandlers(
		clusterCfg,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		mirrorSpecs,
		kindAction,
		k3dAction,
		talosAction,
	)

	handler, ok := handlers[clusterCfg.Spec.Cluster.Distribution]
	if !ok {
		return nil
	}

	return executeRegistryStage(
		cmd,
		deps,
		info,
		handler.prepare,
		handler.action,
		firstActivityShown,
	)
}

//nolint:funlen // Registry stage requires multiple validation and merge steps
func runRegistryStageWithRole(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	role registryStageRole,
	firstActivityShown *bool,
) error {
	// Get mirror specs from --mirror-registry flag
	flagSpecs := registry.ParseMirrorSpecs(
		cfgManager.Viper.GetStringSlice("mirror-registry"),
	)

	// Try to read existing hosts.toml files from the configured mirrors directory.
	// ReadExistingHostsToml returns (nil, nil) for missing directories, and an error for actual I/O issues.
	existingSpecs, err := registry.ReadExistingHostsToml(getKindMirrorsDir(clusterCfg))
	if err != nil {
		return fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	// For Talos, also extract mirror hosts from the loaded Talos config.
	// The Talos config includes any mirror-registries.yaml patches that were applied.
	if talosConfig != nil {
		talosHosts := talosConfig.ExtractMirrorHosts()
		for _, host := range talosHosts {
			// Only add if not already present in existingSpecs
			found := false

			for _, spec := range existingSpecs {
				if spec.Host == host {
					found = true

					break
				}
			}

			if !found {
				existingSpecs = append(existingSpecs, registry.MirrorSpec{
					Host:   host,
					Remote: registry.GenerateUpstreamURL(host),
				})
			}
		}
	}

	// Merge specs: flag specs override existing specs for the same host
	mirrorSpecs := registry.MergeSpecs(existingSpecs, flagSpecs)

	definition, ok := registryStageDefinitions[role]
	if !ok {
		return nil
	}

	stageCtx := &registryStageContext{
		cmd:         cmd,
		clusterCfg:  clusterCfg,
		kindConfig:  kindConfig,
		k3dConfig:   k3dConfig,
		talosConfig: talosConfig,
		mirrorSpecs: mirrorSpecs,
	}

	kindAction := definition.kindAction(stageCtx)
	k3dAction := definition.k3dAction(stageCtx)
	talosAction := definition.talosAction(stageCtx)

	return handleRegistryStage(
		cmd,
		clusterCfg,
		deps,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		definition.info,
		mirrorSpecs,
		kindAction,
		k3dAction,
		talosAction,
		firstActivityShown,
	)
}

func executeRegistryStage(
	cmd *cobra.Command,
	deps cmdhelpers.LifecycleDeps,
	info registryStageInfo,
	shouldPrepare func() bool,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
) error {
	if !shouldPrepare() {
		return nil
	}

	return runRegistryStage(cmd, deps, info, action, firstActivityShown)
}

func runRegistryStage(
	cmd *cobra.Command,
	deps cmdhelpers.LifecycleDeps,
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

		outputTimer := cmdhelpers.MaybeTimer(cmd, deps.Timer)

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

// createHelmClientForCluster creates a Helm client configured for the cluster.
func createHelmClientForCluster(clusterCfg *v1alpha1.Cluster) (*helm.Client, string, error) {
	kubeconfig, err := cmdhelpers.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	// Validate file exists
	_, err = os.Stat(kubeconfig)
	if err != nil {
		return nil, "", fmt.Errorf("failed to access kubeconfig file: %w", err)
	}

	helmClient, err := helm.NewClient(kubeconfig, clusterCfg.Spec.Cluster.Connection.Context)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Helm client: %w", err)
	}

	return helmClient, kubeconfig, nil
}

// installCiliumCNI installs Cilium CNI on the cluster.
func installCiliumCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Install CNI...",
		Emoji:   "🌐",
		Writer:  cmd.OutOrStdout(),
	})

	helmClient, kubeconfig, err := createHelmClientForCluster(clusterCfg)
	if err != nil {
		return err
	}

	err = helmClient.AddRepository(cmd.Context(), &helm.RepositoryEntry{
		Name: "cilium",
		URL:  "https://helm.cilium.io/",
	})
	if err != nil {
		return fmt.Errorf("failed to add Cilium Helm repository: %w", err)
	}

	installer := newCiliumInstaller(helmClient, kubeconfig, clusterCfg)

	return runCiliumInstallation(cmd, installer, tmr)
}

func newCiliumInstaller(
	helmClient *helm.Client,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
) *ciliuminstaller.CiliumInstaller {
	timeout := installer.GetInstallTimeout(clusterCfg)

	// Map cluster distribution to Cilium installer distribution
	var distribution ciliuminstaller.Distribution

	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionTalos:
		distribution = ciliuminstaller.DistributionTalos
	case v1alpha1.DistributionKind:
		distribution = ciliuminstaller.DistributionKind
	case v1alpha1.DistributionK3d:
		distribution = ciliuminstaller.DistributionK3d
	}

	return ciliuminstaller.NewCiliumInstallerWithDistribution(
		helmClient,
		kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
		distribution,
	)
}

// cniInstaller defines the interface for CNI installers.
type cniInstaller interface {
	Install(ctx context.Context) error
	WaitForReadiness(ctx context.Context) error
}

func runCiliumInstallation(
	cmd *cobra.Command,
	installer *ciliuminstaller.CiliumInstaller,
	tmr timer.Timer,
) error {
	return runCNIInstallation(cmd, installer, "cilium", tmr)
}

// installCalicoCNI installs Calico CNI on the cluster.
func installCalicoCNI(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Install CNI...",
		Emoji:   "🌐",
		Writer:  cmd.OutOrStdout(),
	})

	helmClient, kubeconfig, err := createHelmClientForCluster(clusterCfg)
	if err != nil {
		return err
	}

	installer := newCalicoInstaller(helmClient, kubeconfig, clusterCfg)

	return runCalicoInstallation(cmd, installer, tmr)
}

func newCalicoInstaller(
	helmClient *helm.Client,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
) *calicoinstaller.CalicoInstaller {
	timeout := installer.GetInstallTimeout(clusterCfg)

	// Map cluster distribution to Calico installer distribution
	var distribution calicoinstaller.Distribution

	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionTalos:
		distribution = calicoinstaller.DistributionTalos
	case v1alpha1.DistributionKind:
		distribution = calicoinstaller.DistributionKind
	case v1alpha1.DistributionK3d:
		distribution = calicoinstaller.DistributionK3d
	}

	return calicoinstaller.NewCalicoInstallerWithDistribution(
		helmClient,
		kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
		distribution,
	)
}

func runCalicoInstallation(
	cmd *cobra.Command,
	installer *calicoinstaller.CalicoInstaller,
	tmr timer.Timer,
) error {
	return runCNIInstallation(cmd, installer, "calico", tmr)
}

// runCNIInstallation is the generic implementation for running CNI installation.
func runCNIInstallation(
	cmd *cobra.Command,
	installer cniInstaller,
	cniName string,
	tmr timer.Timer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "installing " + strings.ToLower(cniName),
		Writer:  cmd.OutOrStdout(),
	})

	installErr := installer.Install(cmd.Context())
	if installErr != nil {
		return fmt.Errorf("%s installation failed: %w", cniName, installErr)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "awaiting " + strings.ToLower(cniName) + " to be ready",
		Writer:  cmd.OutOrStdout(),
	})

	readinessErr := installer.WaitForReadiness(cmd.Context())
	if readinessErr != nil {
		return fmt.Errorf("%s readiness check failed: %w", cniName, readinessErr)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cni installed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// prepareKindConfigWithMirrors prepares the Kind config by setting up hosts directory for mirrors.
// Returns true if mirror configuration is needed, false otherwise.
// This uses the modern hosts directory pattern instead of deprecated ContainerdConfigPatches.
func prepareKindConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
) bool {
	// Only for Kind distribution
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKind || kindConfig == nil {
		return false
	}

	// Check for --mirror-registry flag
	mirrorRegistries := cfgManager.Viper.GetStringSlice("mirror-registry")

	// Also check for existing hosts.toml files
	existingSpecs, err := registry.ReadExistingHostsToml(getKindMirrorsDir(clusterCfg))
	if err != nil {
		// Log error but don't fail - missing configuration is acceptable
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "failed to read existing hosts configuration: %v",
			Args:    []any{err},
			Writer:  os.Stderr,
		})
	}

	// If we have either flag specs or existing specs, configuration is needed
	if len(mirrorRegistries) > 0 || len(existingSpecs) > 0 {
		return true
	}

	return false
}

func prepareK3dConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	mirrorSpecs []registry.MirrorSpec,
) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3d || k3dConfig == nil {
		return false
	}

	original := k3dConfig.Registries.Config

	hostEndpoints := k3dconfigmanager.ParseRegistryConfig(original)

	updatedMap, _ := registry.BuildHostEndpointMap(mirrorSpecs, "", hostEndpoints)
	if len(updatedMap) == 0 {
		return false
	}

	rendered := registry.RenderK3dMirrorConfig(updatedMap)

	if strings.TrimSpace(rendered) == strings.TrimSpace(original) {
		return strings.TrimSpace(original) != ""
	}

	k3dConfig.Registries.Config = rendered

	return true
}

func prepareTalosConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
	mirrorSpecs []registry.MirrorSpec,
) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if len(mirrorSpecs) == 0 {
		return false
	}

	// Apply mirror registries to the Talos config.
	// This enables --mirror-registry CLI flags to work for both:
	// 1. Clusters created solely from CLI with no declarative config
	// 2. Clusters with declarative config where additional mirrors are added via CLI
	if talosConfig != nil {
		mirrors := make([]talosconfigmanager.MirrorRegistry, 0, len(mirrorSpecs))
		for _, spec := range mirrorSpecs {
			mirrors = append(mirrors, talosconfigmanager.MirrorRegistry{
				Host:      spec.Host,
				Endpoints: []string{"http://" + spec.Host + ":5000"},
			})
		}

		// Apply mirrors to the Talos config - this merges with any existing mirrors
		_ = talosConfig.ApplyMirrorRegistries(mirrors)
	}

	return true
}

// handleMetricsServer manages metrics-server installation based on cluster configuration.
// For K3d, metrics-server should be disabled via config (handled in setupK3dMetricsServer), not uninstalled.
func handleMetricsServer(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	// Check if distribution provides metrics-server by default
	hasMetricsByDefault := clusterCfg.Spec.Cluster.Distribution.ProvidesMetricsServerByDefault()

	// Enabled: Install if not present by default
	if clusterCfg.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		if hasMetricsByDefault {
			// Already present, no action needed
			return nil
		}

		if *firstActivityShown {
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		}

		*firstActivityShown = true

		tmr.NewStage()

		return installMetricsServer(cmd, clusterCfg, tmr)
	}

	// Disabled: For K3d, this is handled via config before cluster creation (setupK3dMetricsServer)
	// No post-creation action needed for K3d
	if clusterCfg.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerDisabled {
		if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionK3d {
			// K3d metrics-server is disabled via config, no action needed here
			return nil
		}

		if !hasMetricsByDefault {
			// Not present, no action needed
			return nil
		}

		// For other distributions that have it by default, we would uninstall here
		// But currently only K3d has it by default, and that's handled via config
	}

	return nil
}

// installMetricsServer installs metrics-server on the cluster.
func installMetricsServer(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, tmr timer.Timer) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Install Metrics Server...",
		Emoji:   "📊",
		Writer:  cmd.OutOrStdout(),
	})

	helmClient, kubeconfig, err := createHelmClientForCluster(clusterCfg)
	if err != nil {
		return err
	}

	timeout := installer.GetInstallTimeout(clusterCfg)
	msInstaller := metricsserverinstaller.NewMetricsServerInstaller(
		helmClient,
		kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
	)

	return runMetricsServerInstallation(cmd, msInstaller, tmr)
}

// runMetricsServerInstallation performs the metrics-server installation.
func runMetricsServerInstallation(
	cmd *cobra.Command,
	installer *metricsserverinstaller.MetricsServerInstaller,
	tmr timer.Timer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "installing metrics-server",
		Writer:  cmd.OutOrStdout(),
	})

	installErr := installer.Install(cmd.Context())
	if installErr != nil {
		return fmt.Errorf("metrics-server installation failed: %w", installErr)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "metrics server installed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func installCSIIfConfigured(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	// Only install if CSI is set to LocalPathStorage
	if clusterCfg.Spec.Cluster.CSI != v1alpha1.CSILocalPathStorage {
		return nil
	}

	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Install CSI...",
		Emoji:   "💾",
		Writer:  cmd.OutOrStdout(),
	})

	csiInstallerFactoryMu.RLock()

	csiInstaller, err := csiInstallerFactory(clusterCfg)

	csiInstallerFactoryMu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to create CSI installer: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "installing local-path-storage",
		Writer:  cmd.OutOrStdout(),
	})

	installErr := csiInstaller.Install(cmd.Context())
	if installErr != nil {
		return fmt.Errorf("local-path-storage installation failed: %w", installErr)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "csi installed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func installCertManagerIfConfigured(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	if clusterCfg.Spec.Cluster.CertManager != v1alpha1.CertManagerEnabled {
		return nil
	}

	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: certManagerStageTitle,
		Emoji:   certManagerStageEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	if tmr != nil {
		tmr.NewStage()
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	installer, err := newCertManagerInstallerForCluster(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create cert-manager installer: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: certManagerStageActivity,
		Writer:  cmd.OutOrStdout(),
	})

	err = installer.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: certManagerStageSuccess,
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// newCertManagerInstallerForCluster returns an installer tuned for the cluster context.
//
//nolint:ireturn // factory returns interface for dependency injection in tests
func newCertManagerInstallerForCluster(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	certManagerInstallerFactoryMu.RLock()

	factory := certManagerInstallerFactory

	certManagerInstallerFactoryMu.RUnlock()

	if factory == nil {
		return nil, errCertManagerInstallerFactoryNil
	}

	return factory(clusterCfg)
}

func installArgoCDIfConfigured(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	if clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineArgoCD {
		return nil
	}

	// We need kubeconfig for readiness/resource configuration.
	_, kubeconfig, err := createHelmClientForCluster(clusterCfg)
	if err != nil {
		return err
	}

	argoInstaller, err := newArgoCDInstallerForCluster(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create argocd installer: %w", err)
	}

	err = runArgoCDInstallation(cmd, argoInstaller, tmr, firstActivityShown)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: argoCDResourcesActivity,
		Writer:  cmd.OutOrStdout(),
	})

	ensureArgoCDResourcesMu.RLock()

	ensureFn := ensureArgoCDResourcesFunc

	ensureArgoCDResourcesMu.RUnlock()

	err = ensureFn(cmd.Context(), kubeconfig, clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to configure Argo CD resources: %w", err)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: argoCDResourcesSuccess,
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Print access guidance for ArgoCD UI
	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// newArgoCDInstallerForCluster returns an installer tuned for the cluster context.
//
//nolint:ireturn // factory returns interface for dependency injection in tests
func newArgoCDInstallerForCluster(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	argocdInstallerFactoryMu.RLock()

	factory := argocdInstallerFactory

	argocdInstallerFactoryMu.RUnlock()

	if factory == nil {
		return nil, errArgoCDInstallerFactoryNil
	}

	return factory(clusterCfg)
}

func runArgoCDInstallation(
	cmd *cobra.Command,
	installer installer.Installer,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: argoCDStageTitle,
		Emoji:   argoCDStageEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: argoCDStageActivity,
		Writer:  cmd.OutOrStdout(),
	})

	err := installer.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install argocd: %w", err)
	}

	return nil
}

func ensureArgoCDResources(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
) error {
	if clusterCfg == nil {
		return errClusterConfigNil
	}

	installTimeout := installer.GetInstallTimeout(clusterCfg)

	err := argocdinstaller.EnsureDefaultResources(ctx, kubeconfig, installTimeout)
	if err != nil {
		return fmt.Errorf("ensure argocd default resources: %w", err)
	}

	mgr, err := argocdgitops.NewManagerFromKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("create argocd manager: %w", err)
	}

	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = v1alpha1.DefaultSourceDirectory
	}

	repoName := registry.SanitizeRepoName(sourceDir)
	hostPort := net.JoinHostPort(
		registry.LocalRegistryClusterHost,
		strconv.Itoa(registry.DefaultRegistryPort),
	)
	repoURL := fmt.Sprintf("oci://%s/%s", hostPort, repoName)

	// SourcePath is "." because the OCI artifact contains the source directory contents
	// at the root level, not in a subdirectory.
	err = mgr.Ensure(ctx, argocdgitops.EnsureOptions{
		RepositoryURL:   repoURL,
		SourcePath:      ".",
		ApplicationName: "ksail",
		TargetRevision:  "dev",
	})
	if err != nil {
		return fmt.Errorf("ensure argocd resources: %w", err)
	}

	return nil
}

func installFluxIfConfigured(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	if clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineFlux {
		return nil
	}

	helmClient, kubeconfig, err := createHelmClientForCluster(clusterCfg)
	if err != nil {
		return err
	}

	fluxInstaller := newFluxInstallerForCluster(clusterCfg, helmClient)

	err = runFluxInstallation(cmd, fluxInstaller, tmr, firstActivityShown)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fluxResourcesActivity,
		Writer:  cmd.OutOrStdout(),
	})

	err = ensureFluxResourcesFunc(cmd.Context(), kubeconfig, clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to configure Flux resources: %w", err)
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: fluxResourcesSuccess,
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// newFluxInstallerForCluster returns an installer tuned for the cluster context.
//
//nolint:ireturn // factory returns interface for dependency injection in tests
func newFluxInstallerForCluster(
	clusterCfg *v1alpha1.Cluster,
	helmClient helm.Interface,
) installer.Installer {
	timeout := installer.GetInstallTimeout(clusterCfg)

	return fluxInstallerFactory(helmClient, timeout)
}

func runFluxInstallation(
	cmd *cobra.Command,
	installer installer.Installer,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: fluxStageTitle,
		Emoji:   fluxStageEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fluxStageActivity,
		Writer:  cmd.OutOrStdout(),
	})

	err := installer.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install flux controllers: %w", err)
	}

	return nil
}

// getKindMirrorsDir returns the configured Kind mirrors directory or the default.
func getKindMirrorsDir(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Kind.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Kind.MirrorsDir
	}

	return scaffolder.DefaultKindMirrorsDir
}
