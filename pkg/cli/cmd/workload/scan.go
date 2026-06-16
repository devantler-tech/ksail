package workload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	kubescapeclient "github.com/devantler-tech/ksail/v7/pkg/client/kubescape"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/spf13/cobra"
)

// ErrInvalidComplianceThreshold is returned when --compliance-threshold is outside the valid 0-100 range.
var ErrInvalidComplianceThreshold = errors.New("--compliance-threshold must be between 0 and 100")

// NewScanCmd creates the workload scan command.
func NewScanCmd() *cobra.Command {
	var (
		frameworks          []string
		format              string
		output              string
		complianceThreshold float32
		verbose             bool
	)

	cmd := &cobra.Command{
		Use:   "scan [PATH]",
		Short: "Run security scans on Kubernetes manifests",
		Long: `Run security scans on Kubernetes manifests using Kubescape.

This command scans manifests in the specified path against security frameworks
such as NSA-CISA, MITRE ATT&CK, and CIS Benchmarks.

If no path is provided, the path is resolved in order:
  1. spec.workload.sourceDirectory from ksail.yaml (if a config file is found and the field is set)
  2. The default source directory when spec.workload.sourceDirectory is unset ("k8s" directory)
  3. The current directory (fallback when no ksail.yaml config file is found)

Available frameworks: nsa, mitre, cis, pss (and any other framework supported by Kubescape)
Available output formats: pretty-printer, json, sarif, junit (and any other format supported by Kubescape)

For more information, see https://github.com/kubescape/kubescape`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScanCmd(
				cmd.Context(),
				cmd,
				args,
				frameworks,
				format,
				output,
				complianceThreshold,
				verbose,
			)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringSliceVar(&frameworks, "framework", []string{"nsa"},
		"Security frameworks to scan against (e.g. nsa, mitre, cis, pss)")
	cmd.Flags().StringVar(&format, "format", "pretty-printer",
		"Output format (pretty-printer, json, sarif, junit)")
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"Output file path (stdout if empty)")
	cmd.Flags().Float32Var(&complianceThreshold, "compliance-threshold", 0,
		"Fail if compliance score is below this threshold (0-100)")
	cmd.Flags().BoolVar(&verbose, "verbose", false,
		"Show all resources in output, not just failed ones")

	return cmd
}

func runScanCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	frameworks []string,
	format string,
	output string,
	complianceThreshold float32,
	verbose bool,
) error {
	if complianceThreshold < 0 || complianceThreshold > 100 {
		return fmt.Errorf("%w, got %.2f", ErrInvalidComplianceThreshold, complianceThreshold)
	}

	path, err := resolveValidatePathFromCmd(cmd, args)
	if err != nil {
		return err
	}

	canonPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}

	path = canonPath

	if output != "" {
		mkdirErr := os.MkdirAll(filepath.Dir(output), dirPerm)
		if mkdirErr != nil {
			return fmt.Errorf("create output directory: %w", mkdirErr)
		}

		canonOutput, canonErr := fsutil.EvalCanonicalPath(output)
		if canonErr != nil {
			return fmt.Errorf("resolve output path %q: %w", output, canonErr)
		}

		output = canonOutput
	}

	kubescapeClient := kubescapeclient.NewClient()

	scanOpts := &kubescapeclient.ScanOptions{
		Frameworks:          frameworks,
		Format:              format,
		Output:              output,
		ComplianceThreshold: complianceThreshold,
		Verbose:             verbose,
	}

	err = kubescapeClient.ScanDirectory(ctx, path, scanOpts)
	if err != nil {
		return fmt.Errorf("scan directory: %w", err)
	}

	return nil
}
