package util

import (
	"fmt"
	"os"
	"path/filepath"

	yamlMarshaller "devantler.tech/ksail/pkg/marshaller/yaml"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type KindConfigLoader struct {
	Marshaller yamlMarshaller.YamlMarshaller[*v1alpha4.Cluster]
}

func (cl *KindConfigLoader) LoadKindConfig(directory string) (*v1alpha4.Cluster, error) {
	for dir := directory; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "kind.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("✗ %s", err)
			}
			kindConfig := &v1alpha4.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, kindConfig); err != nil {
				return nil, fmt.Errorf("✗ %s", err)
			}
			fmt.Printf("► '%s' found.\n", configPath)
			return kindConfig, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './kind.yaml' not found. Using default configuration.")
	kindConfig := v1alpha4.Cluster{}
	v1alpha4.SetDefaultsCluster(&kindConfig)

	return &kindConfig, nil
}

func NewKindConfigLoader() *KindConfigLoader {
	marshaller := yamlMarshaller.NewYamlMarshaller[*v1alpha4.Cluster]()
	return &KindConfigLoader{
		Marshaller: *marshaller,
	}
}
