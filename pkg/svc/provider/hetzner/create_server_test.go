package hetzner_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type isoBootTestAction struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}

type isoBootTestServerSummary struct {
	ID int64 `json:"id"`
}

type isoBootTestServerCreateResp struct {
	Server isoBootTestServerSummary `json:"server"`
	Action isoBootTestAction        `json:"action"`
}

type isoBootTestActionResp struct {
	Action isoBootTestAction `json:"action"`
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	data := marshalJSON(t, v)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func successTestAction(id int64) isoBootTestAction {
	return isoBootTestAction{ID: id, Status: "success", Progress: 100}
}

func serverCreateResp(serverID, actionID int64) isoBootTestServerCreateResp {
	return isoBootTestServerCreateResp{
		Server: isoBootTestServerSummary{ID: serverID},
		Action: successTestAction(actionID),
	}
}

func registerCreateServerHandlers(
	t *testing.T,
	mux *http.ServeMux,
	createBody *[]byte,
	attachCalled, poweronCalled, resetCalled *atomic.Bool,
) {
	t.Helper()

	mux.HandleFunc("POST /servers", func(w http.ResponseWriter, r *http.Request) {
		*createBody, _ = io.ReadAll(r.Body)

		writeJSONResponse(t, w, serverCreateResp(123, 1))
	})
	mux.HandleFunc(
		"POST /servers/123/actions/attach_iso",
		func(w http.ResponseWriter, _ *http.Request) {
			attachCalled.Store(true)
			writeJSONResponse(t, w, isoBootTestActionResp{Action: successTestAction(2)})
		},
	)
	mux.HandleFunc(
		"POST /servers/123/actions/poweron",
		func(w http.ResponseWriter, _ *http.Request) {
			poweronCalled.Store(true)
			writeJSONResponse(t, w, isoBootTestActionResp{Action: successTestAction(3)})
		},
	)
	mux.HandleFunc("POST /servers/123/actions/reset", func(w http.ResponseWriter, _ *http.Request) {
		resetCalled.Store(true)
		writeJSONResponse(t, w, isoBootTestActionResp{Action: successTestAction(4)})
	})
}

// TestCreateServer_ISOBoot verifies that CreateServer with an ISO:
//   - Creates the server with start_after_create=false (so Debian does not boot first)
//   - Attaches the ISO to the stopped server
//   - Powers on the server for the first boot (not reset — the ISO must be the first boot)
func TestCreateServer_ISOBoot(t *testing.T) {
	t.Parallel()

	var (
		createBody    []byte
		attachCalled  atomic.Bool
		poweronCalled atomic.Bool
		resetCalled   atomic.Bool
	)

	mux := http.NewServeMux()
	registerCreateServerHandlers(t, mux, &createBody, &attachCalled, &poweronCalled, &resetCalled)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))
	_, err := prov.CreateServer(context.Background(), hetzner.CreateServerOpts{
		Name:       "test-node",
		ServerType: "cx22",
		Location:   "fsn1",
		ISOID:      12345,
	})

	require.NoError(t, err)

	var createReq map[string]any
	require.NoError(t, json.Unmarshal(createBody, &createReq))

	startAfterCreate, ok := createReq["start_after_create"].(bool)
	require.True(t, ok, "start_after_create field must be present and bool")
	assert.False(t, startAfterCreate, "server must not start automatically when using ISO")
	assert.True(t, attachCalled.Load(), "ISO must be attached after server creation")
	assert.True(t, poweronCalled.Load(), "server must be powered on after ISO attachment")
	assert.False(t, resetCalled.Load(), "reset must not be called; use poweron for first ISO boot")
}
