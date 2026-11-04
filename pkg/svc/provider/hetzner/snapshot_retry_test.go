package hetzner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errDownloadExited is what the snapshot uploader reports when the command that downloads
// the Talos image on the temporary server exits non-zero. The prefix is the message
// hcloud-upload-image wraps around that step.
var errDownloadExited = errors.New(
	"failed to download and write the image: Process exited with status 1",
)

// errProbeTimeout stands in for an Image Factory probe request that never got an answer.
var errProbeTimeout = errors.New("dial tcp: i/o timeout")

// sequenceUploader returns its results in order, one per Upload call, and records the
// options of each call. Calls past the last result repeat the last one.
type sequenceUploader struct {
	mu      sync.Mutex
	results []uploadOutcome
	calls   []hcloudimages.UploadOptions
}

type uploadOutcome struct {
	image *hcloud.Image
	err   error
}

func (u *sequenceUploader) Upload(
	_ context.Context, opts hcloudimages.UploadOptions,
) (*hcloud.Image, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.calls = append(u.calls, opts)
	outcome := u.results[min(len(u.calls), len(u.results))-1]

	return outcome.image, outcome.err
}

func (u *sequenceUploader) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return len(u.calls)
}

func (u *sequenceUploader) buildLabels() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	labels := make([]string, 0, len(u.calls))
	for _, call := range u.calls {
		labels = append(labels, call.Labels[hetzner.LabelTalosSnapshotBuild])
	}

	return labels
}

func TestSnapshotManager_EnsureTalosSnapshot_RetriesTransientDownloadFailure(t *testing.T) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{
		{err: errDownloadExited},
		{image: &hcloud.Image{ID: 77}},
	}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)

	var logBuf bytes.Buffer

	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	imageID, err := manager.EnsureTalosSnapshot(
		t.Context(),
		"retry-cluster",
		"v1.9.0",
		"abc123",
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, int64(77), imageID)
	assert.Equal(t, 2, uploader.callCount())
	assert.ElementsMatch(
		t,
		[]string{buildServerPath, buildSSHKeyPath},
		api.deletedPaths(),
		"the failed attempt's temporary server and SSH key are deleted before the retry",
	)

	labels := uploader.buildLabels()
	require.Len(t, labels, 2)
	assert.Equal(t, labels[0], labels[1], "every attempt carries the same build label")
	assert.Contains(t, logBuf.String(), "Talos image download failed (attempt 1 of 3)")
}

func TestSnapshotManager_EnsureTalosSnapshot_DownloadFailureOutlastingTheBound(t *testing.T) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{{err: errDownloadExited}}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	_, err := manager.EnsureTalosSnapshot(t.Context(), "retry-cluster", "v1.9.0", "abc123", nil)

	require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
	require.ErrorIs(t, err, errDownloadExited)
	require.ErrorContains(
		t,
		err,
		"downloading the Talos image from the Image Factory failed 3 times",
	)
	assert.Equal(t, 3, uploader.callCount())
	assert.Len(
		t,
		api.deletedPaths(),
		6,
		"the server and SSH key of every attempt are deleted, the last one included",
	)
}

func TestSnapshotManager_EnsureTalosSnapshot_DoesNotRetryOtherFailures(t *testing.T) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{
		{err: fmt.Errorf("creating the temporary server failed: %w", errServerQuotaExceeded)},
		{image: &hcloud.Image{ID: 77}},
	}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	_, err := manager.EnsureTalosSnapshot(t.Context(), "retry-cluster", "v1.9.0", "abc123", nil)

	require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
	require.ErrorIs(t, err, errServerQuotaExceeded)
	assert.Equal(t, 1, uploader.callCount())
	assert.Empty(t, api.deletedPaths())
}

func TestSnapshotManager_EnsureTalosSnapshot_DoesNotRetryAMissingImage(t *testing.T) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{
		{err: errDownloadExited},
		{image: &hcloud.Image{ID: 77}},
	}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	var probed []string

	manager.SetImageProbeForTest(func(_ context.Context, imageURL *url.URL) (int, error) {
		probed = append(probed, imageURL.String())

		return http.StatusNotFound, nil
	})

	_, err := manager.EnsureTalosSnapshot(t.Context(), "retry-cluster", "v9.99.0", "abc123", nil)

	require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
	require.ErrorIs(t, err, errDownloadExited)
	require.ErrorContains(t, err, "HTTP 404")
	assert.Equal(t, 1, uploader.callCount())
	assert.Equal(
		t,
		[]string{"https://factory.talos.dev/image/abc123/v9.99.0/hcloud-amd64.raw.xz"},
		probed,
	)
	assert.ElementsMatch(
		t,
		[]string{buildServerPath, buildSSHKeyPath},
		api.deletedPaths(),
		"the failed attempt's temporary server and SSH key are deleted when the build stops",
	)
}

func TestSnapshotManager_EnsureTalosSnapshot_CancelledWhileAMissingImageIsReported(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	uploader := &sequenceUploader{results: []uploadOutcome{{err: errDownloadExited}}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)
	manager.SetImageProbeForTest(func(context.Context, *url.URL) (int, error) {
		cancel()

		return http.StatusNotFound, nil
	})

	_, err := manager.EnsureTalosSnapshot(ctx, "retry-cluster", "v9.99.0", "abc123", nil)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, errDownloadExited)
	assert.Equal(t, 1, uploader.callCount())
	assert.ElementsMatch(t, []string{buildServerPath, buildSSHKeyPath}, api.deletedPaths())
}

func TestSnapshotManager_EnsureTalosSnapshot_ReportsAFailedCleanupWhenTheBuildStops(t *testing.T) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{{err: errDownloadExited}}}
	client := newBuildResourcesAPI(t, &buildResourcesAPI{failListing: true})
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)
	manager.SetImageProbeForTest(func(context.Context, *url.URL) (int, error) {
		return http.StatusNotFound, nil
	})

	_, err := manager.EnsureTalosSnapshot(t.Context(), "retry-cluster", "v9.99.0", "abc123", nil)

	require.ErrorIs(t, err, errDownloadExited)
	require.ErrorContains(t, err, "HTTP 404")
	require.ErrorContains(t, err, "failed to list temporary snapshot build servers")
}

func TestSnapshotManager_EnsureTalosSnapshot_RetriesWhenTheImageProbeFails(t *testing.T) {
	t.Parallel()

	for _, probe := range []struct {
		name   string
		status int
		err    error
	}{
		{name: "probe request fails", err: errProbeTimeout},
		{name: "factory error", status: http.StatusBadGateway},
		{name: "rate limited", status: http.StatusTooManyRequests},
		{name: "method not allowed", status: http.StatusMethodNotAllowed},
		{name: "forbidden by a proxy", status: http.StatusForbidden},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Parallel()

			uploader := &sequenceUploader{results: []uploadOutcome{
				{err: errDownloadExited},
				{image: &hcloud.Image{ID: 77}},
			}}
			client := newBuildResourcesAPI(t, &buildResourcesAPI{})
			manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)
			manager.SetImageProbeForTest(func(context.Context, *url.URL) (int, error) {
				return probe.status, probe.err
			})

			imageID, err := manager.EnsureTalosSnapshot(
				t.Context(), "retry-cluster", "v1.9.0", "abc123", nil,
			)

			require.NoError(t, err)
			assert.Equal(t, int64(77), imageID)
			assert.Equal(t, 2, uploader.callCount())
		})
	}
}

func TestSnapshotManager_EnsureTalosSnapshot_StopsWhenTheFailedAttemptCannotBeCleanedUp(
	t *testing.T,
) {
	t.Parallel()

	uploader := &sequenceUploader{results: []uploadOutcome{
		{err: errDownloadExited},
		{image: &hcloud.Image{ID: 77}},
	}}
	client := newBuildResourcesAPI(t, &buildResourcesAPI{failListing: true})
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	_, err := manager.EnsureTalosSnapshot(t.Context(), "retry-cluster", "v1.9.0", "abc123", nil)

	require.ErrorIs(t, err, errDownloadExited)
	require.ErrorContains(t, err, "failed to list temporary snapshot build servers")
	assert.Equal(t, 1, uploader.callCount(), "no second server is started")
}

func TestSnapshotManager_EnsureTalosSnapshot_CancelledRetryWaitStops(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	uploader := &sequenceUploader{results: []uploadOutcome{
		{err: errDownloadExited},
		{image: &hcloud.Image{ID: 77}},
	}}
	client := newBuildResourcesAPI(t, &buildResourcesAPI{})
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)
	manager.SetImageProbeForTest(func(context.Context, *url.URL) (int, error) {
		cancel()

		return http.StatusOK, nil
	})

	_, err := manager.EnsureTalosSnapshot(ctx, "retry-cluster", "v1.9.0", "abc123", nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, uploader.callCount())
}

func TestImageMissing_OnlyForStatusesThatSayTheImageDoesNotExist(t *testing.T) {
	t.Parallel()

	missing := map[int]bool{
		http.StatusOK:                  false,
		http.StatusBadRequest:          true,
		http.StatusUnauthorized:        false,
		http.StatusForbidden:           false,
		http.StatusNotFound:            true,
		http.StatusMethodNotAllowed:    false,
		http.StatusRequestTimeout:      false,
		http.StatusGone:                true,
		http.StatusTooManyRequests:     false,
		http.StatusInternalServerError: false,
		http.StatusBadGateway:          false,
	}

	for status, want := range missing {
		assert.Equal(t, want, hetzner.ImageMissingForTest(status), "HTTP %d", status)
	}
}

func TestHeadImageURL_ReportsTheStatusOfAHeadRequest(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			var method string

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					method = r.Method

					w.WriteHeader(status)
				}),
			)
			t.Cleanup(server.Close)

			imageURL, err := url.Parse(server.URL + "/image/abc123/v1.9.0/hcloud-amd64.raw.xz")
			require.NoError(t, err)

			got, err := hetzner.HeadImageURLForTest(t.Context(), imageURL)

			require.NoError(t, err)
			assert.Equal(t, status, got)
			assert.Equal(t, http.MethodHead, method, "the probe never downloads the image")
		})
	}
}
