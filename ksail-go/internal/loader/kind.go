package loader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/pkg/marshaller"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindConfigLoader loads Kind config; uses Default when file isn't found
type KindConfigLoader struct {
    Marshaller marshaller.Marshaller[*v1alpha4.Cluster]
    Default    *v1alpha4.Cluster
}

func (cl *KindConfigLoader) Load() (v1alpha4.Cluster, error) {
    fmt.Println("⏳ Loading Kind config")
    for dir := "./"; ; dir = filepath.Dir(dir) {
        configPath := filepath.Join(dir, "kind.yaml")
        if _, err := os.Stat(configPath); err == nil {
            data, err := os.ReadFile(configPath)
            if err != nil {
                return v1alpha4.Cluster{}, fmt.Errorf("read kind config: %w", err)
            }
            cfg := &v1alpha4.Cluster{}
            if err := cl.Marshaller.Unmarshal(data, cfg); err != nil {
                return v1alpha4.Cluster{}, fmt.Errorf("unmarshal kind config: %w", err)
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
    fmt.Println("► './kind.yaml' not found, using default configuration")
    var cfg *v1alpha4.Cluster
    if cl.Default != nil {
        cfg = cl.Default
    } else {
        kc := v1alpha4.Cluster{}
        v1alpha4.SetDefaultsCluster(&kc)
        cfg = &kc
    }

    fmt.Println("✔ config loaded")
    fmt.Println()
    return *cfg, nil
}

func NewKindConfigLoader() *KindConfigLoader {
    m := marshaller.NewMarshaller[*v1alpha4.Cluster]()
    return &KindConfigLoader{
        Marshaller: m,
        Default:    &v1alpha4.Cluster{},
    }
}
