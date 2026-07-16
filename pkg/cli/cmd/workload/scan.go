package workload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubescapeclient "github.com/devantler-tech/ksail/v7/pkg/client/kubescape"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/spf13/cobra"
)

// ErrInvalidComplianceThreshold is returned when --compliance-threshold is outside the valid 0-100 range.
var ErrInvalidComplianceThreshold = errors.New("--compliance-threshold must be between 0 and 100")

// scanCmdLong is the long help for the scan command.
const scanCmdLong = `Run security scans on Kubernetes manifests using Kubescape.

This command scans manifests in the specified path against security frameworks
such as NSA-CISA, MITRE ATT&CK, and CIS Benchmarks.

When the target directory is a Kustomize root, manifests are rendered before
scanning (Kustomize build + Flux variable substitution + in-process Helm
templating of HelmReleases), so findings reflect overlay patches and chart output
rather than the raw files. HelmReleases that cannot be rendered offline are left
as-is with a warning. Use --no-render to scan the raw files instead.

` + sourcePathResolutionHelp + `

Exceptions: pass --exceptions <file> to forward a Kubescape exceptions file (a JSON
array of PostureExceptionPolicy objects) so justified, runtime-enforced findings
(e.g. Kyverno admission mutation, Cilium network policies, VPA-managed resources) are
suppressed and --compliance-threshold 100 can gate CI. The file loads locally with no
cloud account and the scan stays offline.

The frameworks, exceptions file, and compliance threshold can also be set under
spec.workload.scan in ksail.yaml so 'ksail workload scan' (no args) is a turnkey CI
gate; a relative exceptions path resolves against the ksail.yaml directory and CLI
flags override the config.

Available frameworks: nsa, mitre, cis, pss (and any other framework supported by Kubescape)
Available output formats: pretty-printer, json, sarif, junit (and any other format supported by Kubescape)

For more information, see https://github.com/kubescape/kubescape`

// NewScanCmd creates the workload scan command.
func NewScanCmd() *cobra.Command {
	var (
		frameworks          []string
		format              string
		output              string
		complianceThreshold float32
		verbose             bool
		noRender            bool
		exceptions          string
		ephemeral           bool
	)

	cmd := &cobra.Command{
		Use:   "scan [PATH]",
		Short: "Run security scans on Kubernetes manifests",
		Long:  scanCmdLong,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScanCmd(
				cmd.Context(),
				cmd,
				args,
				scanFlags{
					frameworks:          frameworks,
					format:              format,
					output:              output,
					complianceThreshold: complianceThreshold,
					verbose:             verbose,
					noRender:            noRender,
					exceptions:          exceptions,
					ephemeral:           ephemeral,
				},
			)
		},
		SilenceUsage: true,
	}

	addScanFlags(
		cmd,
		&frameworks,
		&format,
		&output,
		&complianceThreshold,
		&verbose,
		&noRender,
		&exceptions,
		&ephemeral,
	)

	return cmd
}

// scanFlags carries the resolved scan command flags.
type scanFlags struct {
	frameworks          []string
	format              string
	output              string
	complianceThreshold float32
	verbose             bool
	noRender            bool
	exceptions          string
	ephemeral           bool
}

// addScanFlags registers the flags for the scan command.
func addScanFlags(
	cmd *cobra.Command,
	frameworks *[]string,
	format, output *string,
	complianceThreshold *float32,
	verbose, noRender *bool,
	exceptions *string,
	ephemeral *bool,
) {
	cmd.Flags().StringSliceVar(frameworks, "framework", []string{"nsa"},
		"Security frameworks to scan against (e.g. nsa, mitre, cis, pss) "+
			"(overrides spec.workload.scan.frameworks from ksail.yaml)")
	cmd.Flags().StringVar(format, "format", "pretty-printer",
		"Output format (pretty-printer, json, sarif, junit)")
	cmd.Flags().StringVarP(output, "output", "o", "",
		"Output file path (stdout if empty)")
	cmd.Flags().Float32Var(complianceThreshold, "compliance-threshold", 0,
		"Fail if compliance score is below this threshold (0-100) "+
			"(overrides spec.workload.scan.complianceThreshold from ksail.yaml)")
	cmd.Flags().BoolVar(verbose, "verbose", false,
		"Show all resources in output, not just failed ones")
	cmd.Flags().BoolVar(noRender, "no-render", false,
		"Scan the raw manifest files instead of the Kustomize + Helm rendered output "+
			"(skip rendering entirely; restores the pre-rendering behavior)")
	cmd.Flags().StringVar(exceptions, "exceptions", "",
		"Path to a Kubescape exceptions file (a JSON array of PostureExceptionPolicy "+
			"objects) forwarded to Kubescape's --exceptions "+
			"(overrides spec.workload.scan.exceptions from ksail.yaml)")
	cmd.Flags().BoolVar(ephemeral, "ephemeral", false,
		"EXPERIMENTAL (ksail#5919): provision an isolated throwaway Kind cluster for the duration of "+
			"this command (guaranteed teardown) and install the workload's declared Helm charts "+
			"into it, so declared operators' CRDs are registered. Applying rendered manifests "+
			"and scanning operator-rendered children is the next slice — off by default.")
}

// runScanCmd dispatches to runScanCmdInner directly, or — when --ephemeral is
// set — wraps it in an isolated throwaway Kind cluster that is guaranteed to be torn
// down afterwards (see withEphemeralCluster, shared with the validate
// command). While the cluster is live, the workload's declared charts are
// installed into it first (installDeclaredCharts, ksail#5919 Phase 3b-2) so
// the declared operators' CRDs are registered; applying the rendered
// manifests and scanning their operator-rendered children is the remaining
// Phase 3b-3.
func runScanCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags scanFlags,
) error {
	if flags.ephemeral {
		return withPreparedEphemeralCluster(ctx, cmd, args, func(ctx context.Context) error {
			return runScanCmdInner(ctx, cmd, args, flags)
		})
	}

	return runScanCmdInner(ctx, cmd, args, flags)
}

// runScanCmdInner runs the scan itself — config load, target resolution, and
// the Kubescape pass — after any ephemeral-cluster preparation has completed.
func runScanCmdInner(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags scanFlags,
) error {
	// Load ksail.yaml once so the source path, the Helm-render setting, and the
	// spec.workload.scan gate settings are derived from a single read (mirrors
	// the validate command).
	cfg, configFound, loadErr := loadValidateConfigSilently(cmd)

	settings, err := resolveScanSettings(cmd, flags, cfg, configFound)
	if err != nil {
		return err
	}

	path, err := resolveValidatePath(args, cfg, configFound, loadErr)
	if err != nil {
		return err
	}

	canonPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}

	path = canonPath

	output, err := resolveScanOutput(flags.output)
	if err != nil {
		return err
	}

	// Scan the Kustomize + Helm rendered manifests so findings reflect overlay
	// patches and chart output; --no-render (or helmRender: false) scans raw files.
	scanPath, cleanup, err := resolveScanInput(ctx, cmd, path, cfg, configFound, flags.noRender)
	if err != nil {
		return err
	}

	defer cleanup()

	kubescapeClient := kubescapeclient.NewClient()

	scanOpts := &kubescapeclient.ScanOptions{
		Frameworks:          settings.frameworks,
		Format:              flags.format,
		Output:              output,
		ComplianceThreshold: settings.complianceThreshold,
		Verbose:             flags.verbose,
		Exceptions:          settings.exceptions,
	}

	err = kubescapeClient.ScanDirectory(ctx, scanPath, scanOpts)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}

	return nil
}

// scanSettings carries the scan gate settings after merging CLI flags with
// spec.workload.scan from ksail.yaml.
type scanSettings struct {
	frameworks          []string
	complianceThreshold float32
	exceptions          string
}

// resolveScanSettings merges the scan flags with spec.workload.scan from
// ksail.yaml, applying flag > config > default precedence per field (a
// configured value is used only when the corresponding flag was not set). The
// resolved compliance threshold is range-checked and a non-empty exceptions path
// is canonicalized (symlink-safe; tolerates a not-yet-existing file).
func resolveScanSettings(
	cmd *cobra.Command,
	flags scanFlags,
	cfg *v1alpha1.Cluster,
	configFound bool,
) (scanSettings, error) {
	scanCfg := workloadScanConfig(cfg, configFound)

	threshold := resolveScanThreshold(
		cmd.Flags().Changed("compliance-threshold"),
		flags.complianceThreshold,
		scanCfg.ComplianceThreshold,
	)
	if threshold < 0 || threshold > 100 {
		return scanSettings{}, fmt.Errorf("%w, got %.2f", ErrInvalidComplianceThreshold, threshold)
	}

	exceptions, err := resolveScanExceptions(
		cmd.Flags().Changed("exceptions"),
		flags.exceptions,
		scanCfg.Exceptions,
	)
	if err != nil {
		return scanSettings{}, err
	}

	return scanSettings{
		frameworks: resolveScanFrameworks(
			cmd.Flags().Changed("framework"),
			flags.frameworks,
			scanCfg.Frameworks,
		),
		complianceThreshold: threshold,
		exceptions:          exceptions,
	}, nil
}

// resolveScanFrameworks returns the configured frameworks when the --framework
// flag was not set and the config provides them, otherwise the flag value.
func resolveScanFrameworks(flagChanged bool, flagVal, cfgVal []string) []string {
	if !flagChanged && len(cfgVal) > 0 {
		return cfgVal
	}

	return flagVal
}

// resolveScanThreshold returns the configured compliance threshold when the
// --compliance-threshold flag was not set and the config provides one,
// otherwise the flag value.
func resolveScanThreshold(flagChanged bool, flagVal float32, cfgVal *int32) float32 {
	if !flagChanged && cfgVal != nil {
		return float32(*cfgVal)
	}

	return flagVal
}

// resolveScanExceptions selects the exceptions path (flag > config) and
// canonicalizes it (symlink-safe; tolerates a not-yet-existing file). An empty
// result means no exceptions are forwarded.
func resolveScanExceptions(flagChanged bool, flagVal, cfgVal string) (string, error) {
	exceptions := flagVal
	if !flagChanged && cfgVal != "" {
		exceptions = cfgVal
	}

	if exceptions == "" {
		return "", nil
	}

	canon, err := fsutil.EvalCanonicalPath(exceptions)
	if err != nil {
		return "", fmt.Errorf("resolve exceptions path %q: %w", exceptions, err)
	}

	return canon, nil
}

// workloadScanConfig returns spec.workload.scan from the loaded config, or a zero
// ScanConfig when no config file was found (or it failed to load), so the scan
// falls back to flag values only — matching how helmRenderEnabled treats an
// unreadable config.
func workloadScanConfig(cfg *v1alpha1.Cluster, configFound bool) v1alpha1.ScanConfig {
	if !configFound || cfg == nil {
		return v1alpha1.ScanConfig{}
	}

	return cfg.Spec.Workload.Scan
}

// resolveScanOutput canonicalizes the output file path (creating its parent
// directory), returning "" when no output file is configured.
func resolveScanOutput(output string) (string, error) {
	if output == "" {
		return "", nil
	}

	err := os.MkdirAll(filepath.Dir(output), dirPerm)
	if err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	canonOutput, err := fsutil.EvalCanonicalPath(output)
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", output, err)
	}

	return canonOutput, nil
}

// resolveScanInput returns the path kubescape should scan plus a cleanup func.
// When Helm rendering is enabled and the target is a kustomization root, the
// applied manifests are rendered to a temp directory (kustomize build → Flux
// substitution → Helm) and that directory is scanned; otherwise the path is
// scanned as-is (the raw, pre-render behavior — also used for --raw, single
// files, and directories without a kustomization).
func resolveScanInput(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	cfg *v1alpha1.Cluster,
	configFound, noRender bool,
) (string, func(), error) {
	noop := func() {}

	if !helmRenderEnabled(cfg, configFound, noRender) || !hasKustomizationFile(path) {
		return path, noop, nil
	}

	tmpDir, cleanup, err := newGitOpsRenderer().renderToTempDir(ctx, cmd, path)
	if err != nil {
		return "", noop, err
	}

	canonTmp, err := fsutil.EvalCanonicalPath(tmpDir)
	if err != nil {
		cleanup()

		return "", noop, fmt.Errorf("resolve scan temp dir: %w", err)
	}

	return canonTmp, cleanup, nil
}
