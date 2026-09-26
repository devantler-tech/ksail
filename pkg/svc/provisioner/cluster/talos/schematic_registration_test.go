package talosprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFactoryUnavailable = errors.New("image factory unavailable")

type fakeRegistrar struct {
	returnID string
	err      error
	calls    []talosconfigmanager.Schematic
}

func (r *fakeRegistrar) register(
	_ context.Context,
	computed talosconfigmanager.Schematic,
) (string, error) {
	r.calls = append(r.calls, computed)

	return r.returnID, r.err
}

func extensionConfigs(t *testing.T, extensions []string) *talosconfigmanager.Configs {
	t.Helper()

	configs, err := talosconfigmanager.
		NewConfigManager("", "schematic-register", "1.32.0", "10.5.0.0/24").
		WithExtensions(extensions).
		Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

// TestEnsureSchematicRegisteredSendsTheComputedSchematic pins #7132: Image Factory serves
// images for a computed schematic only after it has been sent the schematic body.
func TestEnsureSchematicRegisteredSendsTheComputedSchematic(t *testing.T) {
	t.Parallel()

	configs := extensionConfigs(t, []string{"siderolabs/iscsi-tools"})
	registrar := &fakeRegistrar{returnID: configs.SchematicID()}
	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithLogWriter(io.Discard).
		WithSchematicRegistrarForTest(registrar.register)

	err := prov.EnsureSchematicRegisteredForTest(t.Context(), configs.SchematicID())

	require.NoError(t, err)
	require.Len(t, registrar.calls, 1)

	sentID, err := registrar.calls[0].ID()
	require.NoError(t, err)
	assert.Equal(t, configs.SchematicID(), sentID, "the body sent must hash to the ID in use")
}

func TestEnsureSchematicRegisteredFailsClosed(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		registrar fakeRegistrar
		wantErr   error
	}{
		{
			name:      "factory stores it under another ID",
			registrar: fakeRegistrar{returnID: "other"},
			wantErr:   talosprovisioner.ErrSchematicIDMismatch,
		},
		{
			name:      "factory unavailable",
			registrar: fakeRegistrar{err: errFactoryUnavailable},
			wantErr:   errFactoryUnavailable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configs := extensionConfigs(t, []string{"siderolabs/iscsi-tools"})
			registrar := testCase.registrar
			prov := talosprovisioner.NewProvisioner(configs, nil).
				WithLogWriter(io.Discard).
				WithSchematicRegistrarForTest(registrar.register)

			err := prov.EnsureSchematicRegisteredForTest(t.Context(), configs.SchematicID())

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

func TestEnsureSchematicRegisteredLeavesOtherSchematicsAlone(t *testing.T) {
	t.Parallel()

	const explicitID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	for _, testCase := range []struct {
		name       string
		extensions []string
		inUseID    string
	}{
		{
			name:       "explicit schematicId KSail did not compute",
			extensions: []string{"siderolabs/iscsi-tools"},
			inUseID:    explicitID,
		},
		{name: "no extensions configured", inUseID: explicitID},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registrar := &fakeRegistrar{err: errFactoryUnavailable}
			prov := talosprovisioner.NewProvisioner(extensionConfigs(t, testCase.extensions), nil).
				WithLogWriter(io.Discard).
				WithTalosOptsForTest(&v1alpha1.OptionsTalos{SchematicID: explicitID}).
				WithSchematicRegistrarForTest(registrar.register)

			err := prov.EnsureSchematicRegisteredForTest(t.Context(), testCase.inUseID)

			require.NoError(t, err)
			assert.Empty(t, registrar.calls)
		})
	}
}

// TestUpgradeDistributionRegistersBeforeRollingNodes pins that a Talos upgrade registers the
// computed schematic before any node pulls its installer, and stops there when it cannot.
func TestUpgradeDistributionRegistersBeforeRollingNodes(t *testing.T) {
	t.Parallel()

	registrar := &fakeRegistrar{err: errFactoryUnavailable}
	prov := talosprovisioner.NewProvisioner(extensionConfigs(t, []string{"siderolabs/iscsi-tools"}), nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{}).
		WithSchematicRegistrarForTest(registrar.register)

	err := prov.UpgradeDistribution(t.Context(), "test", "v1.13.3", "v1.13.4")

	require.ErrorIs(t, err, errFactoryUnavailable)
	assert.Len(t, registrar.calls, 1)
}
