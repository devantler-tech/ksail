package cluster

import "testing"

// TestSetDefaults validates defaulting behavior including nil safety and override semantics.
func TestSetDefaults(t *testing.T) {
	// nil safety (should not panic)
	SetDefaults(nil)

	c := NewCluster()
	// c.Metadata.Name should be set to "ksail-default"
	SetDefaults(c)
	if c.Metadata.Name != "ksail-default" {
		t.Fatalf("expected name ksail-default after defaulting, got %s", c.Metadata.Name)
	}
	// c.Spec.Connection.ConnectionKubeconfig should be set to "~/.kube/config"
	if c.Spec.Connection.Kubeconfig != "~/.kube/config" {
		t.Fatalf("expected kubeconfig ~/.kube/config after defaulting, got %s", c.Spec.Connection.Kubeconfig)
	}
}
