package awslbcontrollerinstaller_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	lbcRelease   = "aws-load-balancer-controller"
	lbcNamespace = "kube-system"
)

var errMetadataUnreadable = errors.New("release storage unreadable")

func newDriftInstaller(t *testing.T, client helm.Interface) *awslbcontrollerinstaller.Installer {
	t.Helper()

	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", true,
	)
	require.NoError(t, err)

	return installer
}

// storedValues returns the rendered values as Helm stores and returns them.
func storedValues(t *testing.T, installer *awslbcontrollerinstaller.Installer) map[string]any {
	t.Helper()

	rendered, err := installer.RenderedValues()
	require.NoError(t, err)

	raw, err := json.Marshal(rendered)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectRelease(client *helm.MockInterface, values map[string]any) {
	client.EXPECT().ReleaseExists(mock.Anything, lbcRelease, lbcNamespace).Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, lbcRelease, lbcNamespace).
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().GetReleaseValues(mock.Anything, lbcRelease, lbcNamespace).Return(values, nil)
}

// TestValuesDrifted_ReportsAKSailOwnedReleaseWithOtherValues is ksail#7771: a
// controller KSail installed with other values must report drift, so `cluster
// update` upgrades it although ksail.yaml is unchanged.
func TestValuesDrifted_ReportsAKSailOwnedReleaseWithOtherValues(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	expectRelease(client, map[string]any{"clusterName": "prod-eks", "replicaCount": 1.0})
	client.EXPECT().
		GetReleaseStorageMetadata(mock.Anything, lbcRelease, lbcNamespace).
		Return(&helm.ReleaseStorageMetadata{
			Labels:   map[string]string{awslbcontrollerinstaller.ReleaseOwnershipLabel: "ksail"},
			Identity: "release-uid",
		}, nil)

	drifted, err := newDriftInstaller(t, client).ValuesDrifted(context.Background())

	require.NoError(t, err)
	assert.True(t, drifted)
}

// TestValuesDrifted_LeavesAnUnownedReleaseAlone never reports drift for a
// release without KSail's ownership label, so a hand-installed controller of
// the same name is not upgraded by a values-only update.
func TestValuesDrifted_LeavesAnUnownedReleaseAlone(t *testing.T) {
	t.Parallel()

	for name, metadata := range map[string]*helm.ReleaseStorageMetadata{
		"no ownership label": {Labels: map[string]string{"owner": "helm"}, Identity: "uid"},
		"other owner": {
			Labels:   map[string]string{awslbcontrollerinstaller.ReleaseOwnershipLabel: "someone"},
			Identity: "uid",
		},
		"no metadata": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			expectRelease(client, map[string]any{"clusterName": "other"})
			client.EXPECT().
				GetReleaseStorageMetadata(mock.Anything, lbcRelease, lbcNamespace).
				Return(metadata, nil)

			drifted, err := newDriftInstaller(t, client).ValuesDrifted(context.Background())

			require.NoError(t, err)
			assert.False(t, drifted)
		})
	}
}

// TestValuesDrifted_MatchingValuesNeedNoOwnershipRead is the control: a
// release that already carries the rendered values is not drift, whoever owns it.
func TestValuesDrifted_MatchingValuesNeedNoOwnershipRead(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(t, client)
	expectRelease(client, storedValues(t, installer))

	drifted, err := installer.ValuesDrifted(context.Background())

	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_FailsWhenOwnershipCannotBeRead never turns an unreadable
// ownership record into a verdict.
func TestValuesDrifted_FailsWhenOwnershipCannotBeRead(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	expectRelease(client, map[string]any{"clusterName": "other"})
	client.EXPECT().
		GetReleaseStorageMetadata(mock.Anything, lbcRelease, lbcNamespace).
		Return(nil, errMetadataUnreadable)

	drifted, err := newDriftInstaller(t, client).ValuesDrifted(context.Background())

	require.ErrorIs(t, err, errMetadataUnreadable)
	assert.False(t, drifted)
}
