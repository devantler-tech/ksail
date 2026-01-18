package configmanager_test

import (
	"testing"

	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	configtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/require"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// mockClusterNameProvider is a mock implementation of ClusterNameProvider for testing.
type mockClusterNameProvider struct {
	name string
}

func (m *mockClusterNameProvider) GetClusterName() string {
	return m.name
}

func TestGetClusterName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config   any
		wantName string
		wantErr  bool
	}{
		"kind cluster": {
			config: &v1alpha4.Cluster{
				Name: "kind-custom",
			},
			wantName: "kind-custom",
		},
		"k3d simple config": {
			config: &v1alpha5.SimpleConfig{
				ObjectMeta: configtypes.ObjectMeta{Name: "k3d-custom"},
			},
			wantName: "k3d-custom",
		},
		"cluster name provider interface": {
			config:   &mockClusterNameProvider{name: "talos-custom"},
			wantName: "talos-custom",
		},
		"cluster name provider with empty name": {
			config:   &mockClusterNameProvider{name: ""},
			wantName: "",
		},
		"unsupported type": {
			config:  123,
			wantErr: true,
		},
		"nil config": {
			config:  nil,
			wantErr: true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clusterName, err := configmanager.GetClusterName(testCase.config)
			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.wantName, clusterName)
		})
	}
}
