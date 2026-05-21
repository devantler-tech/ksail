package k3dprovisioner_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errRunnerBoom = errors.New("runner boom")

const twoClusterJSON = `[{"name":"alpha"},{"name":"beta"}]`

// namedConfig builds a SimpleConfig with the given cluster name. The name lives
// on the embedded ObjectMeta, so it cannot be set in a flat struct literal.
func namedConfig(name string) *v1alpha5.SimpleConfig {
	return &v1alpha5.SimpleConfig{ObjectMeta: k3dtypes.ObjectMeta{Name: name}}
}

// TestProvisioner_List verifies that List parses the cluster-list output into
// cluster names and propagates listing errors.
func TestProvisioner_List(t *testing.T) {
	t.Parallel()

	t.Run("parses cluster names", func(t *testing.T) {
		t.Parallel()

		prov := k3dprovisioner.NewProvisioner(nil, "").
			WithListClustersRawForTest(func(_ context.Context) (string, error) {
				return twoClusterJSON, nil
			})

		result, err := prov.List(context.Background())

		require.NoError(t, err)
		assert.Equal(t, []string{"alpha", "beta"}, result)
	})

	t.Run("empty output yields no clusters", func(t *testing.T) {
		t.Parallel()

		prov := k3dprovisioner.NewProvisioner(nil, "").
			WithListClustersRawForTest(func(_ context.Context) (string, error) {
				return "", nil
			})

		result, err := prov.List(context.Background())

		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("propagates listing error", func(t *testing.T) {
		t.Parallel()

		prov := k3dprovisioner.NewProvisioner(nil, "").
			WithListClustersRawForTest(func(_ context.Context) (string, error) {
				return "", errRunnerBoom
			})

		_, err := prov.List(context.Background())

		require.ErrorIs(t, err, errRunnerBoom)
	})
}

// TestProvisioner_Exists verifies membership checks against the listed clusters.
func TestProvisioner_Exists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		listOutput  string
		want        bool
	}{
		{name: "present", clusterName: "alpha", listOutput: twoClusterJSON, want: true},
		{name: "absent", clusterName: "gamma", listOutput: twoClusterJSON, want: false},
		{name: "empty name", clusterName: "", listOutput: twoClusterJSON, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := k3dprovisioner.NewProvisioner(nil, "").
				WithListClustersRawForTest(func(_ context.Context) (string, error) {
					return testCase.listOutput, nil
				})

			got, err := prov.Exists(context.Background(), testCase.clusterName)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestProvisioner_Exists_PropagatesError(t *testing.T) {
	t.Parallel()

	prov := k3dprovisioner.NewProvisioner(nil, "").
		WithListClustersRawForTest(func(_ context.Context) (string, error) {
			return "", errRunnerBoom
		})

	_, err := prov.Exists(context.Background(), "alpha")

	require.ErrorIs(t, err, errRunnerBoom)
}

// TestProvisioner_NewProvisioner tests the constructor.
func TestProvisioner_NewProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
	}{
		{name: "create with config path", configPath: "/path/to/config"},
		{name: "create with empty config path", configPath: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.NewProvisioner(nil, testCase.configPath)
			assert.NotNil(t, provisioner)
		})
	}
}

// TestProvisioner_Create verifies the create command is dispatched with the
// expected flags and that runner errors are wrapped.
//
//nolint:paralleltest // subtests call runLifecycleCommand which constructs k3d's NewCmdClusterCreate; that builder mutates package-level Viper state and is not safe to call concurrently
func TestProvisioner_Create(t *testing.T) {
	t.Parallel()

	t.Run("runs create with config flag", func(t *testing.T) {
		var capturedArgs []string

		mockRunner := runner.NewMockCommandRunner(t)
		mockRunner.EXPECT().
			Run(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(
				_ context.Context,
				_ *cobra.Command,
				args []string,
			) (runner.CommandResult, error) {
				capturedArgs = args

				return runner.CommandResult{}, nil
			})

		prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "/tmp/k3d.yaml").
			WithRunnerForTest(mockRunner)

		err := prov.Create(context.Background(), "my-cluster")

		require.NoError(t, err)
		assert.Contains(t, capturedArgs, "--config")
		assert.Contains(t, capturedArgs, "/tmp/k3d.yaml")
		assert.Contains(t, capturedArgs, "my-cluster")
	})

	t.Run("wraps runner error", func(t *testing.T) {
		mockRunner := runner.NewMockCommandRunner(t)
		mockRunner.EXPECT().
			Run(mock.Anything, mock.Anything, mock.Anything).
			Return(runner.CommandResult{}, errRunnerBoom)

		prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "").
			WithRunnerForTest(mockRunner)

		err := prov.Create(context.Background(), "my-cluster")

		require.ErrorIs(t, err, errRunnerBoom)
		assert.Contains(t, err.Error(), "cluster create")
	})
}

// TestProvisioner_Delete verifies existence is checked before deletion and that
// a missing cluster yields ErrClusterNotFound without running the delete command.
func TestProvisioner_Delete(t *testing.T) {
	t.Parallel()

	t.Run("deletes existing cluster", func(t *testing.T) {
		t.Parallel()

		mockRunner := runner.NewMockCommandRunner(t)
		mockRunner.EXPECT().
			Run(mock.Anything, mock.Anything, mock.Anything).
			Return(runner.CommandResult{}, nil)

		prov := k3dprovisioner.NewProvisioner(namedConfig("alpha"), "").
			WithRunnerForTest(mockRunner).
			WithListClustersRawForTest(func(_ context.Context) (string, error) {
				return twoClusterJSON, nil
			})

		err := prov.Delete(context.Background(), "alpha")

		require.NoError(t, err)
	})

	t.Run("missing cluster returns ErrClusterNotFound", func(t *testing.T) {
		t.Parallel()

		// No runner expectations: delete must not run when the cluster is absent.
		mockRunner := runner.NewMockCommandRunner(t)

		prov := k3dprovisioner.NewProvisioner(namedConfig("missing"), "").
			WithRunnerForTest(mockRunner).
			WithListClustersRawForTest(func(_ context.Context) (string, error) {
				return twoClusterJSON, nil
			})

		err := prov.Delete(context.Background(), "missing")

		require.ErrorIs(t, err, clustererr.ErrClusterNotFound)
	})
}

// TestProvisioner_StartStop verifies the start/stop commands dispatch to the
// runner and propagate errors.
func TestProvisioner_StartStop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(ctx context.Context, p *k3dprovisioner.Provisioner) error
	}{
		{
			name: "start",
			call: func(ctx context.Context, p *k3dprovisioner.Provisioner) error {
				return p.Start(ctx, "alpha")
			},
		},
		{
			name: "stop",
			call: func(ctx context.Context, p *k3dprovisioner.Provisioner) error {
				return p.Stop(ctx, "alpha")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name+" succeeds", func(t *testing.T) {
			t.Parallel()

			mockRunner := runner.NewMockCommandRunner(t)
			mockRunner.EXPECT().
				Run(mock.Anything, mock.Anything, mock.Anything).
				Return(runner.CommandResult{}, nil)

			prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "").
				WithRunnerForTest(mockRunner)

			require.NoError(t, testCase.call(context.Background(), prov))
		})

		t.Run(testCase.name+" propagates error", func(t *testing.T) {
			t.Parallel()

			mockRunner := runner.NewMockCommandRunner(t)
			mockRunner.EXPECT().
				Run(mock.Anything, mock.Anything, mock.Anything).
				Return(runner.CommandResult{}, errRunnerBoom)

			prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "").
				WithRunnerForTest(mockRunner)

			require.ErrorIs(t, testCase.call(context.Background(), prov), errRunnerBoom)
		})
	}
}

// TestParseClusterNames covers the JSON parsing helper directly.
func TestParseClusterNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    []string
		wantErr bool
	}{
		{name: "empty", output: "", want: nil},
		{name: "two clusters", output: twoClusterJSON, want: []string{"alpha", "beta"}},
		{
			name:   "skips empty names",
			output: `[{"name":"alpha"},{"name":""}]`,
			want:   []string{"alpha"},
		},
		{name: "invalid json", output: "{not json", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := k3dprovisioner.ParseClusterNamesForTest(testCase.output)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestResolveName covers name resolution precedence.
func TestResolveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		argName   string
		configCfg *v1alpha5.SimpleConfig
		want      string
	}{
		{
			name:      "explicit name wins",
			argName:   "explicit",
			configCfg: namedConfig("cfg"),
			want:      "explicit",
		},
		{
			name:      "falls back to config name",
			argName:   "",
			configCfg: namedConfig("cfg"),
			want:      "cfg",
		},
		{name: "empty when nothing set", argName: "", configCfg: nil, want: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := k3dprovisioner.NewProvisioner(testCase.configCfg, "")
			assert.Equal(t, testCase.want, prov.ResolveNameForTest(testCase.argName))
		})
	}
}

// TestAppendFlags covers the config/image flag helpers.
func TestAppendFlags(t *testing.T) {
	t.Parallel()

	t.Run("config flag appended when path set", func(t *testing.T) {
		t.Parallel()

		prov := k3dprovisioner.NewProvisioner(nil, "/tmp/k3d.yaml")
		args := prov.AppendConfigFlagForTest(nil)
		assert.Equal(t, []string{"--config", "/tmp/k3d.yaml"}, args)
	})

	t.Run("config flag omitted when path empty", func(t *testing.T) {
		t.Parallel()

		prov := k3dprovisioner.NewProvisioner(nil, "")
		assert.Empty(t, prov.AppendConfigFlagForTest(nil))
	})

	t.Run("image flag appended from config when no config path", func(t *testing.T) {
		t.Parallel()

		cfg := &v1alpha5.SimpleConfig{Image: "rancher/k3s:v1.30"}
		prov := k3dprovisioner.NewProvisioner(cfg, "")
		args := prov.AppendImageFlagForTest(nil)
		assert.True(t, slices.Contains(args, "--image"))
		assert.True(t, slices.Contains(args, "rancher/k3s:v1.30"))
	})

	t.Run("image flag omitted when config path set", func(t *testing.T) {
		t.Parallel()

		cfg := &v1alpha5.SimpleConfig{Image: "rancher/k3s:v1.30"}
		prov := k3dprovisioner.NewProvisioner(cfg, "/tmp/k3d.yaml")
		assert.Empty(t, prov.AppendImageFlagForTest(nil))
	})
}
