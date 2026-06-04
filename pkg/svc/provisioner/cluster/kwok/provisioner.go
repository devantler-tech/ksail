package kwokprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// createAttemptTimeout bounds a single kwokctl create attempt. KWOK creation
// pulls real control-plane images from registry.k8s.io; under heavy CI
// parallelism a pull can stall (a half-open TCP connection that neither
// progresses nor errors) and block indefinitely. Without a per-attempt
// timeout the create would hang until the CI job's own timeout, leaving a
// required check perpetually "in progress" and blocking the PR merge. The
// timeout is generous enough never to interrupt a legitimately slow but
// progressing create (healthy CI creates finish within a few minutes) while
// still rescuing a genuine hang. On timeout the partial cluster is cleaned up
// and the create is retried, the same as any other transient failure.
const createAttemptTimeout = 8 * time.Minute

// transientCreateErrors returns error substrings that indicate transient
// infrastructure failures during KWOK cluster creation that may succeed on
// retry. Almost all stem from pulling the control-plane images from
// registry.k8s.io (Google Artifact Registry, backed by regional AWS S3
// buckets) while many KWOK jobs pull the same images concurrently.
//
//   - "toomanyrequests" / "TOOMANYREQUESTS" / "Quota exceeded": Docker / Google
//     Artifact Registry per-region per-minute quota exceeded (see #4069).
//   - "NoSuchBucket" / "unknown blob" / "blob unknown" / "manifest unknown" /
//     "failed to resolve reference": transient registry/CDN backend errors —
//     e.g. an S3 bucket briefly returning NoSuchBucket mid-pull (see #4495).
//   - "download failed" / "error pulling image" / "failed to pull" /
//     "received unexpected HTTP status" / "registry is unavailable": generic
//     pull failures surfaced by kwok's image puller and the Docker daemon.
//   - "Internal Server Error" / "Bad Gateway" / "Service Unavailable" /
//     "Gateway Timeout" / "Gateway Time-out": transient 5xx from the registry.
//   - Network/transport errors ("i/o timeout", "connection reset by peer",
//     "TLS handshake timeout", "unexpected EOF", "deadline exceeded",
//     "request canceled") and DNS errors ("no such host", "server
//     misbehaving", "temporary failure in name resolution") cover transient
//     infrastructure conditions on CI runners.
func transientCreateErrors() []string {
	return []string{
		// Registry rate limiting.
		"toomanyrequests",
		"TOOMANYREQUESTS",
		"Quota exceeded",
		// Registry / CDN backend errors.
		"NoSuchBucket",
		"unknown blob",
		"blob unknown",
		"manifest unknown",
		"failed to resolve reference",
		"download failed",
		"error pulling image",
		"failed to pull",
		"received unexpected HTTP status",
		"registry is unavailable",
		// Transient registry 5xx responses.
		"Internal Server Error",
		"Bad Gateway",
		"Service Unavailable",
		"Gateway Timeout",
		"Gateway Time-out",
		// Network / transport errors.
		"i/o timeout",
		"connection reset by peer",
		"TLS handshake timeout",
		"unexpected EOF",
		"deadline exceeded",
		"request canceled",
		// DNS errors.
		"no such host",
		"server misbehaving",
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

// createWithRetry calls create and retries on transient errors. Each attempt
// is bounded by attemptTimeout so a stalled image pull is interrupted and
// retried instead of hanging indefinitely. Between attempts, cleanup is called
// to remove the partially-created cluster, followed by a delay to allow rate
// limits to reset.
func createWithRetry(
	ctx context.Context,
	attemptTimeout time.Duration,
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

		var timedOut bool

		timedOut, lastErr = runCreateAttempt(ctx, attemptTimeout, create)
		if lastErr == nil {
			return nil
		}

		if timedOut {
			fmt.Fprintf(
				os.Stderr,
				"KWOK cluster create attempt %d/%d timed out after %s (treating as transient): %v\n",
				attempt+1,
				createMaxAttempts,
				attemptTimeout,
				lastErr,
			)

			continue
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

// runCreateAttempt runs a single create attempt under a timeout derived from
// ctx. It returns whether the attempt was aborted by its own timeout and the
// create error (if any). A timeout is reported only when the per-attempt
// deadline fired while the parent context was still alive — a stalled create
// that should be retried — and not when the parent context itself was
// cancelled, which must propagate and stop the retry loop.
func runCreateAttempt(
	ctx context.Context,
	attemptTimeout time.Duration,
	create kwokCreateFn,
) (bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	err := create(attemptCtx)
	if err == nil {
		return false, nil
	}

	timedOut := errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil

	return timedOut, err
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
	err := p.CreateCluster(ctx, name)
	if err != nil {
		return err
	}

	err = p.ScaleNodes(ctx, name)
	if err != nil {
		// Best-effort cleanup on scale failure to avoid leaving partial cluster.
		_ = p.Delete(ctx, name)

		return err
	}

	return nil
}

// CreateCluster creates the KWOK cluster but does NOT create simulated nodes.
// Use ScaleNodes separately to add nodes. This split is needed for the
// Kubernetes provider where the API server must be port-forwarded before
// scale commands can connect.
func (p *Provisioner) CreateCluster(ctx context.Context, name string) error {
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

	return createWithRetry(
		ctx,
		createAttemptTimeout,
		createRetryDelay,
		createAttempt,
		cleanupAttempt,
	)
}

// ScaleNodes creates one simulated node so the kube-scheduler can place pods.
// This is separated from CreateCluster so that the Kubernetes provider can
// port-forward the API server before calling scale.
func (p *Provisioner) ScaleNodes(ctx context.Context, name string) error {
	target := p.resolveName(name)

	configPath, cleanup, err := p.resolveConfigPath()
	if err != nil {
		return fmt.Errorf("failed to scale KWOK cluster: %w", err)
	}

	if cleanup != nil {
		defer cleanup()
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

	// Silence kwokctl's structured JSON logger. kwokctl commands read the logger
	// from the context (there is no IOStreams parameter to redirect, unlike the
	// Kind SDK), and its INFO-level JSON lines would otherwise pollute KSail's
	// own ►/✔ progress output. NewLogger(nil, …) returns kwok's noop logger.
	ctx = kwoklog.NewContext(ctx, kwoklog.NewLogger(nil, kwoklog.LevelError))

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

// resolveConfigPath returns the config path to pass to kwokctl.
// If an explicit configPath was provided, it is returned as-is.
// Otherwise a temporary directory containing a kustomization.yaml and
// simulation.yaml with the default simulation CRDs is created.
// KWOK's config loader auto-detects directories and runs kustomize.
func (p *Provisioner) resolveConfigPath() (string, func(), error) {
	if p.configPath != "" {
		return p.configPath, nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "kwok-default-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp config dir: %w", err)
	}

	kustomizationContent := "" +
		"apiVersion: kustomize.config.k8s.io/v1beta1\n" +
		"kind: Kustomization\n" +
		"resources:\n" +
		"  - simulation.yaml\n"

	const fileMode = 0o600

	kustomizationPath := filepath.Join(tmpDir, "kustomization.yaml")

	err = os.WriteFile(kustomizationPath, []byte(kustomizationContent), fileMode)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, fmt.Errorf("failed to write temp kustomization.yaml: %w", err)
	}

	simulationPath := filepath.Join(tmpDir, "simulation.yaml")

	err = os.WriteFile(simulationPath, []byte(scaffolder.KWOKDefaultSimulationConfig), fileMode)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, fmt.Errorf("failed to write temp simulation.yaml: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	return tmpDir, cleanup, nil
}
