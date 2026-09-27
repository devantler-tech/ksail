package vclusterprovisioner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

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
)

// defaultVClusterName is used when no name is provided.
const defaultVClusterName = "vcluster-default"

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
		Driver:       "docker",
		ChartVersion: vclusterconfigmanager.ChartVersion(),
		Connect:      false,
		Upgrade:      false,
	}

	// Standalone VCluster-in-Docker (Vind) manages its own storage and is not subject to a
	// host cluster's StorageClass, so persistence is left at vCluster's default here.
	valuesFiles, cleanup, err := buildValuesFiles(p.valuesPath, p.disableFlannel, "", false)
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
		Driver:         "docker",
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
		Driver:         "docker",
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

// buildValuesFiles returns the ordered list of Helm values files for CreateDocker.
// A temp file with the default Kubernetes version is prepended so the user's
// values file (if present) can override it. The returned cleanup function removes
// the temp file(s) and must be deferred by the caller.
//
// When disableFlannel is true, the defaults file also sets
// deploy.cni.flannel.enabled=false to prevent flannel from conflicting with a
// custom CNI that will be installed post-creation.
//
// When disablePersistence is true, an emptyDir override is appended LAST so it wins over the
// defaults and the user's values. Callers must only set this after resolving the storage
// precedence (an explicit user request for persistence is handled before this and never
// silently downgraded here).
func buildValuesFiles(
	userValuesPath string,
	disableFlannel bool,
	extraSAN string,
	disablePersistence bool,
) ([]string, func(), error) {
	defaultsContent := fmt.Sprintf(
		"controlPlane:\n  distro:\n    k8s:\n      image:\n        tag: %s\n",
		vclusterconfigmanager.DefaultKubernetesVersion,
	)

	if extraSAN != "" {
		// Add the stable exposure address to the vCluster proxy certificate SANs so kubectl
		// verifies TLS when connecting via the exposure address.
		defaultsContent += fmt.Sprintf("  proxy:\n    extraSANs:\n    - %q\n", extraSAN)
	}

	if disableFlannel {
		defaultsContent += "deploy:\n  cni:\n    flannel:\n      enabled: false\n"
	}

	var tmpNames []string

	cleanupFn := func() {
		for _, name := range tmpNames {
			_ = os.Remove(name)
		}
	}

	defaultsFile, err := writeTempValuesFile("ksail-vcluster-defaults-*.yaml", defaultsContent)
	if err != nil {
		return nil, func() {}, err
	}

	tmpNames = append(tmpNames, defaultsFile)

	// Defaults first, user values second — later files override earlier ones.
	result := []string{defaultsFile}
	if strings.TrimSpace(userValuesPath) != "" {
		result = append(result, userValuesPath)
	}

	if disablePersistence {
		overrideFile, oErr := writeTempValuesFile(
			"ksail-vcluster-persistence-*.yaml", emptyDirPersistenceValues,
		)
		if oErr != nil {
			cleanupFn()

			return nil, func() {}, oErr
		}

		tmpNames = append(tmpNames, overrideFile)
		result = append(result, overrideFile)
	}

	return result, cleanupFn, nil
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
