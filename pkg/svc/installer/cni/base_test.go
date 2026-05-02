package cni_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var ctx = context.Background()

func expectNoError(t *testing.T, err error, description string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: unexpected error: %v", description, err)
	}
}

func expectErrorContains(t *testing.T, err error, substr, description string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s: expected error containing %q but got nil", description, substr)
	}

	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("%s: expected error to contain %q, got %q", description, substr, err.Error())
	}
}

func expectEqual[T comparable](t *testing.T, got, want T, description string) {
	t.Helper()

	if got != want {
		t.Fatalf("%s: expected %v, got %v", description, want, got)
	}
}

func writeKubeconfig(t *testing.T, dir string) string {
	t.Helper()

	contents := `apiVersion: v1
kind: Config
clusters:
- name: cluster-one
  cluster:
    server: https://cluster-one.example.com
- name: cluster-two
  cluster:
    server: https://cluster-two.example.com
contexts:
- name: primary
  context:
    cluster: cluster-one
    user: user-one
- name: alt
  context:
    cluster: cluster-two
    user: user-two
current-context: primary
users:
- name: user-one
  user:
    token: token-one
- name: user-two
  user:
    token: token-two
`

	path := filepath.Join(dir, "config")

	err := os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("write kubeconfig file: %v", err)
	}

	return path
}

func TestInstallerBaseBuildRESTConfig(t *testing.T) {
	t.Parallel()

	t.Run("ErrorWhenKubeconfigMissing", testBuildRESTConfigErrorWhenKubeconfigMissing)
	t.Run("UsesCurrentContext", testBuildRESTConfigUsesCurrentContext)
	t.Run("OverridesContext", testBuildRESTConfigOverridesContext)
	t.Run("MissingContext", testBuildRESTConfigMissingContext)
}

func testBuildRESTConfigErrorWhenKubeconfigMissing(t *testing.T) {
	t.Helper()
	t.Parallel()

	base := cni.NewInstallerBase(helm.NewMockInterface(t), "", "", time.Second)
	_, err := base.BuildRESTConfig()

	expectErrorContains(t, err, "kubeconfig path is empty", "buildRESTConfig empty path")
}

func testBuildRESTConfigUsesCurrentContext(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := writeKubeconfig(t, t.TempDir())
	base := cni.NewInstallerBase(helm.NewMockInterface(t), path, "", time.Second)

	restConfig, err := base.BuildRESTConfig()

	expectNoError(t, err, "buildRESTConfig current context")
	expectEqual(
		t,
		restConfig.Host,
		"https://cluster-one.example.com",
		"rest config host",
	)
}

func testBuildRESTConfigOverridesContext(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := writeKubeconfig(t, t.TempDir())
	base := cni.NewInstallerBase(helm.NewMockInterface(t), path, "alt", time.Second)

	restConfig, err := base.BuildRESTConfig()

	expectNoError(t, err, "buildRESTConfig override context")
	expectEqual(
		t,
		restConfig.Host,
		"https://cluster-two.example.com",
		"rest config host override",
	)
}

func testBuildRESTConfigMissingContext(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := writeKubeconfig(t, t.TempDir())
	base := cni.NewInstallerBase(
		helm.NewMockInterface(t),
		path,
		"missing",
		time.Second,
	)
	_, err := base.BuildRESTConfig()

	expectErrorContains(
		t,
		err,
		"context \"missing\" does not exist",
		"buildRESTConfig missing context",
	)
}

func TestCheckGitOpsOwnership(t *testing.T) {
	t.Parallel()

	t.Run("SkipsWhenFluxManaged", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseSecretLabels(mock.Anything, "cilium", "kube-system").
			Return(map[string]string{
				"helm.toolkit.fluxcd.io/name": "cilium",
			}, nil)

		base := cni.NewInstallerBase(client, "", "", time.Second)

		skipped, err := base.CheckGitOpsOwnership(ctx, "cilium", "cilium", "kube-system")

		expectNoError(t, err, "check ownership flux")
		expectEqual(t, skipped, true, "should skip")
	})

	t.Run("ProceedsWhenNotManaged", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseSecretLabels(mock.Anything, "cilium", "kube-system").
			Return(map[string]string{
				"name":  "cilium",
				"owner": "helm",
			}, nil)

		base := cni.NewInstallerBase(client, "", "", time.Second)

		skipped, err := base.CheckGitOpsOwnership(ctx, "cilium", "cilium", "kube-system")

		expectNoError(t, err, "check ownership not managed")
		expectEqual(t, skipped, false, "should not skip")
	})

	t.Run("ProceedsWhenNoRelease", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseSecretLabels(mock.Anything, "cilium", "kube-system").
			Return(nil, nil)

		base := cni.NewInstallerBase(client, "", "", time.Second)

		skipped, err := base.CheckGitOpsOwnership(ctx, "cilium", "cilium", "kube-system")

		expectNoError(t, err, "check ownership no release")
		expectEqual(t, skipped, false, "should not skip")
	})

	t.Run("ReturnsErrorOnFailure", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseSecretLabels(mock.Anything, "cilium", "kube-system").
			Return(nil, assert.AnError)

		base := cni.NewInstallerBase(client, "", "", time.Second)

		_, err := base.CheckGitOpsOwnership(ctx, "cilium", "cilium", "kube-system")

		expectErrorContains(t, err, "check release ownership for cilium", "check ownership error")
	})
}
