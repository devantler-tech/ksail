package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// commandContext holds common command execution context.
type commandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
}

// initCommandContext initializes common command context (timer, config manager, config loading).
func initCommandContext(cmd *cobra.Command) (*commandContext, error) {
	tmr := timer.New()
	tmr.Start()

	fieldSelectors := configmanager.DefaultClusterFieldSelectors()
	cfgManager := configmanager.NewCommandConfigManager(cmd, fieldSelectors)
	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(configmanagerinterface.LoadOptions{Timer: outputTimer})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &commandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// resolveSourceDir determines the source directory from flag, config, or default.
func resolveSourceDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	if dir := strings.TrimSpace(pathFlag); dir != "" {
		return dir
	}

	if dir := strings.TrimSpace(cfg.Spec.Workload.SourceDirectory); dir != "" {
		return dir
	}

	return v1alpha1.DefaultSourceDirectory
}

// writeActivityNotification writes an activity notification message.
func writeActivityNotification(content string, writer io.Writer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: content,
		Writer:  writer,
	})
}

// imageCommandContext holds shared state for image commands.
type imageCommandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
	ClusterInfo *clusterdetector.Info
}

// createImageConfigManager creates a config manager for image commands.
// Only includes --context and --kubeconfig flags since image commands
// detect the distribution from the running cluster.
func createImageConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultContextFieldSelector(),
		configmanager.DefaultKubeconfigFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

// initImageCommandContext initializes the shared context for image commands.
// It loads the config using the provided config manager, skipping validation
// since image commands detect cluster info from the running cluster.
func initImageCommandContext(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
) (*imageCommandContext, error) {
	tmr := timer.New()
	tmr.Start()

	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(
		configmanagerinterface.LoadOptions{Silent: true, SkipValidation: true},
	)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &imageCommandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// detectClusterInfo detects the cluster info after printing the header.
// This should be called after initImageCommandContext and after printing the title.
func (ctx *imageCommandContext) detectClusterInfo(cmdCtx context.Context) error {
	ctx.Timer.NewStage()

	clusterInfo, err := clusterdetector.DetectInfo(
		cmdCtx,
		ctx.ClusterCfg.Spec.Cluster.Connection.Kubeconfig,
		ctx.ClusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("detect cluster info: %w", err)
	}

	ctx.ClusterInfo = clusterInfo

	return nil
}

// kubectlCommandCreator is a function that creates a kubectl command given a client and kubeconfig path.
type kubectlCommandCreator func(client *kubectl.Client, kubeconfigPath string) *cobra.Command

// newKubectlCommand creates a kubectl wrapper command using the provided command creator.
// The kubeconfig path is resolved lazily via a PersistentPreRunE hook so that the
// --config persistent flag is honored after cobra has parsed all flags.
func newKubectlCommand(creator kubectlCommandCreator) *cobra.Command {
	// Use a placeholder during command construction so cobra can build the
	// command tree.  The actual kubeconfig path will be resolved in
	// PersistentPreRunE before the command runs.
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	cmd := creator(client, kubeconfig.GetKubeconfigPathSilently(nil))

	wrapWithKubeconfigResolution(cmd)

	return cmd
}

// wrapWithKubeconfigResolution adds a PersistentPreRunE hook that re-resolves the
// kubeconfig path after cobra has parsed all flags, honoring the --config flag.
// It chains to any existing PersistentPreRunE or PersistentPreRun on the command.
func wrapWithKubeconfigResolution(cmd *cobra.Command) {
	origPersistentPreRunE := cmd.PersistentPreRunE
	origPersistentPreRun := cmd.PersistentPreRun

	cmd.PersistentPreRunE = func(child *cobra.Command, args []string) error {
		// Refresh expired Omni kubeconfig tokens before resolving the path,
		// so path resolution picks up the freshly written kubeconfig.
		kubeconfighook.MaybeRefreshOmniKubeconfig(child)

		resolvedPath := kubeconfig.GetKubeconfigPathSilently(child)

		kubeconfigFlag := child.Flags().Lookup("kubeconfig")
		if kubeconfigFlag != nil && !child.Flags().Changed("kubeconfig") {
			err := kubeconfigFlag.Value.Set(resolvedPath)
			if err != nil {
				return fmt.Errorf("failed to set kubeconfig flag: %w", err)
			}

			kubeconfigFlag.DefValue = resolvedPath
		}

		if origPersistentPreRunE != nil {
			return origPersistentPreRunE(child, args)
		}

		if origPersistentPreRun != nil {
			origPersistentPreRun(child, args)
		}

		return nil
	}

	cmd.PersistentPreRun = nil
}

// resolveGitOpsEngine determines the GitOps engine from config, normalizing an
// unset ("") value to GitOpsEngineNone so callers (e.g. push's BuildOptions)
// never see an empty engine. This matches how reconcile.go treats "" as None.
func resolveGitOpsEngine(cfg *v1alpha1.Cluster) v1alpha1.GitOpsEngine {
	engine := cfg.Spec.Cluster.GitOpsEngine
	if engine.IsNone() {
		return v1alpha1.GitOpsEngineNone
	}

	return engine
}

// sourcePathResolutionHelp documents the path-resolution order shared by every command whose
// argument defaults to the configured workload source directory (validate, scan, …) — one wording
// kept in one place instead of copy-pasted into each command's long help text.
const sourcePathResolutionHelp = `If no path is provided, the path is resolved in order:
  1. spec.workload.sourceDirectory from ksail.yaml (if a config file is found and the field is set)
  2. The default source directory when spec.workload.sourceDirectory is unset ("k8s" directory)
  3. The current directory (fallback when no ksail.yaml config file is found)`

// Shared errors.
//
//nolint:staticcheck // "GitOps" is a proper noun and must be capitalized
var errGitOpsEngineRequired = errors.New(
	"A GitOps engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during project init or " +
		"set 'spec.cluster.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

// Sentinels wrapping the per-resource progress-group failures. These are the
// verbose, multi-resource errors whose joined chain is replaced by the concise
// diagnostics summary once the report has been printed (see
// runReconcileWithDiagnostics). Other, more specific reconcile errors keep their
// original actionable message.
var (
	errKustomizationReconcile = errors.New("reconcile kustomizations")
	errApplicationReconcile   = errors.New("reconcile argocd applications")
)

// Shared constants for reconciliation.
const (
	defaultReconcileTimeout       = 5 * time.Minute
	fluxKustomizationPollInterval = 500 * time.Millisecond
	argoCDApplicationPollInterval = 500 * time.Millisecond
	reconcileConcurrency          = 5
	reconcileCmdLong              = "Trigger reconciliation/sync and wait for completion. " +
		"For Flux, tracks the OCIRepository and each Kustomization individually. " +
		"For ArgoCD, tracks each Application until synced and healthy."
	// kwokReconcileSkipMsg is emitted when reconciliation is skipped for KWOK.
	// KWOK simulates GitOps controller pods as Running at the API level, but the
	// actual controller processes are not running and cannot sync any resources.
	kwokReconcileSkipMsg = "KWOK distribution: GitOps controllers are simulated and cannot sync — reconciliation skipped"
)

// Retry configuration for the push command.
//
//nolint:gochecknoglobals // package-level vars for retry configuration
var (
	pushMaxRetryAttempts = 3
	pushRetryBaseWait    = 5 * time.Second
	pushRetryMaxWait     = 30 * time.Second
)

// Retry configuration for the reconcile command.
//
//nolint:gochecknoglobals // package-level vars for retry configuration
var (
	reconcileMaxRetryAttempts = 3
	reconcileRetryBaseWait    = 5 * time.Second
	reconcileRetryMaxWait     = 30 * time.Second
)

// retryOnTransientError retries fn up to maxAttempts times when the returned
// error is transient according to netretry.IsRetryable. It uses exponential
// backoff between attempts and emits a warning notification on each retry so
// the user can see what is happening. Returns nil on the first successful
// attempt, or the last error once all attempts are exhausted.
func retryOnTransientError(
	ctx context.Context,
	cmd *cobra.Command,
	maxAttempts int,
	baseWait, maxWait time.Duration,
	operation func() error,
) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		if !netretry.IsRetryable(lastErr) || attempt == maxAttempts {
			break
		}

		waitErr := waitBeforeRetry(ctx, cmd, attempt, maxAttempts, baseWait, maxWait, lastErr)
		if waitErr != nil {
			return waitErr
		}
	}

	if !netretry.IsRetryable(lastErr) {
		return lastErr
	}

	return fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// waitBeforeRetry blocks for the exponential backoff delay before the next
// retry attempt, returning a non-nil error if the context is cancelled so the
// caller stops retrying.
func waitBeforeRetry(
	ctx context.Context,
	cmd *cobra.Command,
	attempt, maxAttempts int,
	baseWait, maxWait time.Duration,
	lastErr error,
) error {
	// Stop immediately if the operation cancelled the context. Without this
	// check the backoff timer below races ctx.Done() in the select: when the
	// goroutine is descheduled past the (often sub-millisecond) delay, both
	// cases are ready and select picks one at random, sometimes running an
	// extra attempt.
	ctxErr := ctx.Err()
	if ctxErr != nil {
		return fmt.Errorf("retry cancelled: %w", ctxErr)
	}

	delay := netretry.ExponentialDelay(attempt, baseWait, maxWait)

	notify.Warningf(
		cmd.OutOrStdout(),
		"attempt %d/%d failed (retrying in %s): %v",
		attempt, maxAttempts, delay, lastErr,
	)

	retryTimer := time.NewTimer(delay)

	select {
	case <-ctx.Done():
		if !retryTimer.Stop() {
			<-retryTimer.C
		}

		return fmt.Errorf("retry cancelled: %w", ctx.Err())
	case <-retryTimer.C:
		// Prioritize cancellation even when the timer also fired: select picks a
		// ready case at random, so a context cancelled during the wait can still
		// land here. Re-checking ctx.Err() keeps cancellation deterministic and
		// prevents one extra retry attempt.
		ctxErr = ctx.Err()
		if ctxErr != nil {
			return fmt.Errorf("retry cancelled: %w", ctxErr)
		}

		return nil
	}
}

// getKubeconfigPath returns the kubeconfig path from config or default.
func getKubeconfigPath(clusterCfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	expanded, err := fsutil.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	return expanded, nil
}

// getCanonicalKubeconfigPath resolves and canonicalizes the kubeconfig path from cluster config.
func getCanonicalKubeconfigPath(clusterCfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath, err := getKubeconfigPath(clusterCfg)
	if err != nil {
		return "", err
	}

	canonPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	return canonPath, nil
}
