package oidc

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/spf13/cobra"
)

// NewOIDCCmd creates the parent 'oidc' command group.
func NewOIDCCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oidc",
		Short: "OIDC authentication utilities",
		Long: `OIDC authentication utilities for Kubernetes clusters.

Provides an exec credential plugin that can be used in kubeconfig
to authenticate with OIDC providers (e.g., Dex, Keycloak).`,
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	cmd.AddCommand(newGetTokenCmd())

	return cmd
}
