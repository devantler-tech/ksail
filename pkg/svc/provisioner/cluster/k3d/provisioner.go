package k3dprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	runner "github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	clustercommand "github.com/k3d-io/k3d/v5/cmd/cluster"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3dlogger "github.com/k3d-io/k3d/v5/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// ErrK3dRuntimeUnavailable is returned by the k3d provisioner when the k3d
// runtime (the Docker daemon) is unavailable. k3d's embedded cobra commands react
// to an unreachable runtime by calling logrus Fatal on their own logger, which by
// default calls os.Exit(1) and would terminate the whole host process — fatal for
// a long-lived host such as the desktop app or `ksail ui`, which must keep running
// when Docker Desktop is not started. NewProvisioner permanently rewires k3d's
// logger to raise this sentinel via panic instead, and every site that runs a k3d
// command routes through runK3dSafely, which recovers it into this error — so a
// Docker-down list, lifecycle, or node-scaling operation fails gracefully rather
// than crashing.
var ErrK3dRuntimeUnavailable = errors.New("k3d runtime unavailable (is the Docker daemon running?)")

var (
	// listMutex protects concurrent access to os.Stdout during List operations.
	// This is required because k3d writes directly to os.Stdout before Cobra's output redirection takes effect.
	listMutex sync.Mutex //nolint:gochecknoglobals // Required for thread-safe stdout manipulation

	// logrusConfigOnce ensures logrus is configured exactly once to avoid data races.
	logrusConfigOnce sync.Once //nolint:gochecknoglobals // Required for one-time logrus initialization
)

// defaultKubeconfigPath is where k3d writes (and updates the current context in)
// the kubeconfig when no explicit path is configured.
const defaultKubeconfigPath = "~/.kube/config"

// Provisioner executes k3d lifecycle commands via Cobra.
type Provisioner struct {
	// RecreationRequiredUpgrader supplies the recreation-based Upgrader behavior and
	// metadata accessors shared with Kind/VCluster. K3d keeps its own
	// GetCurrentVersions and PrepareConfigForVersion (see upgrader.go).
	clusterupdate.RecreationRequiredUpgrader

	simpleCfg  *v1alpha5.SimpleConfig
	configPath string
	runner     runner.CommandRunner
	// listClustersRaw returns the raw cluster-list output from k3d. It is a seam
	// so tests can supply canned output without invoking the real k3d runtime
	// (which calls logrus.Fatal when Docker is unavailable). Defaults to
	// defaultListClustersRaw; tests override it via export_test.go.
	listClustersRaw   func(ctx context.Context) (string, error)
	componentDetector *detector.ComponentDetector
	// kubeconfig is the path k3d writes the cluster's kubeconfig to. Used to
	// build a client for the post-start readiness wait. Defaults to
	// defaultKubeconfigPath; override via WithKubeconfig.
	kubeconfig string
	// waitForReady blocks until the cluster is genuinely ready (API reachable
	// and a basic authorized read succeeds). It is a seam so tests can run
	// Start() without a live cluster. Defaults to k8s.WaitForClusterReady.
	waitForReady func(ctx context.Context, kubeconfigPath, contextName string) error
}

// NewProvisioner constructs a new command-backed provisioner.
func NewProvisioner(
	simpleCfg *v1alpha5.SimpleConfig,
	configPath string,
) *Provisioner {
	// Configure logrus for k3d's console output once
	// k3d uses logrus for logging, so we need to set it up properly
	// Use sync.Once to prevent data races when called from parallel tests
	logrusConfigOnce.Do(func() {
		logrus.SetOutput(os.Stdout)
		logrus.SetFormatter(&logrus.TextFormatter{
			ForceColors:      true,
			DisableTimestamp: false,
			FullTimestamp:    false,
			TimestampFormat:  "2006-01-02T15:04:05Z",
		})
		logrus.SetLevel(logrus.InfoLevel)

		// k3d logs through its OWN logrus instance (k3dlogger.Log(), not the standard
		// logger above), and its embedded cobra commands call logrus Fatal on it when
		// the runtime (Docker) is unreachable — Create alone has 19 such calls. logrus
		// Fatal invokes ExitFunc, which defaults to os.Exit(1) and would terminate the
		// whole host process: fatal for a long-lived host such as the desktop app or
		// `ksail ui`, where running create/delete/start/stop (or background discovery)
		// with Docker stopped must yield an error, not a crash. Permanently replace
		// ExitFunc with one that panics ErrK3dRuntimeUnavailable; every site that runs a
		// k3d command routes through runK3dSafely, which recovers it into an ordinary
		// error. Set once and never restored —
		// embedded k3d must never os.Exit the host, and a fixed value avoids races
		// between concurrent commands sharing this process-global logger (a long-running
		// Create must not race a quick List save/restore of the shared ExitFunc).
		k3dlogger.Log().ExitFunc = func(int) { panic(ErrK3dRuntimeUnavailable) }
	})

	prov := &Provisioner{
		RecreationRequiredUpgrader: newRecreationUpgrader(),
		simpleCfg:                  simpleCfg,
		configPath:                 configPath,
		runner:                     runner.NewCobraCommandRunner(nil, nil),
		kubeconfig:                 defaultKubeconfigPath,
		waitForReady:               k8s.WaitForClusterReady,
	}
	prov.listClustersRaw = prov.defaultListClustersRaw

	return prov
}

// WithKubeconfig sets the kubeconfig path used to build a client for the
// post-start readiness wait. An empty path leaves the default unchanged.
// Returns the provisioner for chaining.
func (k *Provisioner) WithKubeconfig(path string) *Provisioner {
	trimmed := strings.TrimSpace(path)
	if trimmed != "" {
		k.kubeconfig = trimmed
	}

	return k
}

// Create provisions a k3d cluster using the native Cobra command.
func (k *Provisioner) Create(ctx context.Context, name string) error {
	args := k.appendConfigFlag(nil)
	args = k.appendImageFlag(args)

	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterCreate,
		args,
		name,
		"cluster create",
		func(target string) {
			if k.simpleCfg != nil {
				k.simpleCfg.Name = target
			}
		},
	)
}

// Delete removes a k3d cluster via the Cobra command.
// Returns clustererr.ErrClusterNotFound if the cluster does not exist.
func (k *Provisioner) Delete(ctx context.Context, name string) error {
	// Check if cluster exists before attempting to delete
	target := k.resolveName(name)

	exists, err := k.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	args := k.appendConfigFlag(nil)

	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterDelete,
		args,
		name,
		"cluster delete",
		nil,
	)
}

// Start resumes a stopped k3d cluster via Cobra, then waits for the cluster to
// be genuinely ready (API reachable + a basic authorized read succeeds) so
// callers get a usable cluster rather than one that races the API server's
// authorizer warm-up.
func (k *Provisioner) Start(ctx context.Context, name string) error {
	err := k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterStart,
		nil,
		name,
		"cluster start",
		nil,
	)
	if err != nil {
		return err
	}

	target := k.resolveName(name)
	if target == "" {
		// Without a resolved name we cannot derive the kubeconfig context, so
		// fall back to the prior behavior of returning once k3d reports started.
		return nil
	}

	err = k.waitForReady(ctx, k.kubeconfig, "k3d-"+target)
	if err != nil {
		return fmt.Errorf("wait for k3d cluster ready: %w", err)
	}

	return nil
}

// Stop halts a running k3d cluster via Cobra.
func (k *Provisioner) Stop(ctx context.Context, name string) error {
	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterStop,
		nil,
		name,
		"cluster stop",
		nil,
	)
}

// List returns cluster names reported by the Cobra command.
func (k *Provisioner) List(ctx context.Context) ([]string, error) {
	raw, err := k.listClustersRaw(ctx)
	if err != nil {
		return nil, err
	}

	return parseClusterNames(raw)
}

// Exists returns whether the target cluster is present.
func (k *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusters, err := k.List(ctx)
	if err != nil {
		return false, fmt.Errorf("list: %w", err)
	}

	target := k.resolveName(name)
	if target == "" {
		return false, nil
	}

	return slices.Contains(clusters, target), nil
}

// WithComponentDetector sets the component detector for querying cluster state.
func (k *Provisioner) WithComponentDetector(d *detector.ComponentDetector) {
	k.componentDetector = d
}

// SetComponentDetector sets the component detector for querying cluster state.
// This implements the ComponentDetectorAware interface.
func (k *Provisioner) SetComponentDetector(d *detector.ComponentDetector) {
	k.WithComponentDetector(d)
}

// defaultListClustersRaw runs the k3d cluster list command and returns its raw
// JSON output. k3d's PrintClusters writes directly to os.Stdout using
// fmt.Println (not Cobra's cmd.OutOrStdout()), so the output is captured by
// temporarily redirecting os.Stdout.
//
// k3d reacts to an unreachable runtime (Docker down) by calling logrus Fatal,
// which by default calls os.Exit(1) and would kill the whole host process. That
// Fatal is neutralized once in NewProvisioner (k3dlogger ExitFunc → panic
// ErrK3dRuntimeUnavailable); the list goroutine (runListCommandToPipe) recovers
// the panic into a normal error return, and silenceK3dLogging keeps k3d's noise
// off the console while discovery captures the cluster JSON.
func (k *Provisioner) defaultListClustersRaw(ctx context.Context) (string, error) {
	// Lock first so that the logger save/restore and the os.Stdout save/restore
	// are both protected by the same critical section. Without this ordering a
	// second concurrent caller could read the saved output as io.Discard (the
	// value set by the first caller) and later restore to io.Discard, leaving
	// the global loggers permanently muted.
	listMutex.Lock()

	restoreLogging := silenceK3dLogging()

	originalStdout := os.Stdout

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		restoreLogging()
		listMutex.Unlock()

		return "", fmt.Errorf("cluster list: create stdout pipe: %w", err)
	}

	os.Stdout = pipeWriter

	// Run the command in a goroutine since we need to read from the pipe
	// while the command is running (otherwise it may block on a full pipe buffer)
	errChan := make(chan error, 1)

	go k.runListCommandToPipe(ctx, pipeWriter, errChan)

	// Read all output from the pipe (this is the JSON from k3d)
	var outputBuf bytes.Buffer

	_, copyErr := io.Copy(&outputBuf, pipeReader)
	_ = pipeReader.Close()

	// Wait for the command goroutine to finish BEFORE restoring os.Stdout. The
	// channel receive is the happens-before edge with the goroutine's access to
	// os.Stdout — k3d's docker client reads it via moby/term.StdStreams — so
	// restoring earlier races that read under the race detector. (The pipe EOF that
	// unblocked io.Copy is not a tracked synchronization edge.)
	runErr := <-errChan

	// Restore stdout and the loggers, then release the lock.
	os.Stdout = originalStdout

	restoreLogging()

	listMutex.Unlock()

	if copyErr != nil {
		return "", fmt.Errorf("cluster list: read stdout pipe: %w", copyErr)
	}

	if runErr != nil {
		return "", fmt.Errorf("cluster list: %w", runErr)
	}

	return strings.TrimSpace(outputBuf.String()), nil
}

// runListCommandToPipe runs the k3d cluster-list command as its own goroutine (so
// the caller can drain the pipe concurrently — k3d writes its JSON directly to
// os.Stdout), closing pipeWriter to signal EOF and reporting the command's error
// (or ErrK3dRuntimeUnavailable, via runK3dSafely) on errChan.
func (k *Provisioner) runListCommandToPipe(
	ctx context.Context,
	pipeWriter *os.File,
	errChan chan<- error,
) {
	// Always close the write end so the pipe reader (io.Copy) unblocks — whether
	// the command returns, errors, or its runtime-down Fatal is recovered.
	defer func() { _ = pipeWriter.Close() }()

	errChan <- k.runK3dSafely(func() error {
		_, runErr := k.runListCommand(ctx)

		return runErr
	})
}

// silenceK3dLogging mutes both the standard logrus logger and k3d's dedicated
// logger for the duration of a list, so k3d's progress lines and its (already
// neutralized) runtime-down Fatal message do not leak to the console while
// discovery captures the cluster JSON from os.Stdout. It returns a function that
// restores both, which the caller must invoke before releasing listMutex.
//
// The process-exiting Fatal itself is neutralized once and for all in
// NewProvisioner (k3dlogger ExitFunc → panic ErrK3dRuntimeUnavailable), not here;
// this only silences output. k3d logs through its own logrus instance
// (k3dlogger.Log()), not the standard logger, so muting only
// logrus.StandardLogger() would not suppress k3d's output — both are handled.
func silenceK3dLogging() func() {
	originalStdOut := logrus.StandardLogger().Out

	logrus.SetOutput(io.Discard)

	k3dLog := k3dlogger.Log()
	originalK3dOut := k3dLog.Out

	k3dLog.SetOutput(io.Discard)

	return func() {
		logrus.SetOutput(originalStdOut)
		k3dLog.SetOutput(originalK3dOut)
	}
}

// runListCommand executes the k3d cluster list command and returns the output.
func (k *Provisioner) runListCommand(ctx context.Context) (string, error) {
	cmd := clustercommand.NewCmdClusterList()
	args := []string{"--output", "json"}

	// Use a buffer runner to capture command output
	var buf bytes.Buffer

	listRunner := runner.NewCobraCommandRunner(&buf, io.Discard)

	res, runErr := listRunner.Run(ctx, cmd, args)
	if runErr != nil {
		return "", fmt.Errorf("run k3d cluster list: %w", runErr)
	}

	return strings.TrimSpace(res.Stdout), nil
}

// parseClusterNames parses JSON output and extracts cluster names.
func parseClusterNames(output string) ([]string, error) {
	if output == "" {
		return nil, nil
	}

	var entries []struct {
		Name string `json:"name"`
	}

	decodeErr := json.Unmarshal([]byte(output), &entries)
	if decodeErr != nil {
		return nil, fmt.Errorf("cluster list: parse output: %w", decodeErr)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name != "" {
			names = append(names, entry.Name)
		}
	}

	return names, nil
}

func (k *Provisioner) appendConfigFlag(args []string) []string {
	if k.configPath == "" {
		return args
	}

	return append(args, "--config", k.configPath)
}

// appendImageFlag adds the --image flag when no config file is used.
// This ensures the k3d CLI uses the image from our in-memory config
// instead of its internal default (which may be an older version).
func (k *Provisioner) appendImageFlag(args []string) []string {
	// Only add --image flag when no config file is used
	// When a config file exists, the image is read from the config file
	if k.configPath != "" {
		return args
	}

	// Get image from in-memory config, or use empty to let k3d decide
	if k.simpleCfg != nil && k.simpleCfg.Image != "" {
		return append(args, "--image", k.simpleCfg.Image)
	}

	return args
}

func (k *Provisioner) resolveName(name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}

	if k.simpleCfg != nil && strings.TrimSpace(k.simpleCfg.Name) != "" {
		return k.simpleCfg.Name
	}

	return ""
}

func (k *Provisioner) runLifecycleCommand(
	ctx context.Context,
	builder func() *cobra.Command,
	args []string,
	name string,
	errorPrefix string,
	onTarget func(string),
) error {
	cmd := builder()

	target := k.resolveName(name)
	if target != "" {
		args = append(args, target)
		if onTarget != nil {
			onTarget(target)
		}
	}

	runErr := k.runK3dSafely(func() error {
		_, runErr := k.runner.Run(ctx, cmd, args)

		return runErr //nolint:wrapcheck // wrapped below with the operation prefix
	})
	if runErr != nil {
		return fmt.Errorf("%s: %w", errorPrefix, runErr)
	}

	return nil
}

// runK3dSafely runs action and converts the runtime-down panic raised by k3d's
// neutralized Fatal (the ExitFunc set in NewProvisioner) into a returned
// ErrK3dRuntimeUnavailable, so a Docker-down k3d command fails with an error
// instead of terminating the host process (e.g. the desktop app, where these run
// in background goroutines). It is the single recover seam every site that runs an
// embedded k3d command routes through — List, the create/delete/start/stop
// lifecycle commands, and the node create/delete/list commands in update.go — so
// "embedded k3d can never exit the host" holds package-wide rather than as a
// per-call-site checklist. A command may have partially run, so the panic is
// surfaced as an error and execution does not continue past it; any non-sentinel
// panic is a genuine bug and is re-raised.
func (k *Provisioner) runK3dSafely(action func() error) (err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		recoveredErr, ok := recovered.(error)
		if ok && errors.Is(recoveredErr, ErrK3dRuntimeUnavailable) {
			err = ErrK3dRuntimeUnavailable

			return
		}

		// Not our sentinel: a genuine bug. Re-panic so it is not swallowed.
		panic(recovered)
	}()

	return action()
}
