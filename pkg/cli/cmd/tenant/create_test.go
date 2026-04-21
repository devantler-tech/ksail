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

	// --cluster-role default "edit"
	cr, err := cmd.Flags().GetString("cluster-role")
	require.NoError(t, err)
	require.Equal(t, "edit", cr)

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
