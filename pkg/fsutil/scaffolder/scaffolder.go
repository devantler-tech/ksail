package scaffolder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil/generator"
	k3dgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/k3d"
	kindgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/kind"
	kustomizationgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/kustomization"
	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
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
	}
}

// WithClusterName sets an explicit cluster name override for the scaffolder.
// When set, this name is used for distribution configs (kind.yaml Name, k3d.yaml metadata.name),
// Talos cluster patches, and to derive the kubeconfig context in ksail.yaml.
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
//
// Parameters:
//   - output: The output directory for generated files
//   - force: If true, overwrites existing files; if false, skips existing files
//
// Returns:
//   - error: Any error encountered during scaffolding
func (s *Scaffolder) Scaffold(output string, force bool) error {
	previousDistributionConfig := strings.TrimSpace(s.KSailConfig.Spec.Cluster.DistributionConfig)

	err := s.generateKSailConfig(output, force)
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

	// Generate GitOps CR manifests if a GitOps engine is configured
	err = s.generateGitOpsConfig(output, force)
	if err != nil {
		return err
	}

	return s.generateKustomizationConfig(output, force)
}

// Configuration defaults and helpers.

// applyKSailConfigDefaults applies distribution-specific defaults to the KSail configuration.
// This ensures the generated ksail.yaml has consistent context and distributionConfig values
// that match the distribution-specific configuration files being generated.
func (s *Scaffolder) applyKSailConfigDefaults() v1alpha1.Cluster {
	config := s.KSailConfig

	// Set the expected context if it's empty, based on the distribution and cluster name
	if config.Spec.Cluster.Connection.Context == "" {
		var expectedContext string
		if s.ClusterName != "" {
			// Use custom cluster name to derive context
			expectedContext = s.contextNameForDistribution(config.Spec.Cluster.Distribution)
		} else {
			// Use default context name
			expectedContext = v1alpha1.ExpectedContextName(config.Spec.Cluster.Distribution)
		}

		if expectedContext != "" {
			config.Spec.Cluster.Connection.Context = expectedContext
		}
	}

	// Set the expected distribution config filename if it's empty or set to default
	if config.Spec.Cluster.DistributionConfig == "" ||
		config.Spec.Cluster.DistributionConfig == KindConfigFile {
		expectedConfigName := v1alpha1.ExpectedDistributionConfigName(
			config.Spec.Cluster.Distribution,
		)
		config.Spec.Cluster.DistributionConfig = expectedConfigName
	}

	return config
}

// contextNameForDistribution returns the kubeconfig context name for a given distribution
// using the scaffolder's ClusterName. Returns empty string if ClusterName is not set.
func (s *Scaffolder) contextNameForDistribution(distribution v1alpha1.Distribution) string {
	return distribution.ContextName(s.ClusterName)
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
			Gen:         s.KSailYAMLGenerator,
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

// generateGitOpsConfig is a no-op placeholder for GitOps configuration generation.
// GitOps resources (FluxInstance, ArgoCD Application) are NOT scaffolded as YAML files.
// Instead, they are created via Kubernetes API during cluster creation, which ensures:
// - FluxInstance has all required fields (Distribution.Artifact, Sync.Provider, etc.)
// - ArgoCD Application is properly configured with server-side defaults
// - No conflicts between scaffolded YAML and API-created resources
// Users can overtake the source-of-truth via GitOps if they wish after cluster creation.
func (s *Scaffolder) generateGitOpsConfig(_ string, _ bool) error {
	// All GitOps engines (Flux, ArgoCD) create their resources server-side during cluster creation.
	// This simplifies scaffolding and ensures proper configuration.
	return nil
}

// generateKustomizationConfig generates the kustomization.yaml file.
func (s *Scaffolder) generateKustomizationConfig(output string, force bool) error {
	kustomization := ktypes.Kustomization{}

	// Add GitOps resources if a GitOps engine is configured
	kustomization.Resources = s.getKustomizationResources()

	opts := yamlgenerator.Options{
		Output: filepath.Join(
			output,
			s.KSailConfig.Spec.Workload.SourceDirectory,
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
				s.KSailConfig.Spec.Workload.SourceDirectory,
				"kustomization.yaml",
			),
			Force: force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKustomizationGeneration, err)
			},
		},
	)
}

// getKustomizationResources returns the resources to include in the kustomization.
// GitOps resources (FluxInstance, ArgoCD Application) are created server-side via Kubernetes API,
// not scaffolded, so this returns an empty slice.
func (s *Scaffolder) getKustomizationResources() []string {
	return []string{}
}
