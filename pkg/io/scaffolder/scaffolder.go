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
	"github.com/devantler-tech/ksail/v5/pkg/io/generator"
	k3dgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/k3d"
	kindgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/kind"
	kustomizationgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/kustomization"
	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/talosindocker"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
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

	// TalosInDockerConfigDir is the default directory for TalosInDocker distribution configuration (Talos patches).
	TalosInDockerConfigDir = "talos"
)

const (
	// File permission constants.
	dirPerm  = 0o750
	filePerm = 0o600
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

	// ErrTalosInDockerConfigGeneration wraps failures when creating TalosInDocker configuration.
	ErrTalosInDockerConfigGeneration = errors.New("failed to generate talosindocker configuration")

	// ErrKustomizationGeneration wraps failures when creating kustomization.yaml.
	ErrKustomizationGeneration = errors.New("failed to generate kustomization configuration")
)

// Scaffolder is responsible for generating KSail project files and configurations.
type Scaffolder struct {
	KSailConfig            v1alpha1.Cluster
	KSailYAMLGenerator     generator.Generator[v1alpha1.Cluster, yamlgenerator.Options]
	KindGenerator          generator.Generator[*v1alpha4.Cluster, yamlgenerator.Options]
	K3dGenerator           generator.Generator[*k3dv1alpha5.SimpleConfig, yamlgenerator.Options]
	TalosInDockerGenerator *talosgenerator.TalosInDockerGenerator
	KustomizationGenerator generator.Generator[*ktypes.Kustomization, yamlgenerator.Options]
	Writer                 io.Writer
	MirrorRegistries       []string // Format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
}

// NewScaffolder creates a new Scaffolder instance with the provided KSail cluster configuration.
func NewScaffolder(cfg v1alpha1.Cluster, writer io.Writer, mirrorRegistries []string) *Scaffolder {
	ksailGenerator := yamlgenerator.NewYAMLGenerator[v1alpha1.Cluster]()
	kindGenerator := kindgenerator.NewKindGenerator()
	k3dGenerator := k3dgenerator.NewK3dGenerator()
	talosGen := talosgenerator.NewTalosInDockerGenerator()
	kustomizationGenerator := kustomizationgenerator.NewKustomizationGenerator()

	return &Scaffolder{
		KSailConfig:            cfg,
		KSailYAMLGenerator:     ksailGenerator,
		KindGenerator:          kindGenerator,
		K3dGenerator:           k3dGenerator,
		TalosInDockerGenerator: talosGen,
		KustomizationGenerator: kustomizationGenerator,
		Writer:                 writer,
		MirrorRegistries:       mirrorRegistries,
	}
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

	return s.generateKustomizationConfig(output, force)
}

// Registry configuration helpers.

// GenerateK3dRegistryConfig generates K3d registry configuration for mirror registry.
// Input format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
// K3d requires one registry per proxy, so we generate multiple create configs.
func (s *Scaffolder) GenerateK3dRegistryConfig() k3dv1alpha5.SimpleConfigRegistries {
	registryConfig := k3dv1alpha5.SimpleConfigRegistries{}

	if s.KSailConfig.Spec.Cluster.Distribution != v1alpha1.DistributionK3d {
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
// Node counts can be set via --control-planes and --workers CLI flags.
func (s *Scaffolder) CreateK3dConfig() k3dv1alpha5.SimpleConfig {
	config := k3dv1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		Image: defaultK3sImage,
	}

	// Apply node counts from CLI flags (stored in TalosInDocker options)
	// These values are used across all distributions for consistency
	controlPlanes := int(s.KSailConfig.Spec.Cluster.Talos.ControlPlanes)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)
	if controlPlanes > 0 {
		config.Servers = controlPlanes
	}
	if workers > 0 {
		config.Agents = workers
	}

	// Initialize ExtraArgs slice
	var extraArgs []k3dv1alpha5.K3sArgWithNodeFilters

	// Disable default CNI (Flannel) if using a non-default CNI (Cilium or Calico)
	if s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICilium ||
		s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICalico {
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
	if s.KSailConfig.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerDisabled {
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
	if config.Spec.Cluster.Connection.Context == "" {
		expectedContext := v1alpha1.ExpectedContextName(config.Spec.Cluster.Distribution)
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
	switch s.KSailConfig.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		return s.generateKindConfig(output, force)
	case v1alpha1.DistributionK3d:
		return s.generateK3dConfig(output, force)
	case v1alpha1.DistributionTalosInDocker:
		return s.generateTalosInDockerConfig(output, force)
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

// generateKindConfig generates the kind.yaml configuration file.
func (s *Scaffolder) generateKindConfig(output string, force bool) error {
	kindConfig := s.buildKindConfig(output)

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

// buildKindConfig creates the Kind cluster configuration object.
// Node counts can be set via --control-planes and --workers CLI flags.
func (s *Scaffolder) buildKindConfig(output string) *v1alpha4.Cluster {
	kindConfig := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			APIVersion: "kind.x-k8s.io/v1alpha4",
			Kind:       "Cluster",
		},
		Name: "kind",
	}

	// Disable default CNI if using a non-default CNI (Cilium or Calico)
	if s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICilium ||
		s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICalico {
		kindConfig.Networking.DisableDefaultCNI = true
	}

	// Apply node counts from CLI flags (stored in TalosInDocker options)
	s.applyKindNodeCounts(kindConfig)

	s.addMirrorMountsToKindConfig(kindConfig, output)

	return kindConfig
}

// applyKindNodeCounts sets up Kind nodes based on --control-planes and --workers CLI flags.
func (s *Scaffolder) applyKindNodeCounts(kindConfig *v1alpha4.Cluster) {
	controlPlanes := int(s.KSailConfig.Spec.Cluster.Talos.ControlPlanes)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)

	// Only generate nodes if explicitly configured
	if controlPlanes <= 0 && workers <= 0 {
		return
	}

	// Default to 1 control-plane if workers specified but not control-planes
	if controlPlanes <= 0 {
		controlPlanes = 1
	}

	// Build nodes slice
	var nodes []v1alpha4.Node
	for range controlPlanes {
		nodes = append(nodes, v1alpha4.Node{Role: v1alpha4.ControlPlaneRole})
	}
	for range workers {
		nodes = append(nodes, v1alpha4.Node{Role: v1alpha4.WorkerRole})
	}

	kindConfig.Nodes = nodes
}

// addMirrorMountsToKindConfig adds extraMounts for mirror registries to the Kind config.
func (s *Scaffolder) addMirrorMountsToKindConfig(kindConfig *v1alpha4.Cluster, output string) {
	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)
	if len(specs) == 0 {
		return
	}

	kindMirrorsDir := s.GetKindMirrorsDir()
	mirrorsDir := filepath.Join(output, kindMirrorsDir)

	absHostsDir, err := filepath.Abs(mirrorsDir)
	if err != nil {
		absHostsDir = mirrorsDir
	}

	if len(kindConfig.Nodes) == 0 {
		kindConfig.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}
	}

	for _, spec := range specs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		mount := v1alpha4.Mount{
			HostPath:      filepath.Join(absHostsDir, host),
			ContainerPath: "/etc/containerd/certs.d/" + host,
			Readonly:      true,
		}

		for i := range kindConfig.Nodes {
			kindConfig.Nodes[i].ExtraMounts = append(kindConfig.Nodes[i].ExtraMounts, mount)
		}
	}
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

// generateTalosInDockerConfig generates the TalosInDocker patches directory structure.
func (s *Scaffolder) generateTalosInDockerConfig(output string, force bool) error {
	// Get worker count from TalosInDocker options (default 0)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)

	// Disable default CNI (Flannel) if using any non-default CNI (e.g., Cilium, Calico, None)
	// Empty string is treated as default CNI (for imperative mode without config file)
	disableDefaultCNI := s.KSailConfig.Spec.Cluster.CNI != v1alpha1.CNIDefault &&
		s.KSailConfig.Spec.Cluster.CNI != ""

	config := &talosgenerator.TalosInDockerConfig{
		PatchesDir:        TalosInDockerConfigDir,
		MirrorRegistries:  s.MirrorRegistries,
		WorkerNodes:       workers,
		DisableDefaultCNI: disableDefaultCNI,
	}

	opts := yamlgenerator.Options{
		Output: output,
		Force:  force,
	}

	_, err := s.TalosInDockerGenerator.Generate(config, opts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTalosInDockerConfigGeneration, err)
	}

	s.notifyTalosInDockerGenerated(workers, disableDefaultCNI)

	return nil
}

// notifyTalosInDockerGenerated sends notifications about generated TalosInDocker files.
func (s *Scaffolder) notifyTalosInDockerGenerated(workers int, disableDefaultCNI bool) {
	// Determine which directories have patches (no .gitkeep generated there)
	clusterHasPatches := workers == 0 || len(s.MirrorRegistries) > 0 || disableDefaultCNI

	// Notify about .gitkeep files only for directories without patches
	subdirs := []string{"cluster", "control-planes", "workers"}
	for _, subdir := range subdirs {
		// Skip .gitkeep notification for cluster/ if it has patches
		if subdir == "cluster" && clusterHasPatches {
			continue
		}

		displayPath := filepath.Join(TalosInDockerConfigDir, subdir, ".gitkeep")
		notify.WriteMessage(notify.Message{
			Type:    notify.GenerateType,
			Content: "created '%s'",
			Args:    []any{displayPath},
			Writer:  s.Writer,
		})
	}

	// Notify about allow-scheduling-on-control-planes patch (only created when no workers)
	if workers == 0 {
		displayPath := filepath.Join(
			TalosInDockerConfigDir,
			"cluster",
			"allow-scheduling-on-control-planes.yaml",
		)
		notify.WriteMessage(notify.Message{
			Type:    notify.GenerateType,
			Content: "created '%s'",
			Args:    []any{displayPath},
			Writer:  s.Writer,
		})
	}

	// Notify about mirror registries patch if created
	if len(s.MirrorRegistries) > 0 {
		displayPath := filepath.Join(TalosInDockerConfigDir, "cluster", "mirror-registries.yaml")
		notify.WriteMessage(notify.Message{
			Type:    notify.GenerateType,
			Content: "created '%s'",
			Args:    []any{displayPath},
			Writer:  s.Writer,
		})
	}

	// Notify about disable-default-cni patch if created
	if disableDefaultCNI {
		displayPath := filepath.Join(TalosInDockerConfigDir, "cluster", "disable-default-cni.yaml")
		notify.WriteMessage(notify.Message{
			Type:    notify.GenerateType,
			Content: "created '%s'",
			Args:    []any{displayPath},
			Writer:  s.Writer,
		})
	}
}

// generateKustomizationConfig generates the kustomization.yaml file.
func (s *Scaffolder) generateKustomizationConfig(output string, force bool) error {
	kustomization := ktypes.Kustomization{}

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

// DefaultKindMirrorsDir is the default directory name for Kind containerd host mirror configuration.
const DefaultKindMirrorsDir = "kind/mirrors"

// GetKindMirrorsDir returns the configured mirrors directory or the default.
func (s *Scaffolder) GetKindMirrorsDir() string {
	if s.KSailConfig.Spec.Cluster.Kind.MirrorsDir != "" {
		return s.KSailConfig.Spec.Cluster.Kind.MirrorsDir
	}
	return DefaultKindMirrorsDir
}

// generateKindMirrorsConfig generates hosts.toml files for Kind registry mirrors.
// Each mirror registry specification creates a subdirectory under the configured mirrors directory
// (default: kind/mirrors) with a hosts.toml file that configures containerd to use the specified upstream.
func (s *Scaffolder) generateKindMirrorsConfig(output string, force bool) error {
	if s.KSailConfig.Spec.Cluster.Distribution != v1alpha1.DistributionKind {
		return nil
	}

	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)
	if len(specs) == 0 {
		return nil
	}

	kindMirrorsDir := s.GetKindMirrorsDir()
	mirrorsDir := filepath.Join(output, kindMirrorsDir)

	for _, spec := range specs {
		registryDir := filepath.Join(mirrorsDir, spec.Host)
		hostsPath := filepath.Join(registryDir, "hosts.toml")
		displayName := filepath.Join(kindMirrorsDir, spec.Host, "hosts.toml")

		skip, existed, previousModTime := s.checkFileExistsAndSkip(hostsPath, displayName, force)
		if skip {
			continue
		}

		// Create directory structure
		err := os.MkdirAll(registryDir, dirPerm)
		if err != nil {
			return fmt.Errorf("failed to create mirror directory %s: %w", registryDir, err)
		}

		// Generate hosts.toml content
		content := registry.GenerateScaffoldedHostsToml(spec)

		// Write hosts.toml file
		err = os.WriteFile(hostsPath, []byte(content), filePerm)
		if err != nil {
			return fmt.Errorf("failed to write hosts.toml to %s: %w", hostsPath, err)
		}

		if force && existed {
			err = ensureOverwriteModTime(hostsPath, previousModTime)
			if err != nil {
				return fmt.Errorf("failed to update mod time for %s: %w", displayName, err)
			}
		}

		s.notifyFileAction(displayName, existed)
	}

	return nil
}
