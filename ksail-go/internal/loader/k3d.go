package loader

import (
    "fmt"
    "os"
    "path/filepath"

    "devantler.tech/ksail/pkg/marshaller"
    confv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dConfigLoader loads K3d config; uses Default when file isn't found.
type K3dConfigLoader struct {
    Marshaller marshaller.Marshaller[*confv1alpha5.SimpleConfig]
    Default    *confv1alpha5.SimpleConfig
}

func (cl *K3dConfigLoader) Load() (any, error) {
    fmt.Println("⏳ Loading K3d configuration...")
    for dir := "./"; ; dir = filepath.Dir(dir) {
        configPath := filepath.Join(dir, "k3d.yaml")
        if _, err := os.Stat(configPath); err == nil {
            data, err := os.ReadFile(configPath)
            if err != nil {
                return nil, fmt.Errorf("read k3d config: %w", err)
            }
            cfg := &confv1alpha5.SimpleConfig{}
            if err := cl.Marshaller.Unmarshal(data, cfg); err != nil {
                return nil, fmt.Errorf("unmarshal k3d config: %w", err)
            }
            fmt.Printf("► '%s' found.\n", configPath)
            fmt.Println("✔ k3d configuration loaded")
            fmt.Println("")
            return cfg, nil
        }
        parent := filepath.Dir(dir)
        if parent == dir || dir == "" {
            break
        }
    }
    fmt.Println("► './k3d.yaml' not found. Using default configuration.")
    var cfg *confv1alpha5.SimpleConfig
    if cl.Default != nil {
        cfg = cl.Default
    } else {
        cfg = &confv1alpha5.SimpleConfig{Servers: 1, Agents: 0}
    }
    fmt.Println("✔ k3d configuration loaded")
    fmt.Println("")
    return cfg, nil
}

func NewK3dConfigLoader(defaultModel *confv1alpha5.SimpleConfig) *K3dConfigLoader {
    m := marshaller.NewMarshaller[*confv1alpha5.SimpleConfig]()
    return &K3dConfigLoader{Marshaller: m, Default: defaultModel}
}
