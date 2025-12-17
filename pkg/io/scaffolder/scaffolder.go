package scaffolder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail-go/pkg/io/generator"
	k3dgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/k3d"
	kindgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/kind"
	kustomizationgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/kustomization"
	yamlgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
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
)

const (
	// Default images.

	// defaultK3sImage pins K3d clusters to a Flux-compatible Kubernetes version.
	defaultK3sImage = "rancher/k3s:v1.29.4-k3s1"
)

var (
	// Scaffolding errors.

	// ErrUnknownDistribution indicates an unsupported distribution was requested.
	ErrUnknownDistribution = errors.New("unknown distribution")

	// ErrKSailConfigGeneration wraps failures when creating ksail.yaml.
	ErrKSailConfigGeneration = errors.New("failed to generate ksail configuration")

	// ErrKindConfigGeneration wraps failures when creating Kind configuration.
	ErrKindConfigGeneration = errors.New("failed to generate kind configuration")

	// ErrK3dConfigGeneration wraps failures when creating K3d configuration.
	ErrK3dConfigGeneration = errors.New("failed to generate k3d configuration")

	// ErrKustomizationGeneration wraps failures when creating kustomization.yaml.
	ErrKustomizationGeneration = errors.New("failed to generate kustomization configuration")
)

// Scaffolder is responsible for generating KSail project files and configurations.
type Scaffolder struct {
	KSailConfig            v1alpha1.Cluster
	KSailYAMLGenerator     generator.Generator[v1alpha1.Cluster, yamlgenerator.Options]
	KindGenerator          generator.Generator[*v1alpha4.Cluster, yamlgenerator.Options]
	K3dGenerator           generator.Generator[*k3dv1alpha5.SimpleConfig, yamlgenerator.Options]
	KustomizationGenerator generator.Generator[*ktypes.Kustomization, yamlgenerator.Options]
	Writer                 io.Writer
	MirrorRegistries       []string // Format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
}

// NewScaffolder creates a new Scaffolder instance with the provided KSail cluster configuration.
func NewScaffolder(cfg v1alpha1.Cluster, writer io.Writer) *Scaffolder {
	ksailGenerator := yamlgenerator.NewYAMLGenerator[v1alpha1.Cluster]()
	kindGenerator := kindgenerator.NewKindGenerator()
	k3dGenerator := k3dgenerator.NewK3dGenerator()
	kustomizationGenerator := kustomizationgenerator.NewKustomizationGenerator()

	return &Scaffolder{
		KSailConfig:            cfg,
		KSailYAMLGenerator:     ksailGenerator,
		KindGenerator:          kindGenerator,
		K3dGenerator:           k3dGenerator,
		KustomizationGenerator: kustomizationGenerator,
		Writer:                 writer,
	}
}

// Main scaffolding operations.

// Scaffold generates project files and configurations.
//
// This method orchestrates the generation of:
//   - ksail.yaml configuration
//   - Distribution-specific configuration (kind.yaml or k3d.yaml)
//   - kustomization.yaml in the source directory
//
// Parameters:
//   - output: The output directory for generated files
//   - force: If true, overwrites existing files; if false, skips existing files
//
// Returns:
//   - error: Any error encountered during scaffolding
func (s *Scaffolder) Scaffold(output string, force bool) error {
	previousDistributionConfig := strings.TrimSpace(s.KSailConfig.Spec.DistributionConfig)

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

	return s.generateKustomizationConfig(output, force)
}

// Registry configuration helpers.

// GenerateContainerdPatches generates containerd config patches for Kind mirror registry.
// Input format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
// Container names match the registry host after sanitization to align with runtime provisioning.
func (s *Scaffolder) GenerateContainerdPatches() []string {
	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)
	if len(specs) == 0 {
		return nil
	}

	entries := registry.BuildMirrorEntries(specs, "", nil, nil, nil)
	patches := make([]string, 0, len(entries))

	for _, entry := range entries {
		patches = append(patches, registry.KindPatch(entry))
	}

	return patches
}

// GenerateK3dRegistryConfig generates K3d registry configuration for mirror registry.
// Input format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
// K3d requires one registry per proxy, so we generate multiple create configs.
func (s *Scaffolder) GenerateK3dRegistryConfig() k3dv1alpha5.SimpleConfigRegistries {
	registryConfig := k3dv1alpha5.SimpleConfigRegistries{}

	if s.KSailConfig.Spec.Distribution != v1alpha1.DistributionK3d {
		return registryConfig
	}

	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)

	hostEndpoints, updated := registry.BuildHostEndpointMap(specs, "", nil)
	if len(hostEndpoints) == 0 || !updated {
		return registryConfig
	}

	registryConfig.Config = registry.RenderK3dMirrorConfig(hostEndpoints)

	return registryConfig
}

// Distribution configuration builders.

// CreateK3dConfig creates a K3d configuration with distribution-specific settings.
func (s *Scaffolder) CreateK3dConfig() k3dv1alpha5.SimpleConfig {
	config := k3dv1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		Image: defaultK3sImage,
		// Additional configuration will be handled by the provisioner with sensible defaults
		// Users can override any settings in this generated config file
	}

	// Initialize ExtraArgs slice
	var extraArgs []k3dv1alpha5.K3sArgWithNodeFilters

	// Disable default CNI (Flannel) if Cilium is requested
	if s.KSailConfig.Spec.CNI == v1alpha1.CNICilium {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--flannel-backend=none",
				NodeFilters: []string{"server:*"},
			},
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable-network-policy",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	// Disable metrics-server if explicitly disabled (K3s includes it by default)
	if s.KSailConfig.Spec.MetricsServer == v1alpha1.MetricsServerDisabled {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable=metrics-server",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	// Set ExtraArgs if we have any
	if len(extraArgs) > 0 {
		config.Options.K3sOptions.ExtraArgs = extraArgs
	}

	// Add registry configuration for mirror registries
	if len(s.MirrorRegistries) > 0 {
		config.Registries = s.GenerateK3dRegistryConfig()
	}

	return config
}

// Configuration defaults and helpers.

// applyKSailConfigDefaults applies distribution-specific defaults to the KSail configuration.
// This ensures the generated ksail.yaml has consistent context and distributionConfig values
// that match the distribution-specific configuration files being generated.
func (s *Scaffolder) applyKSailConfigDefaults() v1alpha1.Cluster {
	config := s.KSailConfig

	// Set the expected context if it's empty, based on the distribution and default cluster names
	if config.Spec.Connection.Context == "" {
		expectedContext := v1alpha1.ExpectedContextName(config.Spec.Distribution)
		if expectedContext != "" {
			config.Spec.Connection.Context = expectedContext
		}
	}

	// Set the expected distribution config filename if it's empty or set to default
	if config.Spec.DistributionConfig == "" || config.Spec.DistributionConfig == KindConfigFile {
		expectedConfigName := v1alpha1.ExpectedDistributionConfigName(config.Spec.Distribution)
		config.Spec.DistributionConfig = expectedConfigName
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

	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
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
	switch s.KSailConfig.Spec.Distribution {
	case v1alpha1.DistributionKind:
		return s.generateKindConfig(output, force)
	case v1alpha1.DistributionK3d:
		return s.generateK3dConfig(output, force)
	default:
		return ErrUnknownDistribution
	}
}

func (s *Scaffolder) removeFormerDistributionConfig(output, previous string) error {
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return nil
	}

	newConfigName := v1alpha1.ExpectedDistributionConfigName(s.KSailConfig.Spec.Distribution)
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

// generateKindConfig generates the kind.yaml configuration file.
func (s *Scaffolder) generateKindConfig(output string, force bool) error {
	// Create Kind cluster configuration with standard KSail name
	kindConfig := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			APIVersion: "kind.x-k8s.io/v1alpha4",
			Kind:       "Cluster",
		},
		Name: "kind",
	}

	// Disable default CNI if Cilium is requested
	if s.KSailConfig.Spec.CNI == v1alpha1.CNICilium {
		kindConfig.Networking.DisableDefaultCNI = true
	}

	// Add containerd config patches for mirror registries
	if len(s.MirrorRegistries) > 0 {
		kindConfig.ContainerdConfigPatches = s.GenerateContainerdPatches()
	}

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, KindConfigFile),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*v1alpha4.Cluster]{
			Gen:         s.KindGenerator,
			Model:       kindConfig,
			Opts:        opts,
			DisplayName: "kind.yaml",
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKindConfigGeneration, err)
			},
		},
	)
}

// generateK3dConfig generates the k3d.yaml configuration file.
func (s *Scaffolder) generateK3dConfig(output string, force bool) error {
	k3dConfig := s.CreateK3dConfig()

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, "k3d.yaml"),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*k3dv1alpha5.SimpleConfig]{
			Gen:         s.K3dGenerator,
			Model:       &k3dConfig,
			Opts:        opts,
			DisplayName: "k3d.yaml",
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrK3dConfigGeneration, err)
			},
		},
	)
}

// generateKustomizationConfig generates the kustomization.yaml file.
func (s *Scaffolder) generateKustomizationConfig(output string, force bool) error {
	kustomization := ktypes.Kustomization{}

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, s.KSailConfig.Spec.SourceDirectory, "kustomization.yaml"),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*ktypes.Kustomization]{
			Gen:         s.KustomizationGenerator,
			Model:       &kustomization,
			Opts:        opts,
			DisplayName: filepath.Join(s.KSailConfig.Spec.SourceDirectory, "kustomization.yaml"),
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKustomizationGeneration, err)
			},
		},
	)
}
