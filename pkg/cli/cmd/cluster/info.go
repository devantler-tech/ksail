package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
)

// NewInfoCmd creates the cluster info command.
// The command queries the infrastructure provider API first, then attempts
// kubectl cluster-info, and only fails if no information is available at all.
func NewInfoCmd() *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
	)

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display cluster information",
		Long: "Display cluster information from the infrastructure provider" +
			" and Kubernetes API. Succeeds if information is available from any source.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfoCmd(cmd, nameFlag, providerFlag)
		},
	}

	lifecycle.BindNameAndProviderFlags(cmd, &nameFlag, &providerFlag)

	return cmd
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
		resolved.AWSOpts,
		resolved.AWSRegion,
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
	awsOpts v1alpha1.OptionsAWS,
	awsRegion string,
) (*provider.ClusterStatus, error) {
	switch prov {
	case v1alpha1.ProviderDocker, "":
		return getDockerProviderStatus(cmd, clusterName)
	case v1alpha1.ProviderHetzner:
		return getHetznerProviderStatus(cmd.Context(), clusterName)
	case v1alpha1.ProviderOmni:
		return getOmniProviderStatus(cmd.Context(), clusterName, omniOpts)
	case v1alpha1.ProviderAWS:
		return getAWSProviderStatus(cmd.Context(), clusterName, awsOpts, awsRegion)
	case v1alpha1.ProviderGCP, v1alpha1.ProviderAzure:
		// GCP/GKE and Azure/AKS status inspection is not yet implemented. Return
		// a minimal stub so callers that rely on this helper do not fail for them.
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

	err := withDockerClient(cmd, func(dockerClient dockerclient.Client) error {
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

// getAWSProviderStatus queries Amazon EKS for cluster status via eksctl. The
// region is resolved from the cluster config (the RegionEnvVar env var, else
// eks.yaml's region) so the lookup targets the cluster's configured region
// rather than only eksctl's AWS_REGION/profile default.
func getAWSProviderStatus(
	ctx context.Context,
	clusterName string,
	awsOpts v1alpha1.OptionsAWS,
	region string,
) (*provider.ClusterStatus, error) {
	_, eksctlOptions, providerOptions := credentials.ResolveAWSClientOptions(
		credentials.NewAWSOptionsResolver(awsOpts),
		os.Environ(),
		eksctlclient.WithEnvironment,
		eksctlclient.RequireCredentialValues,
		awsprovider.WithCredentialValues,
		awsprovider.RequireCredentialValues,
	)

	return awsProviderStatus(
		ctx,
		eksctlclient.NewClient(eksctlOptions...),
		clusterName,
		region,
		providerOptions...,
	)
}

// awsProviderStatus is the injectable core of getAWSProviderStatus: it accepts
// the eksctl client so tests can substitute a scripted runner. The resolved
// region is passed through to scope the lookup; an empty region defers to
// eksctl's own resolution (AWS_REGION / the active profile). A missing eksctl
// binary is reported as errProviderNotConfigured (a soft error
// classifyProviderError maps to "no provider info"), so 'cluster info' falls
// back to kubectl instead of failing when AWS tooling is absent, mirroring the
// Hetzner/Omni credential paths.
func awsProviderStatus(
	ctx context.Context,
	client *eksctlclient.Client,
	clusterName string,
	region string,
	opts ...awsprovider.Option,
) (*provider.ClusterStatus, error) {
	err := client.CheckAvailable()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errProviderNotConfigured, err)
	}

	awsProv, err := awsprovider.NewProvider(client, region, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws provider: %w", err)
	}

	result, err := awsProv.GetClusterStatus(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("aws provider status: %w", err)
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

	info, err := clusterdetector.DetectInfo(cmd.Context(), kubeconfigPath, contextName)
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
