package kwokprovisioner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	runner "github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/spf13/pflag"
	"sigs.k8s.io/kwok/pkg/config"
	createcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/create/cluster"
	deletecluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/delete/cluster"
	kwokscale "sigs.k8s.io/kwok/pkg/kwokctl/cmd/scale"
	startcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/start/cluster"
	stopcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/stop/cluster"
	kwokruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"
	_ "sigs.k8s.io/kwok/pkg/kwokctl/runtime/compose" // Register the Docker compose runtime so kwokctl can find it.
	kwoklog "sigs.k8s.io/kwok/pkg/log"
)

// kwokControllerImageVersion is the released KWOK image version to use.
// We pin to v0.7.0 because our Go dependency uses a main-branch
// pseudo-version whose embedded version (v0.8.0) has no published images.
const kwokControllerImageVersion = "v0.7.0"

// kwokControllerImage is the full image reference for the KWOK controller.
const kwokControllerImage = "registry.k8s.io/kwok/kwok:" + kwokControllerImageVersion

// createMaxAttempts is the number of times to retry cluster creation for
// transient infrastructure failures. The KWOK Docker runtime pulls real
// control-plane images from registry.k8s.io; running many KWOK jobs in
// parallel (e.g. CI matrix) can hit Google Artifact Registry per-region
// per-minute rate limits. Three attempts with 30-second delays provide
// a total retry window of ~60 seconds, sufficient for the per-minute
// quota to reset.
const createMaxAttempts = 3

// createRetryDelay is the delay between create retry attempts, giving the
// registry rate limit time to reset.
const createRetryDelay = 30 * time.Second

// transientCreateErrors returns error substrings that indicate transient
// infrastructure failures during KWOK cluster creation.
// "toomanyrequests" / "TOOMANYREQUESTS" are returned by Docker / Google
// Artifact Registry when per-region per-minute quota is exceeded.
// "Quota exceeded" appears in the Google Artifact Registry error detail.
// Network-level errors cover transient infrastructure conditions on CI runners.
func transientCreateErrors() []string {
	return []string{
		"toomanyrequests",
		"TOOMANYREQUESTS",
		"Quota exceeded",
		"i/o timeout",
		"connection reset by peer",
		"TLS handshake timeout",
		"no such host",
		"temporary failure in name resolution",
	}
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

// kwokCreateFn is the function that performs the actual kwokctl create operation.
type kwokCreateFn func(ctx context.Context) error

// kwokCleanupFn is called between retry attempts to delete a partially-created cluster.
type kwokCleanupFn func(ctx context.Context)

// createWithRetry calls create and retries on transient errors. Between
// attempts, cleanup is called to remove the partially-created cluster,
// followed by a delay to allow rate limits to reset.
func createWithRetry(
	ctx context.Context,
	retryDelay time.Duration,
	create kwokCreateFn,
	cleanup kwokCleanupFn,
) error {
	var lastErr error

	for attempt := range createMaxAttempts {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr,
				"Retrying KWOK cluster create (attempt %d/%d)...\n",
				attempt+1, createMaxAttempts,
			)

			cleanup(ctx)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during create retry: %w", ctx.Err())
			case <-time.After(retryDelay):
			}
		}

		lastErr = create(ctx)
		if lastErr == nil {
			return nil
		}

		if !isTransientCreateError(lastErr) {
			return lastErr
		}

		fmt.Fprintf(os.Stderr,
			"KWOK cluster create attempt %d/%d failed (transient): %v\n",
			attempt+1, createMaxAttempts, lastErr,
		)
	}

	return fmt.Errorf(
		"failed to create KWOK cluster after %d attempts: %w",
		createMaxAttempts, lastErr,
	)
}

// globalMu serialises access to process-global state that kwokctl
// reads/writes (os.Args, config.DefaultCluster). Without this, concurrent
// provisioner calls (e.g. parallel tests or multi-cluster operations)
// could race on these globals.
//
//nolint:gochecknoglobals // kwokctl mutates os.Args and config.DefaultCluster; process-wide mutex required.
var globalMu sync.Mutex

// Provisioner manages KWOK clusters using kwokctl's Cobra commands.
// It uses the docker (compose) runtime which runs etcd + kube-apiserver +
// kwok-controller as Docker containers.
type Provisioner struct {
	name          string
	configPath    string
	infraProvider provider.Provider
	runner        runner.CommandRunner
}

// NewProvisioner creates a new KWOK provisioner.
// configPath is the optional path to a kwok.yaml configuration file.
func NewProvisioner(
	name string,
	configPath string,
	infraProvider provider.Provider,
) *Provisioner {
	return &Provisioner{
		name:          name,
		configPath:    configPath,
		infraProvider: infraProvider,
		runner:        runner.NewCobraCommandRunner(os.Stdout, os.Stderr),
	}
}

// SetProvider sets the infrastructure provider for node operations.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create creates a KWOK cluster using kwokctl's create command.
// It retries on transient errors (e.g. registry rate limits) with a fixed delay between attempts.
// globalMu is held only for the duration of each kwokctl command; the retry sleep runs unlocked.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	configPath, cleanup, err := p.resolveConfigPath()
	if err != nil {
		return fmt.Errorf("failed to create KWOK cluster: %w", err)
	}

	if cleanup != nil {
		defer cleanup()
	}

	createAttempt := func(ctx context.Context) error {
		return p.withCluster(ctx, target, configPath, p.runCreateCommand)
	}

	cleanupAttempt := func(ctx context.Context) {
		cleanupCtx := context.WithoutCancel(ctx)

		const cleanupTimeout = 30 * time.Second

		cleanupCtx, cancel := context.WithTimeout(cleanupCtx, cleanupTimeout)
		defer cancel()

		err := p.withCluster(cleanupCtx, target, configPath, p.runDeleteCommand)
		if err != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"failed to clean up KWOK cluster after create failure: %v\n",
				err,
			)
		}
	}

	err = createWithRetry(ctx, createRetryDelay, createAttempt, cleanupAttempt)
	if err != nil {
		return err
	}

	return p.withCluster(ctx, target, configPath, p.runScaleCommand)
}

// Delete deletes a KWOK cluster using kwokctl's delete command.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	globalMu.Lock()
	defer globalMu.Unlock()

	kwokCtx, err := p.initContext(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	exists, err := p.existsWithContext(kwokCtx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	defer setDefaultCluster(target)()

	cmd := deletecluster.NewCommand(kwokCtx)

	_, err = p.runner.Run(kwokCtx, cmd, []string{})
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	return nil
}

// Start starts a stopped KWOK cluster.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	return p.withCluster(ctx, p.resolveName(name), "", func(kwokCtx context.Context) error {
		cmd := startcluster.NewCommand(kwokCtx)

		_, err := p.runner.Run(kwokCtx, cmd, []string{})
		if err != nil {
			return fmt.Errorf("failed to start KWOK cluster: %w", err)
		}

		return nil
	})
}

// Stop stops a running KWOK cluster.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	return p.withCluster(ctx, p.resolveName(name), "", func(kwokCtx context.Context) error {
		cmd := stopcluster.NewCommand(kwokCtx)

		_, err := p.runner.Run(kwokCtx, cmd, []string{})
		if err != nil {
			return fmt.Errorf("failed to stop KWOK cluster: %w", err)
		}

		return nil
	})
}

// List returns all KWOK cluster names.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	kwokCtx, err := p.lockAndInit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}

	defer globalMu.Unlock()

	return p.listWithContext(kwokCtx)
}

// Exists checks if a KWOK cluster exists.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	kwokCtx, err := p.lockAndInit(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster existence: %w", err)
	}

	defer globalMu.Unlock()

	return p.existsWithContext(kwokCtx, p.resolveName(name))
}

// runCreateCommand executes the kwokctl create-cluster command.
// Must be called with globalMu held (via withCluster).
func (p *Provisioner) runCreateCommand(kwokCtx context.Context) error {
	cmd := createcluster.NewCommand(kwokCtx)

	args := []string{
		"--runtime", "docker",
		"--kwok-controller-image", kwokControllerImage,
	}

	_, err := p.runner.Run(kwokCtx, cmd, args)
	if err != nil {
		return fmt.Errorf("failed to create KWOK cluster: %w", err)
	}

	return nil
}

// runDeleteCommand executes the kwokctl delete-cluster command.
// Must be called with globalMu held (via withCluster).
func (p *Provisioner) runDeleteCommand(kwokCtx context.Context) error {
	cmd := deletecluster.NewCommand(kwokCtx)

	_, err := p.runner.Run(kwokCtx, cmd, []string{})
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	return nil
}

// runScaleCommand creates one simulated node so the kube-scheduler can place pods.
// Must be called with globalMu held (via withCluster).
func (p *Provisioner) runScaleCommand(kwokCtx context.Context) error {
	cmd := kwokscale.NewCommand(kwokCtx)

	_, err := p.runner.Run(kwokCtx, cmd, []string{"node", "--replicas", "1"})
	if err != nil {
		return fmt.Errorf("failed to create simulated node in KWOK cluster: %w", err)
	}

	return nil
}

// initContext initializes a Go context compatible with kwokctl's internal
// expectations. kwokctl stores configuration objects inside the context via
// config.InitFlags / log.InitFlags, both of which read os.Args directly.
// We temporarily override os.Args so that the kwokctl flag parsers receive
// only the flags relevant to KWOK (e.g. --config <path>).
//
// configPath overrides p.configPath when non-empty (used by Create to pass
// the resolved temp-file path). The caller must hold globalMu.
func (p *Provisioner) initContext(
	ctx context.Context,
	configPath string,
) (context.Context, error) {
	origArgs := os.Args

	defer func() { os.Args = origArgs }()

	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = p.configPath
	}

	args := []string{"kwokctl"}
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}

	os.Args = args

	flagset := pflag.NewFlagSet("kwokctl", pflag.ContinueOnError)
	flagset.ParseErrorsAllowlist.UnknownFlags = true
	flagset.Usage = func() {}

	ctx, _ = kwoklog.InitFlags(ctx, flagset)

	ctx, err := config.InitFlags(ctx, flagset)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize kwokctl config: %w", err)
	}

	return ctx, nil
}

// withCluster acquires globalMu, initializes the kwokctl context,
// sets config.DefaultCluster for the duration of action, and restores it
// on return. configPath overrides p.configPath for context init.
func (p *Provisioner) withCluster(
	ctx context.Context,
	target string,
	configPath string,
	action func(kwokCtx context.Context) error,
) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	kwokCtx, err := p.initContext(ctx, configPath)
	if err != nil {
		return err
	}

	defer setDefaultCluster(target)()

	return action(kwokCtx)
}

// lockAndInit acquires globalMu and initializes the kwokctl context.
// The caller must defer globalMu.Unlock().
func (p *Provisioner) lockAndInit(ctx context.Context) (context.Context, error) {
	globalMu.Lock()

	kwokCtx, err := p.initContext(ctx, "")
	if err != nil {
		globalMu.Unlock()

		return nil, err
	}

	return kwokCtx, nil
}

func (p *Provisioner) listWithContext(kwokCtx context.Context) ([]string, error) {
	clusters, err := kwokruntime.ListClusters(kwokCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}

	return clusters, nil
}

func (p *Provisioner) existsWithContext(kwokCtx context.Context, target string) (bool, error) {
	clusters, err := p.listWithContext(kwokCtx)
	if err != nil {
		return false, err
	}

	if slices.Contains(clusters, target) {
		return true, nil
	}

	return false, nil
}

func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}

// setDefaultCluster sets config.DefaultCluster and returns a function that
// restores the previous value. The caller must hold globalMu.
func setDefaultCluster(name string) func() {
	prev := config.DefaultCluster
	config.DefaultCluster = name

	return func() { config.DefaultCluster = prev }
}

// resolveConfigPath returns the config file path to pass to kwokctl.
// If an explicit configPath was provided, it is returned as-is.
// Otherwise a temporary file containing the default simulation CRDs
// is created and a cleanup function is returned that removes it.
func (p *Provisioner) resolveConfigPath() (string, func(), error) {
	if p.configPath != "" {
		return p.configPath, nil, nil
	}

	tmpFile, err := os.CreateTemp("", "kwok-default-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp config: %w", err)
	}

	tmpName := tmpFile.Name()

	_, writeErr := tmpFile.WriteString(scaffolder.KWOKDefaultSimulationConfig)
	if writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)

		return "", nil, fmt.Errorf("failed to write temp config: %w", writeErr)
	}

	closeErr := tmpFile.Close()
	if closeErr != nil {
		_ = os.Remove(tmpName)

		return "", nil, fmt.Errorf("failed to close temp config: %w", closeErr)
	}

	cleanup := func() { _ = os.Remove(tmpName) }

	return tmpName, cleanup, nil
}
