package talosgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewGenerator()
	require.NotNil(t, gen)
}

func TestGenerator_Generate_CreatesDirectoryStructure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// With workers > 0, no patches are generated, so cluster/, control-planes/, and
	// workers/ all receive a .gitkeep.
	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify directory structure - cluster/, control-planes/, and workers/ should have .gitkeep
	gitkeepPaths := []string{
		filepath.Join(tempDir, "talos", "cluster", ".gitkeep"),
		filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"),
		filepath.Join(tempDir, "talos", "workers", ".gitkeep"),
	}

	for _, path := range gitkeepPaths {
		info, err := os.Stat(path)
		require.NoError(t, err, "expected path to exist: %s", path)
		assert.False(t, info.IsDir(), "expected file, got directory: %s", path)
	}
}

func TestGenerator_Generate_NilConfig(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewGenerator()
	opts := yamlgenerator.Options{
		Output: t.TempDir(),
	}

	result, err := gen.Generate(nil, opts)
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "config is required")
}

func TestGenerator_Generate_DefaultPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "", // Empty should default to "talos"
		WorkerNodes: 1,  // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify the default directory was created with .gitkeep
	_, err = os.Stat(filepath.Join(tempDir, "talos", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

func TestGenerator_Generate_CustomPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "custom-patches",
		WorkerNodes: 1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "custom-patches"), result)

	// Verify the custom directory was created with .gitkeep
	_, err = os.Stat(filepath.Join(tempDir, "custom-patches", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

//nolint:paralleltest // t.Chdir cannot be used with t.Parallel
func TestGenerator_Generate_DefaultOutputDir(t *testing.T) {
	// Create a temporary directory and change to it
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: "", // Empty should default to "."
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(".", "talos"), result)

	// Verify directory was created in current directory with .gitkeep
	_, err = os.Stat(filepath.Join(".", "talos", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

func TestGenerator_Generate_SkipsExistingWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch, so .gitkeep should be preserved
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  false,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify existing file content was preserved
	content, err := os.ReadFile(gitkeepPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Equal(t, "existing content", string(content))
}

func TestGenerator_Generate_OverwritesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch, so .gitkeep should be written
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify file was overwritten (now empty)
	content, err := os.ReadFile(gitkeepPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Empty(t, string(content))
}

func TestGenerator_Generate_DisableDefaultCNI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		DisableDefaultCNI: true,
		WorkerNodes:       1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify disable-default-cni.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "network:")
	assert.Contains(t, string(content), "cni:")
	assert.Contains(t, string(content), "name: none")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")

	// Verify .gitkeep WAS created in control-planes/ (no patches there)
	_, err = os.Stat(filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in control-planes/")

	// Verify .gitkeep WAS created in workers/ (no worker patches are generated there)
	_, err = os.Stat(filepath.Join(tempDir, "talos", "workers", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in workers/")
}

func TestGenerator_Generate_NoDisableCNIPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		DisableDefaultCNI: false,
		WorkerNodes:       1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify disable-default-cni.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected disable-default-cni.yaml to not exist")

	// Verify .gitkeep WAS created since no patches in cluster/
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	require.NoError(t, err, "expected .gitkeep in cluster/ when no patches generated")
}

func TestGenerator_Generate_AllowSchedulingOnControlPlanes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0, // Zero workers triggers allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify allow-scheduling-on-control-planes.yaml was created
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	patchPath := filepath.Join(clusterDir, "allow-scheduling-on-control-planes.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "allowSchedulingOnControlPlanes: true")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

func TestGenerator_Generate_WorkerRoleLabel_NotGeneratedWithWorkers(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 3,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// ksail no longer scaffolds a worker role label patch. Kubernetes 1.33+ rejects
	// node-role.kubernetes.io/* labels passed via kubelet --node-labels, which prevents
	// worker kubelets from starting. The label is cosmetic, so it is dropped entirely.
	patchPath := filepath.Join(tempDir, "talos", "workers", "worker-role-label.yaml")
	_, err = os.Stat(patchPath)
	assert.True(
		t,
		os.IsNotExist(err),
		"expected worker-role-label.yaml to not be generated when workers are configured",
	)
}

func TestGenerator_Generate_WorkerRoleLabel_NotGeneratedWithoutWorkers(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify worker-role-label.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "workers", "worker-role-label.yaml")
	_, err = os.Stat(patchPath)
	assert.True(
		t,
		os.IsNotExist(err),
		"expected worker-role-label.yaml to not exist when WorkerNodes == 0",
	)
}

func TestGenerator_Generate_MirrorRegistries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		MirrorRegistries: []string{
			"docker.io=https://registry-1.docker.io",
			"gcr.io=https://gcr.io",
		},
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify mirror-registries.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "machine:")
	assert.Contains(t, string(content), "registries:")
	assert.Contains(t, string(content), "mirrors:")
	assert.Contains(t, string(content), "docker.io:")
	assert.Contains(t, string(content), "gcr.io:")
	assert.Contains(t, string(content), "endpoints:")
	assert.Contains(t, string(content), "http://")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

func TestGenerator_Generate_EmptyMirrorRegistries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{}, // Empty array should not create patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify mirror-registries.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected mirror-registries.yaml to not exist")
}

func TestGenerator_Generate_KubeletCertRotation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify kubelet-cert-rotation.yaml was created
	certRotationPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-cert-rotation.yaml")
	content, err := os.ReadFile(certRotationPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "machine:")
	assert.Contains(t, string(content), "kubelet:")
	assert.Contains(t, string(content), "extraArgs:")
	assert.Contains(t, string(content), "rotate-server-certificates")
	assert.Contains(t, string(content), `"true"`)

	// Verify kubelet-csr-approver.yaml was also created
	csrApproverPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-csr-approver.yaml")
	csrContent, err := os.ReadFile(csrApproverPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(csrContent), "cluster:")
	assert.Contains(t, string(csrContent), "inlineManifests:")
	assert.Contains(t, string(csrContent), "kubelet-serving-cert-approver")
}

func TestGenerator_Generate_NoKubeletCertRotationPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: false,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify kubelet-cert-rotation.yaml was NOT created
	certRotationPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-cert-rotation.yaml")
	_, err = os.Stat(certRotationPath)
	assert.True(t, os.IsNotExist(err), "expected kubelet-cert-rotation.yaml to not exist")

	// Verify kubelet-csr-approver.yaml was also NOT created
	csrApproverPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-csr-approver.yaml")
	_, err = os.Stat(csrApproverPath)
	assert.True(t, os.IsNotExist(err), "expected kubelet-csr-approver.yaml to not exist")
}

func TestGenerator_Generate_ClusterName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-custom-cluster",
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify cluster-name.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "clusterName: my-custom-cluster")
}

func TestGenerator_Generate_NoClusterNamePatchWhenEmpty(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "", // Empty should not create patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify cluster-name.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected cluster-name.yaml to not exist")
}

func TestGenerator_Generate_AllPatchesCombined(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               0, // Triggers allow-scheduling patch
		MirrorRegistries:          []string{"docker.io=https://registry-1.docker.io"},
		DisableDefaultCNI:         true,
		EnableKubeletCertRotation: true,
		ClusterName:               "test-cluster",
		EnableImageVerification:   true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify all patches were created
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	// Check allow-scheduling patch
	_, err = os.Stat(filepath.Join(clusterDir, "allow-scheduling-on-control-planes.yaml"))
	require.NoError(t, err, "expected allow-scheduling-on-control-planes.yaml")

	// Check mirror registries patch
	_, err = os.Stat(filepath.Join(clusterDir, "mirror-registries.yaml"))
	require.NoError(t, err, "expected mirror-registries.yaml")

	// Check disable CNI patch
	_, err = os.Stat(filepath.Join(clusterDir, "disable-default-cni.yaml"))
	require.NoError(t, err, "expected disable-default-cni.yaml")

	// Check kubelet cert rotation patch
	_, err = os.Stat(filepath.Join(clusterDir, "kubelet-cert-rotation.yaml"))
	require.NoError(t, err, "expected kubelet-cert-rotation.yaml")

	// Check kubelet CSR approver patch
	_, err = os.Stat(filepath.Join(clusterDir, "kubelet-csr-approver.yaml"))
	require.NoError(t, err, "expected kubelet-csr-approver.yaml")

	// Check cluster name patch
	_, err = os.Stat(filepath.Join(clusterDir, "cluster-name.yaml"))
	require.NoError(t, err, "expected cluster-name.yaml")

	// Check image verification config (in cluster/ alongside other config documents)
	_, err = os.Stat(filepath.Join(clusterDir, "image-verification.yaml"))
	require.NoError(t, err, "expected image-verification.yaml")

	// Verify .gitkeep was NOT created in cluster/ since we have patches there
	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")

	// Verify .gitkeep WAS created in other directories
	_, err = os.Stat(filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in control-planes/")
	_, err = os.Stat(filepath.Join(tempDir, "talos", "workers", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in workers/")
}

func TestGenerator_Generate_SkipsExistingPatchesWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing patch with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	patchPath := filepath.Join(clusterDir, "disable-default-cni.yaml")
	err = os.WriteFile(patchPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  false,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify existing file content was preserved
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Equal(t, "existing content", string(content))
}

func TestGenerator_Generate_OverwritesExistingPatchesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing patch with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	patchPath := filepath.Join(clusterDir, "disable-default-cni.yaml")
	err = os.WriteFile(patchPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify file was overwritten with new content
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "cni:")
	assert.Contains(t, string(content), "name: none")
}

func TestGenerator_Generate_Talos14KubernetesDocuments(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()
	caContent := "-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----\n"
	caPath := filepath.Join(tempDir, "oidc-ca.crt")
	require.NoError(t, os.WriteFile(caPath, []byte(caContent), 0o600))

	config := &talosgenerator.Config{
		PatchesDir:                    "talos",
		WorkerNodes:                   1,
		DisableDefaultCNI:             true,
		EnableOIDC:                    true,
		OIDCIssuerURL:                 "https://dex.example.com",
		OIDCClientID:                  "ksail",
		OIDCUsernameClaim:             "email",
		OIDCUsernamePrefix:            "oidc:",
		OIDCGroupsClaim:               "groups",
		OIDCGroupsPrefix:              "oidc:",
		OIDCCAFile:                    caPath,
		MultiDocumentKubernetesConfig: true,
	}

	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir})
	require.NoError(t, err)

	cniContent, err := os.ReadFile( //nolint:gosec // Test file path is safe
		filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml"),
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		"apiVersion: v1alpha1\nkind: KubeFlannelCNIConfig\n$patch: delete\n",
		string(cniContent),
	)

	oidcContent, err := os.ReadFile( //nolint:gosec // Test file path is safe
		filepath.Join(tempDir, "talos", "cluster", "oidc.yaml"),
	)
	require.NoError(t, err)
	assert.Contains(t, string(oidcContent), "kind: KubeAuthenticationConfig")
	assert.Contains(t, string(oidcContent), "url: \"https://dex.example.com\"")
	assert.Contains(t, string(oidcContent), "audiences:\n          - \"ksail\"")
	assert.Contains(t, string(oidcContent), "claim: \"email\"")
	assert.Contains(t, string(oidcContent), "prefix: \"oidc:\"")
	assert.Contains(t, string(oidcContent), "certificateAuthority: |")
	assert.Contains(t, string(oidcContent), "          test-ca")
	assert.NotContains(t, string(oidcContent), "cluster:\n  apiServer:")
	assert.NotContains(t, string(oidcContent), "machine:\n  files:")

	assertTalos14OIDCConfigLoads(t, tempDir, caContent)
}

func assertTalos14OIDCConfigLoads(t *testing.T, tempDir, caContent string) {
	t.Helper()

	manager := talosconfigmanager.
		NewConfigManager(filepath.Join(tempDir, "talos"), "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)
	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	authConfig := configs.ControlPlane().K8sAuthenticationConfig().Configuration()
	jwt, found := authConfig["jwt"].([]any)
	require.True(t, found)
	require.Len(t, jwt, 1)
	authenticator, found := jwt[0].(map[string]any)
	require.True(t, found)
	issuer, found := authenticator["issuer"].(map[string]any)
	require.True(t, found)
	assert.Equal(t, "https://dex.example.com", issuer["url"])
	assert.Equal(t, caContent, issuer["certificateAuthority"])
}

func TestGenerator_Generate_ImageVerification(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify image-verification.yaml was created in cluster/ (loaded as config document)
	patchPath := filepath.Join(tempDir, "talos", "cluster", "image-verification.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "apiVersion: v1alpha1")
	assert.Contains(t, string(content), "kind: ImageVerificationConfig")
	assert.Contains(t, string(content), "rules:")
	assert.Contains(t, string(content), `image: "*"`)
	assert.Contains(t, string(content), "skip: true")
	// Verify commented examples are included
	assert.Contains(t, string(content), "keyless:")
	assert.Contains(t, string(content), "publicKey:")
	assert.Contains(t, string(content), "deny: true")

	// Verify .gitkeep was NOT created in cluster/ since we have a config document there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

func TestGenerator_Generate_NoImageVerificationPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: false,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify image-verification.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "image-verification.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected image-verification.yaml to not exist")
}

// TestGenerator_Generate_ExternalCloudProvider tests that external-cloud-provider.yaml
// is generated when EnableExternalCloudProvider is true.
func TestGenerator_Generate_ExternalCloudProvider(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                  "talos",
		EnableExternalCloudProvider: true,
		WorkerNodes:                 1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify external-cloud-provider.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "external-cloud-provider.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "externalCloudProvider:")
	assert.Contains(t, string(content), "enabled: true")
	assert.Contains(t, string(content), "cloud-provider: external")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

// TestGenerator_Generate_ExternalCloudProviderDisabled tests that external-cloud-provider.yaml
// is NOT generated when EnableExternalCloudProvider is false.
func TestGenerator_Generate_ExternalCloudProviderDisabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                  "talos",
		WorkerNodes:                 1,
		EnableExternalCloudProvider: false,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify external-cloud-provider.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "external-cloud-provider.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected external-cloud-provider.yaml to not exist")
}

// TestGenerator_Generate_IngressFirewall tests that ingress firewall documents
// are generated when EnableIngressFirewall is true.
func TestGenerator_Generate_IngressFirewall(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:            "talos",
		EnableIngressFirewall: true,
		NetworkCIDR:           "10.0.0.0/16",
		CNIPort:               8472,
		WorkerNodes:           1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify ingress-firewall-default-action.yaml was created in cluster/
	defaultActionPath := filepath.Join(
		tempDir,
		"talos",
		"cluster",
		"ingress-firewall-default-action.yaml",
	)
	defaultActionContent, err := os.ReadFile( //nolint:gosec // Test file path is safe
		defaultActionPath,
	)
	require.NoError(t, err)
	assert.Contains(t, string(defaultActionContent), "kind: NetworkDefaultActionConfig")
	assert.Contains(t, string(defaultActionContent), "ingress: block")

	// Verify control-plane ingress-firewall-rules.yaml was created
	cpRulesPath := filepath.Join(tempDir, "talos", "control-planes", "ingress-firewall-rules.yaml")
	cpContent, err := os.ReadFile(cpRulesPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)

	cpStr := string(cpContent)
	assert.Contains(t, cpStr, "name: etcd")
	assert.Contains(t, cpStr, "name: trustd")
	assert.Contains(t, cpStr, "name: kubernetes-api")
	assert.Contains(t, cpStr, "name: apid")
	assert.Contains(t, cpStr, "name: kubelet")
	assert.Contains(t, cpStr, "name: cni-vxlan")
	assert.Contains(t, cpStr, "8472")
	assert.Contains(t, cpStr, "10.0.0.0/16")

	// Verify worker ingress-firewall-rules.yaml was created
	workerRulesPath := filepath.Join(tempDir, "talos", "workers", "ingress-firewall-rules.yaml")
	workerContent, err := os.ReadFile(workerRulesPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)

	workerStr := string(workerContent)
	assert.Contains(t, workerStr, "name: kubelet")
	assert.Contains(t, workerStr, "name: apid")
	assert.Contains(t, workerStr, "name: cni-vxlan")
	assert.NotContains(t, workerStr, "name: etcd")
	assert.NotContains(t, workerStr, "name: trustd")
	assert.NotContains(t, workerStr, "name: kubernetes-api")
	assert.Contains(t, workerStr, "10.0.0.0/16")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

// TestGenerator_Generate_IngressFirewallDisabled tests that ingress firewall documents
// are NOT generated when EnableIngressFirewall is false.
func TestGenerator_Generate_IngressFirewallDisabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:            "talos",
		WorkerNodes:           1,
		EnableIngressFirewall: false,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify none of the ingress firewall files were created
	defaultActionPath := filepath.Join(
		tempDir,
		"talos",
		"cluster",
		"ingress-firewall-default-action.yaml",
	)
	_, err = os.Stat(defaultActionPath)
	assert.True(t, os.IsNotExist(err), "expected ingress-firewall-default-action.yaml to not exist")

	cpRulesPath := filepath.Join(tempDir, "talos", "control-planes", "ingress-firewall-rules.yaml")
	_, err = os.Stat(cpRulesPath)
	assert.True(
		t,
		os.IsNotExist(err),
		"expected control-planes/ingress-firewall-rules.yaml to not exist",
	)

	workerRulesPath := filepath.Join(tempDir, "talos", "workers", "ingress-firewall-rules.yaml")
	_, err = os.Stat(workerRulesPath)
	assert.True(t, os.IsNotExist(err), "expected workers/ingress-firewall-rules.yaml to not exist")
}

// TestGenerator_Generate_IngressFirewallFlannelPort tests that the CNI VXLAN port
// is correctly set when using Flannel's default port (4789).
func TestGenerator_Generate_IngressFirewallFlannelPort(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:            "talos",
		EnableIngressFirewall: true,
		NetworkCIDR:           "10.0.0.0/16",
		CNIPort:               4789,
		WorkerNodes:           1,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify CP rules contain port 4789
	cpRulesPath := filepath.Join(tempDir, "talos", "control-planes", "ingress-firewall-rules.yaml")
	cpContent, err := os.ReadFile(cpRulesPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(cpContent), "4789")

	// Verify worker rules contain port 4789
	workerRulesPath := filepath.Join(tempDir, "talos", "workers", "ingress-firewall-rules.yaml")
	workerContent, err := os.ReadFile(workerRulesPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(workerContent), "4789")
}

// TestIngressFirewallCPRulesYAML tests the exported CP rules YAML function directly.
func TestIngressFirewallCPRulesYAML(t *testing.T) {
	t.Parallel()

	result := talosgenerator.IngressFirewallCPRulesYAML("10.0.0.0/16", 8472, nil)

	// Verify all expected rule names are present
	assert.Contains(t, result, "name: kubelet")
	assert.Contains(t, result, "name: apid")
	assert.Contains(t, result, "name: kubernetes-api")
	assert.Contains(t, result, "name: trustd")
	assert.Contains(t, result, "name: etcd")
	assert.Contains(t, result, "name: cni-vxlan")

	// Verify network CIDR is injected in restricted rules
	assert.Contains(t, result, "subnet: 10.0.0.0/16")

	// Verify CNI port is injected
	assert.Contains(t, result, "8472")

	// Verify apid and kubernetes-api are open to all (0.0.0.0/0 and ::/0)
	assert.Contains(t, result, "subnet: 0.0.0.0/0")
	assert.Contains(t, result, "subnet: ::/0")

	// Verify correct protocols
	assert.Contains(t, result, "protocol: tcp")
	assert.Contains(t, result, "protocol: udp")

	// Verify known ports
	assert.Contains(t, result, "10250")     // kubelet
	assert.Contains(t, result, "50000")     // apid
	assert.Contains(t, result, "6443")      // kubernetes-api
	assert.Contains(t, result, "50001")     // trustd
	assert.Contains(t, result, "2379-2380") // etcd
}

// TestIngressFirewallCPRulesYAMLWithAllowedCIDRs tests that allowedCIDRs restricts
// apid and kubernetes-api ingress subnets.
func TestIngressFirewallCPRulesYAMLWithAllowedCIDRs(t *testing.T) {
	t.Parallel()

	allowedCIDRs := []string{"203.0.113.0/24", "198.51.100.0/24"}
	result := talosgenerator.IngressFirewallCPRulesYAML("10.0.0.0/16", 8472, allowedCIDRs)

	// Verify apid and kubernetes-api use the provided CIDRs
	assert.Contains(t, result, "subnet: 203.0.113.0/24")
	assert.Contains(t, result, "subnet: 198.51.100.0/24")

	// Verify open-to-all CIDRs are NOT present for API rules
	assert.NotContains(t, result, "subnet: 0.0.0.0/0")
	assert.NotContains(t, result, "subnet: ::/0")

	// Verify private-network-restricted rules still use the network CIDR
	assert.Contains(t, result, "subnet: 10.0.0.0/16")

	// Verify all expected rule names are still present
	assert.Contains(t, result, "name: kubelet")
	assert.Contains(t, result, "name: apid")
	assert.Contains(t, result, "name: kubernetes-api")
	assert.Contains(t, result, "name: trustd")
	assert.Contains(t, result, "name: etcd")
	assert.Contains(t, result, "name: cni-vxlan")
}

// TestIngressFirewallCPRulesYAMLNormalizesCIDRs verifies that CIDRs with host bits set
// are normalized to network addresses (consistent with Hetzner firewall path).
func TestIngressFirewallCPRulesYAMLNormalizesCIDRs(t *testing.T) {
	t.Parallel()

	// "203.0.113.5/24" has host bits; should be normalized to "203.0.113.0/24"
	allowedCIDRs := []string{"203.0.113.5/24", "2001:db8::1/32"}
	result := talosgenerator.IngressFirewallCPRulesYAML("10.0.0.0/16", 8472, allowedCIDRs)

	assert.Contains(t, result, "subnet: 203.0.113.0/24")
	assert.NotContains(t, result, "subnet: 203.0.113.5/24")
	assert.Contains(t, result, "subnet: 2001:db8::/32")
	assert.NotContains(t, result, "subnet: 2001:db8::1/32")
}

// TestIngressFirewallWorkerRulesYAML tests the exported worker rules YAML function directly.
func TestIngressFirewallWorkerRulesYAML(t *testing.T) {
	t.Parallel()

	result := talosgenerator.IngressFirewallWorkerRulesYAML("192.168.0.0/24", 4789)

	// Verify expected rule names are present
	assert.Contains(t, result, "name: kubelet")
	assert.Contains(t, result, "name: apid")
	assert.Contains(t, result, "name: cni-vxlan")

	// Verify CP-only rules are NOT present
	assert.NotContains(t, result, "name: etcd")
	assert.NotContains(t, result, "name: trustd")
	assert.NotContains(t, result, "name: kubernetes-api")

	// Verify network CIDR is injected
	assert.Contains(t, result, "subnet: 192.168.0.0/24")

	// Verify CNI port is injected
	assert.Contains(t, result, "4789")

	// Verify worker rules are restricted to the network CIDR (no 0.0.0.0/0)
	assert.NotContains(t, result, "subnet: 0.0.0.0/0")
	assert.NotContains(t, result, "subnet: ::/0")

	// Verify known ports
	assert.Contains(t, result, "10250") // kubelet
	assert.Contains(t, result, "50000") // apid
}
