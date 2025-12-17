package flux

import (
	"context"
	"fmt"
	"net/url"
	"time"

	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type sourceHelmFlags struct {
	url             string
	secretRef       string
	interval        time.Duration
	export          bool
	ociProvider     string
	passCredentials bool
}

func (c *Client) newCreateSourceHelmCmd() *cobra.Command {
	flags := &sourceHelmFlags{
		interval: time.Minute,
	}

	cmd := &cobra.Command{
		Use:   "helm [name]",
		Short: "Create or update a HelmRepository source",
		Long:  "Create or update a HelmRepository source using Flux APIs",
		Example: `  # Create a source from an HTTPS Helm repository
  ksail workload create source helm podinfo \
    --url=https://stefanprodan.github.io/podinfo

  # Create a source for an OCI Helm repository
  ksail workload create source helm podinfo \
    --url=oci://ghcr.io/stefanprodan/charts/podinfo \
    --secret-ref=docker-config`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace := extractNameAndNamespace(cmd, args)

			return c.createHelmRepository(cmd.Context(), name, namespace, flags)
		},
	}

	cmd.Flags().StringVar(&flags.url, "url", "", "Helm repository address")
	cmd.Flags().
		StringVar(&flags.secretRef, "secret-ref", "", "the name of an existing secret containing credentials")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "source sync interval")
	cmd.Flags().BoolVar(&flags.export, "export", false, "export in YAML format to stdout")
	cmd.Flags().StringVar(&flags.ociProvider, "oci-provider", "", "OCI provider for authentication")
	cmd.Flags().
		BoolVar(&flags.passCredentials, "pass-credentials", false, "pass credentials to all domains")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func (c *Client) createHelmRepository(
	ctx context.Context,
	name, namespace string,
	flags *sourceHelmFlags,
) error {
	helmRepo := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL:      flags.url,
			Interval: metav1.Duration{Duration: flags.interval},
		},
	}

	// Check if URL is OCI
	parsedURL, err := url.Parse(flags.url)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	if parsedURL.Scheme == sourcev1.HelmRepositoryTypeOCI {
		helmRepo.Spec.Type = sourcev1.HelmRepositoryTypeOCI
		helmRepo.Spec.Provider = flags.ociProvider
	}

	// Set secret reference if provided
	if flags.secretRef != "" {
		helmRepo.Spec.SecretRef = &meta.LocalObjectReference{
			Name: flags.secretRef,
		}
		helmRepo.Spec.PassCredentials = flags.passCredentials
	}

	// Export mode
	if flags.export {
		return c.exportResource(helmRepo)
	}

	// Create or update the resource
	return c.upsertResource(ctx, helmRepo, &sourcev1.HelmRepository{}, "HelmRepository")
}
