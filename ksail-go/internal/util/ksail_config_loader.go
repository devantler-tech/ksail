package util

import (
	"fmt"
	"os"
	"path/filepath"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlmarshal "devantler.tech/ksail/pkg/marshaller/yaml"
)

type KSailConfigLoader struct { Marshaller yamlmarshal.Marshaller[*ksailcluster.Cluster] }

func (cl *KSailConfigLoader) LoadKSailConfig() (*ksailcluster.Cluster, error) {
	fmt.Println("⏳ Loading KSail configuration...")
	for dir := "."; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "ksail.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("read ksail config: %w", err)
			}
			ksailConfig := &ksailcluster.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, ksailConfig); err != nil {
				return nil, fmt.Errorf("unmarshal ksail config: %w", err)
			}
			fmt.Printf("► '%s' found.\n", configPath)
			fmt.Println("✔ ksail configuration loaded")
			fmt.Println("")
			return ksailConfig, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './ksail.yaml' not found. Using default configuration.")
	ksailConfig := ksailcluster.NewCluster()
	fmt.Println("✔ ksail configuration loaded")
	fmt.Println("")
	return ksailConfig, nil
}

func NewKSailConfigLoader() *KSailConfigLoader {
	marshaller := yamlmarshal.NewMarshaller[*ksailcluster.Cluster]()
	return &KSailConfigLoader{
		Marshaller: *marshaller,
	}
}
