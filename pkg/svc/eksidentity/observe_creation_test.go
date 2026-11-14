package eksidentity_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	creationTagKey  = "ksail.io/cluster-creation-id"
	creationCluster = "observe-creation"
	creationARN     = "arn:aws:eks:eu-north-1:123456789012:cluster/observe-creation"
)

// TestObserveCreationRequiresTheAttemptTag binds a failed create's claim to the creation tag that
// only that attempt wrote, read from the same response as the identity it returns.
func TestObserveCreationRequiresTheAttemptTag(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)

	testCases := []struct {
		name    string
		tags    map[string]string
		wantErr bool
	}{
		{name: "matching tag", tags: map[string]string{creationTagKey: "attempt-1"}},
		{name: "no tags", tags: nil, wantErr: true},
		{
			name:    "another attempt",
			tags:    map[string]string{creationTagKey: "attempt-2"},
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := eksCluster(creationCluster, creationARN, createdAt)
			cluster.Tags = testCase.tags
			client := &fakeClient{accountID: "123456789012", cluster: cluster}

			got, err := eksidentity.ObserveCreation(
				t.Context(), client, creationCluster, "eu-north-1", creationTagKey, "attempt-1",
			)
			if testCase.wantErr {
				require.ErrorIs(t, err, eksidentity.ErrCreationUnattributed)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, creationARN, got.ClusterARN)
			assert.Equal(t, createdAt, got.CreatedAt)
		})
	}
}

// TestObserveCreationRefusesAnEmptyIdentifier never matches an untagged cluster to a missing ID.
func TestObserveCreationRefusesAnEmptyIdentifier(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		accountID: "123456789012",
		cluster:   eksCluster(creationCluster, creationARN, time.Now()),
	}

	_, err := eksidentity.ObserveCreation(
		t.Context(), client, creationCluster, "eu-north-1", creationTagKey, "",
	)
	require.ErrorIs(t, err, eksidentity.ErrCreationUnattributed)
	assert.Zero(t, client.describeCalls)
}
