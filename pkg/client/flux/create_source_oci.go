package flux

import (
	"context"
	"errors"
	"time"

	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrOCIRefRequired is returned when none of tag, semver, or digest is specified for OCI repository.
var ErrOCIRefRequired = errors.New("one of --tag, --tag-semver or --digest is required")

type sourceOCIFlags struct {
	url       string
	tag       string
	semver    string
	digest    string
	secretRef string
	provider  string
	interval  time.Duration
	export    bool
	insecure  bool
}

func (c *Client) newCreateSourceOCICmd() *cobra.Command {
	flags := &sourceOCIFlags{
		interval: time.Minute,
		provider: "generic",
	}

	cmd := &cobra.Command{
		Use:   "oci [name]",
		Short: "Create or update an OCIRepository source",
		Long:  "Create or update an OCIRepository source using Flux APIs",
		Example: `  # Create a source for an OCI artifact
  ksail workload create source oci podinfo \
    --url=oci://ghcr.io/stefanprodan/manifests/podinfo \
    --tag=6.6.2 \
    --namespace=flux-system

  # Create a source with semver range
  ksail workload create source oci podinfo \
    --url=oci://ghcr.io/stefanprodan/manifests/podinfo \
    --tag-semver=">=6.6.0 <7.0.0"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace := extractNameAndNamespace(cmd, args)

			return c.createOCIRepository(cmd.Context(), name, namespace, flags)
		},
	}

	cmd.Flags().StringVar(&flags.url, "url", "", "OCI repository URL")
	cmd.Flags().StringVar(&flags.tag, "tag", "", "OCI artifact tag")
	cmd.Flags().StringVar(&flags.semver, "tag-semver", "", "OCI artifact tag semver range")
	cmd.Flags().StringVar(&flags.digest, "digest", "", "OCI artifact digest")
	cmd.Flags().
		StringVar(&flags.secretRef, "secret-ref", "", "the name of an existing secret containing credentials")
	cmd.Flags().StringVar(&flags.provider, "provider", "generic", "OCI provider")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "source sync interval")
	cmd.Flags().BoolVar(&flags.export, "export", false, "export in YAML format to stdout")
	cmd.Flags().BoolVar(&flags.insecure, "insecure", false, "allow insecure connections")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func (c *Client) createOCIRepository(
	ctx context.Context,
	name, namespace string,
	flags *sourceOCIFlags,
) error {
	if flags.tag == "" && flags.semver == "" && flags.digest == "" {
		return ErrOCIRefRequired
	}

	ociRepo := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sourcev1.OCIRepositorySpec{
			URL:       flags.url,
			Provider:  flags.provider,
			Insecure:  flags.insecure,
			Interval:  metav1.Duration{Duration: flags.interval},
			Reference: &sourcev1.OCIRepositoryRef{},
		},
	}

	// Set reference based on flags
	switch {
	case flags.digest != "":
		ociRepo.Spec.Reference.Digest = flags.digest
	case flags.semver != "":
		ociRepo.Spec.Reference.SemVer = flags.semver
	case flags.tag != "":
		ociRepo.Spec.Reference.Tag = flags.tag
	}

	// Set secret reference if provided
	if flags.secretRef != "" {
		ociRepo.Spec.SecretRef = &meta.LocalObjectReference{
			Name: flags.secretRef,
		}
	}

	// Export mode
	if flags.export {
		return c.exportResource(ociRepo)
	}

	// Create or update the resource
	return c.upsertResource(ctx, ociRepo, &sourcev1.OCIRepository{}, "OCIRepository")
}
