package helm_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/pkg/client/helm"
	"github.com/stretchr/testify/require"
)

func TestChartSpec_DefaultValues(t *testing.T) {
	t.Parallel()

	spec := &helm.ChartSpec{
		ReleaseName: "test-release",
		ChartName:   "test-chart",
		Namespace:   "default",
	}

	require.Equal(t, "test-release", spec.ReleaseName)
	require.Equal(t, "test-chart", spec.ChartName)
	require.Equal(t, "default", spec.Namespace)
	require.False(t, spec.CreateNamespace)
	require.False(t, spec.Atomic)
	require.False(t, spec.Wait)
	require.False(t, spec.WaitForJobs)
	require.Equal(t, time.Duration(0), spec.Timeout)
	require.False(t, spec.Silent)
}

func TestChartSpec_WithValues(t *testing.T) {
	t.Parallel()

	spec := &helm.ChartSpec{
		ReleaseName:     "my-release",
		ChartName:       "my-chart",
		Namespace:       "my-namespace",
		Version:         "1.0.0",
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         5 * time.Minute,
		Silent:          true,
		UpgradeCRDs:     true,
		ValuesYaml:      "key: value",
		ValueFiles:      []string{"values.yaml"},
		SetValues: map[string]string{
			"replicas": "3",
		},
	}

	require.Equal(t, "my-release", spec.ReleaseName)
	require.Equal(t, "my-chart", spec.ChartName)
	require.Equal(t, "my-namespace", spec.Namespace)
	require.Equal(t, "1.0.0", spec.Version)
	require.True(t, spec.CreateNamespace)
	require.True(t, spec.Atomic)
	require.True(t, spec.Wait)
	require.True(t, spec.WaitForJobs)
	require.Equal(t, 5*time.Minute, spec.Timeout)
	require.True(t, spec.Silent)
	require.True(t, spec.UpgradeCRDs)
	require.Equal(t, "key: value", spec.ValuesYaml)
	require.Equal(t, []string{"values.yaml"}, spec.ValueFiles)
	require.Equal(t, map[string]string{"replicas": "3"}, spec.SetValues)
}

func TestRepositoryEntry_DefaultValues(t *testing.T) {
	t.Parallel()

	entry := &helm.RepositoryEntry{
		Name: "test-repo",
		URL:  "https://charts.example.com",
	}

	require.Equal(t, "test-repo", entry.Name)
	require.Equal(t, "https://charts.example.com", entry.URL)
	require.Empty(t, entry.Username)
	require.Empty(t, entry.Password)
	require.False(t, entry.InsecureSkipTLSverify)
	require.False(t, entry.PlainHTTP)
}

func TestRepositoryEntry_WithAuthentication(t *testing.T) {
	t.Parallel()

	entry := &helm.RepositoryEntry{
		Name:                  "secure-repo",
		URL:                   "https://charts.secure.com",
		Username:              "user",
		Password:              "pass",
		CertFile:              "/path/to/cert",
		KeyFile:               "/path/to/key",
		CaFile:                "/path/to/ca",
		InsecureSkipTLSverify: true,
		PlainHTTP:             false,
	}

	require.Equal(t, "secure-repo", entry.Name)
	require.Equal(t, "https://charts.secure.com", entry.URL)
	require.Equal(t, "user", entry.Username)
	require.Equal(t, "pass", entry.Password)
	require.Equal(t, "/path/to/cert", entry.CertFile)
	require.Equal(t, "/path/to/key", entry.KeyFile)
	require.Equal(t, "/path/to/ca", entry.CaFile)
	require.True(t, entry.InsecureSkipTLSverify)
	require.False(t, entry.PlainHTTP)
}

func TestReleaseInfo_Structure(t *testing.T) {
	t.Parallel()

	now := time.Now()
	info := &helm.ReleaseInfo{
		Name:       "my-release",
		Namespace:  "default",
		Revision:   1,
		Status:     "deployed",
		Chart:      "my-chart",
		AppVersion: "1.0.0",
		Updated:    now,
		Notes:      "Installation notes",
	}

	require.Equal(t, "my-release", info.Name)
	require.Equal(t, "default", info.Namespace)
	require.Equal(t, 1, info.Revision)
	require.Equal(t, "deployed", info.Status)
	require.Equal(t, "my-chart", info.Chart)
	require.Equal(t, "1.0.0", info.AppVersion)
	require.Equal(t, now, info.Updated)
	require.Equal(t, "Installation notes", info.Notes)
}

func TestDefaultTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Minute, helm.DefaultTimeout)
}
