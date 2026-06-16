package controller_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcile_IgnoredFieldsConditionFalseWhenUnset(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	reconciler := newReconciler(scheme, fakeClient, &fakeProvisioner{exists: true})

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))

	ignored := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionIgnoredFields)
	require.NotNil(t, ignored, "the IgnoredFields condition is always reported")
	assert.Equal(t, metav1.ConditionFalse, ignored.Status)
	assert.Equal(t, "None", ignored.Reason)
}

func TestReconcile_IgnoredFieldsConditionTrueWhenCLIOnlyFieldsSet(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	cluster := newCluster(true)
	// Set CLI-only spec fields the operator never reconciles.
	cluster.Spec.Editor = "code --wait"
	cluster.Spec.Chat = v1alpha1.ChatSpec{Model: "auto"}
	cluster.Spec.Cluster.DistributionConfig = "kind.yaml"
	cluster.Spec.Cluster.Connection = v1alpha1.Connection{Context: "kind-kind"}
	cluster.Spec.Workload.Watch.Hooks = []string{"make generate"}

	fakeClient := newFakeClient(scheme, cluster)
	reconciler := newReconciler(scheme, fakeClient, &fakeProvisioner{exists: true})

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))

	ignored := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionIgnoredFields)
	require.NotNil(t, ignored)
	assert.Equal(t, metav1.ConditionTrue, ignored.Status)
	assert.Equal(t, "CLIOnlyFieldsSet", ignored.Reason)
	// The message lists every offending field path in a stable order.
	assert.Contains(t, ignored.Message, "spec.editor")
	assert.Contains(t, ignored.Message, "spec.chat")
	assert.Contains(t, ignored.Message, "spec.cluster.distributionConfig")
	assert.Contains(t, ignored.Message, "spec.cluster.connection")
	assert.Contains(t, ignored.Message, "spec.workload.watch.hooks")

	// Surfacing ignored fields is purely informational and must never affect readiness.
	ready := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
}
