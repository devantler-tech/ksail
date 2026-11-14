package argocdinstaller_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	argoRelease   = "argocd"
	argoNamespace = "argocd"
)

// renderedAsStored returns the values a high-availability installer sends to
// Helm, as Helm stores and returns them: persisted as JSON.
func renderedAsStored(t *testing.T, sopsEnabled bool) map[string]any {
	t.Helper()

	values, err := helm.UserSuppliedValues(
		argocdinstaller.NewInstaller(nil, 5*time.Minute, sopsEnabled, true).ChartSpec(),
	)
	require.NoError(t, err)

	raw, err := json.Marshal(values)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectArgoRelease(
	client *helm.MockInterface,
	labels map[string]string,
	values map[string]any,
) {
	client.EXPECT().ReleaseExists(mock.Anything, argoRelease, argoNamespace).Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, argoRelease, argoNamespace).
		Return(labels, nil)

	if values != nil {
		client.EXPECT().
			GetReleaseValues(mock.Anything, argoRelease, argoNamespace).
			Return(values, nil)
	}
}

// TestValuesDrifted_DetectsArgoCDWithOtherValues is ksail#7772: a cluster that
// gained SOPS decryption after Argo CD was installed must report drift.
func TestValuesDrifted_DetectsArgoCDWithOtherValues(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	expectArgoRelease(client, map[string]string{"owner": "helm"}, renderedAsStored(t, false))

	drifted, err := argocdinstaller.NewInstaller(client, 5*time.Minute, true, true).
		ValuesDrifted(context.Background())

	require.NoError(t, err)
	assert.True(t, drifted)
}

// TestValuesDrifted_MatchingArgoCDIsNotDrift is the control.
func TestValuesDrifted_MatchingArgoCDIsNotDrift(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	expectArgoRelease(client, map[string]string{"owner": "helm"}, renderedAsStored(t, true))

	drifted, err := argocdinstaller.NewInstaller(client, 5*time.Minute, true, true).
		ValuesDrifted(context.Background())

	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_GitOpsOwnedArgoCDIsNotDrift leaves a release Argo CD or
// Flux manages to its owner, without reading its values.
func TestValuesDrifted_GitOpsOwnedArgoCDIsNotDrift(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	expectArgoRelease(client, map[string]string{"helm.toolkit.fluxcd.io/name": "argocd"}, nil)

	drifted, err := argocdinstaller.NewInstaller(client, 5*time.Minute, true, true).
		ValuesDrifted(context.Background())

	require.NoError(t, err)
	assert.False(t, drifted)
}
