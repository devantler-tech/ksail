// Package operator provides the hidden `ksail operator` command that runs the KSail
// Kubernetes operator inside a hub cluster.
package operator

import (
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	operatorsvc "github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Environment variables carrying the OIDC secrets, mounted from a Secret by the Helm chart so they
// never appear in the process argument list.
const (
	envOIDCClientSecret  = "KSAIL_OPERATOR_OIDC_CLIENT_SECRET"  //nolint:gosec // G101: env var name, not a secret
	envOIDCSessionSecret = "KSAIL_OPERATOR_OIDC_SESSION_SECRET" //nolint:gosec // G101: env var name, not a secret
)

// oidcFlags holds the non-secret OIDC flag values; the client and session secrets come from the
// environment so they are not exposed in the process argument list.
type oidcFlags struct {
	issuerURL   string
	clientID    string
	redirectURL string
	scopes      string
}

// config assembles the OIDC configuration, reading the secrets from the environment.
func (f *oidcFlags) config() api.OIDCConfig {
	return api.OIDCConfig{
		IssuerURL:     f.issuerURL,
		ClientID:      f.clientID,
		ClientSecret:  os.Getenv(envOIDCClientSecret),
		RedirectURL:   f.redirectURL,
		Scopes:        parseScopes(f.scopes),
		SessionSecret: []byte(os.Getenv(envOIDCSessionSecret)),
		SecureCookies: strings.HasPrefix(f.redirectURL, "https://"),
	}
}

// NewOperatorCmd creates the `ksail operator` command.
func NewOperatorCmd() *cobra.Command {
	var (
		opts operatorsvc.Options
		oidc oidcFlags
	)

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Run the KSail Kubernetes operator",
		Long: `Run the KSail Kubernetes operator.

The operator runs inside a hub Kubernetes cluster, watches Cluster custom resources, and
continuously reconciles them by creating, updating, and deleting the underlying clusters.
It is intended to be deployed via the KSail operator Helm chart rather than run directly.`,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			opts.OIDC = oidc.config()

			// SetupSignalHandler returns a context cancelled on SIGINT/SIGTERM for graceful shutdown.
			err := operatorsvc.Run(ctrl.SetupSignalHandler(), opts)
			if err != nil {
				return fmt.Errorf("run operator: %w", err)
			}

			return nil
		},
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	bindOperatorFlags(cmd, &opts, &oidc)

	return cmd
}

// bindOperatorFlags registers the operator command flags onto cmd.
func bindOperatorFlags(cmd *cobra.Command, opts *operatorsvc.Options, oidc *oidcFlags) {
	cmd.Flags().StringVar(
		&opts.MetricsBindAddress,
		"metrics-bind-address",
		"0",
		"Address the metrics endpoint binds to (\"0\" disables it)",
	)
	cmd.Flags().StringVar(
		&opts.HealthProbeBindAddress,
		"health-probe-bind-address",
		":8081",
		"Address the health and readiness probes bind to",
	)
	cmd.Flags().BoolVar(
		&opts.LeaderElection,
		"leader-elect",
		false,
		"Enable leader election to ensure only one active operator instance",
	)
	cmd.Flags().StringVar(
		&opts.APIBindAddress,
		"api-bind-address",
		"",
		"Address the REST API binds to (empty disables it, e.g. \":8080\")",
	)
	cmd.Flags().BoolVar(
		&opts.ReadOnly,
		"read-only",
		false,
		"Run the REST API in read-only mode, rejecting all mutating requests",
	)
	cmd.Flags().BoolVar(
		&opts.HostCluster,
		"host-cluster",
		true,
		"Register the cluster the operator runs on as a Cluster resource (named \"host\") "+
			"so it appears in the cluster list",
	)
	cmd.Flags().BoolVar(
		&opts.DevLogging,
		"dev-logging",
		false,
		"Emit human-readable console logs instead of structured JSON (for local development)",
	)
	bindOIDCFlags(cmd, oidc)
}

// bindOIDCFlags registers the OIDC flags onto cmd (split from bindOperatorFlags for length).
func bindOIDCFlags(cmd *cobra.Command, oidc *oidcFlags) {
	cmd.Flags().StringVar(
		&oidc.issuerURL,
		"oidc-issuer-url",
		"",
		"OIDC issuer URL; enables app-driven OIDC authentication on the REST API when set",
	)
	cmd.Flags().StringVar(
		&oidc.clientID,
		"oidc-client-id",
		"",
		"OIDC client ID (the client secret is read from "+envOIDCClientSecret+")",
	)
	cmd.Flags().StringVar(
		&oidc.redirectURL,
		"oidc-redirect-url",
		"",
		"OIDC callback URL reachable by the browser (e.g. https://host/api/v1/auth/callback)",
	)
	cmd.Flags().StringVar(
		&oidc.scopes,
		"oidc-scopes",
		"openid email profile",
		"OIDC scopes, space- or comma-separated (openid is always included)",
	)
}

// parseScopes splits a space- or comma-separated scope list, dropping the always-included openid
// scope so it is not requested twice.
func parseScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ','
	})

	scopes := make([]string, 0, len(fields))

	for _, field := range fields {
		if field == "openid" {
			continue
		}

		scopes = append(scopes, field)
	}

	return scopes
}
