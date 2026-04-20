package helm_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestParseChartRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chartRef  string
		wantRepo  string
		wantChart string
	}{
		{
			name:      "simple chart name without repo",
			chartRef:  "nginx",
			wantRepo:  "",
			wantChart: "nginx",
		},
		{
			name:      "chart with repo prefix",
			chartRef:  "bitnami/nginx",
			wantRepo:  "bitnami",
			wantChart: "nginx",
		},
		{
			name:      "chart with multiple slashes keeps only first split",
			chartRef:  "bitnami/sub/path",
			wantRepo:  "bitnami",
			wantChart: "sub/path",
		},
		{
			name:      "empty string",
			chartRef:  "",
			wantRepo:  "",
			wantChart: "",
		},
		{
			name:      "single slash",
			chartRef:  "/",
			wantRepo:  "",
			wantChart: "",
		},
		{
			name:      "trailing slash",
			chartRef:  "bitnami/",
			wantRepo:  "bitnami",
			wantChart: "",
		},
		{
			name:      "leading slash",
			chartRef:  "/nginx",
			wantRepo:  "",
			wantChart: "nginx",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			repo, chart := helm.ParseChartRef(testCase.chartRef)

			assert.Equal(t, testCase.wantRepo, repo, "repo")
			assert.Equal(t, testCase.wantChart, chart, "chart")
		})
	}
}

func TestBuildChartPathOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    *helm.ChartSpec
		repoURL string
	}{
		{
			name: "all fields populated",
			spec: &helm.ChartSpec{
				Version:               "1.2.3",
				Username:              "user",
				Password:              "pass",
				CertFile:              "/path/cert",
				KeyFile:               "/path/key",
				CaFile:                "/path/ca",
				InsecureSkipTLSverify: true,
			},
			repoURL: "https://charts.example.com",
		},
		{
			name:    "empty spec",
			spec:    &helm.ChartSpec{},
			repoURL: "",
		},
		{
			name: "version only",
			spec: &helm.ChartSpec{
				Version: "3.0.0",
			},
			repoURL: "https://my-repo.io",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := helm.BuildChartPathOptions(testCase.spec, testCase.repoURL)

			assert.Equal(t, testCase.repoURL, opts.RepoURL)
			assert.Equal(t, testCase.spec.Version, opts.Version)
			assert.Equal(t, testCase.spec.Username, opts.Username)
			assert.Equal(t, testCase.spec.Password, opts.Password)
			assert.Equal(t, testCase.spec.CertFile, opts.CertFile)
			assert.Equal(t, testCase.spec.KeyFile, opts.KeyFile)
			assert.Equal(t, testCase.spec.CaFile, opts.CaFile)
			assert.Equal(t, testCase.spec.InsecureSkipTLSverify, opts.InsecureSkipTLSVerify)
		})
	}
}

func TestApplyChartPathOptions(t *testing.T) {
	t.Parallel()

	t.Run("applies to Install action", func(t *testing.T) {
		t.Parallel()

		install := helm.NewInstallAction()
		opts := helm.BuildChartPathOptions(&helm.ChartSpec{
			Version:  "2.0.0",
			Username: "admin",
			Password: "secret",
		}, "https://repo.io")

		helm.ApplyChartPathOptions(install, opts)

		assert.Equal(t, "https://repo.io", install.RepoURL)
		assert.Equal(t, "2.0.0", install.Version)
		assert.Equal(t, "admin", install.Username)
		assert.Equal(t, "secret", install.Password)
	})

	t.Run("applies to Upgrade action", func(t *testing.T) {
		t.Parallel()

		upgrade := helm.NewUpgradeAction()
		opts := helm.BuildChartPathOptions(&helm.ChartSpec{
			Version: "3.0.0",
			CaFile:  "/ca.pem",
		}, "https://charts.io")

		helm.ApplyChartPathOptions(upgrade, opts)

		assert.Equal(t, "https://charts.io", upgrade.RepoURL)
		assert.Equal(t, "3.0.0", upgrade.Version)
		assert.Equal(t, "/ca.pem", upgrade.CaFile)
	})

	t.Run("no-op for unsupported client type", func(t *testing.T) {
		t.Parallel()

		// Should not panic when passed an unsupported type.
		require.NotPanics(t, func() {
			helm.ApplyChartPathOptions(
				"unsupported",
				helm.BuildChartPathOptions(&helm.ChartSpec{}, ""),
			)
		})
	})
}
