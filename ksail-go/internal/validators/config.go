package validators

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// ConfigValidator validates consistency between ksail cluster config and distribution config files.
// Only a subset of the .NET validator is implemented (cluster name + context naming) because
// other advanced spec fields (mirror registries, flux source, metrics server toggles, etc.) are not yet modeled.
type ConfigValidator struct {
	cfg *ksailcluster.Cluster
}

func NewConfigValidator(cfg *ksailcluster.Cluster) *ConfigValidator {
	return &ConfigValidator{cfg: cfg}
}

// Validate performs validation of configuration files.
func (v *ConfigValidator) Validate() error {
	if v.cfg == nil {
		return errors.New("config is nil")
	}
	fmt.Println("🕵 Validating project files and config")
	fmt.Println("► locating config")
	projectRoot, err := locateProjectRoot()
	if err != nil {
		fmt.Println("✔ skipping config validation")
		fmt.Println("  - no configuration files found in current or parent directories")
		return nil
	}
	fmt.Printf("✔ located config in '%s'\n", projectRoot)
	fmt.Printf("► validating config in '%s'\n", projectRoot)
	if err := v.checkContextName(v.cfg); err != nil {
		return err
	}
	if err := v.checkDistributionConfig(projectRoot, v.cfg); err != nil {
		return err
	}
	fmt.Println("✔ configuration is valid")
	return nil
}

// --- internals ---

// locateProjectRoot ascends directories until ksail.yaml is found.
func locateProjectRoot() (string, error) {
	dir := "./"
	for {
		if _, err := os.Stat(filepath.Join(dir, "ksail.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			return "", fmt.Errorf("no 'ksail.yaml' found in '%s' or parent directories", dir)
		}
		dir = parent
	}
}

func (v *ConfigValidator) checkContextName(cfg *ksailcluster.Cluster) error {
	expected := ""
	switch cfg.Spec.Distribution {
	case ksailcluster.DistributionKind:
		expected = fmt.Sprintf("kind-%s", cfg.Metadata.Name)
	case ksailcluster.DistributionK3d:
		expected = fmt.Sprintf("k3d-%s", cfg.Metadata.Name)
	default:
		return fmt.Errorf("unsupported distribution '%s'", cfg.Spec.Distribution)
	}
	if ctx := cfg.Spec.Connection.Context; ctx != "" && ctx != expected {
		return fmt.Errorf("spec.connection.context '%s' does not match expected '%s'", ctx, expected)
	}
	return nil
}

func (v *ConfigValidator) checkDistributionConfig(root string, cfg *ksailcluster.Cluster) error {
	fileName := cfg.Spec.DistributionConfig
	path := filepath.Join(root, fileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("► '%s' not found, skipping distribution config validation\n", path)
			return nil
		}
		return fmt.Errorf("read distribution config '%s': %w", path, err)
	}

	switch cfg.Spec.Distribution {
	case ksailcluster.DistributionKind:
		var kindCfg v1alpha4.Cluster
		if err := yaml.Unmarshal(data, &kindCfg); err != nil {
			return fmt.Errorf("unmarshal kind config: %w", err)
		}
		if kindCfg.Name != "" && kindCfg.Name != cfg.Metadata.Name {
			return fmt.Errorf("%s name '%s' does not match ksail.yaml metadata.name '%s'", cfg.Spec.DistributionConfig, kindCfg.Name, cfg.Metadata.Name)
		}
	case ksailcluster.DistributionK3d:
		var k3dCfg v1alpha5.SimpleConfig
		if err := yaml.Unmarshal(data, &k3dCfg); err != nil {
			return fmt.Errorf("unmarshal k3d config: %w", err)
		}
		if k3dCfg.Name != "" && k3dCfg.Name != cfg.Metadata.Name {
			return fmt.Errorf("%s metadata.name '%s' does not match ksail.yaml metadata.name '%s'", cfg.Spec.DistributionConfig, k3dCfg.Name, cfg.Metadata.Name)
		}
	}
	return nil
}
