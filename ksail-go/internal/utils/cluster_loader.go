package ksail

import (
	"fmt"
	"os"
	"path/filepath"

	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	marshaller_core "devantler.tech/ksail/pkg/marshaller/core"
)

type ClusterLoader struct {
	Marshaller marshaller_core.Marshaller[*cluster.Cluster]
}

func (cl *ClusterLoader) LoadCluster(directory string) (*cluster.Cluster, error) {
	// Ensure marshaller is initialized to avoid nil pointer dereference
	if cl == nil || cl.Marshaller == nil {
		return nil, fmt.Errorf("✗ marshaller is not initialized")
	}

	for dir := directory; ; dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, "ksail.yaml")
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("✗ %s", err)
			}
			clusterObj := &cluster.Cluster{}
			if err := cl.Marshaller.Unmarshal(data, clusterObj); err != nil {
				return nil, fmt.Errorf("✗ %s", err)
			}
			fmt.Printf("► '%s' found.\n", configPath)
			return clusterObj, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == "" {
			break
		}
	}
	fmt.Println("► './ksail.yaml' not found. Using default configuration.")
  clusterObj := cluster.NewCluster()

	return clusterObj, nil
}

func NewClusterLoader(marshaller marshaller_core.Marshaller[*cluster.Cluster]) *ClusterLoader {
	if marshaller == nil {
		fmt.Fprintln(os.Stderr, "✗ marshaller is not initialized")
		return nil
	}
	return &ClusterLoader{
		Marshaller: marshaller,
	}
}
