package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/docker/docker/client"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
)

// diagnoseLongDesc describes the `ksail cluster diagnose` command.
const diagnoseLongDesc = `Surface failing Kubernetes resources for the ` +
	`current cluster.

This command inspects the live cluster via the Kubernetes API and reports
any pods that are not running successfully, any nodes that are not Ready,
and any PersistentVolumeClaims stuck in Pending phase. Each finding
includes a severity classification and, where a known failure pattern is
detected, a proactive remediation suggestion.

A health score (0–100) summarises overall cluster health: each critical
finding deducts 25 points and each warning deducts 10 points.

The output is intentionally compact so it can be consumed directly by users
or by the KSail AI chat assistant (ksail chat) and MCP server, which expose
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
func NewDiagnoseCmd(_ *di.Runtime) *cobra.Command {
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

// runInfoCmd orchestrates the cluster info command flow:
// 1. Resolve cluster identity (name, provider, kubeconfig)
// 2. Query provider API for cluster status
// 3. Attempt kubectl cluster-info
// 4. Display combined results
// 5. Return nil (exit 0) if any info available, error (exit 1) if nothing.
func runInfoCmd(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
) error {
	resolved, err := lifecycle.ResolveClusterInfo(
		cmd, nameFlag, providerFlag, "",
	)
	if err != nil {
		return fmt.Errorf("resolve cluster info: %w", err)
	}

	writer := cmd.OutOrStdout()

	// Phase 1: Query provider API
	status, provErr := getProviderStatus(
		cmd,
		resolved.Provider,
		resolved.ClusterName,
		resolved.OmniOpts,
	)

	if errors.Is(provErr, errUnsupportedProvider) {
		return provErr
	}

	provErr = classifyProviderError(provErr)

	hasProviderInfo := provErr == nil && status != nil
	if hasProviderInfo {
		displayProviderStatus(writer, resolved.Provider, resolved.ClusterName, status)
	}

	// Resolve the requested cluster's kubeconfig context. Phases 2 and 3 are
	// scoped to it so a non-existent cluster does not silently fall back to the
	// kubeconfig's current context (which would misreport an unrelated cluster
	// as the requested one). An empty result means no context matched.
	contextName, ctxErr := resolveClusterContext(resolved.KubeconfigPath, resolved.ClusterName)
	if errors.Is(ctxErr, ErrAmbiguousCluster) {
		// Surface the ambiguity (like 'cluster switch') instead of silently
		// behaving like "not found".
		return ctxErr
	}

	// Phase 2: Attempt kubectl cluster-info, scoped to the resolved context.
	hasKubeInfo := tryScopedKubeInfo(
		cmd, writer, resolved.KubeconfigPath, contextName, hasProviderInfo,
	)

	// Phase 3: Append KSail details (TTL, components). Only when we resolved a
	// context — displayKSailDetails would otherwise fall back to the current
	// context and reintroduce the cross-cluster false-positive.
	if hasProviderInfo || hasKubeInfo {
		displayKSailDetails(cmd, resolved.KubeconfigPath, contextName)

		return nil
	}

	return buildNoInfoError(resolved.ClusterName, provErr)
}

// tryScopedKubeInfo runs kubectl cluster-info scoped to contextName, skipping
// entirely when it is empty (falling back to the current context there is the
// cross-cluster false-positive we are preventing). When the API is unreachable
// but provider info is present, it prints an "unreachable" notice. It returns
// whether kube info was obtained.
func tryScopedKubeInfo(
	cmd *cobra.Command,
	writer io.Writer,
	kubeconfigPath, contextName string,
	hasProviderInfo bool,
) bool {
	hasKubeInfo := false
	if contextName != "" {
		hasKubeInfo = tryKubeClusterInfo(cmd, kubeconfigPath, contextName) == nil
	}

	if !hasKubeInfo && hasProviderInfo {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, "  Kubernetes API: unreachable")
	}

	return hasKubeInfo
}

// classifyProviderError returns nil for soft errors that mean "no provider info"
// (missing credentials, cluster not found) and passes through real errors.
func classifyProviderError(err error) error {
	if errors.Is(err, errProviderNotConfigured) ||
		errors.Is(err, provider.ErrClusterNotFound) {
		return nil
	}

	return err
}

// buildNoInfoError creates the final error when no info is available.
func buildNoInfoError(clusterName string, provErr error) error {
	if provErr != nil {
		return fmt.Errorf(
			"%w for %q: provider: %w",
			errNoClusterInfo,
			clusterName,
			provErr,
		)
	}

	return fmt.Errorf(
		"%w for %q",
		errNoClusterInfo,
		clusterName,
	)
}

// resolveClusterContext returns the kubeconfig context name for the requested
// cluster. It reuses the same name→context resolution as 'cluster switch' so
// 'cluster info' only inspects the cluster it was asked about.
//
// It returns ("", nil) when the kubeconfig cannot be read or parsed (best
// effort — callers fall back to provider info), ("", ErrContextNotFound) when
// no context matches, and ("", ErrAmbiguousCluster) when the name matches more
// than one context. Callers should surface the ambiguity error rather than
// treating it as "not found".
func resolveClusterContext(kubeconfigPath, clusterName string) (string, error) {
	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", nil //nolint:nilerr // unresolvable kubeconfig path is non-fatal for info
	}

	configBytes, err := os.ReadFile(canonicalPath) //nolint:gosec // canonicalized above
	if err != nil {
		return "", nil //nolint:nilerr // unreadable kubeconfig is non-fatal for info
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return "", nil //nolint:nilerr // unparseable kubeconfig is non-fatal for info
	}

	return resolveContextName(config, clusterName)
}

// getProviderStatus queries the infrastructure provider for cluster status.
// Returns nil status if the cluster doesn't exist in the provider.
func getProviderStatus(
	cmd *cobra.Command,
	prov v1alpha1.Provider,
	clusterName string,
	omniOpts v1alpha1.OptionsOmni,
) (*provider.ClusterStatus, error) {
	switch prov {
	case v1alpha1.ProviderDocker, "":
		return getDockerProviderStatus(cmd, clusterName)
	case v1alpha1.ProviderHetzner:
		return getHetznerProviderStatus(cmd.Context(), clusterName)
	case v1alpha1.ProviderOmni:
		return getOmniProviderStatus(cmd.Context(), clusterName, omniOpts)
	case v1alpha1.ProviderAWS:
		// AWS/EKS status is derived from the EKS API through the provisioner,
		// not from local container inspection. Return a minimal stub so callers
		// that rely on this helper do not fail for EKS.
		return &provider.ClusterStatus{Phase: "unknown"}, nil
	case v1alpha1.ProviderKubernetes:
		// Kubernetes provider status is a stub: full pod/namespace status inspection
		// is not yet implemented. Return unknown so callers do not fail.
		return &provider.ClusterStatus{Phase: "unknown"}, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedProvider, prov)
	}
}

// getDockerProviderStatus queries Docker for cluster status by trying all label schemes.
func getDockerProviderStatus(
	cmd *cobra.Command,
	clusterName string,
) (*provider.ClusterStatus, error) {
	var result *provider.ClusterStatus

	err := withDockerClient(cmd, func(dockerClient client.APIClient) error {
		schemes := []dockerprovider.LabelScheme{
			dockerprovider.LabelSchemeKind,
			dockerprovider.LabelSchemeK3d,
			dockerprovider.LabelSchemeTalos,
			dockerprovider.LabelSchemeVCluster,
			dockerprovider.LabelSchemeKWOK,
		}

		for _, scheme := range schemes {
			prov := dockerprovider.NewProvider(dockerClient, scheme)

			status, err := prov.GetClusterStatus(cmd.Context(), clusterName)
			if err != nil {
				if errors.Is(err, provider.ErrClusterNotFound) {
					continue
				}

				return fmt.Errorf(
					"docker label scheme %s: %w", scheme, err,
				)
			}

			if status != nil && status.NodesTotal > 0 {
				result = status

				return nil
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("docker provider status: %w", err)
	}

	return result, nil
}

// getHetznerProviderStatus queries Hetzner Cloud for cluster status.
func getHetznerProviderStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HCLOUD_TOKEN: %w", errProviderNotConfigured)
	}

	hetznerClient := hcloud.NewClient(hcloud.WithToken(token))
	prov := hetzner.NewProvider(hetznerClient)

	result, err := prov.GetClusterStatus(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("hetzner provider status: %w", err)
	}

	return result, nil
}

// getOmniProviderStatus queries Omni for cluster status.
func getOmniProviderStatus(
	ctx context.Context,
	clusterName string,
	omniOpts v1alpha1.OptionsOmni,
) (*provider.ClusterStatus, error) {
	omniProvider, err := omni.NewProviderFromOptions(omniOpts)
	if err != nil {
		if errors.Is(err, omni.ErrEndpointRequired) ||
			errors.Is(err, omni.ErrServiceAccountKeyRequired) {
			return nil, fmt.Errorf(
				"%w: %w", errProviderNotConfigured, err,
			)
		}

		return nil, fmt.Errorf("omni provider: %w", err)
	}

	result, err := omniProvider.GetClusterStatus(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("omni provider status: %w", err)
	}

	return result, nil
}

// displayProviderStatus prints the provider-level cluster status.
func displayProviderStatus(
	writer io.Writer,
	prov v1alpha1.Provider,
	clusterName string,
	status *provider.ClusterStatus,
) {
	_, _ = fmt.Fprintf(writer, "Provider:     %s\n", prov)
	_, _ = fmt.Fprintf(writer, "Cluster:      %s\n", clusterName)

	if status.Endpoint != "" {
		_, _ = fmt.Fprintf(writer, "Endpoint:     %s\n", status.Endpoint)
	}

	_, _ = fmt.Fprintf(writer, "Status:       %s\n", strings.ToUpper(status.Phase))
	_, _ = fmt.Fprintf(writer, "Ready:        %d/%d (ready/total)\n",
		status.NodesReady, status.NodesTotal)

	if len(status.Nodes) > 0 {
		_, _ = fmt.Fprintln(writer, "Nodes:")

		for _, node := range status.Nodes {
			_, _ = fmt.Fprintf(writer, "  - %-40s %-15s %s\n",
				node.Name, node.Role, node.State)
		}
	}
}

// Retry configuration for kubectl cluster-info.
// The API server may not be ready immediately after cluster creation
// (e.g., K3d reports "created successfully" before K3s API is reachable).
const (
	clusterInfoMaxAttempts = 3
	clusterInfoRetryDelay  = 2 * time.Second
)

// tryKubeClusterInfo attempts kubectl cluster-info with retries and writes
// output to cmd's writer. Output is buffered during retries so that failed
// attempts do not leak partial output. Returns nil on success, an error if
// the Kubernetes API is unreachable after all attempts.
func tryKubeClusterInfo(cmd *cobra.Command, kubeconfigPath, contextName string) error {
	var lastErr error

	for attempt := 1; attempt <= clusterInfoMaxAttempts; attempt++ {
		var buf bytes.Buffer

		kubectlClient := kubectl.NewClient(genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    &buf,
			ErrOut: io.Discard,
		})

		kubeCmd := kubectlClient.CreateClusterInfoCommand(kubeconfigPath, contextName)

		// Suppress kubectl's own error output
		kubeCmd.SetErr(io.Discard)
		kubeCmd.SilenceErrors = true
		kubeCmd.SilenceUsage = true
		// Prevent Cobra from parsing the parent ksail command's os.Args.
		kubeCmd.SetArgs([]string{})

		_, lastErr = kubeCmd.ExecuteC()
		if lastErr == nil {
			// Success — flush buffered output to the real writer.
			_, _ = io.Copy(cmd.OutOrStdout(), &buf)

			return nil
		}

		if attempt < clusterInfoMaxAttempts {
			select {
			case <-time.After(clusterInfoRetryDelay):
			case <-cmd.Context().Done():
				return fmt.Errorf("kubectl cluster-info cancelled: %w", cmd.Context().Err())
			}
		}
	}

	return fmt.Errorf("kubectl cluster-info failed after %d attempts: %w",
		clusterInfoMaxAttempts, lastErr)
}

// displayKSailDetails appends KSail-specific cluster metadata after kubectl output.
// This includes cluster identity (name, distribution, provider), TTL status,
// and enabled component summary from persisted state. Each section fails gracefully.
//
// It requires a resolved contextName: with an empty context, DetectInfo would
// fall back to the kubeconfig current context and could report an unrelated
// cluster, so details are skipped entirely in that case.
func displayKSailDetails(cmd *cobra.Command, kubeconfigPath, contextName string) {
	if contextName == "" {
		return
	}

	info, err := clusterdetector.DetectInfo(kubeconfigPath, contextName)
	if err != nil || info == nil {
		// If detection fails, skip KSail details because cluster identity could not be determined.
		return
	}

	writer := cmd.OutOrStdout()

	// Blank line to separate from kubectl output.
	_, _ = fmt.Fprintln(writer)

	displayClusterIdentity(writer, info)
	displayTTLInfo(writer, info.ClusterName)
	displayComponents(writer, info.ClusterName)
}

// displayClusterIdentity prints the cluster name, distribution, provider, kubeconfig context,
// server URL, and kubeconfig path.
func displayClusterIdentity(writer io.Writer, info *clusterdetector.Info) {
	_, _ = fmt.Fprintln(writer, "KSail Cluster Details:")
	_, _ = fmt.Fprintf(writer, "  Cluster:        %s\n", info.ClusterName)
	_, _ = fmt.Fprintf(writer, "  Distribution:   %s\n", info.Distribution)
	_, _ = fmt.Fprintf(writer, "  Provider:       %s\n", info.Provider)

	if info.Context != "" {
		_, _ = fmt.Fprintf(writer, "  Context:        %s\n", info.Context)
	}

	if info.ServerURL != "" {
		_, _ = fmt.Fprintf(writer, "  Server:         %s\n", info.ServerURL)
	}

	if info.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(writer, "  Kubeconfig:     %s\n", info.KubeconfigPath)
	}
}

// displayTTLInfo prints TTL status if set.
func displayTTLInfo(writer io.Writer, clusterName string) {
	ttlInfo, err := state.LoadClusterTTL(clusterName)
	if err != nil || ttlInfo == nil {
		return
	}

	_, _ = fmt.Fprintln(writer)

	remaining := ttlInfo.Remaining()
	if remaining <= 0 {
		notify.Warningf(writer,
			"cluster TTL has EXPIRED (was set to %s)", ttlInfo.Duration)
	} else {
		notify.Infof(
			writer,
			"cluster TTL: %s remaining (set to %s)",
			formatRemainingDuration(remaining),
			ttlInfo.Duration,
		)
	}
}

// displayComponents loads the persisted ClusterSpec and prints the enabled components summary.
func displayComponents(writer io.Writer, clusterName string) {
	spec, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		return
	}

	type row struct{ label, value string }

	rows := []row{
		{"GitOps Engine:", componentLabel(string(spec.GitOpsEngine))},
		{"CNI:", componentLabel(string(spec.CNI))},
		{"CSI:", componentLabel(string(spec.CSI))},
		{"Metrics Server:", componentLabel(string(spec.MetricsServer))},
		{"Load Balancer:", componentLabel(string(spec.LoadBalancer))},
		{"Cert Manager:", componentLabel(string(spec.CertManager))},
		{"Policy Engine:", componentLabel(string(spec.PolicyEngine))},
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "  Components:")

	for _, r := range rows {
		_, _ = fmt.Fprintf(writer, "    %-16s%s\n", r.label, r.value)
	}
}

// componentLabel returns a display label for a component value.
// Empty strings and "None" sentinel values are shown as "(none)".
// "Disabled" sentinel values (used by CSI, MetricsServer, CertManager, etc.) are shown as "(disabled)".
func componentLabel(value string) string {
	switch value {
	case "":
		return "(none)"
	case "None":
		return "(none)"
	case "Disabled":
		return "(disabled)"
	default:
		return value
	}
}
