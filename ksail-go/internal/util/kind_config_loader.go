package util

import (
	"fmt"
	"os"
	"path/filepath"

	marshalleryaml "devantler.tech/ksail/pkg/marshaller/yaml"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type KindConfigLoader struct {
	Marshaller marshalleryaml.Marshaller[*v1alpha4.Cluster]
}

func (cl *KindConfigLoader) LoadKindConfig() (*v1alpha4.Cluster, error) {
	fmt.Println("⏳ Loading Kind configuration...")
	for dir := "./"; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "kind.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("read kind config: %w", err)
			}
			kindConfig := &v1alpha4.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, kindConfig); err != nil {
				return nil, fmt.Errorf("unmarshal kind config: %w", err)
			}
			fmt.Printf("► '%s' found.\n", configPath)
			fmt.Println("✔ kind configuration loaded")
			fmt.Println("")
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

	fmt.Println("✔ kind configuration loaded")
	fmt.Println("")
	return &kindConfig, nil
}

func NewKindConfigLoader() *KindConfigLoader {
	marshaller := marshalleryaml.NewMarshaller[*v1alpha4.Cluster]()
	return &KindConfigLoader{
		Marshaller: *marshaller,
	}
}
