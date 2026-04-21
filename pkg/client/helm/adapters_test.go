package helm_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestApplyCommonActionConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		spec             *helm.ChartSpec
		wantWaitStrategy helmv4kube.WaitStrategy
		wantWaitForJobs  bool
		wantTimeout      time.Duration
		wantVersion      string
	}{
		{
			name: "wait enabled uses StatusWatcherStrategy",
			spec: &helm.ChartSpec{
				Wait:        true,
				WaitForJobs: true,
				Timeout:     10 * time.Minute,
				Version:     "1.0.0",
			},
			wantWaitStrategy: helmv4kube.StatusWatcherStrategy,
			wantWaitForJobs:  true,
			wantTimeout:      10 * time.Minute,
			wantVersion:      "1.0.0",
		},
		{
			name: "wait disabled uses HookOnlyStrategy",
			spec: &helm.ChartSpec{
				Wait:        false,
				WaitForJobs: false,
				Timeout:     3 * time.Minute,
				Version:     "2.0.0",
			},
			wantWaitStrategy: helmv4kube.HookOnlyStrategy,
			wantWaitForJobs:  false,
			wantTimeout:      3 * time.Minute,
			wantVersion:      "2.0.0",
		},
		{
			name: "zero timeout defaults to DefaultTimeout",
			spec: &helm.ChartSpec{
				Wait:    false,
				Timeout: 0,
			},
			wantWaitStrategy: helmv4kube.HookOnlyStrategy,
			wantWaitForJobs:  false,
			wantTimeout:      helm.DefaultTimeout,
			wantVersion:      "",
		},
		{
			name: "wait with WaitForJobs false",
			spec: &helm.ChartSpec{
				Wait:        true,
				WaitForJobs: false,
				Timeout:     7 * time.Minute,
				Version:     "3.0.0-rc1",
			},
			wantWaitStrategy: helmv4kube.StatusWatcherStrategy,
			wantWaitForJobs:  false,
			wantTimeout:      7 * time.Minute,
			wantVersion:      "3.0.0-rc1",
		},
	}

	for _, tc := range tests { //nolint:varnamelen
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			action := &helm.TestableActionConfig{}

			helm.ApplyCommonActionConfig(action, tc.spec)

			assert.Equal(t, tc.wantWaitStrategy, action.WaitStrategy, "WaitStrategy")
			assert.Equal(t, tc.wantWaitForJobs, action.WaitForJobs, "WaitForJobs")
			assert.Equal(t, tc.wantTimeout, action.Timeout, "Timeout")
			assert.Equal(t, tc.wantVersion, action.Version, "Version")
		})
	}
}
