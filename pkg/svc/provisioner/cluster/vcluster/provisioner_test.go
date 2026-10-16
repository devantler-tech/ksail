package vclusterprovisioner_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var (
	errMockStartNodesFailed      = errors.New("start nodes failed")
	errMockStopNodesFailed       = errors.New("stop nodes failed")
	errMockListAllClustersFailed = errors.New("list all clusters failed")
)

const (
	standardControllers137 = "--controllers=*,-nodeipam,-nodelifecycle,-persistentvolume-binder," +
		"-attachdetach,-persistentvolume-expander,-ttl"
	schedulerControllers137 = "--controllers=*,-nodeipam,-persistentvolume-binder,-attachdetach," +
		"-persistentvolume-expander,-ttl"
)

type controllerCompatibilityTestCase struct {
	name             string
	version          string
	userValues       string
	wantControllers  string
	wantPreservedArg string
	wantOverride     bool
	defaultPrivate   bool
}

// runNodeOperationTest is a shared helper for testing Start/Stop node lifecycle operations.
func runNodeOperationTest(
	t *testing.T,
	clusterName, inputName, mockFn string,
	mockErr error,
	operation func(*vclusterprovisioner.Provisioner, context.Context, string) error,
) {
	t.Helper()

	mockProvider := provider.NewMockProvider()
	mockProvider.On(mockFn, mock.Anything, clusterName).Return(mockErr)

	provisioner := vclusterprovisioner.NewProvisioner(clusterName, "", false, mockProvider)
	err := operation(provisioner, context.Background(), inputName)

	if mockErr != nil {
		require.Error(t, err)
		require.ErrorContains(t, err, mockErr.Error())
	} else {
		require.NoError(t, err)
	}

	mockProvider.AssertExpectations(t)
}

func TestNewProvisioner(t *testing.T) {
	t.Parallel()

	t.Run("creates_with_provided_name", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		mockProvider.On("StartNodes", mock.Anything, "test-cluster").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
		require.NotNil(t, provisioner)

		err := provisioner.Start(context.Background(), "test-cluster")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})

	t.Run("uses_default_name_when_empty", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		// When cluster name is empty, the provisioner should use "vcluster-default"
		mockProvider.On("StartNodes", mock.Anything, "vcluster-default").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner(
			"",
			"/path/to/values.yaml",
			false,
			mockProvider,
		)
		require.NotNil(t, provisioner)

		// Pass empty name to Start; the provisioner should resolve to the default name
		err := provisioner.Start(context.Background(), "")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})

	t.Run("preserves_all_options", func(t *testing.T) {
		t.Parallel()

		mockProvider := provider.NewMockProvider()
		mockProvider.On("StartNodes", mock.Anything, "custom").Return(nil)

		provisioner := vclusterprovisioner.NewProvisioner(
			"custom",
			"/custom/path.yaml",
			true,
			mockProvider,
		)
		require.NotNil(t, provisioner)

		err := provisioner.Start(context.Background(), "custom")
		require.NoError(t, err)
		mockProvider.AssertExpectations(t)
	})
}

func TestSetProvider(t *testing.T) {
	t.Parallel()

	mockProvider1 := provider.NewMockProvider()
	mockProvider2 := provider.NewMockProvider()

	provisioner := vclusterprovisioner.NewProvisioner(
		"test-cluster",
		"",
		false,
		mockProvider1,
	)

	provisioner.SetProvider(mockProvider2)

	assert.NotNil(t, provisioner, "provisioner should still be valid after SetProvider")
}

func TestStart_Success(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StartNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStart_Error(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StartNodes", errMockStartNodesFailed,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStart_DefaultName(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "default-cluster", "", "StartNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Start(ctx, name)
		},
	)
}

func TestStop_Success(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StopNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestStop_Error(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "test-cluster", "test-cluster", "StopNodes", errMockStopNodesFailed,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestStop_DefaultName(t *testing.T) {
	t.Parallel()

	runNodeOperationTest(t, "default-cluster", "", "StopNodes", nil,
		func(p *vclusterprovisioner.Provisioner, ctx context.Context, name string) error {
			return p.Stop(ctx, name)
		},
	)
}

func TestList_WithClusters(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"cluster1", "cluster2"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"cluster1", "cluster2"}, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_Empty(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{}, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_Error(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string(nil), errMockListAllClustersFailed)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	clusters, err := provisioner.List(context.Background())

	require.Error(t, err)
	require.ErrorContains(t, err, "list all clusters failed")
	assert.Nil(t, clusters)
	mockProvider.AssertExpectations(t)
}

func TestList_NilProvider(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, nil)
	clusters, err := provisioner.List(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrProviderNotSet, "should return ErrProviderNotSet")
	assert.Nil(t, clusters, "clusters should be nil on error")
}

func TestExists_Found(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"test-cluster", "other-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.True(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_NotFound(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"other-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_EmptyList(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.NoError(t, err)
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_Error(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string(nil), errMockListAllClustersFailed)

	provisioner := vclusterprovisioner.NewProvisioner("test-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorContains(t, err, "list all clusters failed")
	assert.False(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestExists_DefaultName(t *testing.T) {
	t.Parallel()

	mockProvider := provider.NewMockProvider()
	mockProvider.On("ListAllClusters", mock.Anything).
		Return([]string{"default-cluster"}, nil)

	provisioner := vclusterprovisioner.NewProvisioner("default-cluster", "", false, mockProvider)
	exists, err := provisioner.Exists(context.Background(), "")

	require.NoError(t, err)
	assert.True(t, exists)
	mockProvider.AssertExpectations(t)
}

func TestBuildValuesFiles_ExtraSAN(t *testing.T) {
	t.Parallel()

	t.Run("adds proxy extraSANs when address provided", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest(
			"",
			false,
			"5.6.7.8",
			false,
		)
		require.NoError(t, err)

		defer cleanup()

		require.NotEmpty(t, files)

		content, err := os.ReadFile(files[0])
		require.NoError(t, err)

		assert.Contains(t, string(content), "proxy:")
		assert.Contains(t, string(content), "extraSANs:")
		assert.Contains(t, string(content), "5.6.7.8")
	})

	t.Run("omits proxy extraSANs when address empty", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest("", false, "", false)
		require.NoError(t, err)

		defer cleanup()

		require.NotEmpty(t, files)

		content, err := os.ReadFile(files[0])
		require.NoError(t, err)

		assert.NotContains(t, string(content), "extraSANs:")
	})
}

func TestBuildValuesFiles_ControllerManagerCompatibilityVersions(t *testing.T) {
	t.Parallel()

	tests := []controllerCompatibilityTestCase{
		{
			name:    "keeps Kubernetes 1.36 defaults",
			version: "v1.36.5",
		},
		{
			name:            "removes retired controller for Kubernetes 1.37",
			version:         "v1.37.1",
			wantControllers: standardControllers137,
			wantOverride:    true,
		},
		{
			name:            "uses the Kubernetes image tag selected by the user",
			version:         "v1.36.5",
			userValues:      "controlPlane:\n  distro:\n    k8s:\n      image:\n        tag: v1.37.1\n",
			wantControllers: standardControllers137,
			wantOverride:    true,
		},
		{
			name:            "uses the standalone Kubernetes version selected by the user",
			version:         "v1.36.5",
			userValues:      "controlPlane:\n  distro:\n    k8s:\n      version: v1.37.1\n",
			wantControllers: standardControllers137,
			wantOverride:    true,
		},
		{
			name:         "keeps a user-selected Kubernetes 1.36 version unchanged",
			version:      "v1.37.1",
			userValues:   "controlPlane:\n  distro:\n    k8s:\n      version: v1.36.5\n",
			wantOverride: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runControllerCompatibilityTest(t, test)
		})
	}
}

func TestBuildValuesFiles_ControllerManagerCompatibilityModes(t *testing.T) {
	t.Parallel()

	tests := []controllerCompatibilityTestCase{
		{
			name:            "preserves Docker private node behavior",
			version:         "v1.37.1",
			wantControllers: "--controllers=*,bootstrapsigner,tokencleaner",
			wantOverride:    true,
			defaultPrivate:  true,
		},
		{
			name:            "honors explicit virtual node mode for Docker",
			version:         "v1.37.1",
			userValues:      "privateNodes:\n  enabled: false\n",
			wantControllers: standardControllers137,
			wantOverride:    true,
			defaultPrivate:  true,
		},
		{
			name:    "preserves virtual scheduler behavior and other arguments",
			version: "v1.37.1",
			userValues: "controlPlane:\n  distro:\n    k8s:\n      scheduler:\n        enabled: true\n" +
				"      controllerManager:\n        extraArgs:\n        - --profiling=false\n",
			wantControllers:  schedulerControllers137,
			wantPreservedArg: "--profiling=false",
			wantOverride:     true,
		},
		{
			name:    "preserves private node and resource claim behavior",
			version: "v1.37.1",
			userValues: "privateNodes:\n  enabled: true\nsync:\n  toHost:\n" +
				"    resourceClaimTemplates:\n      enabled: true\n",
			wantControllers: "--controllers=*,bootstrapsigner,tokencleaner,-resourceclaim-controller",
			wantOverride:    true,
		},
		{
			name:    "leaves an explicit controller list unchanged",
			version: "v1.37.1",
			userValues: "controlPlane:\n  distro:\n    k8s:\n      controllerManager:\n" +
				"        extraArgs:\n        - --controllers=*,bootstrapsigner\n",
			wantOverride: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runControllerCompatibilityTest(t, test)
		})
	}
}

func runControllerCompatibilityTest(t *testing.T, test controllerCompatibilityTestCase) {
	t.Helper()

	userValuesPath := ""
	if test.userValues != "" {
		userValuesPath = t.TempDir() + "/vcluster.yaml"
		require.NoError(t, os.WriteFile(userValuesPath, []byte(test.userValues), 0o600))
	}

	files, cleanup, err := vclusterprovisioner.BuildValuesFilesForVersionForTest(
		userValuesPath, false, "", false, test.version, test.defaultPrivate,
	)
	require.NoError(t, err)

	defer cleanup()

	baseFileCount := 1
	if userValuesPath != "" {
		baseFileCount++
	}

	if !test.wantOverride {
		require.Len(t, files, baseFileCount)

		return
	}

	require.Len(t, files, baseFileCount+1)
	extraArgs := readControllerManagerArgs(t, files[len(files)-1])
	assert.Contains(t, extraArgs, test.wantControllers)
	assert.NotContains(t, test.wantControllers, "cloud-node-lifecycle")

	if test.wantPreservedArg != "" {
		assert.Contains(t, extraArgs, test.wantPreservedArg)
	}
}

func readControllerManagerArgs(t *testing.T, valuesPath string) []string {
	t.Helper()

	//nolint:gosec // path is returned by the function under test
	content, err := os.ReadFile(valuesPath)
	require.NoError(t, err)

	var parsed struct {
		ControlPlane struct {
			Distro struct {
				K8S struct {
					ControllerManager struct {
						ExtraArgs []string `yaml:"extraArgs"`
					} `yaml:"controllerManager"`
				} `yaml:"k8s"`
			} `yaml:"distro"`
		} `yaml:"controlPlane"`
	}
	require.NoError(t, yaml.Unmarshal(content, &parsed))

	return parsed.ControlPlane.Distro.K8S.ControllerManager.ExtraArgs
}

func TestBuildValuesFiles_Persistence(t *testing.T) {
	t.Parallel()

	t.Run("appends emptyDir override last when persistence disabled", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest("", false, "", true)
		require.NoError(t, err)

		defer cleanup()

		// The persistence override must be LAST so it wins over defaults, user values,
		// and any Kubernetes-version compatibility values.
		require.GreaterOrEqual(t, len(files), 2)

		content, err := os.ReadFile(files[len(files)-1])
		require.NoError(t, err)

		var parsed struct {
			ControlPlane struct {
				StatefulSet struct {
					Persistence struct {
						VolumeClaim struct {
							Enabled bool `yaml:"enabled"`
						} `yaml:"volumeClaim"`
					} `yaml:"persistence"`
				} `yaml:"statefulSet"`
			} `yaml:"controlPlane"`
		}

		require.NoError(t, yaml.Unmarshal(content, &parsed))
		assert.False(t, parsed.ControlPlane.StatefulSet.Persistence.VolumeClaim.Enabled)
	})

	t.Run("no persistence override when not disabled", func(t *testing.T) {
		t.Parallel()

		files, cleanup, err := vclusterprovisioner.BuildValuesFilesForTest("", false, "", false)
		require.NoError(t, err)

		defer cleanup()

		for _, valuesPath := range files {
			//nolint:gosec // generated test paths
			content, readErr := os.ReadFile(valuesPath)
			require.NoError(t, readErr)
			assert.NotContains(t, string(content), "persistence:")
		}
	})
}
