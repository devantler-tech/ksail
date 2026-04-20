package helm_test

import (
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

var errHelmInstallFailed = errors.New("install failed")

//nolint:funlen // Table-driven release conversion cases are clearer inline.
func TestReleaseToInfo(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		release  func() any // returns *v1.Release via NewTestRelease, or nil
		wantInfo *helm.ReleaseInfo
	}{
		{
			name: "nil release returns nil",
			release: func() any {
				return nil
			},
			wantInfo: nil,
		},
		{
			name: "fully populated release",
			release: func() any {
				return helm.NewTestRelease(
					"my-release",
					"my-namespace",
					"nginx",
					"1.25.0",
					"Install complete",
					releasecommon.StatusDeployed,
					3,
					now,
				)
			},
			wantInfo: &helm.ReleaseInfo{
				Name:       "my-release",
				Namespace:  "my-namespace",
				Revision:   3,
				Status:     "deployed",
				Chart:      "nginx",
				AppVersion: "1.25.0",
				Updated:    now,
				Notes:      "Install complete",
			},
		},
		{
			name: "release with empty notes",
			release: func() any {
				return helm.NewTestRelease(
					"test",
					"default",
					"chart",
					"2.0.0",
					"",
					releasecommon.StatusDeployed,
					1,
					now,
				)
			},
			wantInfo: &helm.ReleaseInfo{
				Name:       "test",
				Namespace:  "default",
				Revision:   1,
				Status:     "deployed",
				Chart:      "chart",
				AppVersion: "2.0.0",
				Updated:    now,
				Notes:      "",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rel := testCase.release()

			var got *helm.ReleaseInfo
			if rel == nil {
				got = helm.ReleaseToInfo(nil)
			} else {
				release, ok := rel.(*v1.Release)
				require.True(t, ok)

				got = helm.ReleaseToInfo(release)
			}

			assert.Equal(t, testCase.wantInfo, got)
		})
	}
}

func TestExecuteAndExtractRelease(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("successful extraction", func(t *testing.T) {
		t.Parallel()

		expected := helm.NewTestRelease(
			"test", "ns", "chart", "1.0", "notes",
			releasecommon.StatusDeployed, 1, now,
		)
		runFn := func() (any, error) {
			return expected, nil
		}

		rel, err := helm.ExecuteAndExtractRelease(runFn)

		require.NoError(t, err)
		require.NotNil(t, rel)
		assert.Equal(t, "test", rel.Name)
	})

	t.Run("runFn returns error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errHelmInstallFailed
		runFn := func() (any, error) {
			return nil, expectedErr
		}

		rel, err := helm.ExecuteAndExtractRelease(runFn)

		require.Error(t, err)
		assert.Nil(t, rel)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("unexpected release type", func(t *testing.T) {
		t.Parallel()

		runFn := func() (any, error) {
			return "not-a-release", nil
		}

		rel, err := helm.ExecuteAndExtractRelease(runFn)

		require.Error(t, err)
		assert.Nil(t, rel)
		assert.ErrorIs(t, err, helm.ErrUnexpectedReleaseType)
	})
}
