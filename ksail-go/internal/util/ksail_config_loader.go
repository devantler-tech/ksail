package util

import (
	"fmt"
	"os"
	"path/filepath"

	color "devantler.tech/ksail/internal/util/fmt"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlMarshaller "devantler.tech/ksail/pkg/marshaller/yaml"
)

type KSailConfigLoader struct {
	Marshaller yamlMarshaller.YamlMarshaller[*cluster.Cluster]
}

func (cl *KSailConfigLoader) LoadKSailConfig() *cluster.Cluster {
	fmt.Println("⏳ Loading configuration...")
	for dir := "."; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "ksail.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				color.PrintError("%s", err)
				os.Exit(1)
			}
			ksailConfig := &cluster.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, ksailConfig); err != nil {
				color.PrintError("%s", err)
				os.Exit(1)
			}
			fmt.Printf("► '%s' found.\n", configPath)
			return ksailConfig
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './ksail.yaml' not found. Using default configuration.")
	ksailConfig := cluster.NewCluster()

	fmt.Println("✔ configuration loaded")
	fmt.Println("")
	return ksailConfig
}

func NewKSailConfigLoader() *KSailConfigLoader {
	marshaller := yamlMarshaller.NewYamlMarshaller[*cluster.Cluster]()
	return &KSailConfigLoader{
		Marshaller: *marshaller,
	}
}
