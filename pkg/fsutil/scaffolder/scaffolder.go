package scaffolder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator"
	k3dgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/k3d"
	kindgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kind"
	kustomizationgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kustomization"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

const (
	// Configuration file names.

	// KindConfigFile is the default filename for Kind distribution configuration.
	KindConfigFile = "kind.yaml"

	// K3dConfigFile is the default filename for K3d distribution configuration.
	K3dConfigFile = "k3d.yaml"

	// TalosConfigDir is the default directory for Talos distribution configuration (Talos patches).
	TalosConfigDir = "talos"

	// SchemaHeader is the yaml-language-server directive prepended to generated ksail.yaml
	// files so editors validate them against the published KSail configuration schema.
	SchemaHeader = "# yaml-language-server: $schema=" +
		"https://raw.githubusercontent.com/devantler-tech/ksail/main/schemas/ksail-config.schema.json"
)

const (
	// File permission constants.
	dirPerm  = 0o750
	filePerm = 0o600
)

// Scaffolder is responsible for generating KSail project files and configurations.
type Scaffolder struct {
	KSailConfig            v1alpha1.Cluster
	KSailYAMLGenerator     generator.Generator[v1alpha1.Cluster, yamlgenerator.Options]
	KindGenerator          generator.Generator[*v1alpha4.Cluster, yamlgenerator.Options]
	K3dGenerator           generator.Generator[*k3dv1alpha5.SimpleConfig, yamlgenerator.Options]
	TalosGenerator         *talosgenerator.Generator
	KustomizationGenerator generator.Generator[*ktypes.Kustomization, yamlgenerator.Options]
	Writer                 io.Writer
	MirrorRegistries       []string // Format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
	ClusterName            string   // Optional override for cluster name. If set, overrides distribution defaults.
	// Devcontainer, when true (the default), scaffolds .devcontainer/devcontainer.json.
	// Disable it via WithDevcontainer(false) (wired to init's --no-devcontainer flag).
	Devcontainer bool
	// MultiClusterEnv, when non-empty (wired to init's --multi-cluster flag), scaffolds a
	// multi-cluster source layout (clusters/base/ + clusters/<env>/) instead of the flat
	// single-cluster kustomization, and defaults the generated ksail.yaml's
	// spec.workload.kustomizationFile to the environment overlay so the GitOps engine
	// syncs from it.
	MultiClusterEnv string
}

// NewScaffolder creates a new Scaffolder instance with the provided KSail cluster configuration.
func NewScaffolder(cfg v1alpha1.Cluster, writer io.Writer, mirrorRegistries []string) *Scaffolder {
	ksailGenerator := yamlgenerator.NewGenerator[v1alpha1.Cluster]()
	kindGenerator := kindgenerator.NewGenerator()
	k3dGenerator := k3dgenerator.NewGenerator()
	talosGen := talosgenerator.NewGenerator()
	kustomizationGenerator := kustomizationgenerator.NewGenerator()

	return &Scaffolder{
		KSailConfig:            cfg,
		KSailYAMLGenerator:     ksailGenerator,
		KindGenerator:          kindGenerator,
		K3dGenerator:           k3dGenerator,
		TalosGenerator:         talosGen,
		KustomizationGenerator: kustomizationGenerator,
		Writer:                 writer,
		MirrorRegistries:       mirrorRegistries,
		Devcontainer:           true,
	}
}

// WithDevcontainer toggles scaffolding of .devcontainer/devcontainer.json.
// Dev Container scaffolding is enabled by default; pass false (wired to the
// init command's --no-devcontainer flag) to skip it.
func (s *Scaffolder) WithDevcontainer(enabled bool) *Scaffolder {
	s.Devcontainer = enabled

	return s
}

// WithMultiClusterEnv enables the multi-cluster source layout with envName as the
// initial environment overlay (wired to the init command's --multi-cluster flag).
// The name is validated when the layout is derived at the start of Scaffold, so an
// invalid or reserved name fails before any file is written.
func (s *Scaffolder) WithMultiClusterEnv(envName string) *Scaffolder {
	s.MultiClusterEnv = envName

	return s
}

// WithClusterName sets an explicit cluster name override for the scaffolder.
// When set, this name is used for ksail.yaml metadata.name and distribution configs
// (kind.yaml Name, k3d.yaml metadata.name, Talos cluster patches). The kubeconfig
// context is derived from the name at runtime and is not scaffolded.
func (s *Scaffolder) WithClusterName(name string) *Scaffolder {
	s.ClusterName = name

	return s
}

// Main scaffolding operations.

// Scaffold generates project files and configurations.
//
// This method orchestrates the generation of:
//   - ksail.yaml configuration
//   - Distribution-specific configuration (kind.yaml or k3d.yaml)
//   - kind/mirrors directory with hosts.toml files (for Kind with mirror registries)
//   - kustomization.yaml in the source directory
//   - .devcontainer/devcontainer.json (unless disabled via WithDevcontainer(false))
//
// Parameters:
//   - output: The output directory for generated files
//   - force: If true, overwrites existing files; if false, skips existing files
//
// Returns:
//   - error: Any error encountered during scaffolding
func (s *Scaffolder) Scaffold(output string, force bool) error {
	previousDistributionConfig := strings.TrimSpace(s.KSailConfig.Spec.Cluster.DistributionConfig)

	// Derive the multi-cluster layout up front so an invalid or reserved environment
	// name aborts before any file is written.
	multiClusterFiles, err := s.deriveMultiClusterFiles()
	if err != nil {
		return err
	}

	err = s.generateKSailConfig(output, force)
	if err != nil {
		return err
	}

	if force {
		cleanupErr := s.removeFormerDistributionConfig(output, previousDistributionConfig)
		if cleanupErr != nil {
			return cleanupErr
		}
	}

	err = s.generateDistributionConfig(output, force)
	if err != nil {
		return err
	}

	// Generate Kind mirror hosts configuration if applicable
	err = s.generateKindMirrorsConfig(output, force)
	if err != nil {
		return err
	}

	// Generate K3d containerd config template if image verification is enabled
	err = s.generateK3dContainerdConfig(output, force)
	if err != nil {
		return err
	}

	err = s.generateWorkloadKustomizations(output, force, multiClusterFiles)
	if err != nil {
		return err
	}

	if s.Devcontainer {
		return s.generateDevcontainerConfig(output, force)
	}

	return nil
}

// deriveMultiClusterFiles derives the multi-cluster layout when MultiClusterEnv is
// set, so Scaffold can validate the environment name before writing any file. It
// returns nil files in single-cluster mode.
func (s *Scaffolder) deriveMultiClusterFiles() ([]environment.LayoutFile, error) {
	if s.MultiClusterEnv == "" {
		return nil, nil
	}

	files, err := environment.DeriveMultiClusterLayout(s.MultiClusterEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to derive multi-cluster layout: %w", err)
	}

	return files, nil
}

// generateWorkloadKustomizations writes the source tree's kustomizations: in
// multi-cluster mode the environment overlay (pointed at by the generated
// ksail.yaml's kustomizationFile) replaces the flat single-cluster kustomization.
func (s *Scaffolder) generateWorkloadKustomizations(
	output string,
	force bool,
	multiClusterFiles []environment.LayoutFile,
) error {
	if multiClusterFiles != nil {
		return s.generateMultiClusterLayout(output, force, multiClusterFiles)
	}

	return s.generateKustomizationConfig(output, force)
}

// generateMultiClusterLayout writes the derived multi-cluster layout under the source
// directory via the shared writer, then emits the same skip/create/overwrite
// notifications the single-cluster generators produce (the writer itself is silent;
// with force=false it leaves existing files untouched, matching the pre-stat below).
func (s *Scaffolder) generateMultiClusterLayout(
	output string,
	force bool,
	files []environment.LayoutFile,
) error {
	sourceDir := filepath.Join(output, s.KSailConfig.Spec.Workload.SourceDirectory)

	existed := make([]bool, len(files))
	skipped := make([]bool, len(files))

	for i, file := range files {
		target := filepath.Join(sourceDir, filepath.FromSlash(file.RelPath))
		skipped[i], existed[i], _ = s.checkFileExistsAndSkip(
			target,
			s.multiClusterDisplayName(file),
			force,
		)
	}

	_, err := environment.WriteMultiClusterLayout(
		s.KustomizationGenerator,
		sourceDir,
		files,
		force,
	)
	if err != nil {
		return fmt.Errorf("failed to generate multi-cluster layout: %w", err)
	}

	for i, file := range files {
		if skipped[i] {
			continue
		}

		s.notifyFileAction(s.multiClusterDisplayName(file), existed[i])
	}

	return nil
}

// multiClusterDisplayName renders a layout file's path relative to the project root
// (source directory included) for user-facing notifications.
func (s *Scaffolder) multiClusterDisplayName(file environment.LayoutFile) string {
	return filepath.Join(
		s.KSailConfig.Spec.Workload.SourceDirectory,
		filepath.FromSlash(file.RelPath),
	)
}

// Configuration defaults and helpers.

// applyKSailConfigDefaults applies distribution-specific defaults to the KSail configuration.
// This ensures the generated ksail.yaml has a distributionConfig value that matches the
// distribution-specific configuration files being generated.
//
// The kubeconfig context (spec.cluster.connection.context) is deliberately NOT scaffolded:
// it is fully derived from the distribution and cluster name at runtime, so writing it to
// ksail.yaml would only duplicate metadata.name and go stale on rename.
func (s *Scaffolder) applyKSailConfigDefaults() v1alpha1.Cluster {
	config := s.KSailConfig

	// Set metadata.name if a cluster name override was provided
	if s.ClusterName != "" {
		config.Name = s.ClusterName
	}

	// Set the expected distribution config filename if it's empty or set to default
	if config.Spec.Cluster.DistributionConfig == "" ||
		config.Spec.Cluster.DistributionConfig == KindConfigFile {
		expectedConfigName := v1alpha1.ExpectedDistributionConfigName(
			config.Spec.Cluster.Distribution,
		)
		config.Spec.Cluster.DistributionConfig = expectedConfigName
	}

	// In multi-cluster mode, point the GitOps sync path at the environment overlay
	// (clusters/<env>/) unless the user pinned kustomizationFile explicitly.
	if s.MultiClusterEnv != "" &&
		strings.TrimSpace(config.Spec.Workload.KustomizationFile) == "" {
		config.Spec.Workload.KustomizationFile = path.Join(
			environment.ClustersDir,
			s.MultiClusterEnv,
		)
	}

	return config
}

// File handling helpers.

// checkFileExistsAndSkip checks if a file exists and should be skipped based on force flag.
// Returns true if the file should be skipped (exists and force=false), false otherwise.
// Outputs appropriate warning message if skipping.
func (s *Scaffolder) checkFileExistsAndSkip(
	filePath string,
	fileName string,
	force bool,
) (bool, bool, time.Time) {
	info, statErr := os.Stat(filePath)
	if statErr == nil {
		if !force {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: "skipped '%s', file exists use --force to overwrite",
				Args:    []any{fileName},
				Writer:  s.Writer,
			})

			return true, true, info.ModTime()
		}

		return false, true, info.ModTime()
	}

	// statErr is non-nil here (since statErr == nil was handled above)
	// If it's not ErrNotExist, return without file info
	if !errors.Is(statErr, os.ErrNotExist) {
		return false, false, time.Time{}
	}

	return false, false, time.Time{}
}

// Template generation helpers.

// GenerationParams groups parameters for generateWithFileHandling.
type GenerationParams[T any] struct {
	Gen         generator.Generator[T, yamlgenerator.Options]
	Model       T
	Opts        yamlgenerator.Options
	DisplayName string
	Force       bool
	WrapErr     func(error) error
}

// generateWithFileHandling wraps template generation with common file existence checks and notifications.

func generateWithFileHandling[T any](
	scaffolder *Scaffolder,
	params GenerationParams[T],
) error {
	skip, existed, previousModTime := scaffolder.checkFileExistsAndSkip(
		params.Opts.Output,
		params.DisplayName,
		params.Force,
	)

	if skip {
		return nil
	}

	_, err := params.Gen.Generate(params.Model, params.Opts)
	if err != nil {
		if params.WrapErr != nil {
			return params.WrapErr(err)
		}

		return fmt.Errorf("failed to generate %s: %w", params.DisplayName, err)
	}

	if params.Force && existed {
		err := ensureOverwriteModTime(params.Opts.Output, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", params.DisplayName, err)
		}
	}

	scaffolder.notifyFileAction(params.DisplayName, existed)

	return nil
}

func ensureOverwriteModTime(path string, previous time.Time) error {
	if path == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	current := info.ModTime()
	if previous.IsZero() || current.After(previous) {
		return nil
	}

	// Ensure the new mod time is strictly greater than the previous timestamp.
	newModTime := previous.Add(time.Millisecond)

	now := time.Now()
	if now.After(newModTime) {
		newModTime = now
	}

	err = os.Chtimes(path, newModTime, newModTime)
	if err != nil {
		return fmt.Errorf("failed to update mod time for %s: %w", path, err)
	}

	return nil
}

func (s *Scaffolder) notifyFileAction(displayName string, overwritten bool) {
	action := "created"
	if overwritten {
		action = "overwrote"
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.GenerateType,
		Content: "%s '%s'",
		Args:    []any{action, displayName},
		Writer:  s.Writer,
	})
}

// Configuration file generators.

// schemaHeaderGenerator wraps a KSail YAML generator and prepends SchemaHeader to its
// output so editors with YAML language-server support validate ksail.yaml out of the box.
type schemaHeaderGenerator struct {
	inner generator.Generator[v1alpha1.Cluster, yamlgenerator.Options]
}

// Generate renders the model via the wrapped generator, prepends the schema header,
// and writes the result to opts.Output when set.
func (g *schemaHeaderGenerator) Generate(
	model v1alpha1.Cluster,
	opts yamlgenerator.Options,
) (string, error) {
	contentOpts := opts
	contentOpts.Output = ""

	body, err := g.inner.Generate(model, contentOpts)
	if err != nil {
		return "", fmt.Errorf("failed to generate ksail.yaml content: %w", err)
	}

	content := SchemaHeader + "\n" + body

	if opts.Output == "" {
		return content, nil
	}

	result, err := fsutil.TryWriteFile(content, opts.Output, opts.Force)
	if err != nil {
		return "", fmt.Errorf("failed to write YAML to file: %w", err)
	}

	return result, nil
}

// generateKSailConfig generates the ksail.yaml configuration file.
func (s *Scaffolder) generateKSailConfig(output string, force bool) error {
	// Apply distribution-specific defaults to ensure consistency with generated files
	config := s.applyKSailConfigDefaults()

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, "ksail.yaml"),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[v1alpha1.Cluster]{
			Gen:         &schemaHeaderGenerator{inner: s.KSailYAMLGenerator},
			Model:       config,
			Opts:        opts,
			DisplayName: "ksail.yaml",
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKSailConfigGeneration, err)
			},
		},
	)
}

// generateDistributionConfig generates the distribution-specific configuration file.
func (s *Scaffolder) generateDistributionConfig(output string, force bool) error {
	switch s.KSailConfig.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return s.generateKindConfig(output, force)
	case v1alpha1.DistributionK3s:
		return s.generateK3dConfig(output, force)
	case v1alpha1.DistributionTalos:
		return s.generateTalosConfig(output, force)
	case v1alpha1.DistributionVCluster:
		return s.generateVClusterConfig(output, force)
	case v1alpha1.DistributionKWOK:
		return s.generateKWOKConfig(output, force)
	case v1alpha1.DistributionEKS:
		return s.generateEKSConfig(output, force)
	case v1alpha1.DistributionGKE:
		// GKE has no scaffolded distribution config: gke.yaml is optional
		// (project/location resolve from the environment, and the GKE API owns
		// the cluster shape), so nothing is generated.
		return nil
	case v1alpha1.DistributionAKS:
		// AKS has no scaffolded distribution config: aks.yaml is optional
		// (subscription/resource group resolve from the environment, and the
		// AKS API owns the cluster shape), so nothing is generated.
		return nil
	default:
		return ErrUnknownDistribution
	}
}

func (s *Scaffolder) removeFormerDistributionConfig(output, previous string) error {
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return nil
	}

	newConfigName := v1alpha1.ExpectedDistributionConfigName(
		s.KSailConfig.Spec.Cluster.Distribution,
	)
	newConfigPath := filepath.Join(output, newConfigName)

	previousPath := previous
	if !filepath.IsAbs(previousPath) {
		previousPath = filepath.Join(output, previous)
	}

	if filepath.Clean(previousPath) != filepath.Clean(newConfigPath) {
		return nil
	}

	info, err := os.Stat(previousPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("failed to inspect previous distribution config '%s': %w", previous, err)
	}

	if info.IsDir() {
		// Clear directory so file generation can succeed on the expected path.
		removeErr := os.RemoveAll(previousPath)
		if removeErr != nil {
			return fmt.Errorf(
				"failed to remove previous distribution config '%s': %w",
				previous,
				removeErr,
			)
		}

		return nil
	}

	// Keep existing files in place so overwrite detection can log accurately.
	return nil
}

// generateKustomizationConfig generates the kustomization.yaml file.
// When kustomizationFile is set to a subdirectory, the kustomization.yaml is generated
// at that subdirectory rather than at the root of the source directory.
func (s *Scaffolder) generateKustomizationConfig(output string, force bool) error {
	kustomizationDir, err := s.resolveKustomizationDir()
	if err != nil {
		return err
	}

	// GitOps resources (FluxInstance, ArgoCD Application) are created server-side
	// via the Kubernetes API during cluster creation, not scaffolded, so the
	// kustomization starts empty (the generator normalizes resources to []).
	kustomization := ktypes.Kustomization{}

	opts := yamlgenerator.Options{
		Output: filepath.Join(
			output,
			kustomizationDir,
			"kustomization.yaml",
		),
		Force: force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*ktypes.Kustomization]{
			Gen:   s.KustomizationGenerator,
			Model: &kustomization,
			Opts:  opts,
			DisplayName: filepath.Join(
				kustomizationDir,
				"kustomization.yaml",
			),
			Force: force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKustomizationGeneration, err)
			},
		},
	)
}

// resolveKustomizationDir resolves the directory where kustomization.yaml should be written.
// It normalizes the kustomizationFile path using slash semantics and validates it.
func (s *Scaffolder) resolveKustomizationDir() (string, error) {
	rawPath := strings.TrimSpace(s.KSailConfig.Spec.Workload.KustomizationFile)

	switch rawPath {
	case "", ".", "./":
		return s.KSailConfig.Spec.Workload.SourceDirectory, nil
	default:
		cleanPath, err := validateKustomizationPath(rawPath)
		if err != nil {
			return "", err
		}

		return filepath.Join(
			s.KSailConfig.Spec.Workload.SourceDirectory,
			filepath.FromSlash(cleanPath),
		), nil
	}
}

// validateKustomizationPath normalizes rawPath to slash semantics and validates it.
// It returns the cleaned slash path ready for filepath.FromSlash conversion.
func validateKustomizationPath(rawPath string) (string, error) {
	// Normalize Windows-style backslashes to forward slashes before validation.
	// The OS-independent path package must be used to reject absolute paths and
	// traversal consistently on all platforms.
	cleanPath := path.Clean(strings.ReplaceAll(rawPath, "\\", "/"))

	if path.IsAbs(cleanPath) {
		return "", fmt.Errorf("%w: %q is absolute", ErrInvalidKustomizationFilePath, rawPath)
	}

	if isWindowsDriveLetter(cleanPath) {
		return "", fmt.Errorf(
			"%w: %q contains a Windows drive letter",
			ErrInvalidKustomizationFilePath,
			rawPath,
		)
	}

	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf(
			"%w: %q traverses parent directories",
			ErrInvalidKustomizationFilePath,
			rawPath,
		)
	}

	return cleanPath, nil
}

// jscpd:ignore-start
// isWindowsDriveLetter reports whether slashPath begins with a Windows drive-letter prefix
// (a letter A-Z or a-z followed by ':' and then '/' or end of string).
// slashPath must already be slash-normalized before calling this function.
func isWindowsDriveLetter(slashPath string) bool {
	if len(slashPath) < 2 { //nolint:mnd // minimum length for drive letter "X:"
		return false
	}

	first := slashPath[0]

	return ((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')) &&
		slashPath[1] == ':' &&
		(len(slashPath) == 2 || slashPath[2] == '/')
}

// jscpd:ignore-end
