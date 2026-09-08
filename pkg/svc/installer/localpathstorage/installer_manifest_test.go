package localpathstorageinstaller_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/localpathstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newManifestServer serves the checked-in local-path-storage fixture and reports how
// many times it was called, so a test can prove the installer fetched from here and
// not from the upstream host.
func newManifestServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	manifest, err := os.ReadFile(filepath.Join("testdata", "local-path-storage.yaml"))
	require.NoError(t, err, "fixture manifest must be readable")

	var hits atomic.Int64

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			hits.Add(1)

			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write(manifest)
		}),
	)
	t.Cleanup(server.Close)

	return server, &hits
}

// newFixtureInstallerWithTimeout returns an installer for distribution that reads its
// manifest from a local test server rather than the network.
func newFixtureInstallerWithTimeout(
	t *testing.T,
	distribution v1alpha1.Distribution,
	timeout time.Duration,
) (*localpathstorageinstaller.Installer, *atomic.Int64) {
	t.Helper()

	server, hits := newManifestServer(t)

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		timeout,
		distribution,
	)
	localpathstorageinstaller.SetManifestURLForTest(installer, server.URL)

	return installer, hits
}

// newFixtureInstaller returns an installer for distribution that reads its manifest
// from a local test server rather than the network.
func newFixtureInstaller(
	t *testing.T,
	distribution v1alpha1.Distribution,
) (*localpathstorageinstaller.Installer, *atomic.Int64) {
	t.Helper()

	return newFixtureInstallerWithTimeout(t, distribution, 30*time.Second)
}

func TestInstaller_Images_ServesManifestFromFixture(t *testing.T) {
	t.Parallel()

	installer, hits := newFixtureInstaller(t, v1alpha1.DistributionVanilla)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1), hits.Load(), "manifest must come from the test server, not the network")
	assert.Contains(t, images, "docker.io/rancher/local-path-provisioner:v0.0.37")
	assert.Contains(t, images, "docker.io/library/busybox:latest")
}

func TestInstaller_Images_PropagatesNonOKStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNotFound)
		}),
	)
	t.Cleanup(server.Close)

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVanilla,
	)
	localpathstorageinstaller.SetManifestURLForTest(installer, server.URL)

	images, err := installer.Images(context.Background())

	require.ErrorIs(t, err, localpathstorageinstaller.ErrManifestFetchFailed)
	assert.Empty(t, images)
}

func TestInstaller_ManifestURLDefaultsToUpstream(t *testing.T) {
	t.Parallel()

	installer := localpathstorageinstaller.NewInstaller(
		"/path/to/kubeconfig",
		"test-context",
		30*time.Second,
		v1alpha1.DistributionVanilla,
	)

	version := localpathstorageinstaller.LocalPathProvisionerVersionForTest()
	require.NotEmpty(t, version, "the Dockerfile pin must resolve to a version")

	assert.Equal(
		t,
		"https://raw.githubusercontent.com/rancher/local-path-provisioner/"+
			version+"/deploy/local-path-storage.yaml",
		localpathstorageinstaller.ManifestURLForTest(installer),
		"the production default must track the version pinned in the embedded Dockerfile",
	)
}
