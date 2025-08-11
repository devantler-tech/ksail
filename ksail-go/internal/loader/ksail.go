package loader

import (
	"fmt"
	"os"
	"path/filepath"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/devantler-tech/ksail/pkg/marshaller"
)

// KSailConfigLoader loads KSail config; uses Default when file isn't found
type KSailConfigLoader struct {
	Marshaller marshaller.Marshaller[*ksailcluster.Cluster]
	Default    *ksailcluster.Cluster
}

func (cl *KSailConfigLoader) Load() (ksailcluster.Cluster, error) {
	fmt.Println("⏳ Loading KSail config")
	for dir := "."; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "ksail.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return ksailcluster.Cluster{}, fmt.Errorf("read ksail config: %w", err)
			}
			ksailConfig := &ksailcluster.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, ksailConfig); err != nil {
				return ksailcluster.Cluster{}, fmt.Errorf("unmarshal ksail config: %w", err)
			}
			fmt.Printf("► '%s' found\n", configPath)
			fmt.Println("✔ config loaded")
			return *ksailConfig, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './ksail.yaml' not found, using default configuration")
	ksailConfig := cl.Default
	if ksailConfig == nil {
		ksailConfig = ksailcluster.NewCluster()
	}
	fmt.Println("✔ config loaded")
	return *ksailConfig, nil
}

func NewKSailConfigLoader() *KSailConfigLoader {
	m := marshaller.NewMarshaller[*ksailcluster.Cluster]()
	return &KSailConfigLoader{
		Marshaller: m,
		Default:    ksailcluster.NewCluster(),
	}
}
