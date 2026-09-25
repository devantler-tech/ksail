package talosprovisioner_test

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schematicState(t *testing.T, ids ...string) state.State {
	t.Helper()

	resourceState := newInMemStateForOmniTest()

	for index, schematicID := range ids {
		ext := runtime.NewExtensionStatus(runtime.NamespaceName, strconv.Itoa(index))
		ext.TypedSpec().Metadata.Name = constants.ImageFactorySchematicExtensionName
		ext.TypedSpec().Metadata.Version = schematicID
		require.NoError(t, resourceState.Create(t.Context(), ext))
	}

	return resourceState
}

func TestSchematicFromState(t *testing.T) {
	t.Parallel()

	schematicID := strings.Repeat("a", 64)
	for _, testCase := range []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{name: "running factory image", ids: []string{schematicID}},
		{name: "missing identity", wantErr: true},
		{name: "empty identity", ids: []string{""}, wantErr: true},
		{name: "malformed identity", ids: []string{"not-a-schematic"}, wantErr: true},
		{name: "ambiguous identity", ids: []string{schematicID, schematicID}, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			resourceState := schematicState(t, testCase.ids...)
			other := runtime.NewExtensionStatus(runtime.NamespaceName, "unrelated")
			other.TypedSpec().Metadata.Name = "iscsi-tools"
			other.TypedSpec().Metadata.Version = "1.0"
			require.NoError(t, resourceState.Create(t.Context(), other))

			got, err := talosprovisioner.SchematicFromStateForTest(t.Context(), resourceState)
			if testCase.wantErr {
				require.ErrorIs(t, err, talosprovisioner.ErrSchematicUndetermined)
				assert.Empty(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, schematicID, got)
			}
		})
	}
}

type failedSchematicState struct {
	state.State

	err error
}

func (s failedSchematicState) List(
	context.Context,
	resource.Kind,
	...state.ListOption,
) (resource.List, error) {
	return resource.List{}, s.err
}

func TestRunningImageMatchesTarget(t *testing.T) {
	t.Parallel()

	oldID := strings.Repeat("a", 64)
	newID := strings.Repeat("b", 64)

	readErr := io.ErrUnexpectedEOF
	for _, testCase := range []struct {
		name    string
		running string
		actual  string
		desired string
		readErr error
		want    bool
	}{
		{name: "same version old image must roll", running: "v1.13.10", actual: oldID, desired: newID},
		{name: "same version new image skips", running: "v1.13.10", actual: newID, desired: newID, want: true},
		{name: "unprefixed version", running: "1.13.10", actual: newID, desired: newID, want: true},
		{name: "old version must roll", running: "v1.13.9", actual: newID, desired: newID},
		{name: "no schematic preserves version-only behavior", running: "v1.13.10", want: true},
		{name: "unknown image is not success", running: "v1.13.10", desired: newID, readErr: readErr},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resourceState := schematicState(t, testCase.actual)
			if testCase.readErr != nil {
				resourceState = failedSchematicState{State: resourceState, err: testCase.readErr}
			}

			got, err := talosprovisioner.RunningImageMatchesTargetForTest(
				t.Context(), resourceState, testCase.running, "v1.13.10", testCase.desired,
			)
			if testCase.readErr != nil {
				require.ErrorIs(t, err, testCase.readErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSchematicsChanged(t *testing.T) {
	t.Parallel()

	readErr := io.ErrUnexpectedEOF
	for _, testCase := range []struct {
		name    string
		ids     []string
		lastErr error
		wantErr error
		want    bool
	}{
		{name: "all matched", ids: []string{"target", "target"}},
		{name: "partial rollout", ids: []string{"target", "old"}, want: true},
		{name: "old first does not hide unknown last", ids: []string{"old", "target"}, lastErr: readErr, wantErr: readErr},
		{name: "empty identity", ids: []string{""}, wantErr: talosprovisioner.ErrSchematicUndetermined},
		{name: "empty inventory", wantErr: clustererr.ErrNoNodesFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			nodes := make([]talosprovisioner.NodeWithRoleForTest, len(testCase.ids))
			calls := 0
			read := func(context.Context, string) (string, error) {
				schematicID := testCase.ids[calls]

				calls++
				if calls == len(testCase.ids) {
					return schematicID, testCase.lastErr
				}

				return schematicID, nil
			}

			got, err := talosprovisioner.SchematicsChangedForTest(
				t.Context(),
				nodes,
				"target",
				read,
			)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, testCase.want, got)
			assert.Len(t, testCase.ids, calls)
		})
	}
}

func TestDistributionImageChangedSkipsUnmanagedImages(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		hetzner   bool
		omni      bool
		schematic string
	}{
		{name: "Docker cannot roll factory images", schematic: strings.Repeat("a", 64)},
		{name: "Omni owns its rollouts", hetzner: true, omni: true, schematic: strings.Repeat("a", 64)},
		{name: "no configured schematic", hetzner: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner := talosprovisioner.NewProvisioner(nil, nil).
				WithTalosOptions(v1alpha1.OptionsTalos{SchematicID: testCase.schematic})
			if testCase.hetzner {
				provisioner.WithHetznerOptions(v1alpha1.OptionsHetzner{})
			}

			if testCase.omni {
				provisioner.WithOmniOptions(v1alpha1.OptionsOmni{})
			}

			changed, err := provisioner.DistributionImageChanged(t.Context(), "test")
			require.NoError(t, err)
			assert.False(t, changed)
		})
	}
}
