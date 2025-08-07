package models

import (
	"testing"
)

func TestNewDefaultProject(t *testing.T) {
	project := NewDefaultProject()
	
	if project == nil {
		t.Fatal("NewDefaultProject returned nil")
	}
	
	// Test default values
	if project.ConfigPath != "ksail.yaml" {
		t.Errorf("Expected ConfigPath to be 'ksail.yaml', got '%s'", project.ConfigPath)
	}
	
	if project.Distribution != DistributionKind {
		t.Errorf("Expected Distribution to be '%s', got '%s'", DistributionKind, project.Distribution)
	}
	
	if project.ContainerEngine != ContainerEngineDocker {
		t.Errorf("Expected ContainerEngine to be '%s', got '%s'", ContainerEngineDocker, project.ContainerEngine)
	}
	
	if !project.MetricsServer {
		t.Error("Expected MetricsServer to be true")
	}
}

func TestNewDefaultCluster(t *testing.T) {
	cluster := NewDefaultCluster("test-cluster")
	
	if cluster == nil {
		t.Fatal("NewDefaultCluster returned nil")
	}
	
	// Test metadata
	if cluster.Metadata.Name != "test-cluster" {
		t.Errorf("Expected cluster name to be 'test-cluster', got '%s'", cluster.Metadata.Name)
	}
	
	// Test spec
	if cluster.Spec.Name != "test-cluster" {
		t.Errorf("Expected spec name to be 'test-cluster', got '%s'", cluster.Spec.Name)
	}
	
	// Test API version and kind
	if cluster.ApiVersion != "ksail.io/v1alpha1" {
		t.Errorf("Expected ApiVersion to be 'ksail.io/v1alpha1', got '%s'", cluster.ApiVersion)
	}
	
	if cluster.Kind != "Cluster" {
		t.Errorf("Expected Kind to be 'Cluster', got '%s'", cluster.Kind)
	}
}

func TestDefaultClusterWithEmptyName(t *testing.T) {
	cluster := NewDefaultCluster("")
	
	if cluster.Metadata.Name != "ksail-default" {
		t.Errorf("Expected default name to be 'ksail-default', got '%s'", cluster.Metadata.Name)
	}
}