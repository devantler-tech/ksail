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
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/spf13/cobra"
)

// ErrInvalidComplianceThreshold is returned when --compliance-threshold is outside the valid 0-100 range.
var ErrInvalidComplianceThreshold = errors.New("--compliance-threshold must be between 0 and 100")

// scanLongDescription is the long help text for the workload scan command.
const scanLongDescription = `Run security scans on Kubernetes manifests using Kubescape.

This command scans manifests in the specified path against security frameworks
such as NSA-CISA, MITRE ATT&CK, and CIS Benchmarks.

When the target directory is a Kustomize root, manifests are rendered before
scanning (Kustomize build + Flux variable substitution + in-process Helm
templating of HelmReleases), so findings reflect overlay patches and chart output
rather than the raw files. HelmReleases that cannot be rendered offline are left
as-is with a warning. Use --no-render to scan the raw files instead.

If no path is provided, the path is resolved in order:
  1. spec.workload.sourceDirectory from ksail.yaml (if a config file is found and the field is set)
  2. The default source directory when spec.workload.sourceDirectory is unset ("k8s" directory)
  3. The current directory (fallback when no ksail.yaml config file is found)

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
	)

	cmd := &cobra.Command{
		Use:   "scan [PATH]",
		Short: "Run security scans on Kubernetes manifests",
		Long:  scanLongDescription,
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
}

// addScanFlags registers the flags for the scan command.
func addScanFlags(
	cmd *cobra.Command,
	frameworks *[]string,
	format, output *string,
	complianceThreshold *float32,
	verbose, noRender *bool,
	exceptions *string,
) {
	cmd.Flags().StringSliceVar(frameworks, "framework", []string{"nsa"},
		"Security frameworks to scan against (e.g. nsa, mitre, cis, pss)")
	cmd.Flags().StringVar(format, "format", "pretty-printer",
		"Output format (pretty-printer, json, sarif, junit)")
	cmd.Flags().StringVarP(output, "output", "o", "",
		"Output file path (stdout if empty)")
	cmd.Flags().Float32Var(complianceThreshold, "compliance-threshold", 0,
		"Fail if compliance score is below this threshold (0-100)")
	cmd.Flags().BoolVar(verbose, "verbose", false,
		"Show all resources in output, not just failed ones")
	cmd.Flags().BoolVar(noRender, "no-render", false,
		"Scan the raw manifest files instead of the Kustomize + Helm rendered output "+
			"(skip rendering entirely; restores the pre-rendering behavior)")
	cmd.Flags().StringVar(exceptions, "exceptions", "",
		"Path to a Kubescape exceptions file (forwarded to Kubescape's --exceptions) "+
			"to suppress justified findings for runtime-enforced controls a static scan cannot see")
}

func runScanCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags scanFlags,
) error {
	// Load ksail.yaml once so the source path, the Helm-render setting, and the
	// scan defaults are derived from a single read (mirrors the validate command).
	cfg, configFound, loadErr := loadValidateConfigSilently(cmd)

	// spec.workload.scan supplies defaults for frameworks/exceptions/threshold so
	// `ksail workload scan` (no args) can act as a turnkey CI gate; an explicitly
	// set flag wins.
	flags = applyScanConfig(cmd, flags, cfg, configFound, loadErr)

	if flags.complianceThreshold < 0 || flags.complianceThreshold > 100 {
		return fmt.Errorf("%w, got %.2f", ErrInvalidComplianceThreshold, flags.complianceThreshold)
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
		Frameworks:          flags.frameworks,
		Format:              flags.format,
		Output:              output,
		ComplianceThreshold: flags.complianceThreshold,
		Verbose:             flags.verbose,
		Exceptions:          flags.exceptions,
	}

	err = kubescapeClient.ScanDirectory(ctx, scanPath, scanOpts)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}

	return nil
}

// applyScanConfig overlays spec.workload.scan from the single config load onto
// the resolved flags so `ksail workload scan` (no args) honors the configured
// frameworks, exceptions file, and compliance threshold. An explicitly-set flag
// always wins (checked via cmd.Flags().Changed). A config file that exists but
// failed to load does not fail the scan (the flag defaults still apply) but
// emits a warning so an unreadable ksail.yaml does not silently drop the gate.
func applyScanConfig(
	cmd *cobra.Command,
	flags scanFlags,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
) scanFlags {
	if loadErr != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "could not read spec.workload.scan from ksail.yaml: %v",
			Args:    []any{loadErr},
			Writer:  cmd.ErrOrStderr(),
		})

		return flags
	}

	if !configFound || cfg == nil {
		return flags
	}

	scan := cfg.Spec.Workload.Scan

	if !cmd.Flags().Changed("framework") && len(scan.Frameworks) > 0 {
		flags.frameworks = scan.Frameworks
	}

	if !cmd.Flags().Changed("exceptions") && scan.Exceptions != "" {
		flags.exceptions = scan.Exceptions
	}

	if !cmd.Flags().Changed("compliance-threshold") && scan.ComplianceThreshold > 0 {
		flags.complianceThreshold = float32(scan.ComplianceThreshold)
	}

	return flags
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
