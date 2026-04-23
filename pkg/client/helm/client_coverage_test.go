package helm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
)

// ---------------------------------------------------------------------------
// TemplateChart edge cases
// ---------------------------------------------------------------------------

func TestTemplateChart_NilSpec(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	_, err = client.TemplateChart(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrChartSpecRequired)
}

func TestTemplateChart_CancelledContext(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.TemplateChart(ctx, &helm.ChartSpec{
		ChartName: "test-chart",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "template chart context cancelled")
}

// ---------------------------------------------------------------------------
// UninstallRelease edge cases
// ---------------------------------------------------------------------------

func TestUninstallRelease_EmptyName(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	err = client.UninstallRelease(context.Background(), "", "default")

	require.Error(t, err)
	require.ErrorIs(t, err, helm.ErrReleaseNameRequired)
}

func TestUninstallRelease_CancelledContext(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.UninstallRelease(ctx, "my-release", "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "uninstall release context cancelled")
}

// ---------------------------------------------------------------------------
// ReleaseExists edge cases
// ---------------------------------------------------------------------------

func TestReleaseExists_EmptyName(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	exists, err := client.ReleaseExists(context.Background(), "", "default")

	require.Error(t, err)
	require.ErrorIs(t, err, helm.ErrReleaseNameRequired)
	assert.False(t, exists)
}

// ---------------------------------------------------------------------------
// ListReleases edge cases
// ---------------------------------------------------------------------------

func TestListReleases_CancelledContext(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	releases, err := client.ListReleases(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list releases context cancelled")
	assert.Nil(t, releases)
}

func TestListReleases_TemplateOnlyClient(t *testing.T) {
	t.Parallel()

	// NewTemplateOnlyClient sets actionConfig.Releases = nil, so ListReleases
	// must return the unsupported sentinel rather than panicking.
	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	_, err = client.ListReleases(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, helm.ErrListReleasesUnsupported)
}

// TestListReleases_ReturnsAllNamespaces is a regression test for the Helm v4
// AllNamespaces scoping bug: when actionConfig is initialized with a specific
// namespace (e.g., "default"), only releases from that namespace are visible.
// ListReleases must re-initialize with namespace "" to return releases from all
// namespaces — otherwise an ArgoCD release in the "argocd" namespace is missed.
//
// A minimal fake Kubernetes API server is used to satisfy the IsReachable()
// check performed by helmv4action.List.Run(), since this test exercises the
// real Client.ListReleases path (not a mock). The test cannot run in parallel
// because it sets the HELM_DRIVER env var.
func TestListReleases_ReturnsAllNamespaces(t *testing.T) {
	t.Setenv("HELM_DRIVER", "memory")

	// Fake Kubernetes API server — only /version is needed for IsReachable().
	srv := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/version" {
			resp.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(resp, `{"major":"1","minor":"29","gitVersion":"v1.29.0"}`)

			return
		}

		http.NotFound(resp, req)
	}))
	t.Cleanup(srv.Close)

	// Write a kubeconfig pointing to the fake server.
	kubeconfig := fmt.Sprintf(`apiVersion: v1
clusters:
- cluster:
    server: %s
  name: fake
contexts:
- context:
    cluster: fake
    user: fake
  name: fake
current-context: fake
users:
- name: fake
  user: {}
`, srv.URL)
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))

	settings := helmv4cli.New()
	settings.KubeConfig = kubeconfigPath
	settings.SetNamespace("default")

	cfg := new(helmv4action.Configuration)
	// Initialize at "default" namespace — simulates a client scoped to a single
	// namespace, which would miss releases in other namespaces without the fix.
	err := cfg.Init(settings.RESTClientGetter(), "default", "memory")
	require.NoError(t, err)

	// Seed a release in the "argocd" namespace. Memory.Create uses the release's
	// own Namespace field to determine where to store it.
	rel := helm.NewTestRelease(
		"argo-cd", "argocd", "argo-cd", "v2.10.0", "",
		releasecommon.StatusDeployed, 1, time.Now(),
	)
	err = cfg.Releases.Create(rel)
	require.NoError(t, err)

	// After Create the memory driver namespace is "argocd"; reset it to "default"
	// to reproduce the regression: without the re-init in ListReleases the driver
	// stays scoped to "default" and the "argocd" release would be invisible.
	memDriver, ok := cfg.Releases.Driver.(*helmv4driver.Memory)
	require.True(t, ok, "expected memory driver after HELM_DRIVER=memory init")
	memDriver.SetNamespace("default")

	client := helm.NewClientFromParts(cfg, settings)
	releases, err := client.ListReleases(context.Background())

	require.NoError(t, err)
	require.Len(t, releases, 1, "expected release from non-default namespace to be visible")
	assert.Equal(t, "argo-cd", releases[0].Name)
	assert.Equal(t, "argocd", releases[0].Namespace)
}

// ---------------------------------------------------------------------------
// InstallChart / InstallOrUpgradeChart nil spec
// ---------------------------------------------------------------------------

func TestInstallChart_NilSpec(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	_, err = client.InstallChart(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrChartSpecRequired)
}

func TestInstallOrUpgradeChart_NilSpec(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	_, err = client.InstallOrUpgradeChart(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrChartSpecRequired)
}

// ---------------------------------------------------------------------------
// Adapters coverage: install and upgrade action adapters
// ---------------------------------------------------------------------------

func TestInstallActionAdapter(t *testing.T) {
	t.Parallel()

	adapter, install := helm.NewInstallActionAdapter()
	spec := &helm.ChartSpec{
		Wait:        true,
		WaitForJobs: true,
		Timeout:     3 * time.Minute,
		Version:     "1.5.0",
	}

	helm.ApplyCommonActionConfig(adapter, spec)

	// The underlying Install action should have the values set via the adapter
	assert.Equal(t, helmv4kube.StatusWatcherStrategy, install.WaitStrategy)
	assert.True(t, install.WaitForJobs)
	assert.Equal(t, 3*time.Minute, install.Timeout)
	assert.Equal(t, "1.5.0", install.Version)
}

func TestUpgradeActionAdapter(t *testing.T) {
	t.Parallel()

	adapter, upgrade := helm.NewUpgradeActionAdapter()
	spec := &helm.ChartSpec{
		Wait:        false,
		WaitForJobs: false,
		Timeout:     7 * time.Minute,
		Version:     "2.1.0",
	}

	helm.ApplyCommonActionConfig(adapter, spec)

	// The underlying Upgrade action should have the values set via the adapter
	assert.Equal(t, helmv4kube.HookOnlyStrategy, upgrade.WaitStrategy)
	assert.False(t, upgrade.WaitForJobs)
	assert.Equal(t, 7*time.Minute, upgrade.Timeout)
	assert.Equal(t, "2.1.0", upgrade.Version)
}

// ---------------------------------------------------------------------------
// applyChartPathOptions coverage
// ---------------------------------------------------------------------------

func TestApplyChartPathOptions_Install(t *testing.T) {
	t.Parallel()

	spec := &helm.ChartSpec{
		Version:               "1.0.0",
		Username:              "user",
		Password:              "pass",
		CertFile:              "/cert",
		KeyFile:               "/key",
		CaFile:                "/ca",
		InsecureSkipTLSverify: true,
		RepoURL:               "https://example.com",
	}

	opts := helm.BuildChartPathOptions(spec, spec.RepoURL)
	action := helm.NewInstallAction()
	helm.ApplyChartPathOptions(action, opts)

	assert.Equal(t, "1.0.0", action.Version)
	assert.Equal(t, "https://example.com", action.RepoURL)
	assert.Equal(t, "user", action.Username)
	assert.True(t, action.InsecureSkipTLSVerify)
}

func TestApplyChartPathOptions_Upgrade(t *testing.T) {
	t.Parallel()

	spec := &helm.ChartSpec{
		Version: "3.0.0",
		RepoURL: "https://upgrade.example.com",
	}

	opts := helm.BuildChartPathOptions(spec, spec.RepoURL)
	action := helm.NewUpgradeAction()
	helm.ApplyChartPathOptions(action, opts)

	assert.Equal(t, "3.0.0", action.Version)
	assert.Equal(t, "https://upgrade.example.com", action.RepoURL)
}

func TestApplyChartPathOptions_UnsupportedType(t *testing.T) {
	t.Parallel()

	spec := &helm.ChartSpec{Version: "1.0.0"}
	opts := helm.BuildChartPathOptions(spec, "")

	// Should not panic for unsupported types
	helm.ApplyChartPathOptions("not-a-client", opts)
}

// ---------------------------------------------------------------------------
// releaseToInfo
// ---------------------------------------------------------------------------

func TestReleaseToInfo_Nil(t *testing.T) {
	t.Parallel()

	result := helm.ReleaseToInfo(nil)
	assert.Nil(t, result)
}

func TestReleaseToInfo_FullRelease(t *testing.T) {
	t.Parallel()

	now := time.Now()
	rel := helm.NewTestRelease(
		"test-release", "test-ns", "test-chart", "1.0.0", "test notes",
		releasecommon.StatusDeployed, 3, now,
	)

	info := helm.ReleaseToInfo(rel)

	require.NotNil(t, info)
	assert.Equal(t, "test-release", info.Name)
	assert.Equal(t, "test-ns", info.Namespace)
	assert.Equal(t, 3, info.Revision)
	assert.Equal(t, "deployed", info.Status)
	assert.Equal(t, "test-chart", info.Chart)
	assert.Equal(t, "1.0.0", info.AppVersion)
	assert.Equal(t, now, info.Updated)
	assert.Equal(t, "test notes", info.Notes)
}

// ---------------------------------------------------------------------------
// executeAndExtractRelease
// ---------------------------------------------------------------------------

func TestExecuteAndExtractRelease_ErrorFromRunFn(t *testing.T) {
	t.Parallel()

	runFn := func() (any, error) {
		return nil, assert.AnError
	}

	_, err := helm.ExecuteAndExtractRelease(runFn)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestExecuteAndExtractRelease_WrongType(t *testing.T) {
	t.Parallel()

	runFn := func() (any, error) {
		return "not-a-release", nil
	}

	_, err := helm.ExecuteAndExtractRelease(runFn)

	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrUnexpectedReleaseType)
}

// ---------------------------------------------------------------------------
// MockInterface conformance
// ---------------------------------------------------------------------------

func TestMockInterface_Implements(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)

	var _ helm.Interface = mockClient
}
