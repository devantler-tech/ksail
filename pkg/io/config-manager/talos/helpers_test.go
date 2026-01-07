package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talos "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/stretchr/testify/assert"
)

func TestResolveClusterName_NilConfigs(t *testing.T) {
	t.Parallel()

	name := talos.ResolveClusterName(nil, nil)
	assert.Equal(t, talos.DefaultClusterName, name)
}

func TestResolveClusterName_TalosConfigName(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: "my-talos-cluster"}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "ignored-context"

	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, "my-talos-cluster", name)
}

func TestResolveClusterName_FallbackToContext(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "my-context"

	// Context without admin@ prefix is returned as-is
	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, "my-context", name)
}

func TestResolveClusterName_ExtractsClusterNameFromAdminContext(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "admin@my-talos-cluster"

	// Should extract cluster name from admin@<cluster-name> pattern
	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, "my-talos-cluster", name)
}

func TestResolveClusterName_AdminPrefixWithoutClusterName(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "admin@"

	// Context "admin@" without cluster name should return DefaultClusterName
	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, talos.DefaultClusterName, name)
}

func TestResolveClusterName_NilTalosConfig(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "admin@my-cluster"

	// Should extract cluster name from admin@<cluster-name> pattern
	name := talos.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, "my-cluster", name)
}

func TestResolveClusterName_EmptyNames(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = ""

	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, talos.DefaultClusterName, name)
}

func TestResolveClusterName_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: "  my-cluster  "}

	name := talos.ResolveClusterName(nil, talosConfig)
	assert.Equal(t, "my-cluster", name)
}

func TestResolveClusterName_WhitespaceOnlyName(t *testing.T) {
	t.Parallel()

	talosConfig := &talos.Configs{Name: "   "}
	clusterCfg := &v1alpha1.Cluster{}

	name := talos.ResolveClusterName(clusterCfg, talosConfig)
	assert.Equal(t, talos.DefaultClusterName, name)
}
