package flux

import (
	"context"
	"errors"
	"fmt"
	"time"

	meta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Commit-signature verification errors for `workload create source git`.
var (
	// ErrVerifyModeWithoutSecret is returned when --verify-mode is set without --verify-secret-ref.
	ErrVerifyModeWithoutSecret = errors.New("--verify-mode requires --verify-secret-ref")
	// ErrInvalidVerifyMode is returned for a --verify-mode Flux does not support.
	ErrInvalidVerifyMode = errors.New("invalid --verify-mode")
	// ErrVerifyTagModeWithoutTag is returned when a tag verification mode has no tag reference.
	ErrVerifyTagModeWithoutTag = errors.New("tag verification requires --tag or --tag-semver")
)

type sourceGitFlags struct {
	url       string
	branch    string
	tag       string
	semver    string
	commit    string
	secretRef string
	interval  time.Duration
	export    bool

	verifySecretRef string
	verifyMode      string
}

func (c *Client) newCreateSourceGitCmd() *cobra.Command {
	flags := &sourceGitFlags{
		interval: time.Minute,
	}

	cmd := &cobra.Command{
		Use:   "git [name]",
		Short: "Create or update a GitRepository source",
		Long:  "Create or update a GitRepository source using Flux APIs",
		Example: `  # Create a source from a public Git repository master branch
  ksail workload create source git podinfo \
    --url=https://github.com/stefanprodan/podinfo \
    --branch=master

  # Create a source for a Git repository pinned to specific git tag
  ksail workload create source git podinfo \
    --url=https://github.com/stefanprodan/podinfo \
    --tag="3.2.3"

  # Create a source from a Git repository using SSH authentication
  ksail workload create source git podinfo \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --branch=master \
    --secret-ref=git-credentials

  # Create a source that only accepts SSH- or OpenPGP-signed commits from trusted keys
  ksail workload create source git podinfo \
    --url=https://github.com/stefanprodan/podinfo \
    --branch=master \
    --verify-secret-ref=git-signing-keys`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace := extractNameAndNamespace(cmd, args)

			return c.createGitRepository(cmd.Context(), name, namespace, flags)
		},
	}

	cmd.Flags().StringVar(&flags.url, "url", "", "git address, e.g. ssh://git@host/org/repository")
	cmd.Flags().StringVar(&flags.branch, "branch", "", "git branch")
	cmd.Flags().StringVar(&flags.tag, "tag", "", "git tag")
	cmd.Flags().StringVar(&flags.semver, "tag-semver", "", "git tag semver range")
	cmd.Flags().StringVar(&flags.commit, "commit", "", "git commit")
	cmd.Flags().
		StringVar(&flags.secretRef, "secret-ref", "", "the name of an existing secret containing SSH or basic credentials")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "source sync interval")
	cmd.Flags().StringVar(&flags.verifySecretRef, "verify-secret-ref", "",
		"verify commit signatures against the public keys in this existing secret "+
			"(SSH keys under *.sshpub keys, OpenPGP keys under *.asc)")
	cmd.Flags().StringVar(&flags.verifyMode, "verify-mode", "",
		"Git object to verify: HEAD, Tag or TagAndHEAD (default HEAD; requires --verify-secret-ref)")
	cmd.Flags().BoolVar(&flags.export, "export", false, "export in YAML format to stdout")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func (c *Client) createGitRepository(
	ctx context.Context,
	name, namespace string,
	flags *sourceGitFlags,
) error {
	gitRepo := &sourcev1.GitRepository{
		Name:      name,
		Namespace: namespace,
		Spec: sourcev1.GitRepositorySpec{
			URL:      flags.url,
			Interval: metav1.Duration{Duration: flags.interval},
		},
	}

	// Set reference based on flags
	switch {
	case flags.branch != "":
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Branch: flags.branch,
		}
	case flags.tag != "":
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Tag: flags.tag,
		}
	case flags.semver != "":
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			SemVer: flags.semver,
		}
	case flags.commit != "":
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Commit: flags.commit,
		}
	}

	err := setGitVerification(&gitRepo.Spec, flags)
	if err != nil {
		return err
	}

	// Set secret reference if provided
	if flags.secretRef != "" {
		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{
			Name: flags.secretRef,
		}
	}

	// Export mode
	if flags.export {
		return c.exportResource(gitRepo)
	}

	// Create or update the resource
	return c.upsertResource(ctx, gitRepo, &sourcev1.GitRepository{}, "GitRepository")
}

// setGitVerification sets the GitRepository spec.verify block. Verification is opt-in:
// without --verify-secret-ref the source is unchanged, and a mode alone is refused rather
// than silently ignored. Tag modes need a tag reference for Flux to verify.
func setGitVerification(spec *sourcev1.GitRepositorySpec, flags *sourceGitFlags) error {
	if flags.verifySecretRef == "" {
		if flags.verifyMode != "" {
			return ErrVerifyModeWithoutSecret
		}

		return nil
	}

	mode := sourcev1.ModeGitHEAD
	if flags.verifyMode != "" {
		mode = sourcev1.GitVerificationMode(flags.verifyMode)
	}

	switch mode {
	case sourcev1.ModeGitHEAD:
	case sourcev1.ModeGitTag, sourcev1.ModeGitTagAndHEAD:
		if flags.tag == "" && flags.semver == "" {
			return fmt.Errorf("%w: --verify-mode=%s", ErrVerifyTagModeWithoutTag, mode)
		}
	default:
		return fmt.Errorf("%w: %q (want HEAD, Tag or TagAndHEAD)", ErrInvalidVerifyMode, mode)
	}

	spec.Verification = &sourcev1.GitRepositoryVerification{
		Mode:      mode,
		SecretRef: meta.LocalObjectReference{Name: flags.verifySecretRef},
	}

	return nil
}
