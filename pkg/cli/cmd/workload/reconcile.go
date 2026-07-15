package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	reconcilerclient "github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	registryhelpers "github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
)

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         reconcileCmdLong,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().Duration(
		"timeout",
		0,
		"timeout for waiting for reconciliation to complete (overrides config timeout)",
	)

	cmd.Flags().StringSlice(
		"exclude",
		nil,
		"kustomization names to skip during progress monitoring (repeatable, comma-separated)",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runReconcile(cmd)
	}

	return cmd
}

// runReconcile executes the reconcile command logic.
func runReconcile(cmd *cobra.Command) error {
	ctx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := ctx.ClusterCfg
	outputTimer := ctx.OutputTimer
	tmr := ctx.Timer

	// Determine GitOps engine - use config if set, otherwise auto-detect
	gitOpsEngine := clusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine.IsNone() {
		detected, detectErr := autoDetectGitOpsEngine(cmd, clusterCfg, tmr, outputTimer)
		if detectErr != nil {
			return detectErr
		}

		gitOpsEngine = detected
	}

	timeout, err := getReconcileTimeout(cmd, clusterCfg)
	if err != nil {
		return err
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔄",
		Content: "Trigger Reconciliation...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	kubeconfigPath, err := getCanonicalKubeconfigPath(clusterCfg)
	if err != nil {
		return err
	}

	err = runReconcileWithDiagnostics(
		cmd,
		clusterCfg,
		kubeconfigPath,
		gitOpsEngine,
		timeout,
		outputTimer,
	)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "reconciliation completed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// runReconcileWithDiagnostics retries reconciliation and on failure runs
// best-effort diagnostics (unless the context was cancelled by the user).
func runReconcileWithDiagnostics(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	kubeconfigPath string,
	gitOpsEngine v1alpha1.GitOpsEngine,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	// --timeout is a per-attempt bound: each retry attempt creates a fresh
	// context.WithTimeout(timeout) inside executeReconciliation, so total
	// runtime can be up to reconcileMaxRetryAttempts*timeout + cumulative
	// backoff. This is intentional: each attempt deserves the full window.
	err := retryOnTransientError(
		cmd.Context(), cmd,
		reconcileMaxRetryAttempts, reconcileRetryBaseWait, reconcileRetryMaxWait,
		func() error {
			return executeReconciliation(
				cmd,
				clusterCfg,
				kubeconfigPath,
				gitOpsEngine,
				timeout,
				outputTimer,
			)
		},
	)
	if err == nil {
		return nil
	}

	// Skip diagnostics when the user explicitly cancelled (Ctrl+C) to avoid
	// blocking for the diagnostic timeout after a deliberate abort.
	if errors.Is(cmd.Context().Err(), context.Canceled) {
		return err
	}

	// Diagnostics render to stdout, alongside the rest of the command's progress
	// UI, rather than stderr. The error executor redirects stderr into a buffer
	// and re-emits it as the command error; writing the report there would bundle
	// the whole report into the error message and re-indent it under a single
	// symbol. Stdout keeps the report rendering with its own formatting.
	report := reconcilediag.Diagnose(
		context.WithoutCancel(cmd.Context()),
		cmd.OutOrStdout(),
		kubeconfigPath,
		gitOpsEngine,
	)

	// For the verbose per-resource reconcile failures, replace the joined error
	// chain with a concise one-line summary — the actionable detail is already in
	// the diagnostics report printed above. Other, more specific errors keep their
	// original message.
	if summary := report.Summary(); summary != "" && isAggregatedReconcileError(err) {
		return &reconcileSummaryError{summary: summary, cause: err}
	}

	return err
}

// isAggregatedReconcileError reports whether err is one of the per-resource
// progress-group failures whose joined chain should be collapsed to the
// diagnostics summary.
func isAggregatedReconcileError(err error) bool {
	return errors.Is(err, errKustomizationReconcile) || errors.Is(err, errApplicationReconcile)
}

// reconcileSummaryError carries a concise, de-duplicated summary of a failed
// reconciliation for display, while preserving the underlying error chain for
// errors.Is/errors.As consumers. The full reconciliation detail is shown in the
// diagnostics report printed before this error is returned.
type reconcileSummaryError struct {
	summary string
	cause   error
}

// Error returns the concise summary message.
func (e *reconcileSummaryError) Error() string { return e.summary }

// Unwrap exposes the underlying reconciliation error chain.
func (e *reconcileSummaryError) Unwrap() error { return e.cause }

// autoDetectGitOpsEngine detects the GitOps engine from the cluster, honoring
// the kubeconfig/context resolved from the cluster config.
func autoDetectGitOpsEngine(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (v1alpha1.GitOpsEngine, error) {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Auto-detect GitOps engine...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "detecting gitops engine in cluster",
		Writer:  cmd.OutOrStdout(),
	})

	engine, err := registryhelpers.DetectGitOpsEngine(cmd.Context(), &registryhelpers.Clients{
		Kubeconfig: clusterCfg.Spec.Cluster.Connection.Kubeconfig,
		Context:    clusterCfg.Spec.Cluster.Connection.Context,
	})
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("detect gitops engine: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "%s detected",
		Args:    []any{engine},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return engine, nil
}

// getReconcileTimeout determines the timeout from flag, config, or default.
func getReconcileTimeout(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) (time.Duration, error) {
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return 0, fmt.Errorf("get timeout flag: %w", err)
	}

	if timeout == 0 {
		if clusterCfg.Spec.Cluster.Connection.Timeout.Duration > 0 {
			timeout = clusterCfg.Spec.Cluster.Connection.Timeout.Duration
		} else {
			timeout = defaultReconcileTimeout
		}
	}

	return timeout, nil
}

// executeReconciliation runs the appropriate reconciliation based on GitOps engine.
func executeReconciliation(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	kubeconfigPath string,
	gitOpsEngine v1alpha1.GitOpsEngine,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	// Check both config and detected distribution for KWOK.
	// When no ksail.yaml exists (e.g. init=false), clusterCfg.Spec.Cluster.Distribution
	// defaults to empty; fall back to detecting from the active kubeconfig context.
	// Only detect when dist is unspecified so an explicitly-configured distribution
	// is never overridden by kubeconfig-based detection.
	dist := clusterCfg.Spec.Cluster.Distribution
	if dist == "" {
		info, detectErr := clusterdetector.DetectInfo(cmd.Context(), kubeconfigPath, "")
		if detectErr == nil {
			dist = info.Distribution
		}
	}

	if dist == v1alpha1.DistributionKWOK {
		notify.Warningf(cmd.OutOrStdout(), kwokReconcileSkipMsg)

		return nil
	}

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineArgoCD:
		return reconcileArgoCD(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineFlux:
		return reconcileFlux(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineNone:
		return errGitOpsEngineRequired
	default:
		return errGitOpsEngineRequired
	}
}

// reconcileFlux triggers and waits for Flux reconciliation using the client reconciler.
// It uses ProgressGroup to show per-resource reconciliation status in real-time.
func reconcileFlux(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	fluxReconciler, err := flux.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create flux reconciler: %w", err)
	}

	writer := cmd.OutOrStdout()

	deadlineCtx, deadlineCancel := context.WithTimeout(cmd.Context(), timeout)
	defer deadlineCancel()

	deadline, _ := deadlineCtx.Deadline()

	// Sub-phase 1: OCI source reconciliation
	writeActivityNotification("reconciling oci source...", writer)

	// The token proves the source controller has handled *this* request before
	// its Ready condition is trusted — otherwise a stale Ready from a prior
	// reconcile is accepted before the just-pushed artifact is ingested, and
	// kustomization readiness then races the ingest (bug #5717).
	ociReconcileToken, err := fluxReconciler.TriggerOCIRepositoryReconciliation(deadlineCtx)
	if err != nil {
		return fmt.Errorf("trigger oci repository reconciliation: %w", err)
	}

	ociGroup := notify.NewProgressGroup(
		"",
		"",
		writer,
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithAppendOnly(),
	)

	err = ociGroup.Run(deadlineCtx, notify.ProgressTask{
		Name: "flux-system",
		Fn: func(ctx context.Context) error {
			return fluxReconciler.WaitForOCIRepositoryReady(
				ctx, time.Until(deadline), ociReconcileToken,
			)
		},
	})
	if err != nil {
		return fmt.Errorf("reconcile oci source: %w", err)
	}

	// Sub-phase 1.5: Reset stuck HelmReleases before Kustomization polling.
	resetStuckHelmReleases(deadlineCtx, fluxReconciler, writer)

	// Sub-phase 2: Kustomization reconciliation with per-resource tracking
	writeActivityNotification("reconciling kustomizations...", writer)

	err = fluxReconciler.TriggerKustomizationReconciliation(deadlineCtx)
	if err != nil {
		return fmt.Errorf("trigger kustomization reconciliation: %w", err)
	}

	err = reconcileFluxKustomizationsWithProgress(deadlineCtx, cmd, fluxReconciler, outputTimer)
	if err != nil {
		return err
	}

	return nil
}

// resetStuckHelmReleases detects and resets HelmReleases that are stuck in a
// non-recoverable Failed/Stalled state before Kustomization polling begins.
// Errors are treated as best-effort — if the CRD is not installed or the API
// is unreachable, reconciliation continues normally.
func resetStuckHelmReleases(
	ctx context.Context,
	fluxReconciler *flux.Reconciler,
	writer io.Writer,
) {
	stuckReleases, err := fluxReconciler.ListStuckHelmReleases(ctx)
	if err != nil {
		writeActivityNotification(
			fmt.Sprintf("warning: could not check for stuck helmreleases: %v", err),
			writer,
		)

		return
	}

	if len(stuckReleases) == 0 {
		return
	}

	writeActivityNotification(
		fmt.Sprintf("resetting %d stuck helmrelease(s)...", len(stuckReleases)),
		writer,
	)

	resetCount, resetErr := fluxReconciler.ResetStuckHelmReleases(ctx, stuckReleases)
	if resetErr != nil {
		writeActivityNotification(
			fmt.Sprintf("warning: some helmreleases could not be reset: %v", resetErr),
			writer,
		)
	}

	if resetCount > 0 {
		writeActivityNotification(
			fmt.Sprintf("reset %d stuck helmrelease(s)", resetCount),
			writer,
		)
	}
}

// errDependencyBlocked is the sentinel wrapped by cascade failures: a
// kustomization that cannot reconcile because one of its dependencies already
// failed. It deliberately does not embed the upstream error — repeating the
// root-cause message at every level of a deep dependency chain is what made the
// failure output unreadable. The upstream failure is reported on its own row in
// the reconciliation diagnostics instead.
var errDependencyBlocked = errors.New("blocked by failed dependency")

// failedKustomizations tracks kustomizations that have permanently failed.
// When an upstream kustomization fails, all dependents fail immediately
// instead of waiting for the full timeout.
type failedKustomizations struct {
	m sync.Map
}

// record stores a permanent failure for the named kustomization.
func (f *failedKustomizations) record(name string, err error) {
	f.m.Store(name, err)
}

// checkDependencies returns an error if any dependency has already failed.
// Returns nil if all dependencies are still healthy or pending. The error names
// only the direct dependency that blocked this kustomization; the upstream
// failure surfaces on its own diagnostics row, so it is not repeated here.
func (f *failedKustomizations) checkDependencies(dependsOn []string) error {
	for _, dep := range dependsOn {
		if _, ok := f.m.Load(dep); ok {
			return fmt.Errorf("%w %q", errDependencyBlocked, dep)
		}
	}

	return nil
}

// reconcileFluxKustomizationsWithProgress lists all Flux Kustomizations, sorts
// them in topological (dependency) order, and monitors each individually using
// a ProgressGroup. Flux's controller handles the actual dependency-driven
// triggering; we just poll and display status.
//
// A shared failure tracker propagates permanent failures to dependents: when an
// upstream kustomization fails, all downstream dependents fail immediately
// instead of waiting for the full timeout.
func reconcileFluxKustomizationsWithProgress(
	deadlineCtx context.Context,
	cmd *cobra.Command,
	fluxReconciler *flux.Reconciler,
	outputTimer timer.Timer,
) error {
	kustomizations, err := fluxReconciler.ListKustomizations(deadlineCtx)
	if err != nil {
		return fmt.Errorf("list kustomizations: %w", err)
	}

	if len(kustomizations) == 0 {
		return nil
	}

	excludeSet, err := buildExcludeSet(cmd)
	if err != nil {
		return err
	}

	sorted := topologicalSortKustomizations(kustomizations)

	var failed failedKustomizations

	tasks := buildKustomizationTasks(sorted, excludeSet, fluxReconciler, &failed)
	if len(tasks) == 0 {
		return nil
	}

	ksGroup := notify.NewProgressGroup(
		"",
		"",
		cmd.OutOrStdout(),
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithContinueOnError(),
		notify.WithAppendOnly(),
		notify.WithCountLabel("kustomizations"),
		notify.WithConcurrency(reconcileConcurrency),
	)

	err = ksGroup.Run(deadlineCtx, tasks...)
	if err != nil {
		return fmt.Errorf("%w: %w", errKustomizationReconcile, err)
	}

	return nil
}

// buildExcludeSet reads the --exclude flag and returns a set of trimmed,
// non-empty kustomization names that should be skipped during reconciliation.
func buildExcludeSet(cmd *cobra.Command) (map[string]bool, error) {
	excludeNames, err := cmd.Flags().GetStringSlice("exclude")
	if err != nil {
		return nil, fmt.Errorf("get exclude flag: %w", err)
	}

	excludeSet := make(map[string]bool, len(excludeNames))
	for _, name := range excludeNames {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			excludeSet[trimmed] = true
		}
	}

	return excludeSet, nil
}

// buildKustomizationTasks creates a progress task for each kustomization that
// is not excluded, capturing the name and dependency slice per iteration to
// avoid the classic loop-variable closure bug.
func buildKustomizationTasks(
	sorted []flux.KustomizationInfo,
	excludeSet map[string]bool,
	fluxReconciler *flux.Reconciler,
	failed *failedKustomizations,
) []notify.ProgressTask {
	tasks := make([]notify.ProgressTask, 0, len(sorted))
	for _, kustomization := range sorted {
		if isKustomizationExcluded(kustomization, excludeSet) {
			continue
		}

		name := kustomization.Name
		deps := kustomization.DependsOn

		tasks = append(tasks, notify.ProgressTask{
			Name: name,
			Fn: func(ctx context.Context) error {
				return pollUntilKustomizationReady(ctx, fluxReconciler, name, deps, failed)
			},
		})
	}

	return tasks
}

// isKustomizationExcluded returns true if the kustomization should be excluded
// from KSail's progress monitoring and readiness polling, either via the
// ReconcileExcludeAnnotation on the CR or via the --exclude CLI flag.
// Flux still reconciles the resource; only KSail's waiting phase is skipped.
func isKustomizationExcluded(
	kustomization flux.KustomizationInfo,
	excludeSet map[string]bool,
) bool {
	return kustomization.Excluded || excludeSet[kustomization.Name]
}

// pollUntilKustomizationReady polls a named Flux Kustomization until it is
// ready or the context's deadline expires. On permanent failure, it returns an
// actionable error including the resource name and failure reason, and records
// the failure in the shared tracker so dependents can fail-fast.
//
// On each poll iteration, it first checks whether any dependency has already
// permanently failed (tracked via the shared failedKustomizations). If so, the
// polling stops immediately instead of waiting for the timeout.
func pollUntilKustomizationReady(
	ctx context.Context,
	fluxReconciler *flux.Reconciler,
	name string,
	dependsOn []string,
	failed *failedKustomizations,
) error {
	return reconcilerclient.PollUntilReady( //nolint:wrapcheck // identity preserved
		ctx,
		fluxKustomizationPollInterval,
		func(ctx context.Context) (reconcilerclient.CheckResult, error) {
			// Fail-fast: check if any dependency has permanently failed.
			depErr := failed.checkDependencies(dependsOn)
			if depErr != nil {
				// Record cascaded failure so further dependents also fail-fast,
				// and halt so the error is returned verbatim (not wrapped).
				failed.record(name, depErr)

				return reconcilerclient.CheckResult{}, reconcilerclient.Halt(depErr)
			}

			ready, status, err := fluxReconciler.CheckNamedKustomizationReady(ctx, name)
			if err != nil {
				// Record permanent failure so dependents can fail-fast. Context
				// errors are not permanent, so they are left unrecorded.
				if !reconcilerclient.IsContextError(err) {
					failed.record(name, err)
				}

				return reconcilerclient.CheckResult{}, err //nolint:wrapcheck // identity preserved
			}

			return reconcilerclient.CheckResult{Ready: ready, Status: status}, nil
		},
		func(lastStatus string) error {
			return kustomizationReadinessTimeoutError(name, lastStatus)
		},
	)
}

// kustomizationReadinessTimeoutError returns an actionable error for a
// kustomization that did not become ready within the timeout.
func kustomizationReadinessTimeoutError(name, lastStatus string) error {
	if lastStatus != "" {
		return fmt.Errorf(
			"%w (last status: %s) — "+
				"run 'ksail workload get kustomizations.kustomize.toolkit.fluxcd.io %s -n flux-system' to inspect",
			flux.ErrReconcileTimeout, lastStatus, name,
		)
	}

	return fmt.Errorf(
		"%w — "+
			"run 'ksail workload get kustomizations.kustomize.toolkit.fluxcd.io %s -n flux-system' to inspect",
		flux.ErrReconcileTimeout, name,
	)
}

// topologicalSortKustomizations returns kustomizations in topological order
// (dependencies before dependents) for display purposes.
// Uses Kahn's algorithm. If cycles are detected, remaining items are appended
// in their original order so the ProgressGroup still shows all resources.
//
//nolint:cyclop // Kahn's algorithm has inherent branching; complexity is structural, not avoidable.
func topologicalSortKustomizations(
	kustomizations []flux.KustomizationInfo,
) []flux.KustomizationInfo {
	if len(kustomizations) <= 1 {
		return kustomizations
	}

	byName := make(map[string]flux.KustomizationInfo, len(kustomizations))
	inDegree := make(map[string]int, len(kustomizations))

	dependents := make(map[string][]string, len(kustomizations))
	for _, kust := range kustomizations {
		byName[kust.Name] = kust
		inDegree[kust.Name] = 0
	}

	for _, kust := range kustomizations {
		seen := make(map[string]struct{}, len(kust.DependsOn))
		for _, dep := range kust.DependsOn {
			if _, dup := seen[dep]; dup {
				continue
			}

			seen[dep] = struct{}{}
			if _, exists := byName[dep]; exists {
				inDegree[kust.Name]++
				dependents[dep] = append(dependents[dep], kust.Name)
			}
		}
	}

	queue := make([]string, 0, len(kustomizations))
	for _, kust := range kustomizations {
		if inDegree[kust.Name] == 0 {
			queue = append(queue, kust.Name)
		}
	}

	sorted := make([]flux.KustomizationInfo, 0, len(kustomizations))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		sorted = append(sorted, byName[name])
		for _, child := range dependents[name] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(sorted) < len(kustomizations) {
		for _, kust := range kustomizations {
			if inDegree[kust.Name] > 0 {
				sorted = append(sorted, kust)
			}
		}
	}

	return sorted
}

// reconcileArgoCD triggers and waits for ArgoCD application sync using the client reconciler.
// It uses ProgressGroup to show per-application reconciliation status in real-time.
func reconcileArgoCD(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	argoReconciler, err := argocd.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd reconciler: %w", err)
	}

	writer := cmd.OutOrStdout()

	deadlineCtx, deadlineCancel := context.WithTimeout(cmd.Context(), timeout)
	defer deadlineCancel()

	gateArgoCDControlPlaneReady(deadlineCtx, argoReconciler, timeout, writer)

	writeActivityNotification("triggering argocd refresh...", writer)

	err = argoReconciler.TriggerRefresh(deadlineCtx, true)
	if err != nil {
		return fmt.Errorf("trigger argocd refresh: %w", err)
	}

	writeActivityNotification("reconciling argocd applications...", writer)

	apps, err := argoReconciler.ListApplications(deadlineCtx)
	if err != nil {
		return fmt.Errorf("list argocd applications: %w", err)
	}

	if len(apps) == 0 {
		return nil
	}

	tasks := buildArgoCDApplicationTasks(apps, argoReconciler)

	appGroup := notify.NewProgressGroup(
		"", "", writer,
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithContinueOnError(),
		notify.WithAppendOnly(),
		notify.WithCountLabel("applications"),
		notify.WithConcurrency(reconcileConcurrency),
	)

	err = appGroup.Run(deadlineCtx, tasks...)
	if err != nil {
		return fmt.Errorf("%w: %w", errApplicationReconcile, err)
	}

	return nil
}

// gateArgoCDControlPlaneReady waits for the ArgoCD control-plane (repo-server /
// redis / server) to be Ready before the first app-sync poll (issue #5948), so a
// just-starting control-plane's transient errors ("connection refused", "unable to
// resolve") are not misclassified as a permanent source-unavailable failure.
//
// It is best-effort (fail-open): a control-plane that never becomes ready is not
// terminal here — reconcile proceeds and any genuine problem surfaces through the
// unchanged poll path — so the gate can only reduce the cold-start race, never add
// a failure mode.
func gateArgoCDControlPlaneReady(
	ctx context.Context,
	argoReconciler *argocd.Reconciler,
	timeout time.Duration,
	writer io.Writer,
) {
	writeActivityNotification("waiting for argocd control-plane...", writer)

	err := argoReconciler.WaitForControlPlaneReady(ctx, timeout)
	if err != nil {
		writeActivityNotification(
			fmt.Sprintf("warning: argocd control-plane not fully ready, proceeding: %v", err),
			writer,
		)
	}
}

// buildArgoCDApplicationTasks builds one progress task per ArgoCD Application,
// capturing the name per iteration to avoid the loop-variable closure bug.
func buildArgoCDApplicationTasks(
	apps []argocd.ApplicationInfo,
	argoReconciler *argocd.Reconciler,
) []notify.ProgressTask {
	tasks := make([]notify.ProgressTask, 0, len(apps))
	for _, app := range apps {
		name := app.Name
		tasks = append(tasks, notify.ProgressTask{
			Name: name,
			Fn: func(ctx context.Context) error {
				return pollUntilApplicationReady(ctx, argoReconciler, name)
			},
		})
	}

	return tasks
}

// pollUntilApplicationReady polls a named ArgoCD Application until it is
// synced and healthy, or the context's deadline expires. On permanent failure,
// it returns an actionable error including the resource name and failure details.
// The caller is expected to provide a context with a deadline (shared across all
// application tasks) so that the total reconcile time is bounded.
func pollUntilApplicationReady(
	ctx context.Context,
	argoReconciler *argocd.Reconciler,
	name string,
) error {
	return reconcilerclient.PollUntilReady( //nolint:wrapcheck // identity preserved
		ctx,
		argoCDApplicationPollInterval,
		func(ctx context.Context) (reconcilerclient.CheckResult, error) {
			ready, err := argoReconciler.CheckNamedApplicationReady(ctx, name)
			if err != nil {
				return reconcilerclient.CheckResult{}, err //nolint:wrapcheck // identity preserved
			}

			// ArgoCD exposes no per-poll status, so Status is left empty.
			return reconcilerclient.CheckResult{Ready: ready}, nil
		},
		func(string) error {
			return applicationReadinessTimeoutError(name)
		},
	)
}

// applicationReadinessTimeoutError returns the actionable error for an ArgoCD
// Application that did not become ready within the timeout. Hoisted out of the
// poll loop so the message is defined once instead of duplicated per branch.
func applicationReadinessTimeoutError(name string) error {
	return fmt.Errorf(
		"%w — "+
			"run 'ksail workload get applications.argoproj.io %s -n argocd' to inspect",
		argocd.ErrReconcileTimeout, name,
	)
}
