package cluster

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/spf13/cobra"
)

// diagnoseLongDesc describes the `ksail cluster diagnose` command.
const diagnoseLongDesc = `Surface failing Kubernetes resources for the ` +
	`current cluster.

This command inspects the live cluster via the Kubernetes API and reports
any pods that are not running successfully, any nodes that are not Ready,
and any PersistentVolumeClaims stuck in Pending phase. Each finding
includes a severity classification and, where a known failure pattern is
detected, a proactive remediation suggestion.

A health score (0–100) summarizes overall cluster health: each critical
finding deducts 25 points and each warning deducts 10 points.

The output is intentionally compact so it can be consumed directly by users
or by the KSail AI chat assistant (ksail open chat) and MCP server, which expose
this command as part of the cluster_read tool. When used from the AI
assistant the output is fed back as context so Copilot can explain the root
cause and suggest remediation.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

Exit code 0 is returned even when pod or node failures are reported.
A non-zero exit code indicates the Kubernetes API could not be queried
(e.g., the cluster is unreachable or the credentials lack sufficient
permissions).`

// NewDiagnoseCmd creates the diagnose command for clusters.
func NewDiagnoseCmd() *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
		outputFlag   string
	)

	cmd := &cobra.Command{
		Use:          "diagnose",
		Short:        "Diagnose failing cluster resources",
		Long:         diagnoseLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiagnoseCmd(cmd, nameFlag, providerFlag, outputFlag)
		},
	}

	lifecycle.BindNameAndProviderFlags(cmd, &nameFlag, &providerFlag)

	cmd.Flags().StringVar(
		&outputFlag,
		"output",
		"text",
		"Output format: text or json. Use json for machine-readable structured output.",
	)

	return cmd
}

// runDiagnoseCmd inspects the live cluster via the Kubernetes API and writes
// a human-readable or JSON diagnostic report to the command's stdout. When every
// resource looks healthy the command writes a short "all healthy" banner.
func runDiagnoseCmd(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	outputFlag string,
) error {
	format := strings.ToLower(outputFlag)
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf(
			"%w: %q (expected %q or %q)",
			ErrUnsupportedOutputFormat,
			format,
			outputFormatText,
			outputFormatJSON,
		)
	}

	resolved, err := lifecycle.ResolveClusterInfo(cmd, nameFlag, providerFlag, "")
	if err != nil {
		return fmt.Errorf("resolve cluster info: %w", err)
	}

	clientset, err := k8s.NewClientset(resolved.KubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}

	report, err := k8s.DiagnoseClusterReport(cmd.Context(), clientset, resolved.ClusterName)
	if err != nil {
		return fmt.Errorf("diagnose cluster %q: %w", resolved.ClusterName, err)
	}

	writer := cmd.OutOrStdout()

	if format == outputFormatJSON {
		return runDiagnoseJSONReport(report, writer)
	}

	return runDiagnoseTextReport(report, writer)
}

// runDiagnoseTextReport writes a human-readable diagnostic report including
// the health score and per-finding remediation hints.
func runDiagnoseTextReport(report k8s.DiagnoseReport, writer io.Writer) error {
	if len(report.Findings) == 0 {
		_, _ = fmt.Fprintf(
			writer,
			"Cluster %q looks healthy — no failing pods, NotReady nodes, or pending PVCs detected.\n",
			report.ClusterName,
		)
		_, _ = fmt.Fprintf(writer, "Health score: %d/100\n", report.HealthScore)

		return nil
	}

	_, _ = fmt.Fprintf(writer, "Diagnostics for cluster %q:\n", report.ClusterName)
	_, _ = fmt.Fprintf(writer, "Health score: %d/100\n", report.HealthScore)

	for _, f := range report.Findings {
		_, _ = fmt.Fprintf(writer, "\n  [%s] %s\n", f.Severity, f.Resource)
		_, _ = fmt.Fprintf(writer, "    Reason: %s\n", f.Reason)

		if f.Remediation != "" {
			_, _ = fmt.Fprintf(writer, "    Suggested fix: %s\n", f.Remediation)
		}
	}

	return nil
}

// runDiagnoseJSONReport serialises the structured DiagnoseReport for clusterName
// as indented JSON to w. It is extracted from runDiagnoseCmd to keep that
// function within the allowed line-count limit.
func runDiagnoseJSONReport(report k8s.DiagnoseReport, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Keep '<', '>', '&' literal (e.g. in remediation hints like "<name>")
	// instead of HTML-escaping them; this is CLI output, not HTML.
	enc.SetEscapeHTML(false)

	err := enc.Encode(report)
	if err != nil {
		return fmt.Errorf("encode diagnose report: %w", err)
	}

	return nil
}
