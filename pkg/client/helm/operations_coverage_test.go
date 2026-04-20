package helm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	errTestTransient = errors.New("get \"https://charts.example.com\": dial tcp: i/o timeout")
	errRepoAddFailed = errors.New("repo add failed")
	errChartNotFound = errors.New("chart not found")
	errInvalidChart  = errors.New("invalid chart")
)

// ---------------------------------------------------------------------------
// InstallOrUpgradeChart (package-level helper)
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive cases
func TestInstallOrUpgradeChart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(m *helm.MockInterface)
		repoConfig    helm.RepoConfig
		chartConfig   helm.ChartConfig
		timeout       time.Duration
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "success",
			setupMock: func(m *helm.MockInterface) {
				m.On("AddRepository", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(&helm.ReleaseInfo{Name: "test"}, nil)
			},
			repoConfig: helm.RepoConfig{
				Name:     "test-repo",
				URL:      "https://charts.example.com",
				RepoName: "Test",
			},
			chartConfig: helm.ChartConfig{
				ReleaseName: "test",
				ChartName:   "test-chart",
				Namespace:   "default",
			},
			timeout: 30 * time.Second,
		},
		{
			name: "add repository failure",
			setupMock: func(m *helm.MockInterface) {
				m.On("AddRepository", mock.Anything, mock.Anything, mock.Anything).
					Return(errRepoAddFailed)
			},
			repoConfig: helm.RepoConfig{
				Name:     "bad-repo",
				URL:      "https://bad.example.com",
				RepoName: "Bad",
			},
			chartConfig:   helm.ChartConfig{ReleaseName: "test", ChartName: "test-chart"},
			timeout:       30 * time.Second,
			wantErr:       true,
			wantErrSubstr: "failed to add Bad repository",
		},
		{
			name: "install failure non-retryable",
			setupMock: func(m *helm.MockInterface) {
				m.On("AddRepository", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(nil, errChartNotFound)
			},
			repoConfig: helm.RepoConfig{
				Name:     "repo",
				URL:      "https://charts.example.com",
				RepoName: "Test",
			},
			chartConfig:   helm.ChartConfig{ReleaseName: "test", ChartName: "missing"},
			timeout:       30 * time.Second,
			wantErr:       true,
			wantErrSubstr: "failed to install Test chart",
		},
		{
			name: "respects SkipWait option",
			setupMock: func(m *helm.MockInterface) {
				m.On("AddRepository", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				m.On("InstallOrUpgradeChart", mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
					return !spec.Wait && !spec.WaitForJobs
				})).
					Return(&helm.ReleaseInfo{Name: "test"}, nil)
			},
			repoConfig: helm.RepoConfig{
				Name:     "repo",
				URL:      "https://example.com",
				RepoName: "Test",
			},
			chartConfig: helm.ChartConfig{ReleaseName: "test", ChartName: "chart", SkipWait: true},
			timeout:     30 * time.Second,
		},
		{
			name: "sets SetJSONVals on spec",
			setupMock: func(m *helm.MockInterface) {
				m.On("AddRepository", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				m.On("InstallOrUpgradeChart", mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
					return spec.SetJSONVals["key"] == `{"val":1}`
				})).
					Return(&helm.ReleaseInfo{Name: "test"}, nil)
			},
			repoConfig: helm.RepoConfig{
				Name:     "repo",
				URL:      "https://example.com",
				RepoName: "Test",
			},
			chartConfig: helm.ChartConfig{
				ReleaseName: "test",
				ChartName:   "chart",
				SetJSONVals: map[string]string{"key": `{"val":1}`},
			},
			timeout: 30 * time.Second,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			testCase.setupMock(mockClient)

			err := helm.InstallOrUpgradeChart(
				context.Background(),
				mockClient,
				testCase.repoConfig,
				testCase.chartConfig,
				testCase.timeout,
			)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// InstallChartWithRetry
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive retry scenarios
func TestInstallChartWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(m *helm.MockInterface)
		ctx           func() (context.Context, context.CancelFunc)
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "success on first attempt",
			setupMock: func(m *helm.MockInterface) {
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(&helm.ReleaseInfo{Name: "test"}, nil).Once()
			},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
		},
		{
			name: "non-retryable error fails immediately",
			setupMock: func(m *helm.MockInterface) {
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(nil, errInvalidChart).Once()
			},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantErr:       true,
			wantErrSubstr: "failed to install",
		},
		{
			name: "transient error retries and succeeds",
			setupMock: func(m *helm.MockInterface) {
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(nil, errTestTransient).Once()
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(&helm.ReleaseInfo{Name: "test"}, nil).Once()
			},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
		},
		{
			name: "context cancellation during retry",
			setupMock: func(m *helm.MockInterface) {
				m.On("InstallOrUpgradeChart", mock.Anything, mock.Anything).
					Return(nil, errTestTransient).Maybe()
			},
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // cancel immediately

				return ctx, cancel
			},
			wantErr:       true,
			wantErrSubstr: "chart install retry cancelled",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			testCase.setupMock(mockClient)

			ctx, cancel := testCase.ctx()
			defer cancel()

			spec := &helm.ChartSpec{
				ReleaseName: "test",
				ChartName:   "test-chart",
				Namespace:   "default",
			}

			err := helm.InstallChartWithRetry(ctx, mockClient, spec, "test-repo")

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
