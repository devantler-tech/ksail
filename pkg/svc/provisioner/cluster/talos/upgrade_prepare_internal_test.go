package talosprovisioner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var errNodeLookupAccessDenied = errors.New("access denied")

func TestPrepareNodeForImageUpgradeRejectsLookupFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
	}{
		{
			name: "forbidden",
			cause: apierrors.NewForbidden(
				schema.GroupResource{Resource: "nodes"}, "", errNodeLookupAccessDenied,
			),
		},
		{
			name:  "throttled",
			cause: apierrors.NewTooManyRequests("temporarily overloaded", 1),
		},
		{name: "deadline", cause: context.DeadlineExceeded},
		{name: "canceled", cause: context.Canceled},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clientset := fake.NewClientset()
			clientset.PrependReactor(
				"list",
				"nodes",
				func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.cause
				},
			)

			prov := NewProvisioner(nil, NewOptions())
			nodeIP := "192.0.2.1"

			nodeName, err := prov.prepareNodeForImageUpgrade(t.Context(), clientset, nodeIP)

			assert.Empty(t, nodeName)
			require.ErrorIs(
				t,
				err,
				testCase.cause,
				"a failed lookup must stop the upgrade before draining or rebooting",
			)
			require.ErrorContains(t, err, nodeIP)

			actions := clientset.Actions()
			require.Len(t, actions, 1, "a lookup failure must not mutate cluster resources")
			assert.True(t, actions[0].Matches("list", "nodes"))
		})
	}
}

func TestPrepareNodeForImageUpgradeAllowsMissingNode(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	prov := NewProvisioner(nil, NewOptions())

	nodeName, err := prov.prepareNodeForImageUpgrade(t.Context(), clientset, "192.0.2.1")

	require.NoError(
		t,
		err,
		"a successful lookup with no matching node retains the recovery fallback",
	)
	assert.Empty(t, nodeName)

	actions := clientset.Actions()
	require.Len(t, actions, 1)
	assert.True(t, actions[0].Matches("list", "nodes"))
}
