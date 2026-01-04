package kind_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kind "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	"github.com/stretchr/testify/assert"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestResolveClusterName_NilConfigs(t *testing.T) {
	t.Parallel()

	name := kind.ResolveClusterName(nil, nil)
	assert.Equal(t, "kind", name)
}

func TestResolveClusterName_KindConfigName(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "my-kind-cluster"}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "ignored-context"

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "my-kind-cluster", name)
}

func TestResolveClusterName_FallbackToContext(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "my-context"

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "my-context", name)
}

func TestResolveClusterName_NilKindConfig(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "my-context"

	name := kind.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, "my-context", name)
}

func TestResolveClusterName_EmptyNames(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = ""

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "kind", name)
}

func TestResolveClusterName_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "  my-cluster  "}

	name := kind.ResolveClusterName(nil, kindConfig)
	assert.Equal(t, "my-cluster", name)
}

func TestResolveClusterName_WhitespaceOnlyName(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "   "}
	clusterCfg := &v1alpha1.Cluster{}

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "kind", name)
}
