package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
)

func TestSaveAndLoadClusterSpec(t *testing.T) {
	t.Parallel()

	spec := &v1alpha1.ClusterSpec{
		Distribution:  v1alpha1.DistributionK3s,
		Provider:      v1alpha1.ProviderDocker,
		CNI:           v1alpha1.CNICalico,
		CSI:           v1alpha1.CSIDisabled,
		MetricsServer: v1alpha1.MetricsServerDisabled,
		LoadBalancer:  v1alpha1.LoadBalancerDisabled,
		CertManager:   v1alpha1.CertManagerDisabled,
		PolicyEngine:  v1alpha1.PolicyEngineGatekeeper,
		GitOpsEngine:  v1alpha1.GitOpsEngineArgoCD,
		LocalRegistry: v1alpha1.LocalRegistry{Registry: "localhost:5050"},
	}

	clusterName := "test-save-load-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	err := state.SaveClusterSpec(clusterName, spec)
	if err != nil {
		t.Fatalf("SaveClusterSpec failed: %v", err)
	}

	loaded, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		t.Fatalf("LoadClusterSpec failed: %v", err)
	}

	assertSpecFieldsMatch(t, loaded, spec)
}

// assertSpecFieldsMatch verifies all fields in loaded match the original spec.
func assertSpecFieldsMatch(t *testing.T, loaded, spec *v1alpha1.ClusterSpec) {
	t.Helper()

	if loaded.Distribution != spec.Distribution {
		t.Errorf("Distribution: got %s, want %s", loaded.Distribution, spec.Distribution)
	}

	if loaded.CNI != spec.CNI {
		t.Errorf("CNI: got %s, want %s", loaded.CNI, spec.CNI)
	}

	if loaded.MetricsServer != spec.MetricsServer {
		t.Errorf("MetricsServer: got %s, want %s", loaded.MetricsServer, spec.MetricsServer)
	}

	if loaded.CSI != spec.CSI {
		t.Errorf("CSI: got %s, want %s", loaded.CSI, spec.CSI)
	}

	if loaded.LoadBalancer != spec.LoadBalancer {
		t.Errorf("LoadBalancer: got %s, want %s", loaded.LoadBalancer, spec.LoadBalancer)
	}

	if loaded.PolicyEngine != spec.PolicyEngine {
		t.Errorf("PolicyEngine: got %s, want %s", loaded.PolicyEngine, spec.PolicyEngine)
	}

	if loaded.GitOpsEngine != spec.GitOpsEngine {
		t.Errorf("GitOpsEngine: got %s, want %s", loaded.GitOpsEngine, spec.GitOpsEngine)
	}

	if loaded.LocalRegistry.Registry != spec.LocalRegistry.Registry {
		t.Errorf("LocalRegistry.Registry: got %s, want %s",
			loaded.LocalRegistry.Registry, spec.LocalRegistry.Registry)
	}
}

func TestLoadClusterSpec_NotFound(t *testing.T) {
	t.Parallel()

	_, err := state.LoadClusterSpec("nonexistent-cluster-" + t.Name())
	if err == nil {
		t.Fatal("expected error for nonexistent cluster, got nil")
	}
}

func TestDeleteClusterState(t *testing.T) {
	t.Parallel()

	clusterName := "test-delete-" + t.Name()

	spec := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		Provider:     v1alpha1.ProviderDocker,
	}

	err := state.SaveClusterSpec(clusterName, spec)
	if err != nil {
		t.Fatalf("SaveClusterSpec failed: %v", err)
	}

	err = state.DeleteClusterState(clusterName)
	if err != nil {
		t.Fatalf("DeleteClusterState failed: %v", err)
	}

	_, err = state.LoadClusterSpec(clusterName)
	if err == nil {
		t.Fatal("expected error loading deleted state, got nil")
	}
}

func TestDeleteClusterState_Idempotent(t *testing.T) {
	t.Parallel()

	err := state.DeleteClusterState("nonexistent-cluster-" + t.Name())
	if err != nil {
		t.Fatalf("deleting nonexistent state should not error, got: %v", err)
	}
}

func TestSaveClusterSpec_OverwritesExisting(t *testing.T) {
	t.Parallel()

	clusterName := "test-overwrite-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	original := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		CNI:          v1alpha1.CNIDefault,
	}

	err := state.SaveClusterSpec(clusterName, original)
	if err != nil {
		t.Fatalf("first SaveClusterSpec failed: %v", err)
	}

	updated := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		CNI:          v1alpha1.CNICalico,
	}

	err = state.SaveClusterSpec(clusterName, updated)
	if err != nil {
		t.Fatalf("second SaveClusterSpec failed: %v", err)
	}

	loaded, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		t.Fatalf("LoadClusterSpec failed: %v", err)
	}

	if loaded.CNI != v1alpha1.CNICalico {
		t.Errorf("expected overwritten CNI %s, got %s", v1alpha1.CNICalico, loaded.CNI)
	}
}

func TestSaveClusterSpec_CreatesDirectories(t *testing.T) {
	t.Parallel()

	clusterName := "test-create-dirs-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	spec := &v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVanilla}

	err := state.SaveClusterSpec(clusterName, spec)
	if err != nil {
		t.Fatalf("SaveClusterSpec failed: %v", err)
	}

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".ksail", "clusters", clusterName, "spec.json")

	_, statErr := os.Stat(statePath)
	if os.IsNotExist(statErr) {
		t.Fatalf("state file not created at %s", statePath)
	}
}
