package tenant

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
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
	cmd.Flags().
		StringSliceP("namespace", "n", nil, "Namespaces to create (repeatable, default: tenant-name)")
	cmd.Flags().StringSlice("cluster-role", []string{"edit"},
		"ClusterRole(s) to bind to the tenant ServiceAccount (repeatable)")
	cmd.Flags().StringP("output", "o", ".", "Output directory for platform manifests")
	cmd.Flags().Bool("force", false, "Overwrite existing tenant directory")
	cmd.Flags().StringP("type", "t", "",
		"Tenant type: flux, argocd, or kubectl "+
			"(default: auto-detect from ksail.yaml gitOpsEngine)")
	cmd.Flags().String("sync-source", "oci", "Flux source type: oci or git")
	cmd.Flags().String("registry", "", "OCI registry URL for Flux OCI source (e.g., oci://ghcr.io)")
	cmd.Flags().String("oci-path", "",
		"Path suffix appended to OCI registry URL to avoid tag collisions "+
			"(e.g., 'manifests' produces oci://registry/owner/repo/manifests)")
	cmd.Flags().String(
		"git-provider", "",
		"Git provider for manifest URLs: github, gitlab, gitea "+
			"(repo scaffolding requires github)",
	)
	cmd.Flags().String("tenant-repo", "", "Tenant repo as owner/repo-name")
	cmd.Flags().String(
		"git-token", "",
		"GitHub API token for repo scaffolding (--git-provider=github)",
	)
	cmd.Flags().
		String("repo-visibility", "Private", "Repo visibility: Private, Internal, or Public")
	cmd.Flags().
		String("source-directory", "k8s",
			"Directory name for tenant manifests in the tenant repo")

	// Phase 2 flags
	cmd.Flags().Bool("register", false, "Register tenant in kustomization.yaml")
	cmd.Flags().
		String("kustomization-path", "", "Path to kustomization.yaml (fallback: auto-discover)")
	cmd.Flags().String("delivery", "commit", "How to deliver platform changes: commit or pr")
	cmd.Flags().String("platform-repo", "",
		"Platform repo as owner/repo-name for PR delivery (default: auto-detect from git remote)")
	cmd.Flags().String("target-branch", "",
		"PR target branch (default: repo's default branch)")

	addProductionFlags(cmd)

	cmd.RunE = handleCreateRunE

	return cmd
}

// addProductionFlags registers the opt-in production hardening flags.
func addProductionFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("production", false,
		"Enable the recommended production baseline (PSS baseline, default-deny "+
			"NetworkPolicy, ResourceQuota, LimitRange, hardened ServiceAccount and Flux sync)")
	cmd.Flags().String("pod-security", "",
		"Pod Security Standards level for namespaces: restricted, baseline, or privileged")
	cmd.Flags().Bool("with-network-policy", false,
		"Generate default-deny NetworkPolicy plus DNS and intra-namespace allow rules")
	cmd.Flags().Bool("with-quota", false, "Generate a ResourceQuota for each namespace")
	cmd.Flags().String("quota-cpu", "4", "CPU quota (sets requests.cpu and limits.cpu)")
	cmd.Flags().
		String("quota-memory", "8Gi", "Memory quota (sets requests.memory and limits.memory)")
	cmd.Flags().Bool("with-limit-range", false,
		"Generate a LimitRange with default container requests/limits")
	cmd.Flags().String("limit-default-cpu", "500m", "Default container CPU limit")
	cmd.Flags().String("limit-default-memory", "512Mi", "Default container memory limit")
	cmd.Flags().String("limit-request-cpu", "100m", "Default container CPU request")
	cmd.Flags().String("limit-request-memory", "128Mi", "Default container memory request")
	cmd.Flags().Bool("disable-token-automount", false,
		"Set automountServiceAccountToken: false on the tenant ServiceAccount")
	cmd.Flags().StringSlice("image-pull-secret", nil,
		"imagePullSecret to add to the tenant ServiceAccount (repeatable)")
	cmd.Flags().
		Bool("flux-wait", false, "(Flux) Set wait: true and timeout on the Flux Kustomization")
	cmd.Flags().String("flux-timeout", "",
		"(Flux) Flux Kustomization timeout; setting it implies --flux-wait (default 5m when waiting)")
	cmd.Flags().String("flux-retry-interval", "", "(Flux) Flux Kustomization retryInterval")
	cmd.Flags().Bool("flux-decryption", false,
		"(Flux) Add a SOPS decryption block referencing the sops-age secret")
}

func handleCreateRunE(cmd *cobra.Command, args []string) error {
	opts, outputStr, delivery, err := resolveCreateOptions(cmd, args)
	if err != nil {
		return err
	}

	// Canonicalize output path.
	outputDir, err := fsutil.EvalCanonicalPath(outputStr)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	opts.OutputDir = outputDir

	err = generateAndRegister(cmd, opts)
	if err != nil {
		return err
	}

	// Scaffold and push tenant repo (only supported for GitHub provider).
	if strings.EqualFold(opts.GitProvider, "github") && opts.TenantRepo != "" {
		err = scaffoldTenantRepo(cmd, opts)
		if err != nil {
			return err
		}
	}

	// Deliver via PR if requested.
	if delivery == "pr" {
		return handlePRDelivery(cmd, opts)
	}

	notify.Successf(cmd.OutOrStdout(), "Tenant %q created successfully in %s", opts.Name, outputDir)

	return nil
}

func generateAndRegister(cmd *cobra.Command, opts tenant.Options) error {
	err := tenant.Generate(opts)
	if err != nil {
		return fmt.Errorf("generating tenant: %w", err)
	}

	if opts.Register {
		regErr := registerTenantWithRBAC(opts)
		if regErr != nil {
			return regErr
		}

		notify.Successf(cmd.OutOrStdout(), "Tenant %q registered in kustomization.yaml", opts.Name)
	}

	return nil
}

func handlePRDelivery(cmd *cobra.Command, opts tenant.Options) error {
	prURL, err := deliverPR(cmd, opts)
	if err != nil {
		return err
	}

	notify.Successf(cmd.OutOrStdout(),
		"Tenant %q created and PR opened: %s", opts.Name, prURL)

	return nil
}

func resolveCreateOptions(
	cmd *cobra.Command,
	args []string,
) (tenant.Options, string, string, error) {
	opts := tenant.Options{
		Name: args[0],
	}

	// Read flags.
	namespaces, _ := cmd.Flags().GetStringSlice("namespace")
	opts.Namespaces = namespaces
	opts.ClusterRoles, _ = cmd.Flags().GetStringSlice("cluster-role")

	readProductionFlags(cmd, &opts)

	// Setting --flux-timeout implies --flux-wait, since the timeout only takes
	// effect while waiting. The flag default is empty so this only triggers on
	// explicit use.
	if cmd.Flags().Changed("flux-timeout") {
		opts.FluxWait = true
	}

	outputStr, _ := cmd.Flags().GetString("output")
	opts.Force, _ = cmd.Flags().GetBool("force")

	typeStr, _ := cmd.Flags().GetString("type")
	syncSourceStr, _ := cmd.Flags().GetString("sync-source")
	opts.Registry, _ = cmd.Flags().GetString("registry")
	opts.OCIPath, _ = cmd.Flags().GetString("oci-path")
	opts.GitProvider, _ = cmd.Flags().GetString("git-provider")
	opts.TenantRepo, _ = cmd.Flags().GetString("tenant-repo")
	opts.GitToken, _ = cmd.Flags().GetString("git-token")
	opts.RepoVisibility, _ = cmd.Flags().GetString("repo-visibility")
	opts.SourceDirectory, _ = cmd.Flags().GetString("source-directory")

	register, _ := cmd.Flags().GetBool("register")
	opts.Register = register
	opts.KustomizationPath, _ = cmd.Flags().GetString("kustomization-path")
	delivery, _ := cmd.Flags().GetString("delivery")
	opts.PlatformRepo, _ = cmd.Flags().GetString("platform-repo")
	opts.TargetBranch, _ = cmd.Flags().GetString("target-branch")

	// Validate delivery mode and its prerequisites.
	deliveryErr := validateDelivery(delivery, opts.GitProvider)
	if deliveryErr != nil {
		return tenant.Options{}, "", "", deliveryErr
	}

	// Resolve tenant type and engine selection from ksail.yaml (loaded once).
	err := resolveTypeAndEngine(cmd, typeStr, &opts)
	if err != nil {
		return tenant.Options{}, "", "", err
	}

	// Expand the --production umbrella (granular flags always win when set).
	applyProductionDefaults(cmd, &opts)

	// Validate sync source only for Flux tenants.
	if opts.TenantType == tenant.TypeFlux {
		syncErr := resolveSyncSource(syncSourceStr, &opts)
		if syncErr != nil {
			return tenant.Options{}, "", "", syncErr
		}
	}

	return opts, outputStr, delivery, nil
}

// resolveTypeAndEngine loads ksail.yaml once to resolve the tenant type
// (auto-detected from gitOpsEngine when --type is unset) and best-effort engine
// selection (NetworkPolicy flavor, policy engine).
func resolveTypeAndEngine(cmd *cobra.Command, typeStr string, opts *tenant.Options) error {
	cfg, cfgFound, cfgErr := loadKSailConfig(cmd)
	if typeStr == "" && cfgErr != nil {
		return cfgErr
	}

	err := resolveTenantType(typeStr, opts, cfg, cfgFound)
	if err != nil {
		return err
	}

	if cfgErr == nil && cfgFound && cfg != nil {
		applyEngineDefaults(opts, cfg)
	}

	return nil
}

// readProductionFlags reads the opt-in production hardening flags into opts.
func readProductionFlags(cmd *cobra.Command, opts *tenant.Options) {
	opts.PodSecurity, _ = cmd.Flags().GetString("pod-security")
	opts.WithNetworkPolicy, _ = cmd.Flags().GetBool("with-network-policy")
	opts.WithQuota, _ = cmd.Flags().GetBool("with-quota")
	opts.QuotaCPU, _ = cmd.Flags().GetString("quota-cpu")
	opts.QuotaMemory, _ = cmd.Flags().GetString("quota-memory")
	opts.WithLimitRange, _ = cmd.Flags().GetBool("with-limit-range")
	opts.LimitDefaultCPU, _ = cmd.Flags().GetString("limit-default-cpu")
	opts.LimitDefaultMemory, _ = cmd.Flags().GetString("limit-default-memory")
	opts.LimitRequestCPU, _ = cmd.Flags().GetString("limit-request-cpu")
	opts.LimitRequestMemory, _ = cmd.Flags().GetString("limit-request-memory")
	opts.DisableTokenAutomount, _ = cmd.Flags().GetBool("disable-token-automount")
	opts.ImagePullSecrets, _ = cmd.Flags().GetStringSlice("image-pull-secret")
	opts.FluxWait, _ = cmd.Flags().GetBool("flux-wait")
	opts.FluxTimeout, _ = cmd.Flags().GetString("flux-timeout")
	opts.FluxRetryInterval, _ = cmd.Flags().GetString("flux-retry-interval")
	opts.FluxDecryption, _ = cmd.Flags().GetBool("flux-decryption")
}

// applyProductionDefaults expands the --production umbrella flag. Granular flags
// the user explicitly set always take precedence.
func applyProductionDefaults(cmd *cobra.Command, opts *tenant.Options) {
	production, _ := cmd.Flags().GetBool("production")
	if !production {
		return
	}

	if !cmd.Flags().Changed("pod-security") {
		opts.PodSecurity = tenant.PodSecurityBaseline
	}

	if !cmd.Flags().Changed("with-network-policy") {
		opts.WithNetworkPolicy = true
	}

	if !cmd.Flags().Changed("with-quota") {
		opts.WithQuota = true
	}

	if !cmd.Flags().Changed("with-limit-range") {
		opts.WithLimitRange = true
	}

	if !cmd.Flags().Changed("disable-token-automount") {
		opts.DisableTokenAutomount = true
	}

	if !cmd.Flags().Changed("flux-wait") {
		opts.FluxWait = true
	}
}

// applyEngineDefaults selects the NetworkPolicy flavor and records the policy
// engine based on the loaded ksail.yaml cluster spec.
func applyEngineDefaults(opts *tenant.Options, cfg *v1alpha1.Cluster) {
	if cfg.Spec.Cluster.CNI == v1alpha1.CNICilium {
		opts.NetworkPolicyEngine = tenant.NetworkPolicyEngineCilium
	} else {
		opts.NetworkPolicyEngine = tenant.NetworkPolicyEngineNative
	}

	opts.PolicyEngine = string(cfg.Spec.Cluster.PolicyEngine)
}

func validateDelivery(delivery, gitProvider string) error {
	switch delivery {
	case "commit":
		return nil
	case "pr":
		if gitProvider == "" {
			return fmt.Errorf(
				"%w: --git-provider is required when --delivery pr is used",
				tenant.ErrGitProviderRequired,
			)
		}

		if !strings.EqualFold(gitProvider, "github") {
			return fmt.Errorf(
				"%w: --delivery pr is only supported with --git-provider github",
				tenant.ErrInvalidDelivery,
			)
		}

		return nil
	default:
		return fmt.Errorf("%w %q: must be 'commit' or 'pr'", tenant.ErrInvalidDelivery, delivery)
	}
}

// loadKSailConfig loads ksail.yaml once, returning the cluster config, whether
// a config file was found, and any load error.
func loadKSailConfig(cmd *cobra.Command) (*v1alpha1.Cluster, bool, error) {
	var configFile string

	cfgPath, err := flags.GetConfigPath(cmd)
	if err == nil {
		configFile = cfgPath
	}

	cfgManager := ksailconfigmanager.NewConfigManager(cmd.OutOrStdout(), configFile)

	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	if err != nil {
		return nil, false, fmt.Errorf("loading config: %w", err)
	}

	return cfg, cfgManager.IsConfigFileFound(), nil
}

func resolveTenantType(
	typeStr string,
	opts *tenant.Options,
	cfg *v1alpha1.Cluster,
	cfgFound bool,
) error {
	if typeStr != "" {
		err := opts.TenantType.Set(typeStr)
		if err != nil {
			return fmt.Errorf("setting tenant type: %w", err)
		}

		return nil
	}

	if !cfgFound || cfg == nil {
		return fmt.Errorf("%w", tenant.ErrConfigNotFound)
	}

	switch cfg.Spec.Cluster.GitOpsEngine {
	case v1alpha1.GitOpsEngineFlux:
		opts.TenantType = tenant.TypeFlux
	case v1alpha1.GitOpsEngineArgoCD:
		opts.TenantType = tenant.TypeArgoCD
	case v1alpha1.GitOpsEngineNone:
		opts.TenantType = tenant.TypeKubectl
	default:
		opts.TenantType = tenant.TypeKubectl
	}

	return nil
}

func resolveSyncSource(syncSourceStr string, opts *tenant.Options) error {
	switch strings.ToLower(syncSourceStr) {
	case "oci":
		opts.SyncSource = tenant.SyncSourceOCI
	case "git":
		opts.SyncSource = tenant.SyncSourceGit
	default:
		return fmt.Errorf(
			"%w %q: must be 'oci' or 'git'",
			tenant.ErrInvalidSyncSource,
			syncSourceStr,
		)
	}

	return nil
}

func deliverPR(cmd *cobra.Command, opts tenant.Options) (string, error) {
	ctx := cmd.Context()

	prURL, err := tenant.DeliverPR(ctx, tenant.DeliverPROptions{
		GitProvider:       opts.GitProvider,
		GitToken:          opts.GitToken,
		PlatformRepo:      opts.PlatformRepo,
		TargetBranch:      opts.TargetBranch,
		TenantName:        opts.Name,
		OutputDir:         opts.OutputDir,
		Register:          opts.Register,
		KustomizationPath: opts.KustomizationPath,
	})
	if err != nil {
		return "", fmt.Errorf("delivering PR: %w", err)
	}

	return prURL, nil
}

func scaffoldTenantRepo(cmd *cobra.Command, opts tenant.Options) error {
	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)
	if token == "" {
		notify.Warningf(cmd.OutOrStdout(),
			"Skipping repo scaffolding: no API token found "+
				"(set --git-token or %s environment variable)",
			strings.ToUpper(opts.GitProvider)+"_TOKEN")

		return nil
	}

	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return fmt.Errorf("creating git provider: %w", err)
	}

	owner, repoName, err := gitprovider.ParseOwnerRepo(opts.TenantRepo)
	if err != nil {
		return fmt.Errorf("parsing tenant-repo: %w", err)
	}

	visibility, err := gitprovider.ParseVisibility(opts.RepoVisibility)
	if err != nil {
		return fmt.Errorf("parsing repo visibility: %w", err)
	}

	ctx := cmd.Context()

	err = provider.CreateRepo(ctx, owner, repoName, visibility)
	if err != nil {
		if errors.Is(err, gitprovider.ErrRepoAlreadyExists) {
			notify.Warningf(
				cmd.OutOrStdout(),
				"Repo %q already exists, pushing scaffold files to existing repo",
				opts.TenantRepo,
			)
		} else {
			return fmt.Errorf("creating tenant repo: %w", err)
		}
	}

	scaffoldFiles := tenant.ScaffoldFiles(opts)
	commitMsg := "feat: initial scaffold for tenant " + opts.Name

	err = provider.PushFiles(ctx, owner, repoName, scaffoldFiles, commitMsg)
	if err != nil {
		return fmt.Errorf("pushing scaffold files: %w", err)
	}

	notify.Successf(cmd.OutOrStdout(), "Tenant repo %q scaffolded successfully", opts.TenantRepo)

	return nil
}

// registerTenantWithRBAC registers the tenant in kustomization.yaml and,
// for ArgoCD tenants, merges RBAC policy into the shared argocd-rbac-cm.
func registerTenantWithRBAC(opts tenant.Options) error {
	err := tenant.RegisterTenant(
		opts.Name, opts.OutputDir, opts.KustomizationPath,
	)
	if err != nil {
		return fmt.Errorf("registering tenant: %w", err)
	}

	if opts.TenantType == tenant.TypeArgoCD {
		err = mergeArgoCDRBACPolicy(opts)
		if err != nil {
			return err
		}
	}

	return nil
}

func mergeArgoCDRBACPolicy(opts tenant.Options) error {
	kPath, err := tenant.ResolveKustomizationPath(opts.OutputDir, opts.KustomizationPath)
	if err != nil {
		return fmt.Errorf("resolving kustomization path for RBAC merge: %w", err)
	}

	kDir := filepath.Dir(kPath)

	rbacCMPath, err := tenant.FindArgoCDRBACCM(kDir)
	if err != nil {
		return fmt.Errorf("scanning for argocd-rbac-cm: %w", err)
	}

	// If no existing file found, create one with the default filename.
	if rbacCMPath == "" {
		rbacCMPath = filepath.Join(kDir, tenant.DefaultArgoCDRBACCMFilename)
	}

	mergeErr := tenant.MergeArgoCDRBACPolicyFile(rbacCMPath, opts.Name)
	if mergeErr != nil {
		return fmt.Errorf("merging ArgoCD RBAC policy: %w", mergeErr)
	}

	// Register the RBAC CM file in the kustomization.yaml resources.
	resourceName := filepath.Base(rbacCMPath)

	regErr := tenant.RegisterResource(kPath, resourceName)
	if regErr != nil {
		return fmt.Errorf("registering argocd-rbac-cm in kustomization: %w", regErr)
	}

	return nil
}
