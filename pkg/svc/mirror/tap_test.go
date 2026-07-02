package mirror_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTarget builds a resolved Target with the given pods and containers, in the
// test namespace, for the SelectTapPoint cases.
func newTarget(pods, containers []string) *mirror.Target {
	return &mirror.Target{
		Namespace:  testNamespace,
		Deployment: testDeploy,
		Pods:       pods,
		Containers: containers,
	}
}

func TestSelectTapPointNilTarget(t *testing.T) {
	t.Parallel()

	point, err := mirror.SelectTapPoint(nil, "")

	require.ErrorIs(t, err, mirror.ErrTargetNil)
	assert.Nil(t, point)
}

func TestSelectTapPointEmptyPods(t *testing.T) {
	t.Parallel()

	point, err := mirror.SelectTapPoint(newTarget(nil, []string{"api"}), "")

	require.ErrorIs(t, err, mirror.ErrNoRunningPods)
	assert.Nil(t, point)
}

func TestSelectTapPointEmptyContainers(t *testing.T) {
	t.Parallel()

	point, err := mirror.SelectTapPoint(newTarget([]string{"api-0"}, nil), "")

	require.ErrorIs(t, err, mirror.ErrDeploymentNoContainers)
	assert.Nil(t, point)
}

// containerSelectionCase is one SelectTapPoint container-resolution scenario.
type containerSelectionCase struct {
	name       string
	containers []string
	requested  string
	wantErr    error
	wantPicked string
}

// containerSelectionCases enumerates the container-resolution branches; it is a
// function (not a package var) to honour the no-globals rule.
func containerSelectionCases() []containerSelectionCase {
	return []containerSelectionCase{
		{name: "sole container, none requested", containers: []string{"api"}, wantPicked: "api"},
		{
			name:       "sole container, requested by name",
			containers: []string{"api"},
			requested:  "api",
			wantPicked: "api",
		},
		{
			name:       "multiple, requested by name",
			containers: []string{"api", "sidecar"},
			requested:  "sidecar",
			wantPicked: "sidecar",
		},
		{
			name:       "multiple, none requested is ambiguous",
			containers: []string{"api", "sidecar"},
			wantErr:    mirror.ErrAmbiguousContainer,
		},
		{
			name:       "requested container absent",
			containers: []string{"api", "sidecar"},
			requested:  "nope",
			wantErr:    mirror.ErrContainerNotFound,
		},
	}
}

func TestSelectTapPointContainerSelection(t *testing.T) {
	t.Parallel()

	for _, testCase := range containerSelectionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			point, err := mirror.SelectTapPoint(
				newTarget([]string{"api-0", "api-1"}, testCase.containers),
				testCase.requested,
			)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
				assert.Nil(t, point)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, point)
			assert.Equal(t, testCase.wantPicked, point.Container)
		})
	}
}

// TestSelectTapPointResolvesFirstRunningPod pins the documented defaults: the tap
// point carries the namespace and the first pod of the Target.
func TestSelectTapPointResolvesFirstRunningPod(t *testing.T) {
	t.Parallel()

	point, err := mirror.SelectTapPoint(
		newTarget([]string{"api-0", "api-1"}, []string{"api"}),
		"",
	)

	require.NoError(t, err)
	require.NotNil(t, point)
	assert.Equal(t, testNamespace, point.Namespace)
	assert.Equal(t, "api-0", point.Pod)
	assert.Equal(t, "api", point.Container)
}
