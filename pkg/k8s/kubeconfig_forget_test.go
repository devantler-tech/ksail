package k8s_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestForgetContextPreservesIndependentReferences(t *testing.T) {
	t.Parallel()

	for _, shared := range []string{"cluster", "user"} {
		t.Run(shared, func(t *testing.T) {
			t.Parallel()

			config := clientcmdapi.NewConfig()
			config.Clusters["connection"] = &clientcmdapi.Cluster{Server: "https://unused.invalid"}
			config.AuthInfos["credential"] = &clientcmdapi.AuthInfo{Token: "preserve-me"}
			config.Contexts["chosen"] = &clientcmdapi.Context{Cluster: "connection", AuthInfo: "credential"}
			config.Contexts["chosen-prefix"] = &clientcmdapi.Context{Namespace: "keep-me"}
			if shared == "cluster" {
				config.Contexts["chosen-prefix"].Cluster = "connection"
			} else {
				config.Contexts["chosen-prefix"].AuthInfo = "credential"
			}
			config.CurrentContext = "chosen-prefix"
			config.Extensions["preserve"] = &runtime.Unknown{Raw: []byte(`{"value":"keep-me"}`)}
			path := filepath.Join(t.TempDir(), "config")
			require.NoError(t, clientcmd.WriteToFile(*config, path))
			expected, err := clientcmd.LoadFromFile(path)
			require.NoError(t, err)
			delete(expected.Contexts, "chosen")
			if shared == "cluster" {
				delete(expected.AuthInfos, "credential")
			} else {
				delete(expected.Clusters, "connection")
			}

			changed, err := k8s.ForgetContext(t.Context(), path, "chosen")
			require.NoError(t, err)
			assert.True(t, changed)
			actual, err := clientcmd.LoadFromFile(path)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
}

func TestForgetContextRefusesReadOnlyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Contexts["chosen"] = &clientcmdapi.Context{}
	require.NoError(t, clientcmd.WriteToFile(*config, path))
	require.NoError(t, os.Chmod(path, 0o400))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	changed, err := k8s.ForgetContext(t.Context(), path, "chosen")
	require.Error(t, err)
	assert.False(t, changed)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestForgetContextHonorsKubeconfigLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Contexts["chosen"] = &clientcmdapi.Context{}
	require.NoError(t, clientcmd.WriteToFile(*config, path))
	require.NoError(t, os.WriteFile(path+".lock", nil, 0o600))
	changed, err := k8s.ForgetContext(t.Context(), path, "chosen")
	require.Error(t, err)
	assert.False(t, changed)
	after, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Contains(t, after.Contexts, "chosen")
	assert.FileExists(t, path+".lock", "another writer's lock is not ours to remove")
}

func TestForgetContextBlocksConcurrentKubectlWrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["shared"] = &clientcmdapi.Cluster{Server: "https://unused.invalid"}
	config.AuthInfos["shared"] = &clientcmdapi.AuthInfo{Token: "keep"}
	config.Contexts["chosen"] = &clientcmdapi.Context{Cluster: "shared", AuthInfo: "shared"}
	require.NoError(t, clientcmd.WriteToFile(*config, path))
	writingStopped := errors.New("intercepted before atomic replacement")
	_, err := k8s.ForgetContextWithWriteForTest(t.Context(), path, "chosen",
		func(_ string, _ []byte, _ os.FileMode) error {
			// A normal kubectl writer cannot add a new shared reference after our snapshot.
			config.Contexts["new-host-context"] = &clientcmdapi.Context{Cluster: "shared", AuthInfo: "shared"}
			access := clientcmd.NewDefaultPathOptions()
			access.GlobalFile = path
			access.LoadingRules.ExplicitPath = path
			err := clientcmd.ModifyConfig(access, *config, false)
			assert.Error(t, err, "hold the client-go lock through the final atomic write")

			return writingStopped
		})
	require.ErrorIs(t, err, writingStopped)
	assert.NoFileExists(t, path+".lock", "release our lock on failure")
}

func TestForgetContextReadErrorsPreserveFile(t *testing.T) {
	t.Parallel()

	for _, contents := range []string{"contexts: [invalid", "not a kubeconfig"} {
		t.Run(contents, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			changed, err := k8s.ForgetContext(t.Context(), path, "chosen")
			require.Error(t, err)
			assert.False(t, changed)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, contents, string(after))
		})
	}

	changed, err := k8s.ForgetContext(t.Context(), filepath.Join(t.TempDir(), "missing"), "chosen")
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.False(t, changed)
}

func TestForgetContextRequiresExplicitSelection(t *testing.T) {
	t.Parallel()

	for _, selection := range [][2]string{{"", "chosen"}, {"config", ""}} {
		changed, err := k8s.ForgetContext(t.Context(), selection[0], selection[1])
		require.ErrorIs(t, err, k8s.ErrExplicitConnectionRequired)
		assert.False(t, changed)
	}
}

func TestForgetContextCancellationPreservesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	contents := []byte("untouched even before parsing")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	changed, err := k8s.ForgetContext(ctx, path, "chosen")
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, changed)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, contents, after)
}

func TestForgetContextWriteFailurePreservesFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Contexts["chosen"] = &clientcmdapi.Context{}
	require.NoError(t, clientcmd.WriteToFile(*config, path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	failure := errors.New("atomic replace failed")
	changed, err := k8s.ForgetContextWithWriteForTest(t.Context(), path, "chosen",
		func(_ string, _ []byte, mode os.FileMode) error {
			assert.Equal(t, os.FileMode(0o600), mode)

			return failure
		})
	require.ErrorIs(t, err, failure)
	assert.False(t, changed)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
