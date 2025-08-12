package loader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/pkg/marshaller"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dConfigLoader loads K3d config; uses Default when file isn't found
type K3dConfigLoader struct {
	Marshaller marshaller.Marshaller[*v1alpha5.SimpleConfig]
	Default    *v1alpha5.SimpleConfig
}

func (cl *K3dConfigLoader) Load() (v1alpha5.SimpleConfig, error) {
	fmt.Println("⏳ Loading K3d config")
	for dir := "./"; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "k3d.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return v1alpha5.SimpleConfig{}, fmt.Errorf("read k3d config: %w", err)
			}
			cfg := &v1alpha5.SimpleConfig{}
			if err := cl.Marshaller.Unmarshal(data, cfg); err != nil {
				return v1alpha5.SimpleConfig{}, fmt.Errorf("unmarshal k3d config: %w", err)
			}
			fmt.Printf("► '%s' found\n", configPath)
			fmt.Println("✔ config loaded")
      fmt.Println()
			return *cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './k3d.yaml' not found, using default configuration")
	var config *v1alpha5.SimpleConfig
	if cl.Default != nil {
		config = cl.Default
	} else {
		config = &v1alpha5.SimpleConfig{Servers: 1, Agents: 0}
	}
	fmt.Println("✔ config loaded")
  fmt.Println()
	return *config, nil

}

func NewK3dConfigLoader() *K3dConfigLoader {
	m := marshaller.NewMarshaller[*v1alpha5.SimpleConfig]()
	return &K3dConfigLoader{
		Marshaller: m,
		Default:    &v1alpha5.SimpleConfig{},
	}
}
