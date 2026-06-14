package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
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

// TestSaveClusterSpec_RedactsRegistryCredentials is a regression test for a
// security issue where `ksail cluster update` persisted the resolved registry
// password (e.g. an expanded ${GITHUB_TOKEN} / GHCR PAT) to spec.json in
// cleartext. The persisted spec is only an update-diff baseline, never a
// credential source — registry auth is resolved at use-time from the live
// config — so the password must be masked before the spec is written to disk.
func TestSaveClusterSpec_RedactsRegistryCredentials(t *testing.T) {
	t.Parallel()

	const (
		marker      = "ghp_REGRESSIONSENTINEL000000000000"
		dirtyReg    = "ksail-bot:" + marker + "@ghcr.io/org/repo"
		redactedReg = "ksail-bot:****@ghcr.io/org/repo"
	)

	clusterName := "test-redact-creds-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	spec := &v1alpha1.ClusterSpec{
		Distribution:  v1alpha1.DistributionTalos,
		Provider:      v1alpha1.ProviderDocker,
		LocalRegistry: v1alpha1.LocalRegistry{Registry: dirtyReg},
	}

	err := state.SaveClusterSpec(clusterName, spec)
	if err != nil {
		t.Fatalf("SaveClusterSpec failed: %v", err)
	}

	raw := readPersistedSpec(t, clusterName)

	// The resolved secret must never reach disk, even transiently.
	if strings.Contains(string(raw), marker) {
		t.Fatalf("persisted spec.json leaked the resolved registry credential:\n%s", raw)
	}

	// The password must be masked, with the username/host/path left intact for
	// the diff baseline.
	if !strings.Contains(string(raw), redactedReg) {
		t.Fatalf("persisted spec.json missing redacted registry %q:\n%s", redactedReg, raw)
	}

	// The caller's in-memory spec must be untouched so live registry operations
	// can still resolve credentials after the save.
	if spec.LocalRegistry.Registry != dirtyReg {
		t.Errorf("SaveClusterSpec mutated caller's spec: got %q, want %q",
			spec.LocalRegistry.Registry, dirtyReg)
	}

	// The baseline round-trips to the redacted form.
	loaded, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		t.Fatalf("LoadClusterSpec failed: %v", err)
	}

	if loaded.LocalRegistry.Registry != redactedReg {
		t.Errorf("loaded registry: got %q, want %q", loaded.LocalRegistry.Registry, redactedReg)
	}
}

// TestSaveClusterSpec_FilePermissions verifies the state directory and file are
// created with locked-down permissions (0700 / 0600) so that even a transient
// write is never group- or world-readable.
func TestSaveClusterSpec_FilePermissions(t *testing.T) {
	t.Parallel()

	clusterName := "test-perms-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	err := state.SaveClusterSpec(
		clusterName,
		&v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVanilla},
	)
	if err != nil {
		t.Fatalf("SaveClusterSpec failed: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	clusterDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	assertMode(t, clusterDir, 0o700)
	assertMode(t, filepath.Join(clusterDir, "spec.json"), 0o600)
}

// TestSaveClusterSpec_NilSpec verifies that persisting a nil spec is a harmless
// no-op write rather than a panic. The credential sanitizer deep-copies the
// spec before masking, and a nil deep copy must not be dereferenced — this
// guards that nil branch against a regression.
func TestSaveClusterSpec_NilSpec(t *testing.T) {
	t.Parallel()

	clusterName := "test-nil-spec-" + t.Name()
	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	err := state.SaveClusterSpec(clusterName, nil)
	if err != nil {
		t.Fatalf("SaveClusterSpec(nil) should not error, got: %v", err)
	}
}

// readPersistedSpec returns the raw on-disk spec.json bytes for a cluster so
// tests can assert on exactly what was written.
func readPersistedSpec(t *testing.T, clusterName string) []byte {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	statePath := filepath.Join(home, ".ksail", "clusters", clusterName, "spec.json")

	raw, err := os.ReadFile(statePath) //nolint:gosec // path built from home + test cluster name
	if err != nil {
		t.Fatalf("reading persisted state failed: %v", err)
	}

	return raw
}

// assertMode fails the test if the file at path does not have exactly the
// expected permission bits.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode: got %#o, want %#o", path, got, want)
	}
}
