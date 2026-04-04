package tenant

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant"
	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates the tenant create subcommand.
func NewCreateCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create <tenant-name>",
		Short:        "Create a new tenant",
		Long:         `Generate RBAC isolation manifests and GitOps sync resources for a new tenant.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	// Phase 1 flags
	cmd.Flags().StringSliceP("namespace", "n", nil, "Namespaces to create (repeatable, default: tenant-name)")
	cmd.Flags().String("cluster-role", "edit", "ClusterRole to bind to the tenant ServiceAccount")
	cmd.Flags().StringP("output", "o", ".", "Output directory for platform manifests")
	cmd.Flags().Bool("force", false, "Overwrite existing tenant directory")
	cmd.Flags().StringP("type", "t", "", "Tenant type: flux, argocd, or kubectl (default: auto-detect from ksail.yaml gitOpsEngine)")
	cmd.Flags().String("sync-source", "oci", "Flux source type: oci or git")
	cmd.Flags().String("registry", "", "OCI registry URL for Flux OCI source (e.g., oci://ghcr.io)")
	cmd.Flags().String("git-provider", "", "Git provider: github, gitlab, gitea")
	cmd.Flags().String("git-repo", "", "Tenant repo as owner/repo-name")
	cmd.Flags().String("git-token", "", "Git provider API token")
	cmd.Flags().String("repo-visibility", "Private", "Repo visibility: Private, Internal, or Public")

	// Phase 2 flags
	cmd.Flags().Bool("register", false, "Register tenant in kustomization.yaml")
	cmd.Flags().String("kustomization-path", "", "Path to kustomization.yaml (fallback: auto-discover)")
	cmd.Flags().String("delivery", "commit", "How to deliver platform changes: commit or pr")

	cmd.RunE = handleCreateRunE

	return cmd
}

func handleCreateRunE(cmd *cobra.Command, args []string) error {
	opts := tenant.Options{
		Name: args[0],
	}

	// Read flags.
	namespaces, _ := cmd.Flags().GetStringSlice("namespace")
	opts.Namespaces = namespaces
	opts.ClusterRole, _ = cmd.Flags().GetString("cluster-role")

	outputStr, _ := cmd.Flags().GetString("output")
	opts.Force, _ = cmd.Flags().GetBool("force")

	typeStr, _ := cmd.Flags().GetString("type")
	syncSourceStr, _ := cmd.Flags().GetString("sync-source")
	opts.Registry, _ = cmd.Flags().GetString("registry")
	opts.GitProvider, _ = cmd.Flags().GetString("git-provider")
	opts.GitRepo, _ = cmd.Flags().GetString("git-repo")
	opts.GitToken, _ = cmd.Flags().GetString("git-token")
	opts.RepoVisibility, _ = cmd.Flags().GetString("repo-visibility")

	register, _ := cmd.Flags().GetBool("register")
	opts.Register = register
	opts.KustomizationPath, _ = cmd.Flags().GetString("kustomization-path")
	opts.Delivery, _ = cmd.Flags().GetString("delivery")

	// Validate delivery mode.
	switch opts.Delivery {
	case "commit":
		// Default: write files locally.
	case "pr":
		return fmt.Errorf("--delivery pr is not yet implemented; use --delivery commit (default)")
	default:
		return fmt.Errorf("invalid --delivery value %q: must be 'commit' or 'pr'", opts.Delivery)
	}

	// Resolve tenant type.
	if typeStr != "" {
		if err := opts.TenantType.Set(typeStr); err != nil {
			return err
		}
	} else {
		cfgManager := ksailconfigmanager.NewConfigManager(cmd.OutOrStdout(), "")
		cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
		if err == nil {
			switch cfg.Spec.Cluster.GitOpsEngine {
			case v1alpha1.GitOpsEngineFlux:
				opts.TenantType = tenant.TenantTypeFlux
			case v1alpha1.GitOpsEngineArgoCD:
				opts.TenantType = tenant.TenantTypeArgoCD
			default:
				opts.TenantType = tenant.TenantTypeKubectl
			}
		} else {
			return fmt.Errorf("no --type specified and no ksail.yaml found: please specify --type (flux, argocd, or kubectl)")
		}
	}

	// Resolve sync source.
	opts.SyncSource = tenant.SyncSource(syncSourceStr)

	// Canonicalize output path.
	outputDir, err := fsutil.EvalCanonicalPath(outputStr)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}
	opts.OutputDir = outputDir

	// Generate tenant files.
	if err := tenant.Generate(opts); err != nil {
		return fmt.Errorf("generating tenant: %w", err)
	}

	// Register in kustomization.yaml if requested.
	if opts.Register {
		if err := tenant.RegisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath); err != nil {
			return fmt.Errorf("registering tenant: %w", err)
		}
	}

	// Scaffold and push tenant repo if git provider, repo, and a valid token are available.
	if opts.GitProvider != "" && opts.GitRepo != "" {
		token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)
		if token != "" {
			provider, err := gitprovider.New(opts.GitProvider, token)
			if err != nil {
				return fmt.Errorf("creating git provider: %w", err)
			}

			owner, repoName, err := gitprovider.ParseOwnerRepo(opts.GitRepo)
			if err != nil {
				return fmt.Errorf("parsing git-repo: %w", err)
			}

			ctx := context.Background()

			if err := provider.CreateRepo(ctx, owner, repoName, gitprovider.RepoVisibility(opts.RepoVisibility)); err != nil {
				return fmt.Errorf("creating tenant repo: %w", err)
			}

			scaffoldFiles := tenant.ScaffoldFiles(opts)
			commitMsg := fmt.Sprintf("feat: initial scaffold for tenant %s", opts.Name)

			if err := provider.PushFiles(ctx, owner, repoName, scaffoldFiles, commitMsg); err != nil {
				return fmt.Errorf("pushing scaffold files: %w", err)
			}

			notify.Successf(cmd.OutOrStdout(), "Tenant repo %q scaffolded successfully", opts.GitRepo)
		}
	}

	notify.Successf(cmd.OutOrStdout(), "Tenant %q created successfully in %s", opts.Name, outputDir)

	return nil
}
