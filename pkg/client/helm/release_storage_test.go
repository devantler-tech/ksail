package helm_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchReleaseStorageMetadataSelectsLatestIncarnation(t *testing.T) {
	t.Parallel()

	clientset := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "sh.helm.release.v1.controller.v1", Namespace: "kube-system",
			UID:    types.UID("old-uid"),
			Labels: map[string]string{"name": "controller", "owner": "helm", "version": "1"},
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "sh.helm.release.v1.controller.v2", Namespace: "kube-system",
			UID:    types.UID("current-uid"),
			Labels: map[string]string{"name": "controller", "owner": "helm", "version": "2"},
		}},
	)

	metadata, err := helm.FetchReleaseStorageMetadata(
		context.Background(), clientset, "secret", "kube-system", "name=controller,owner=helm",
	)

	require.NoError(t, err)
	assert.Equal(t, "current-uid", metadata.Identity)
	assert.Equal(t, "2", metadata.Labels["version"])
	assert.ElementsMatch(t, []string{"old-uid", "current-uid"}, metadata.HistoryIdentities)
}

func TestFetchReleaseStorageMetadataReportsMissingRelease(t *testing.T) {
	t.Parallel()

	_, err := helm.FetchReleaseStorageMetadata(
		context.Background(), fake.NewSimpleClientset(), "secret", "kube-system",
		"name=missing,owner=helm",
	)

	require.ErrorIs(t, err, helm.ErrNoReleaseStorage)
}

func TestFetchReleaseStorageMetadataRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	_, err := helm.FetchReleaseStorageMetadata(
		context.Background(), fake.NewSimpleClientset(), "sql", "kube-system",
		"name=controller,owner=helm",
	)

	require.ErrorContains(t, err, "cannot provide a Kubernetes release identity")
}
