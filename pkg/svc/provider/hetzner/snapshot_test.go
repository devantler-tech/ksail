package hetzner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	hcloudtest "github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockUploader is a test-only snapshotUploader that returns a canned result.
type mockUploader struct {
	image  *hcloudtest.Image
	err    error
	called bool
}

func (m *mockUploader) Upload(
	_ context.Context, _ hcloudimages.UploadOptions,
) (*hcloudtest.Image, error) {
	m.called = true

	return m.image, m.err
}

// hcloudImageListResponse mirrors the Hetzner API response schema for image listing.
type hcloudImageListResponse struct {
	Images []hcloudImageSchema `json:"images"`
}

type hcloudImageSchema struct {
	ID     int64             `json:"id"`
	Name   *string           `json:"name"`
	Labels map[string]string `json:"labels"`
	Type   string            `json:"type"`
	Status string            `json:"status"`
}

// errServerQuotaExceeded is a static sentinel used in uploader-error tests.
var errServerQuotaExceeded = errors.New("server quota exceeded")

// newTestHcloudClient creates an hcloud.Client pointing at a test HTTP server.
func newTestHcloudClient(serverURL string) *hcloudtest.Client {
	return hcloudtest.NewClient(
		hcloudtest.WithToken("test-token"),
		hcloudtest.WithEndpoint(serverURL),
	)
}

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return data
}

func TestSnapshotManager_EnsureTalosSnapshot_ExistingFound(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "test-cluster"
		version     = "v1.9.0"
		schematic   = "abc123"
		imageID     = int64(42)
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/images", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")

		resp := hcloudImageListResponse{
			Images: []hcloudImageSchema{
				{
					ID: imageID,
					Labels: map[string]string{
						"ksail.io/talos-version":   version,
						"ksail.io/talos-schematic": schematic,
					},
					Type:   "snapshot",
					Status: "available",
				},
			},
		}
		_, _ = responseWriter.Write(marshalJSON(t, resp))
	})

	client := newTestHcloudClient(srv.URL)
	uploader := &mockUploader{}

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	gotID, err := sm.EnsureTalosSnapshot(context.Background(), clusterName, version, schematic)

	require.NoError(t, err)
	assert.Equal(t, imageID, gotID)
	// The uploader must not have been invoked when an existing snapshot was found.
	assert.False(t, uploader.called)
	assert.Contains(t, logBuf.String(), "Found existing Talos snapshot")
}

func TestSnapshotManager_EnsureTalosSnapshot_BuildsNew(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "new-cluster"
		version     = "1.9.0" // without "v" prefix — must be normalized
		schematic   = "def456"
		builtID     = int64(99)
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// No images exist yet.
	mux.HandleFunc("/images", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalJSON(t, hcloudImageListResponse{Images: []hcloudImageSchema{}}))
	})

	client := newTestHcloudClient(srv.URL)
	uploader := &mockUploader{
		image: &hcloudtest.Image{ID: builtID},
	}

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	gotID, err := sm.EnsureTalosSnapshot(context.Background(), clusterName, version, schematic)

	require.NoError(t, err)
	assert.Equal(t, builtID, gotID)
	assert.Contains(t, logBuf.String(), "Building Talos snapshot")
	assert.Contains(t, logBuf.String(), "snapshot built")
}

func TestSnapshotManager_EnsureTalosSnapshot_UploaderError(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "err-cluster"
		version     = "v1.9.0"
		schematic   = "ghi789"
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/images", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write(
			marshalJSON(t, hcloudImageListResponse{Images: []hcloudImageSchema{}}),
		)
	})

	client := newTestHcloudClient(srv.URL)
	uploader := &mockUploader{err: errServerQuotaExceeded}

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	_, err := sm.EnsureTalosSnapshot(context.Background(), clusterName, version, schematic)

	require.Error(t, err)
	require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
	assert.ErrorIs(t, err, errServerQuotaExceeded)
}

// registerDeleteTestHandlers wires the Hetzner mock endpoints for snapshot delete tests.
func registerDeleteTestHandlers(
	t *testing.T,
	mux *http.ServeMux,
	clusterName string,
	deletedIDs chan<- int64,
) {
	t.Helper()

	mux.HandleFunc("/images", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")

		resp := hcloudImageListResponse{
			Images: []hcloudImageSchema{
				{
					ID:     10,
					Type:   "snapshot",
					Status: "available",
					Labels: map[string]string{"ksail.io/cluster": clusterName},
				},
				{
					ID:     11,
					Type:   "snapshot",
					Status: "available",
					Labels: map[string]string{"ksail.io/cluster": clusterName},
				},
			},
		}
		_, _ = responseWriter.Write(marshalJSON(t, resp))
	})

	mux.HandleFunc("/images/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletedIDs <- 10

			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux.HandleFunc("/images/11", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletedIDs <- 11

			w.WriteHeader(http.StatusNoContent)
		}
	})
}

func TestSnapshotManager_DeleteTalosSnapshots_DeletesImages(t *testing.T) {
	t.Parallel()

	const clusterName = "del-cluster"

	deletedIDs := make(chan int64, 2)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	registerDeleteTestHandlers(t, mux, clusterName, deletedIDs)

	client := newTestHcloudClient(srv.URL)
	uploader := &mockUploader{}

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	err := sm.DeleteTalosSnapshots(context.Background(), clusterName)

	require.NoError(t, err)
	close(deletedIDs)

	var got []int64
	for id := range deletedIDs {
		got = append(got, id)
	}

	assert.ElementsMatch(t, []int64{10, 11}, got)
	assert.Contains(t, logBuf.String(), "Deleting 2 Talos snapshot(s)")
}

func TestSnapshotManager_DeleteTalosSnapshots_NoOp(t *testing.T) {
	t.Parallel()

	const clusterName = "empty-cluster"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/images", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalJSON(t, hcloudImageListResponse{Images: []hcloudImageSchema{}}))
	})

	client := newTestHcloudClient(srv.URL)
	uploader := &mockUploader{}

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, &logBuf)

	err := sm.DeleteTalosSnapshots(context.Background(), clusterName)

	require.NoError(t, err)
	assert.Empty(t, logBuf.String())
}

func TestNewSnapshotManager(t *testing.T) {
	t.Parallel()

	client := hcloudtest.NewClient(hcloudtest.WithToken("test"))

	var logBuf bytes.Buffer

	sm := hetzner.NewSnapshotManager(client, &logBuf)

	require.NotNil(t, sm)
}

func TestNewSnapshotManager_NilLogWriter(t *testing.T) {
	t.Parallel()

	client := hcloudtest.NewClient(hcloudtest.WithToken("test"))

	sm := hetzner.NewSnapshotManager(client, nil)

	require.NotNil(t, sm)
}

// Compile-time check: EnsureTalosSnapshot URL template is valid.
func TestSnapshotManager_TalosImageURL(t *testing.T) {
	t.Parallel()

	rawURL := "https://factory.talos.dev/image/abc123/v1.9.0/hcloud-amd64.raw.xz"
	parsed, err := url.Parse(rawURL)

	require.NoError(t, err)
	assert.Equal(t, "factory.talos.dev", parsed.Host)
	assert.Equal(t, ".raw.xz", rawURL[len(rawURL)-7:])
}
