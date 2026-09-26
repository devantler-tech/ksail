package hetzner_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	buildServerPath = "/servers/42"
	buildSSHKeyPath = "/ssh_keys/7"

	// cancelledBuildTimeout bounds how long a test waits for a cancelled build to return.
	cancelledBuildTimeout = 10 * time.Second

	// keyWait bounds how long a held server deletion waits for the SSH key deletion.
	keyWait = 2 * time.Second
)

// errUploadAbandoned is what stuckUploader returns once the test releases it.
var errUploadAbandoned = errors.New("upload abandoned")

// stuckUploader stands in for an upload stuck in a step that ignores cancellation, such as
// writing the image over SSH. It records the build label it received, runs onStart, and
// returns only when the test ends.
type stuckUploader struct {
	mu      sync.Mutex
	build   string
	onStart func()
	release chan struct{}
}

func newStuckUploader(t *testing.T, onStart func()) *stuckUploader {
	t.Helper()

	uploader := &stuckUploader{onStart: onStart, release: make(chan struct{})}

	t.Cleanup(func() { close(uploader.release) })

	return uploader
}

func (u *stuckUploader) Upload(
	_ context.Context, opts hcloudimages.UploadOptions,
) (*hcloud.Image, error) {
	u.mu.Lock()
	u.build = opts.Labels[hetzner.LabelTalosSnapshotBuild]
	u.mu.Unlock()

	u.onStart()
	<-u.release

	return nil, errUploadAbandoned
}

func (u *stuckUploader) buildID() string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.build
}

// buildResourcesAPI mocks the Hetzner endpoints a snapshot build is cleaned up through.
// It lists one server and one SSH key for the build label listedBuildID returns (any build
// label when it is nil), nothing for any other selector, and records every delete.
type buildResourcesAPI struct {
	mu            sync.Mutex
	deleted       []string
	failListing   bool
	listedBuildID func() string

	// created, when set, hides the build's resources until it returns true, as a create
	// request still in flight at cancellation would.
	created func() bool

	// holdServerDelete holds each deletion open until the other one starts (or keyWait
	// passes), so only overlapping deletions see each other. The SSH key deletion records
	// whether the server deletion was in flight when it arrived.
	holdServerDelete        bool
	serverDeleteInFlight    bool
	serverDeleteStarted     chan struct{}
	keyDeleted              chan struct{}
	keyDeletedDuringServers bool

	// failedServerActions makes the action of each of the first N server deletions fail.
	failedServerActions int
	serverDeletes       int
}

func newBuildResourcesAPI(t *testing.T, api *buildResourcesAPI) *hcloud.Client {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /images", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(t, writer, schema.ImageListResponse{})
	})

	mux.HandleFunc("GET /servers", func(writer http.ResponseWriter, request *http.Request) {
		if api.failListing {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"error":{"code":"forbidden","message":"forbidden"}}`))

			return
		}

		resp := schema.ServerListResponse{}
		if api.isBuildSelector(request) {
			resp.Servers = []schema.Server{{ID: 42, Name: "hcloud-upload-image-test"}}
		}

		writeJSONResponse(t, writer, resp)
	})

	mux.HandleFunc("GET /ssh_keys", func(writer http.ResponseWriter, request *http.Request) {
		resp := schema.SSHKeyListResponse{}
		if api.isBuildSelector(request) {
			resp.SSHKeys = []schema.SSHKey{{ID: 7, Name: "hcloud-upload-image-test"}}
		}

		writeJSONResponse(t, writer, resp)
	})

	mux.HandleFunc("DELETE /servers/{id}", func(writer http.ResponseWriter, request *http.Request) {
		api.deleteServer(t, writer, request)
	})

	mux.HandleFunc("DELETE /ssh_keys/{id}", api.deleteSSHKey)

	mux.HandleFunc("GET /actions", api.listActions)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return newTestHcloudClient(t, srv.URL)
}

// deleteServer records a server deletion and returns its running action.
func (api *buildResourcesAPI) deleteServer(
	t *testing.T, writer http.ResponseWriter, request *http.Request,
) {
	t.Helper()

	if api.holdServerDelete {
		api.mu.Lock()
		api.serverDeleteInFlight = true
		api.mu.Unlock()
		close(api.serverDeleteStarted)

		select {
		case <-api.keyDeleted:
		case <-time.After(keyWait):
		}

		api.mu.Lock()
		api.serverDeleteInFlight = false
		api.mu.Unlock()
	}

	api.recordDelete(request)

	api.mu.Lock()
	api.serverDeletes++
	actionID := int64(api.serverDeletes)
	api.mu.Unlock()

	writeJSONResponse(t, writer, schema.ServerDeleteResponse{
		Action: schema.Action{ID: actionID, Command: "delete_server", Status: "running"},
	})
}

// deleteSSHKey records an SSH key deletion and signals keyDeleted when a test waits on it.
func (api *buildResourcesAPI) deleteSSHKey(writer http.ResponseWriter, request *http.Request) {
	if api.holdServerDelete {
		select {
		case <-api.serverDeleteStarted:
		case <-time.After(keyWait):
		}

		api.mu.Lock()
		api.keyDeletedDuringServers = api.serverDeleteInFlight
		api.mu.Unlock()
	}

	api.recordDelete(request)
	writer.WriteHeader(http.StatusNoContent)

	if api.keyDeleted != nil {
		close(api.keyDeleted)
	}
}

// listActions reports the actions of server deletions. The first failedServerActions fail.
func (api *buildResourcesAPI) listActions(writer http.ResponseWriter, request *http.Request) {
	resp := schema.ActionListResponse{}

	for _, raw := range request.URL.Query()["id"] {
		actionID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)

			return
		}

		action := schema.Action{ID: actionID, Command: "delete_server", Status: "success"}
		if actionID <= int64(api.failedServerActions) {
			action.Status = "error"
			action.Error = &schema.ActionError{Code: "action_failed", Message: "deletion failed"}
		}

		resp.Actions = append(resp.Actions, action)
	}

	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(resp)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (api *buildResourcesAPI) isBuildSelector(request *http.Request) bool {
	if api.created != nil && !api.created() {
		return false
	}

	selector := request.URL.Query().Get("label_selector")
	if api.listedBuildID == nil {
		return strings.HasPrefix(selector, hetzner.LabelTalosSnapshotBuild+"=")
	}

	buildID := api.listedBuildID()

	return buildID != "" && selector == hetzner.LabelTalosSnapshotBuild+"="+buildID
}

func (api *buildResourcesAPI) recordDelete(request *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()

	api.deleted = append(api.deleted, request.URL.Path)
}

func (api *buildResourcesAPI) deletedPaths() []string {
	api.mu.Lock()
	defer api.mu.Unlock()

	return append([]string(nil), api.deleted...)
}

// ensureTalosSnapshotWithin runs EnsureTalosSnapshot and fails the test if it has not
// returned within cancelledBuildTimeout.
func ensureTalosSnapshotWithin(
	ctx context.Context,
	t *testing.T,
	manager *hetzner.SnapshotManager,
) error {
	t.Helper()

	result := make(chan error, 1)

	go func() {
		_, err := manager.EnsureTalosSnapshot(ctx, "cancel-cluster", "v1.9.0", "abc123", nil)
		result <- err
	}()

	select {
	case err := <-result:
		return err
	case <-time.After(cancelledBuildTimeout):
		t.Fatal("the cancelled snapshot build did not return")

		return nil
	}
}

func TestSnapshotManager_EnsureTalosSnapshot_CancelledBuildDeletesItsResources(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	uploader := newStuckUploader(t, cancel)
	api := &buildResourcesAPI{listedBuildID: uploader.buildID}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	err := ensureTalosSnapshotWithin(ctx, t, manager)

	require.ErrorIs(t, err, hetzner.ErrSnapshotBuildFailed)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotEmpty(t, uploader.buildID(), "the build must label the resources it creates")
	assert.ElementsMatch(t, []string{buildServerPath, buildSSHKeyPath}, api.deletedPaths())
}

func TestSnapshotManager_EnsureTalosSnapshot_ReportsFailedCleanup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	uploader := newStuckUploader(t, cancel)
	api := &buildResourcesAPI{listedBuildID: uploader.buildID, failListing: true}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	err := ensureTalosSnapshotWithin(ctx, t, manager)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "failed to list temporary snapshot build servers")
	assert.True(t, hcloud.IsError(err, hcloud.ErrorCodeForbidden))
}

func TestSnapshotManager_EnsureTalosSnapshot_CompletedBuildDeletesNothing(t *testing.T) {
	t.Parallel()

	uploader := &mockUploader{image: &hcloud.Image{ID: 99}}
	api := &buildResourcesAPI{}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	imageID, err := manager.EnsureTalosSnapshot(t.Context(), "done-cluster", "v1.9.0", "abc123", nil)

	require.NoError(t, err)
	assert.Equal(t, int64(99), imageID)
	assert.Empty(t, api.deletedPaths())
}

// lateUploader stands in for an upload cancelled while a create request was in flight: the
// server and SSH key appear on the provider's side shortly after cancellation, and then the
// upload returns.
type lateUploader struct {
	mu      sync.Mutex
	build   string
	onStart func()
	visible bool
}

func (u *lateUploader) Upload(
	ctx context.Context, opts hcloudimages.UploadOptions,
) (*hcloud.Image, error) {
	u.mu.Lock()
	u.build = opts.Labels[hetzner.LabelTalosSnapshotBuild]
	u.mu.Unlock()

	u.onStart()
	<-ctx.Done()
	time.Sleep(200 * time.Millisecond)

	u.mu.Lock()
	u.visible = true
	u.mu.Unlock()

	return nil, fmt.Errorf("create server: %w", ctx.Err())
}

func (u *lateUploader) buildID() string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.build
}

func (u *lateUploader) created() bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.visible
}

func TestSnapshotManager_EnsureTalosSnapshot_CancelledBuildDeletesResourcesCreatedLate(
	t *testing.T,
) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	uploader := &lateUploader{onStart: cancel}
	api := &buildResourcesAPI{listedBuildID: uploader.buildID, created: uploader.created}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	err := ensureTalosSnapshotWithin(ctx, t, manager)

	require.ErrorIs(t, err, context.Canceled)
	assert.ElementsMatch(t, []string{buildServerPath, buildSSHKeyPath}, api.deletedPaths(),
		"a resource whose create request landed after cancellation must still be deleted")
}

func TestSnapshotManager_EnsureTalosSnapshot_DeletesServerAndKeyConcurrently(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	uploader := newStuckUploader(t, cancel)
	api := &buildResourcesAPI{
		listedBuildID:       uploader.buildID,
		holdServerDelete:    true,
		serverDeleteStarted: make(chan struct{}),
		keyDeleted:          make(chan struct{}),
	}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	err := ensureTalosSnapshotWithin(ctx, t, manager)

	require.ErrorIs(t, err, context.Canceled)
	assert.ElementsMatch(t, []string{buildServerPath, buildSSHKeyPath}, api.deletedPaths())

	api.mu.Lock()
	defer api.mu.Unlock()

	assert.True(t, api.keyDeletedDuringServers,
		"a slow server deletion must not delay the SSH key deletion")
}

func TestSnapshotManager_EnsureTalosSnapshot_RetriesAFailedServerDeletion(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	uploader := newStuckUploader(t, cancel)
	api := &buildResourcesAPI{listedBuildID: uploader.buildID, failedServerActions: 1}
	client := newBuildResourcesAPI(t, api)
	manager := hetzner.NewSnapshotManagerWithUploaderForTest(client, uploader, nil)

	err := ensureTalosSnapshotWithin(ctx, t, manager)

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "failed to delete temporary snapshot build server")
	assert.ElementsMatch(t, []string{buildServerPath, buildServerPath, buildSSHKeyPath},
		api.deletedPaths(), "a deletion whose action failed must be retried")
}
