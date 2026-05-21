package tenant_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	tenantpkg "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/tenant"
	"github.com/stretchr/testify/require"
)

func TestCreateCmd_HasWritePermission(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewCreateCmd(nil)
	require.Equal(t, "write", cmd.Annotations[annotations.AnnotationPermission])
}

func TestCreateCmd_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCreateCmd_FlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewCreateCmd(nil)

	// --namespace default empty
	ns, err := cmd.Flags().GetStringSlice("namespace")
	require.NoError(t, err)
	require.Empty(t, ns)

	// --cluster-role default ["edit"]
	cr, err := cmd.Flags().GetStringSlice("cluster-role")
	require.NoError(t, err)
	require.Equal(t, []string{"edit"}, cr)

	// --output default "."
	out, err := cmd.Flags().GetString("output")
	require.NoError(t, err)
	require.Equal(t, ".", out)

	// --force default false
	force, err := cmd.Flags().GetBool("force")
	require.NoError(t, err)
	require.False(t, force)

	// --type default ""
	typeVal, err := cmd.Flags().GetString("type")
	require.NoError(t, err)
	require.Empty(t, typeVal)

	// --sync-source default "oci"
	ss, err := cmd.Flags().GetString("sync-source")
	require.NoError(t, err)
	require.Equal(t, "oci", ss)

	// --oci-path default ""
	op, err := cmd.Flags().GetString("oci-path")
	require.NoError(t, err)
	require.Empty(t, op)

	// --register default false
	reg, err := cmd.Flags().GetBool("register")
	require.NoError(t, err)
	require.False(t, reg)

	// --delivery default "commit"
	del, err := cmd.Flags().GetString("delivery")
	require.NoError(t, err)
	require.Equal(t, "commit", del)

	// --repo-visibility default "Private"
	rv, err := cmd.Flags().GetString("repo-visibility")
	require.NoError(t, err)
	require.Equal(t, "Private", rv)

	// --source-directory default "k8s"
	sd, err := cmd.Flags().GetString("source-directory")
	require.NoError(t, err)
	require.Equal(t, "k8s", sd)
}

func TestCreateCmd_ProductionFlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewCreateCmd(nil)

	prod, err := cmd.Flags().GetBool("production")
	require.NoError(t, err)
	require.False(t, prod)

	ps, err := cmd.Flags().GetString("pod-security")
	require.NoError(t, err)
	require.Empty(t, ps)

	qc, err := cmd.Flags().GetString("quota-cpu")
	require.NoError(t, err)
	require.Equal(t, "4", qc)

	qm, err := cmd.Flags().GetString("quota-memory")
	require.NoError(t, err)
	require.Equal(t, "8Gi", qm)

	ft, err := cmd.Flags().GetString("flux-timeout")
	require.NoError(t, err)
	require.Empty(t, ft)

	for _, name := range []string{
		"with-network-policy", "with-quota", "with-limit-range",
		"disable-token-automount", "flux-wait", "flux-decryption",
	} {
		val, err := cmd.Flags().GetBool(name)
		require.NoError(t, err)
		require.False(t, val, "%s should default to false", name)
	}
}

func TestCreateCmd_Production(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"prod-tenant", "--type", "kubectl", "--production", "--output", outDir})

	err := cmd.Execute()
	require.NoError(t, err)

	tenantDir := filepath.Join(outDir, "prod-tenant")
	for _, filename := range []string{"networkpolicy.yaml", "resourcequota.yaml", "limitrange.yaml"} {
		_, statErr := os.Stat(filepath.Join(tenantDir, filename))
		require.NoError(t, statErr, "expected %s to exist", filename)
	}

	nsContent, err := os.ReadFile( //nolint:gosec // test path
		filepath.Join(tenantDir, "namespace.yaml"),
	)
	require.NoError(t, err)
	require.Contains(t, string(nsContent), "pod-security.kubernetes.io/enforce: baseline")

	saContent, err := os.ReadFile( //nolint:gosec // test path
		filepath.Join(tenantDir, "serviceaccount.yaml"),
	)
	require.NoError(t, err)
	require.Contains(t, string(saContent), "automountServiceAccountToken: false")
}

func TestCreateCmd_FluxTimeoutImpliesWait(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"timeout-tenant", "--type", "flux",
		"--registry", "oci://ghcr.io", "--tenant-repo", "owner/repo",
		"--flux-timeout", "10m", "--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)

	syncContent, err := os.ReadFile( //nolint:gosec // test path
		filepath.Join(outDir, "timeout-tenant", "sync.yaml"),
	)
	require.NoError(t, err)
	require.Contains(t, string(syncContent), "wait: true")
	require.Contains(t, string(syncContent), "timeout: 10m")
}

//nolint:paralleltest // uses t.Chdir
func TestCreateCmd_CiliumNetworkPolicyFromConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	require.NoError(t, os.WriteFile("kind.yaml",
		[]byte("apiVersion: kind.x-k8s.io/v1alpha4\nkind: Cluster\n"+
			"networking:\n  disableDefaultCNI: true\n"), 0o600))

	ksailYAML := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n  cluster:\n    distribution: Vanilla\n" +
		"    distributionConfig: kind.yaml\n    cni: Cilium\n"
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(ksailYAML), 0o600))

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(
		[]string{"cil-tenant", "--type", "kubectl", "--with-network-policy", "--output", dir},
	)

	err := cmd.Execute()
	require.NoError(t, err)

	npContent, err := os.ReadFile( //nolint:gosec // test path
		filepath.Join(dir, "cil-tenant", "networkpolicy.yaml"),
	)
	require.NoError(t, err)
	require.Contains(t, string(npContent), "kind: CiliumNetworkPolicy")
}

func TestCreateCmd_KubectlType(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"my-tenant", "--type", "kubectl", "--output", outDir})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), `Tenant "my-tenant" created successfully`)

	// Verify tenant directory was created.
	tenantDir := filepath.Join(outDir, "my-tenant")
	info, err := os.Stat(tenantDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Verify RBAC files exist.
	for _, filename := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, filename))
		require.NoError(t, err, "expected %s to exist", filename)
	}
}

func TestCreateCmd_FluxType(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"my-flux-tenant", "--type", "flux",
		"--registry", "oci://ghcr.io/test",
		"--tenant-repo", "owner/repo", "--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), `Tenant "my-flux-tenant" created successfully`)

	// Verify tenant directory was created.
	tenantDir := filepath.Join(outDir, "my-flux-tenant")
	info, err := os.Stat(tenantDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	// Verify kustomization.yaml exists.
	_, err = os.Stat(filepath.Join(tenantDir, "kustomization.yaml"))
	require.NoError(t, err)
}

func TestCreateCmd_FluxTypeWithOCIPath(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"oci-path-tenant", "--type", "flux",
		"--registry", "oci://ghcr.io",
		"--tenant-repo", "owner/repo",
		"--oci-path", "deploy",
		"--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), `Tenant "oci-path-tenant" created successfully`)

	// Verify sync.yaml contains the OCI path suffix.
	tenantDir := filepath.Join(outDir, "oci-path-tenant")
	syncContent, err := os.ReadFile( //nolint:gosec // test path
		filepath.Join(tenantDir, "sync.yaml"),
	)
	require.NoError(t, err)
	require.Contains(t, string(syncContent), "url: oci://ghcr.io/owner/repo/deploy")
}

func TestCreateCmd_ArgoCDType(t *testing.T) {
	// Not parallel: uses t.Setenv to isolate from real gh credentials.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_CONFIG_DIR", t.TempDir())

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"my-argocd-tenant",
		"--type", "argocd",
		"--git-provider", "github",
		"--tenant-repo", "owner/repo",
		"--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), `Tenant "my-argocd-tenant" created successfully`)

	// Verify tenant directory was created.
	tenantDir := filepath.Join(outDir, "my-argocd-tenant")
	info, err := os.Stat(tenantDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestCreateCmd_InvalidType(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"bad-tenant", "--type", "invalid", "--output", outDir})

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid tenant type")
}

func TestCreateCmd_DeliveryPRAccepted(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"pr-tenant", "--type", "kubectl", "--delivery", "pr",
		"--git-provider", "github", "--output", outDir,
	})

	err := cmd.Execute()
	// PR delivery is accepted but may fail at runtime (no git token) —
	// the important thing is that it no longer returns "not implemented".
	if err != nil {
		require.NotContains(t, err.Error(), "not yet implemented")
	}
}

func TestCreateCmd_DeliveryPRRequiresGitProvider(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"pr-tenant", "--type", "kubectl", "--delivery", "pr", "--output", outDir})

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "--git-provider is required")
}

func TestCreateCmd_InvalidDelivery(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(
		[]string{"bad-delivery", "--type", "kubectl", "--delivery", "email", "--output", outDir},
	)

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid --delivery value")
}

func TestCreateCmd_WithRegister(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	// Create a parent kustomization.yaml for registration.
	kPath := filepath.Join(outDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kPath, []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`), 0o600))

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(
		[]string{"registered-tenant", "--type", "kubectl", "--output", outDir, "--register"},
	)

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify the tenant was registered.
	data, err := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, err)
	require.Contains(t, string(data), "registered-tenant")
}

func TestCreateCmd_MultiNamespace(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"multi-ns",
		"--type", "kubectl",
		"--namespace", "ns1",
		"--namespace", "ns2",
		"--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify multi-namespace RBAC.
	tenantDir := filepath.Join(outDir, "multi-ns")
	nsPath := filepath.Join(tenantDir, "namespace.yaml")
	nsContent, err := os.ReadFile(nsPath) //nolint:gosec // test path
	require.NoError(t, err)
	require.Contains(t, string(nsContent), "ns1")
	require.Contains(t, string(nsContent), "ns2")
}

func TestCreateCmd_CustomClusterRole(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"custom-role",
		"--type", "kubectl",
		"--cluster-role", "cluster-admin",
		"--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify ClusterRole.
	rbPath := filepath.Join(outDir, "custom-role", "rolebinding.yaml")
	rbContent, err := os.ReadFile(rbPath) //nolint:gosec // test
	require.NoError(t, err)
	require.Contains(t, string(rbContent), "cluster-admin")
}

//nolint:paralleltest // uses t.Chdir
func TestCreateCmd_NoTypeNoConfig(t *testing.T) {
	outDir := t.TempDir()
	// chdir to a dir without ksail.yaml
	t.Chdir(outDir)

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"orphan-tenant", "--output", outDir})

	err := cmd.Execute()
	// No ksail.yaml and no --type → should error asking for --type.
	require.Error(t, err)
	require.ErrorContains(t, err, "no --type specified")
}

func TestCreateCmd_CustomSourceDirectory(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := tenantpkg.NewCreateCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"custom-dir-tenant", "--type", "flux",
		"--registry", "oci://ghcr.io/test",
		"--tenant-repo", "owner/repo",
		"--source-directory", "deploy",
		"--output", outDir,
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), `Tenant "custom-dir-tenant" created successfully`)

	// Verify sync.yaml references the custom directory.
	tenantDir := filepath.Join(outDir, "custom-dir-tenant")
	syncPath := filepath.Join(tenantDir, "sync.yaml")
	syncContent, err := os.ReadFile(syncPath) //nolint:gosec // test path
	require.NoError(t, err)
	require.Contains(t, string(syncContent), "path: ./deploy")
	require.NotContains(t, string(syncContent), "path: ./k8s")
}
