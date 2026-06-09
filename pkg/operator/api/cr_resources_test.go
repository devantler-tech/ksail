package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const (
	kindConfigMap   = "ConfigMap"
	nsDefault       = "default"
	configMapName   = "cfg"
	sampleClusterID = "c1"
)

var errResolveBoom = errors.New("cannot reach child cluster")

// childResolver builds a child-cluster resolver that always yields the given dynamic client, so the
// operator ResourceService can be tested without the real vcluster connection logic.
func childResolver(
	dyn dynamic.Interface,
) func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
	return func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
		return dyn, nil
	}
}

func TestCRConnectedListsAndGetsChildResources(t *testing.T) {
	t.Parallel()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: nsDefault},
	}
	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, configMap)

	service := api.NewCRClusterServiceWithResources(
		newClient(t, sampleCluster()),
		childResolver(dyn),
	)

	resourceService, ok := service.(api.ResourceService)
	require.True(t, ok, "connected operator service must implement ResourceService")

	list, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: kindConfigMap, Namespace: nsDefault},
	)
	require.NoError(t, err)
	assert.Len(t, list.Items, 1)

	obj, err := resourceService.GetResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: kindConfigMap, Namespace: nsDefault, Name: configMapName},
	)
	require.NoError(t, err)
	assert.Equal(t, configMapName, obj.GetName())
}

func TestCRConnectedClusterNotFound(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	service := api.NewCRClusterServiceWithResources(
		newClient(t),
		childResolver(dyn),
	)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, "missing",
		api.ResourceQuery{Kind: kindConfigMap},
	)
	require.Error(t, err, "listing for an absent Cluster CR must surface the get error")
}

func TestCRConnectedResolverError(t *testing.T) {
	t.Parallel()

	resolver := func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
		return nil, errResolveBoom
	}
	service := api.NewCRClusterServiceWithResources(newClient(t, sampleCluster()), resolver)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: kindConfigMap},
	)
	require.ErrorIs(t, err, errResolveBoom)
}

func TestCRConnectedUnknownKind(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	service := api.NewCRClusterServiceWithResources(
		newClient(t, sampleCluster()),
		childResolver(dyn),
	)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: "NotARealKind"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

// TestCRPlainServiceHasNoResourceBrowser documents that the plain operator backend (no child-cluster
// resolver) does not advertise the resource browser — only the connected variant does.
func TestCRPlainServiceHasNoResourceBrowser(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newClient(t))
	_, ok := service.(api.ResourceService)
	assert.False(t, ok, "plain operator service must not implement ResourceService")
}
