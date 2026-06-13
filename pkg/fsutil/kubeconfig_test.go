package fsutil_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// errMutateSentinel is a static error used to assert mutate-abort behavior.
var errMutateSentinel = errors.New("mutate aborted")

const sampleKubeconfig = `apiVersion: v1
kind: Config
current-context: ctx
clusters:
- name: c1
  cluster:
    server: https://old:6443
contexts:
- name: ctx
  context:
    cluster: c1
    user: u1
users:
- name: u1
  user: {}
`

func writeSampleKubeconfig(t *testing.T, path string) {
	t.Helper()

	err := os.WriteFile(path, []byte(sampleKubeconfig), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func loadServer(t *testing.T, path, clusterName string) string {
	t.Helper()

	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}

	cluster, ok := cfg.Clusters[clusterName]
	if !ok {
		t.Fatalf("cluster %q not found", clusterName)
	}

	return cluster.Server
}

func TestUpdateKubeconfigFile_MutatesExistingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	writeSampleKubeconfig(t, path)

	err := fsutil.UpdateKubeconfigFile(path, func(cfg *api.Config) error {
		cfg.Clusters["c1"].Server = "https://new:6443"

		return nil
	}, fsutil.KubeconfigUpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateKubeconfigFile: %v", err)
	}

	if got := loadServer(t, path, "c1"); got != "https://new:6443" {
		t.Errorf("server = %q, want %q", got, "https://new:6443")
	}
}

func TestUpdateKubeconfigFile_CreatesWhenAbsentByDefault(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")

	err := fsutil.UpdateKubeconfigFile(path, func(cfg *api.Config) error {
		cfg.Clusters["c1"] = &api.Cluster{Server: "https://created:6443"}

		return nil
	}, fsutil.KubeconfigUpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateKubeconfigFile: %v", err)
	}

	if got := loadServer(t, path, "c1"); got != "https://created:6443" {
		t.Errorf("server = %q, want %q", got, "https://created:6443")
	}
}

func TestUpdateKubeconfigFile_RequireExistsFailsWhenAbsent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing")

	err := fsutil.UpdateKubeconfigFile(path, func(_ *api.Config) error {
		t.Error("mutate must not be called when the required file is absent")

		return nil
	}, fsutil.KubeconfigUpdateOptions{RequireExists: true})
	if err == nil {
		t.Fatal("expected an error when RequireExists is set and the file is absent")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected a not-exist error, got %v", err)
	}
}

func TestUpdateKubeconfigFile_MkdirParentCreatesDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "dir", "config")

	err := fsutil.UpdateKubeconfigFile(path, func(cfg *api.Config) error {
		cfg.CurrentContext = "ctx"

		return nil
	}, fsutil.KubeconfigUpdateOptions{MkdirParent: true})
	if err != nil {
		t.Fatalf("UpdateKubeconfigFile: %v", err)
	}

	_, statErr := os.Stat(path)
	if statErr != nil {
		t.Errorf("expected file to be written under created parent: %v", statErr)
	}
}

func TestUpdateKubeconfigFile_ExpandHome(t *testing.T) {
	// No t.Parallel: t.Setenv is incompatible with parallel subtests.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	err := fsutil.UpdateKubeconfigFile("~/config", func(cfg *api.Config) error {
		cfg.CurrentContext = "ctx"

		return nil
	}, fsutil.KubeconfigUpdateOptions{ExpandHome: true})
	if err != nil {
		t.Fatalf("UpdateKubeconfigFile: %v", err)
	}

	_, statErr := os.Stat(filepath.Join(home, "config"))
	if statErr != nil {
		t.Errorf("expected ~/config to be written: %v", statErr)
	}
}

// TestUpdateKubeconfigFile_MutateErrorAbortsWrite verifies the mutation error
// is returned unwrapped and no write happens.
func TestUpdateKubeconfigFile_MutateErrorAbortsWrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	writeSampleKubeconfig(t, path)

	err := fsutil.UpdateKubeconfigFile(path, func(_ *api.Config) error {
		return errMutateSentinel
	}, fsutil.KubeconfigUpdateOptions{})
	if !errors.Is(err, errMutateSentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	// File must be untouched.
	if got := loadServer(t, path, "c1"); got != "https://old:6443" {
		t.Errorf("server = %q, want it unchanged %q", got, "https://old:6443")
	}
}
