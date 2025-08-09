package cluster

import (
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestNewCluster ensures constructor sets the expected TypeMeta/ObjectMeta fields.
func TestNewCluster(t *testing.T) {
	c := NewCluster(
		WithMetadataName("test-cluster"),
		WithSpecConnectionKubeconfig("test-kubeconfig"),
		WithSpecConnectionContext("test-context"),
		WithSpecConnectionTimeout(v1.Duration{Duration: 30 * time.Second}),
	)
	if c.TypeMeta.Kind != Kind {
		t.Fatalf("expected kind %s, got %s", Kind, c.TypeMeta.Kind)
	}
	if c.TypeMeta.APIVersion != APIVersion {
		t.Fatalf("expected apiVersion %s, got %s", APIVersion, c.TypeMeta.APIVersion)
	}
	if c.Metadata.Name != "test-cluster" {
		t.Fatalf("expected name test-cluster, got %s", c.Metadata.Name)
	}
}
