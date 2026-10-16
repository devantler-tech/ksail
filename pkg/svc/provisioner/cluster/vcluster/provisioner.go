package vclusterprovisioner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	dockerengine "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/retry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kernelmod"
	dockernetwork "github.com/docker/docker/api/types/network"
	loftlog "github.com/loft-sh/log"
	"github.com/loft-sh/vcluster/pkg/cli"
	cliconfig "github.com/loft-sh/vcluster/pkg/cli/config"
	"github.com/loft-sh/vcluster/pkg/cli/flags"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

// defaultVClusterName is used when no name is provided.
const defaultVClusterName = "vcluster-default"

// maxTemporaryValuesFiles covers defaults, Kubernetes compatibility, and persistence.
const maxTemporaryValuesFiles = 3

// createMaxAttempts is the number of times to retry CreateDocker for transient
// startup failures. Exit status 22 (EINVAL) and similar infrastructure errors
// are often transient on CI runners (GitHub Actions). Five attempts handles
// persistent-but-transient runner states where 3 attempts are insufficient.
const createMaxAttempts = 5

// createRetryDelay is the delay between CreateDocker retry attempts, giving
// Docker and the runner time to recover from transient issues.
const createRetryDelay = 5 * time.Second

// connectMaxAttempts is the number of times to retry ConnectDocker after cluster
// creation. The SDK's waitForVCluster has a hardcoded 3-minute readiness timeout
// which is too short for CI runners. Retrying gives an effective timeout of
// ~9 minutes (3 attempts x 3 minutes).
const connectMaxAttempts = 3

// networkRemovalTimeout is how long to wait for Docker network cleanup between
// retry attempts. On CI runners, the network may have lingering active endpoints
// that prevent immediate removal.
const networkRemovalTimeout = 30 * time.Second

// networkRemovalInterval is the polling interval for Docker network removal checks.
const networkRemovalInterval = 2 * time.Second

// controlPlaneContainerPrefix is the Docker container name prefix used by the
// vCluster SDK for control plane containers.
const controlPlaneContainerPrefix = "vcluster.cp."

// dockerDriver is the vCluster SDK driver that runs the virtual cluster in Docker.
const dockerDriver = "docker"

// transientCreateErrors returns error substrings that indicate potentially
// transient infrastructure failures during vCluster cluster creation.
// Exit status 22 (EINVAL) has been observed on CI runners where the Docker
// daemon or container runtime hits a temporary invalid-argument condition.
// "fetching blob: denied: denied" has been observed when GHCR transiently
// rejects blob downloads mid-pull for the VCluster Kubernetes base image.
// "Egress is over the account limit" is returned by GHCR when the GitHub
// Actions runner or account hits an egress quota/account limit mid-transfer
// (HTTP 503).
// Network-level errors (i/o timeout, connection reset, TLS failures, DNS
// failures) cover transient infrastructure conditions on CI runners.
// "Node couldn't join" covers the vCluster standalone node join timeout
// (3 minutes) where the kubelet's TLS bootstrap fails to complete on slow
// CI runners. Retrying with a fresh container typically resolves this.
// "Failed to connect to bus" is the systemd/D-Bus startup race in which the
// SDK's install script runs systemctl before D-Bus is up inside the container
// (issue #2261). KSail used to repair it in place; that path went unused across
// every Linux CI matrix leg on vCluster v0.36.1, so a fresh container is now the
// only handling it gets.
func transientCreateErrors() []string {
	return []string{
		"Failed to connect to bus",
		"exit status 22",
		"fetching blob: denied: denied",
		"Egress is over the account limit",
		"i/o timeout",
		"connection reset by peer",
		"TLS handshake timeout",
		"no such host",
		"temporary failure in name resolution",
		"Node couldn't join",
	}
}

// createDockerFn matches the signature of cli.CreateDocker for dependency injection.
type createDockerFn func(
	ctx context.Context,
	opts *cli.CreateOptions,
	globalFlags *flags.GlobalFlags,
	name string,
	logger loftlog.Logger,
) error

// retryCleanupFn is the signature for the cleanup function called between
// retry attempts to remove partially-created cluster state.
type retryCleanupFn func(
	ctx context.Context,
	globalFlags *flags.GlobalFlags,
	clusterName string,
	logger loftlog.Logger,
)

// networkExistsFn checks whether a Docker network with the given name exists.
type networkExistsFn func(ctx context.Context, networkName string) bool

// removeNetworkFn attempts to remove a Docker network. Errors are logged
// but not returned because network removal is best-effort during cleanup.
type removeNetworkFn func(ctx context.Context, networkName string, logger loftlog.Logger)

// Provisioner implements the cluster provisioner interface for vCluster's Docker
// driver (Vind). Create and Delete use the vCluster Go SDK directly, while
// Start/Stop/List/Exists delegate to the Docker infrastructure provider.
type Provisioner struct {
	// RecreationRequiredUpgrader supplies the recreation-based Upgrader behavior and
	// the image-ref/suffix metadata accessors shared with Kind/K3d. VCluster keeps
	// its own GetCurrentVersions and PrepareConfigForVersion, and overrides the two
	// pin accessors because its pins are read from the embedded SDK chart at call
	// time (see upgrader.go).
	clusterupdate.RecreationRequiredUpgrader

	name           string
	valuesPath     string
	disableFlannel bool
	infraProvider  provider.Provider
}

// NewProvisioner constructs a new vCluster provisioner.
//
// Parameters:
//   - name: default cluster name (used when no name is passed to methods)
//   - valuesPath: optional path to a vcluster.yaml values file
//   - disableFlannel: disable the built-in flannel CNI in the vCluster
//   - infraProvider: Docker infrastructure provider for Start/Stop/List/Exists
func NewProvisioner(
	name string,
	valuesPath string,
	disableFlannel bool,
	infraProvider provider.Provider,
) *Provisioner {
	if name == "" {
		name = defaultVClusterName
	}

	return &Provisioner{
		RecreationRequiredUpgrader: newRecreationUpgrader(),
		name:                       name,
		valuesPath:                 valuesPath,
		disableFlannel:             disableFlannel,
		infraProvider:              infraProvider,
	}
}

// SetProvider sets the infrastructure provider for node operations.
// This implements the ProviderAware interface.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create provisions a vCluster using the Docker driver via the vCluster Go SDK.
//
// Cluster creation and connect are split into two phases because the SDK's
// ConnectDocker has a hardcoded 3-minute readiness timeout (waitForVCluster)
// which is too short for CI runners. We retry ConnectDocker up to
// connectMaxAttempts times, giving an effective timeout of ~9 minutes.
//
// On Linux, this method ensures the br_netfilter kernel module is loaded before
// creating the cluster, as it's required for Docker bridge networking features.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	// Ensure required kernel modules are loaded (Linux only)
	err := kernelmod.EnsureBrNetfilter(ctx, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to ensure kernel modules: %w", err)
	}

	opts := &cli.CreateOptions{
		Driver:       dockerDriver,
		ChartVersion: vclusterconfigmanager.ChartVersion(),
		Connect:      false,
		Upgrade:      false,
	}

	// Standalone VCluster-in-Docker (Vind) manages its own storage and is not subject to a
	// host cluster's StorageClass, so persistence is left at vCluster's default here.
	valuesFiles, cleanup, err := buildDockerValuesFiles(p.valuesPath, p.disableFlannel)
	if err != nil {
		return fmt.Errorf("failed to prepare values files: %w", err)
	}
	defer cleanup()

	opts.Values = valuesFiles

	globalFlags := newGlobalFlags()
	logger := newStreamLogger()

	err = createWithRetry(
		ctx, opts, globalFlags, target, logger,
		createRetryDelay, cli.CreateDocker, cleanupFailedCreate,
	)
	if err != nil {
		return err
	}

	return connectWithRetry(ctx, globalFlags, target, logger)
}

// createWithRetry calls CreateDocker and retries on transient errors, deleting
// the partially-created cluster before each fresh attempt.
func createWithRetry(
	ctx context.Context,
	opts *cli.CreateOptions,
	globalFlags *flags.GlobalFlags,
	clusterName string,
	logger loftlog.Logger,
	retryDelay time.Duration,
	create createDockerFn,
	cleanup retryCleanupFn,
) error {
	return retry.Do(ctx, retry.Config{ //nolint:wrapcheck // identity preserved
		MaxAttempts: createMaxAttempts,
		RetryDelay:  retryDelay,
		Attempt: func(ctx context.Context) error {
			return create(ctx, opts, globalFlags, clusterName, logger)
		},
		Cleanup: func(ctx context.Context) {
			cleanup(ctx, globalFlags, clusterName, logger)
		},
		IsTransient: isTransientCreateError,
		Logf:        logger.Warnf,
		WrapNonTransient: func(err error) error {
			return fmt.Errorf("failed to create vCluster: %w", err)
		},
		WrapExhausted: func(attempts int, err error) error {
			return fmt.Errorf("failed to create vCluster after %d attempts: %w", attempts, err)
		},
	})
}

// isTransientCreateError returns true when the error message contains a known
// transient error substring that may succeed on retry.
func isTransientCreateError(err error) bool {
	msg := err.Error()

	for _, s := range transientCreateErrors() {
		if strings.Contains(msg, s) {
			return true
		}
	}

	return false
}

// cleanupFailedCreate attempts to delete a partially-created vCluster so that
// the next CreateDocker call starts from a clean state. After deletion, it
// verifies the Docker network is fully removed — lingering networks with active
// endpoints can cause subsequent attempts to inherit broken state. Errors are
// logged but not propagated because the subsequent retry is expected to handle
// any remaining state.
func cleanupFailedCreate(
	ctx context.Context,
	globalFlags *flags.GlobalFlags,
	clusterName string,
	logger loftlog.Logger,
) {
	deleteOpts := &cli.DeleteOptions{
		Driver:         dockerDriver,
		DeleteContext:  true,
		IgnoreNotFound: true,
	}

	err := cli.DeleteDocker(ctx, nil, deleteOpts, globalFlags, clusterName, logger)
	if err != nil {
		logger.Warnf("cleanup of failed vCluster %q before retry: %v", clusterName, err)
	}

	waitForNetworkRemoval(
		ctx, clusterName, logger,
		dockerNetworkExists, removeDockerNetwork,
		networkRemovalInterval,
	)
}

// waitForNetworkRemoval waits for the Docker network associated with the
// vCluster to be fully removed. The vCluster SDK creates a network named
// "vcluster.<name>" which may linger with active endpoints even after
// DeleteDocker returns success (a known condition on CI runners).
//
// A timeout-scoped context bounds all Docker CLI calls so that a hanging
// Docker daemon cannot block the cleanup step indefinitely. The pollInterval
// controls the delay between removal attempts (use networkRemovalInterval in
// production; tests can pass a smaller value to avoid slow test suites).
func waitForNetworkRemoval(
	ctx context.Context,
	clusterName string,
	logger loftlog.Logger,
	networkExists networkExistsFn,
	removeNetwork removeNetworkFn,
	pollInterval time.Duration,
) {
	networkName := vclusterNetworkPrefix + clusterName

	// Derive a timeout-scoped context so Docker CLI calls are strictly bounded.
	timeoutCtx, cancel := context.WithTimeout(ctx, networkRemovalTimeout)
	defer cancel()

	if !networkExists(timeoutCtx, networkName) {
		return
	}

	if pollInterval <= 0 {
		pollInterval = networkRemovalInterval
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		logger.Infof("Docker network %q still exists, attempting removal...", networkName)
		removeNetwork(timeoutCtx, networkName, logger)

		// Check immediately after removal to avoid unnecessary ticker delay.
		if !networkExists(timeoutCtx, networkName) {
			return
		}

		select {
		case <-timeoutCtx.Done():
			logger.Warnf(
				"Docker network %q still exists after %v; proceeding with retry",
				networkName, networkRemovalTimeout,
			)

			return
		case <-ticker.C:
		}
	}
}

// dockerNetworkExists checks whether a Docker network with the given name
// exists. Returns false if the network is not found or if Docker is unavailable.
func dockerNetworkExists(ctx context.Context, networkName string) bool {
	dockerClient, err := dockerengine.GetDockerClient()
	if err != nil {
		return false
	}

	defer func() { _ = dockerClient.Close() }()

	_, err = dockerClient.NetworkInspect(ctx, networkName, dockernetwork.InspectOptions{})

	return err == nil
}

// removeDockerNetwork attempts to remove a Docker network. Errors are logged
// so operators can diagnose why the network couldn't be removed.
func removeDockerNetwork(
	ctx context.Context,
	networkName string,
	logger loftlog.Logger,
) {
	dockerClient, err := dockerengine.GetDockerClient()
	if err != nil {
		logger.Warnf("failed to create Docker client for network removal %q: %v", networkName, err)

		return
	}

	defer func() { _ = dockerClient.Close() }()

	err = dockerClient.NetworkRemove(ctx, networkName)
	if err != nil {
		logger.Warnf("failed to remove Docker network %q: %v", networkName, err)
	}
}

// connectWithRetry calls ConnectDocker in a retry loop to work around the SDK's
// hardcoded 3-minute readiness timeout. Each attempt gets a fresh 3-minute
// window; on retry the kubeconfig is already available so only the readiness
// poll is retried.
func connectWithRetry(
	ctx context.Context,
	globalFlags *flags.GlobalFlags,
	clusterName string,
	logger loftlog.Logger,
) error {
	connectOpts := &cli.ConnectOptions{
		UpdateCurrent: true,
	}

	var lastErr error

	for attempt := range connectMaxAttempts {
		if attempt > 0 {
			logger.Infof(
				"Retrying vCluster connect (attempt %d/%d)...",
				attempt+1, connectMaxAttempts,
			)
		}

		lastErr = cli.ConnectDocker(
			ctx, connectOpts, globalFlags, clusterName, nil, logger,
		)
		if lastErr == nil {
			return nil
		}

		logger.Warnf(
			"vCluster connect attempt %d/%d failed: %v",
			attempt+1, connectMaxAttempts, lastErr,
		)
	}

	return fmt.Errorf(
		"failed to connect to vCluster after %d attempts: %w",
		connectMaxAttempts, lastErr,
	)
}

// Delete removes a vCluster using the Docker driver via the vCluster Go SDK.
// Returns clustererr.ErrClusterNotFound if the cluster does not exist.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	exists, err := p.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	opts := &cli.DeleteOptions{
		Driver:         dockerDriver,
		DeleteContext:  true,
		IgnoreNotFound: true,
	}

	globalFlags := newGlobalFlags()
	logger := newStreamLogger()

	// platformClient is nil for local Docker-based clusters (no platform integration).
	err = cli.DeleteDocker(ctx, nil, opts, globalFlags, target, logger)
	if err != nil {
		return fmt.Errorf("failed to delete vCluster: %w", err)
	}

	return nil
}

// Start starts a stopped vCluster by starting its Docker containers.
// Delegates to the infrastructure provider for container operations.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	return p.withProvider(ctx, name, "start", func(ctx context.Context, clusterName string) error {
		return p.infraProvider.StartNodes(ctx, clusterName)
	})
}

// Stop stops a running vCluster by stopping its Docker containers.
// Delegates to the infrastructure provider for container operations.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	return p.withProvider(ctx, name, "stop", func(ctx context.Context, clusterName string) error {
		return p.infraProvider.StopNodes(ctx, clusterName)
	})
}

// List returns all vCluster clusters by querying the Docker infrastructure provider.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	if p.infraProvider == nil {
		return nil, fmt.Errorf("%w for vCluster list", clustererr.ErrProviderNotSet)
	}

	clusters, err := p.infraProvider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list vClusters: %w", err)
	}

	return clusters, nil
}

// Exists checks if a vCluster cluster exists by querying the Docker infrastructure provider.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)

	clusters, err := p.List(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list vClusters: %w", err)
	}

	return slices.Contains(clusters, target), nil
}

// --- internals ---

// withProvider executes a provider operation with proper nil check and error wrapping.
func (p *Provisioner) withProvider(
	ctx context.Context,
	name string,
	operationName string,
	providerFunc func(ctx context.Context, clusterName string) error,
) error {
	target := p.resolveName(name)

	err := clustererr.RunProviderOp(
		ctx, p.infraProvider, target, operationName, providerFunc,
	)
	if err != nil {
		return fmt.Errorf("vcluster provider op: %w", err)
	}

	return nil
}

// emptyDirPersistenceValues forces vCluster to back its data volume with an emptyDir instead
// of a PersistentVolumeClaim. Applied as the last values file so it overrides both the KSail
// defaults and the user's values (including an explicit "auto") — only when KSail has resolved
// that persistence must be disabled (see resolvePersistenceDisabled).
const emptyDirPersistenceValues = "controlPlane:\n  statefulSet:\n" +
	"    persistence:\n      volumeClaim:\n        enabled: false\n"

// buildValuesFiles returns the ordered list of Helm values files for vCluster creation.
// A temp file with the default Kubernetes version is prepended so the user's
// values file (if present) can override it. The returned cleanup function removes
// the temp file(s) and must be deferred by the caller.
//
// When disableFlannel is true, the defaults file also sets
// deploy.cni.flannel.enabled=false to prevent flannel from conflicting with a
// custom CNI that will be installed post-creation.
//
// A version-specific controller-manager compatibility file follows user values when required; it
// carries forward the user's other controller arguments. When disablePersistence is true, an
// emptyDir override is appended LAST so it wins over every earlier file. Callers must only set this
// after resolving the storage precedence (an explicit user request for persistence is handled
// before this and never silently downgraded here).
func buildValuesFiles(
	userValuesPath string,
	disableFlannel bool,
	extraSAN string,
	disablePersistence bool,
) ([]string, func(), error) {
	return buildValuesFilesForVersion(
		userValuesPath,
		disableFlannel,
		extraSAN,
		disablePersistence,
		vclusterconfigmanager.DefaultKubernetesVersion,
		false,
	)
}

func buildDockerValuesFiles(userValuesPath string, disableFlannel bool) ([]string, func(), error) {
	return buildValuesFilesForVersion(
		userValuesPath,
		disableFlannel,
		"",
		false,
		vclusterconfigmanager.DefaultKubernetesVersion,
		true,
	)
}

func buildValuesFilesForVersion(
	userValuesPath string,
	disableFlannel bool,
	extraSAN string,
	disablePersistence bool,
	kubernetesVersion string,
	defaultPrivateNodes bool,
) ([]string, func(), error) {
	compatibilityValues, err := controllerManagerCompatibilityValues(
		kubernetesVersion,
		userValuesPath,
		defaultPrivateNodes,
	)
	if err != nil {
		return nil, func() {}, err
	}

	tmpNames := make([]string, 0, maxTemporaryValuesFiles)
	cleanupFn := func() { removeTempValuesFiles(tmpNames) }

	defaultsFile, err := writeTempValuesFile(
		"ksail-vcluster-defaults-*.yaml",
		defaultValuesContent(kubernetesVersion, extraSAN, disableFlannel),
	)
	if err != nil {
		return nil, func() {}, err
	}

	tmpNames = append(tmpNames, defaultsFile)

	// Defaults first, then user values; targeted compatibility overrides follow below.
	result := []string{defaultsFile}
	if strings.TrimSpace(userValuesPath) != "" {
		result = append(result, userValuesPath)
	}

	result, tmpNames, err = appendOptionalTempValuesFile(
		result,
		tmpNames,
		"ksail-vcluster-kubernetes-compatibility-*.yaml",
		compatibilityValues,
	)
	if err != nil {
		cleanupFn()

		return nil, func() {}, err
	}

	result, tmpNames, err = appendOptionalTempValuesFile(
		result,
		tmpNames,
		"ksail-vcluster-persistence-*.yaml",
		optionalPersistenceValues(disablePersistence),
	)
	if err != nil {
		cleanupFn()

		return nil, func() {}, err
	}

	return result, cleanupFn, nil
}

func defaultValuesContent(kubernetesVersion, extraSAN string, disableFlannel bool) string {
	content := fmt.Sprintf(
		"controlPlane:\n  distro:\n    k8s:\n      image:\n        tag: %s\n",
		kubernetesVersion,
	)
	if extraSAN != "" {
		// Add the stable exposure address to the vCluster proxy certificate SANs so kubectl
		// verifies TLS when connecting via the exposure address.
		content += fmt.Sprintf("  proxy:\n    extraSANs:\n    - %q\n", extraSAN)
	}

	if disableFlannel {
		content += "deploy:\n  cni:\n    flannel:\n      enabled: false\n"
	}

	return content
}

func optionalPersistenceValues(disablePersistence bool) string {
	if disablePersistence {
		return emptyDirPersistenceValues
	}

	return ""
}

type controllerManagerCompatibilityConfig struct {
	kubernetesVersion      string
	privateNodes           bool
	virtualScheduler       bool
	resourceClaimTemplates bool
	extraArgs              []string
}

type controllerManagerCompatibilityInput struct {
	ControlPlane struct {
		Advanced struct {
			VirtualScheduler struct {
				Enabled bool `json:"enabled"`
			} `json:"virtualScheduler"`
		} `json:"advanced"`
		Distro struct {
			K8S struct {
				Version string `json:"version"`
				Image   struct {
					Tag string `json:"tag"`
				} `json:"image"`
				ControllerManager struct {
					ExtraArgs []string `json:"extraArgs"`
				} `json:"controllerManager"`
				Scheduler struct {
					Enabled bool `json:"enabled"`
				} `json:"scheduler"`
			} `json:"k8s"`
		} `json:"distro"`
	} `json:"controlPlane"`
	PrivateNodes struct {
		Enabled *bool `json:"enabled"`
	} `json:"privateNodes"`
	Sync struct {
		ToHost struct {
			ResourceClaimTemplates struct {
				Enabled bool `json:"enabled"`
			} `json:"resourceClaimTemplates"`
		} `json:"toHost"`
	} `json:"sync"`
}

// controllerManagerCompatibilityValues removes the cloud-node-lifecycle exclusion for Kubernetes
// 1.37+, where that controller name no longer exists. vCluster 0.37.3 still renders the retired
// name in its built-in virtual-node controller lists, so the complete list must be replaced.
func controllerManagerCompatibilityValues(
	defaultVersion string,
	userValuesPath string,
	defaultPrivateNodes bool,
) (string, error) {
	config, err := loadControllerManagerCompatibilityConfig(
		defaultVersion,
		userValuesPath,
		defaultPrivateNodes,
	)
	if err != nil {
		return "", err
	}

	parsedVersion, err := semver.NewVersion(strings.TrimPrefix(config.kubernetesVersion, "v"))
	if err != nil {
		return "", fmt.Errorf(
			"parse vCluster Kubernetes version %q: %w",
			config.kubernetesVersion,
			err,
		)
	}

	if parsedVersion.Major() < 1 || (parsedVersion.Major() == 1 && parsedVersion.Minor() < 37) {
		return "", nil
	}

	if slices.ContainsFunc(config.extraArgs, isControllerManagerControllersArg) {
		return "", nil
	}

	extraArgs := append(slices.Clone(config.extraArgs), compatibleControllerManagerArg(config))
	values := map[string]any{
		"controlPlane": map[string]any{
			"distro": map[string]any{
				"k8s": map[string]any{
					"controllerManager": map[string]any{
						"extraArgs": extraArgs,
					},
				},
			},
		},
	}

	content, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal vCluster Kubernetes compatibility values: %w", err)
	}

	return string(content), nil
}

func loadControllerManagerCompatibilityConfig(
	defaultVersion string,
	userValuesPath string,
	defaultPrivateNodes bool,
) (controllerManagerCompatibilityConfig, error) {
	config := controllerManagerCompatibilityConfig{
		kubernetesVersion: defaultVersion,
		privateNodes:      defaultPrivateNodes,
	}
	if strings.TrimSpace(userValuesPath) == "" {
		return config, nil
	}

	content, err := os.ReadFile(userValuesPath) //nolint:gosec // user-selected vCluster values path
	if err != nil {
		return config, fmt.Errorf("read vCluster values for Kubernetes compatibility: %w", err)
	}

	var input controllerManagerCompatibilityInput

	err = yaml.Unmarshal(content, &input)
	if err != nil {
		return config, fmt.Errorf("parse vCluster values for Kubernetes compatibility: %w", err)
	}

	k8s := input.ControlPlane.Distro.K8S
	if k8s.Image.Tag != "" {
		config.kubernetesVersion = k8s.Image.Tag
	}

	if k8s.Version != "" {
		config.kubernetesVersion = k8s.Version
	}

	if input.PrivateNodes.Enabled != nil {
		config.privateNodes = *input.PrivateNodes.Enabled
	}

	config.virtualScheduler = k8s.Scheduler.Enabled ||
		input.ControlPlane.Advanced.VirtualScheduler.Enabled
	config.resourceClaimTemplates = input.Sync.ToHost.ResourceClaimTemplates.Enabled
	config.extraArgs = k8s.ControllerManager.ExtraArgs

	return config, nil
}

func compatibleControllerManagerArg(config controllerManagerCompatibilityConfig) string {
	controllers := "--controllers=*,-nodeipam,-nodelifecycle,-persistentvolume-binder," +
		"-attachdetach,-persistentvolume-expander,-ttl"
	if config.privateNodes {
		controllers = "--controllers=*,bootstrapsigner,tokencleaner"
	} else if config.virtualScheduler {
		controllers = "--controllers=*,-nodeipam,-persistentvolume-binder," +
			"-attachdetach,-persistentvolume-expander,-ttl"
	}

	if config.resourceClaimTemplates {
		controllers += ",-resourceclaim-controller"
	}

	return controllers
}

func isControllerManagerControllersArg(arg string) bool {
	arg = strings.TrimSpace(arg)

	return arg == "--controllers" || strings.HasPrefix(arg, "--controllers=")
}

func appendOptionalTempValuesFile(
	files []string,
	tmpNames []string,
	pattern string,
	content string,
) ([]string, []string, error) {
	if content == "" {
		return files, tmpNames, nil
	}

	name, err := writeTempValuesFile(pattern, content)
	if err != nil {
		return files, tmpNames, err
	}

	return append(files, name), append(tmpNames, name), nil
}

func removeTempValuesFiles(names []string) {
	for _, name := range names {
		_ = os.Remove(name)
	}
}

// writeTempValuesFile writes content to a new temp file matching pattern and returns its path.
func writeTempValuesFile(pattern, content string) (string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("create temp values file: %w", err)
	}

	name := tmpFile.Name()

	_, writeErr := tmpFile.WriteString(content)

	closeErr := tmpFile.Close()

	if writeErr != nil {
		_ = os.Remove(name)

		return "", fmt.Errorf("write values file: %w", writeErr)
	}

	if closeErr != nil {
		_ = os.Remove(name)

		return "", fmt.Errorf("close values file: %w", closeErr)
	}

	return name, nil
}

func (p *Provisioner) resolveName(name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}

	return p.name
}

// newGlobalFlags creates a minimal GlobalFlags for the vCluster Go SDK.
// Config is set to the default path (~/.vcluster/config.json) so that
// OCI image caches are stored persistently across runs.
func newGlobalFlags() *flags.GlobalFlags {
	configPath, err := cliconfig.DefaultFilePath()
	if err != nil {
		configPath = ""
	}

	return &flags.GlobalFlags{
		Config: configPath,
	}
}

// newStreamLogger creates a loft-sh/log Logger that writes to stdout.
func newStreamLogger() loftlog.Logger {
	return loftlog.NewStreamLogger(os.Stdout, os.Stderr, logrus.InfoLevel)
}
