package env

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/spf13/cobra"
)

// permissionWrite is the annotations.AnnotationPermission value that marks a
// command as state-modifying (and therefore requiring user confirmation).
const permissionWrite = "write"

// clustersDirSegment is the conventional sub-directory under the source directory
// that holds per-environment cluster overlays (sourceDir/clusters/<env>/), so a
// repository carrying several environments keeps each one in its own overlay.
const clustersDirSegment = "clusters"

// ErrSameEnvironment is returned when the destination environment name equals the
// source: there is nothing to clone and the clone would overwrite the source.
var ErrSameEnvironment = errors.New(
	"destination environment must differ from the source environment",
)

// ErrSourceConfigLoad is returned when the source environment's root config
// (ksail.<env>.yaml) cannot be loaded, e.g. it does not exist.
var ErrSourceConfigLoad = errors.New("failed to load source environment config")

// addEnvironmentLongDesc documents the command, including the SOPS caveat the
// clone deliberately does not handle (re-encryption to the new environment's
// recipients) so the user knows to follow up.
const addEnvironmentLongDesc = `Clone an existing cluster environment into a new one.

Copies the source environment's overlay (<sourceDirectory>/clusters/<from>/) and
its root config (ksail.<from>.yaml) into a new environment <name>, repointing the
structured identity along the way: the cluster-meta cluster_name, the
clusters/<env> path segment and content references, the root config's metadata
name, the connection context (distribution-aware), and — when --provider is given
— the provider. Every other byte is preserved, so the kustomize replacements block,
base wiring, version pins and comments survive intact.

SOPS-encrypted files (*.enc.yaml) are copied verbatim with only their path
repointed; re-encrypt them to the new environment's recipients afterwards with
` + "`ksail cipher`" + ` if the environments use different keys.

Examples:
  # Clone the "prod" environment into a new "staging" environment
  ksail project env add staging --from prod

  # Clone "prod" into "dev" on a different provider, overwriting any existing files
  ksail project env add dev --from prod --provider Docker --force`

// NewAddCmd creates and returns the `project env add` command (formerly
// `project add-environment`; the deprecated aliases delegate here).
func NewAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Clone an existing cluster environment into a new one",
		Long:         addEnvironmentLongDesc,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().StringP("from", "f", "", "Source environment to clone from (required)")
	_ = cmd.MarkFlagRequired("from")
	cmd.Flags().StringP(
		"provider", "p", "",
		"Provider for the new environment (defaults to the source provider)",
	)
	cmd.Flags().Bool("force", false, "Overwrite existing destination files")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return HandleAddRunE(cmd, args[0])
	}

	return cmd
}

// addEnvironmentParams holds the resolved inputs for a clone.
type addEnvironmentParams struct {
	repoRoot     string
	srcName      string
	dstName      string
	srcProvider  string
	dstProvider  string
	distribution v1alpha1.Distribution
	sourceDir    string
	force        bool
}

// HandleAddRunE handles the `project env add` command. It is exported for
// testing.
func HandleAddRunE(cmd *cobra.Command, dstName string) error {
	params, err := resolveAddEnvironmentParams(cmd, dstName)
	if err != nil {
		return err
	}

	return cloneEnvironment(cmd, params)
}

// validateEnvironmentNames rejects an invalid or path-like source or destination
// environment name before either is interpolated into a file path
// (ksail.<env>.yaml, clusters/<env>) — defence in depth ahead of the clone's
// downstream containment guard — and rejects cloning an environment onto itself.
func validateEnvironmentNames(srcName, dstName string) error {
	err := validateEnvironmentName(srcName)
	if err != nil {
		return fmt.Errorf("invalid source environment name: %w", err)
	}

	err = validateEnvironmentName(dstName)
	if err != nil {
		return fmt.Errorf("invalid environment name: %w", err)
	}

	if dstName == srcName {
		return fmt.Errorf("%w: %q", ErrSameEnvironment, dstName)
	}

	return nil
}

// resolveAddEnvironmentParams reads flags, loads the source config, and validates
// the destination before any file is written.
func resolveAddEnvironmentParams(
	cmd *cobra.Command,
	dstName string,
) (addEnvironmentParams, error) {
	srcName, _ := cmd.Flags().GetString("from")
	dstProvider, _ := cmd.Flags().GetString("provider")
	force, _ := cmd.Flags().GetBool("force")

	err := validateEnvironmentNames(srcName, dstName)
	if err != nil {
		return addEnvironmentParams{}, err
	}

	workDir, err := os.Getwd()
	if err != nil {
		return addEnvironmentParams{}, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Canonicalise the repository root so it matches the symlink-resolved paths the
	// clone's containment guard derives (on macOS os.Getwd() returns the unresolved
	// /var/... path while the guard resolves to /private/var/...).
	repoRoot, err := fsutil.EvalCanonicalPath(workDir)
	if err != nil {
		return addEnvironmentParams{}, fmt.Errorf("failed to resolve current directory: %w", err)
	}

	srcCfg, err := loadSourceConfig(cmd, srcName)
	if err != nil {
		return addEnvironmentParams{}, enrichSourceConfigError(cmd, repoRoot, err)
	}

	distribution := srcCfg.Spec.Cluster.Distribution

	// A provider override must be valid for the (inherited) distribution, mirroring
	// the init command's early provider×distribution validation so the clone fails
	// fast rather than writing an unusable config.
	if dstProvider != "" {
		provider := v1alpha1.Provider(dstProvider)

		err = provider.ValidateForDistribution(distribution)
		if err != nil {
			return addEnvironmentParams{}, fmt.Errorf("invalid --provider flag: %w", err)
		}
	}

	sourceDir, err := repoRelativeSourceDir(repoRoot, srcCfg.Spec.Workload.SourceDirectory)
	if err != nil {
		return addEnvironmentParams{}, err
	}

	return addEnvironmentParams{
		repoRoot:     repoRoot,
		srcName:      srcName,
		dstName:      dstName,
		srcProvider:  string(srcCfg.Spec.Cluster.Provider),
		dstProvider:  dstProvider,
		distribution: distribution,
		sourceDir:    sourceDir,
		force:        force,
	}, nil
}

// repoRelativeSourceDir returns the source directory as a path relative to
// repoRoot. The config manager resolves spec.workload.sourceDirectory to an
// absolute path when it reads the config file; the clone needs it repo-relative so
// CloneOverlay can join it onto repoRoot without escaping. A value that is already
// relative is returned unchanged.
func repoRelativeSourceDir(repoRoot, sourceDir string) (string, error) {
	// The silent, validation-skipping load leaves a home-relative ("~/...")
	// sourceDirectory unexpanded (the config manager defers that to
	// ExpandHomePath), so expand it here — otherwise it would be joined onto
	// repoRoot as a literal "~" directory.
	if strings.HasPrefix(sourceDir, "~/") {
		expanded, err := fsutil.ExpandHomePath(sourceDir)
		if err != nil {
			return "", fmt.Errorf("expanding source directory: %w", err)
		}

		sourceDir = expanded
	}

	if !filepath.IsAbs(sourceDir) {
		return sourceDir, nil
	}

	canonSrc, err := fsutil.EvalCanonicalPath(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolving source directory: %w", err)
	}

	rel, err := filepath.Rel(repoRoot, canonSrc)
	if err != nil {
		return "", fmt.Errorf("resolving source directory relative to repository root: %w", err)
	}

	return rel, nil
}

// loadSourceConfig reads the source environment's root config (ksail.<src>.yaml)
// to resolve its provider, distribution and source directory. Validation and
// distribution-specific config (e.g. Talos PKI) are skipped because the clone only
// needs the structured identity, not a provisioning-ready config.
func loadSourceConfig(cmd *cobra.Command, srcName string) (*v1alpha1.Cluster, error) {
	return loadEnvironmentConfig(cmd, "ksail."+srcName+".yaml")
}

// loadEnvironmentConfig loads a single ksail.<name>.yaml root config by file name.
// It is the shared loader behind loadSourceConfig and the environment.ConfigLoader
// that enrichSourceConfigError feeds to DeriveEnvironments, so both resolve a config
// the same silent, validation-skipping way.
func loadEnvironmentConfig(cmd *cobra.Command, configFile string) (*v1alpha1.Cluster, error) {
	manager := ksailconfigmanager.NewConfigManager(cmd.OutOrStdout(), configFile)

	cfg, err := manager.Load(configmanager.LoadOptions{
		Silent:                 true,
		SkipValidation:         true,
		SkipDistributionConfig: true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %w", ErrSourceConfigLoad, configFile, err)
	}

	// This silent, validation-skipping load does not apply field defaults, so
	// a config relying on the documented sourceDirectory default ("k8s") would
	// derive overlay paths from "" — clusters/<name> instead of
	// k8s/clusters/<name> — making a purge miss the real overlay and a clone
	// write to the wrong tree.
	if cfg.Spec.Workload.SourceDirectory == "" {
		cfg.Spec.Workload.SourceDirectory = v1alpha1.DefaultSourceDirectory
	}

	return cfg, nil
}

// enrichSourceConfigError augments a --from load failure with the environments
// actually declared in the workspace, so a mistyped source reports what is
// available instead of only that its config file was missing. It preserves the
// cause's error chain (ErrSourceConfigLoad) and returns it unchanged when discovery
// fails or finds no other environments.
func enrichSourceConfigError(cmd *cobra.Command, repoRoot string, cause error) error {
	loader := func(configFile string) (*v1alpha1.Cluster, error) {
		return loadEnvironmentConfig(cmd, configFile)
	}

	envs, err := environment.DeriveEnvironments(repoRoot, loader)
	if err != nil || len(envs) == 0 {
		return cause
	}

	names := make([]string, 0, len(envs))
	for _, env := range envs {
		names = append(names, env.Name)
	}

	return fmt.Errorf("%w (available environments: %s)", cause, strings.Join(names, ", "))
}

// cloneEnvironment derives the structured rewrites and clones the source overlay
// and root config into the destination environment, reporting what was written.
func cloneEnvironment(cmd *cobra.Command, params addEnvironmentParams) error {
	out := cmd.OutOrStdout()

	notify.Activityf(out, "Cloning environment %q from %q...", params.dstName, params.srcName)

	overlayRewrites := environment.DeriveRewrites(
		params.srcName, params.dstName, params.dstProvider, params.srcProvider,
	)
	srcOverlayDir := path.Join(params.sourceDir, clustersDirSegment, params.srcName)

	written, err := environment.CloneOverlay(
		params.repoRoot, srcOverlayDir, overlayRewrites, params.force,
	)
	if err != nil {
		return fmt.Errorf("cloning environment overlay: %w", err)
	}

	configRewrites := environment.DeriveConfigRewrites(
		params.srcName, params.dstName, params.dstProvider, params.srcProvider,
	)
	if ctxRewrite, ok := environment.DeriveContextRewrite(
		params.distribution, params.srcName, params.dstName,
	); ok {
		configRewrites = append(configRewrites, ctxRewrite)
	}

	configPath, wroteConfig, err := environment.CloneEnvironmentConfig(
		params.repoRoot, "ksail."+params.srcName+".yaml", configRewrites, params.force,
	)
	if err != nil {
		return fmt.Errorf("cloning environment config: %w", err)
	}

	reportClone(out, written, configPath, wroteConfig)

	return nil
}

// reportClone prints the files the clone wrote, or a skip note when --force was not
// set and the destination already existed.
func reportClone(out io.Writer, written []string, configPath string, wroteConfig bool) {
	for _, file := range written {
		notify.Activityf(out, "wrote %s", file)
	}

	if wroteConfig {
		notify.Activityf(out, "wrote %s", configPath)
	}

	if len(written) == 0 && !wroteConfig {
		notify.Infof(out, "nothing written: destination already exists (use --force to overwrite)")

		return
	}

	notify.Successf(out, "cloned environment to %d file(s)", len(written)+boolToInt(wroteConfig))
}

// boolToInt returns 1 for true and 0 for false, used to count the optional config
// file in the written total.
func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}
