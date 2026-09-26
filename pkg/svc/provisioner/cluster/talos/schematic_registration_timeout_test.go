package talosprovisioner_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterSchematicReturnsTheStoredID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"id": "stored-id", "schematic": string(body)})
	}))
	t.Cleanup(server.Close)

	id, err := talosprovisioner.RegisterSchematicForTest(
		context.Background(), server.URL, 5*time.Second, talosconfigmanager.Schematic{},
	)

	require.NoError(t, err)
	assert.Equal(t, "stored-id", id)
}

// A context with no deadline must still not wait on a stalled Image Factory forever.
func TestRegisterSchematicTimesOutOnAStalledFactory(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	start := time.Now()
	_, err := talosprovisioner.RegisterSchematicForTest(
		context.Background(), server.URL, 100*time.Millisecond, talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

// An earlier cancellation on the caller's context still wins over the registration timeout.
func TestRegisterSchematicHonoursCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := talosprovisioner.RegisterSchematicForTest(
		ctx, "http://127.0.0.1:1", time.Minute, talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.Canceled)
}
