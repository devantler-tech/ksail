package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// confirmRecord captures the arguments of a ConfirmTool call so a test can assert the transport routed
// the SPA's decision to the chat service. A pointer is embedded in the (value-receiver) chatStub so the
// record survives the interface copy.
type confirmRecord struct {
	called    bool
	confirmID string
	approved  bool
}

// chatStub is a ClusterService that also implements api.ChatService, emitting canned events so the
// chat transport, capability, and gating can be exercised without GitHub Copilot.
type chatStub struct {
	stubClusterService

	available bool
	events    []api.ChatEvent
	confirm   *confirmRecord
}

func (c chatStub) ChatAvailable(_ context.Context) bool {
	return c.available
}

func (c chatStub) Chat(_ context.Context, _ api.ChatRequest, emit func(api.ChatEvent)) error {
	for _, event := range c.events {
		emit(event)
	}

	return nil
}

func (c chatStub) ConfirmTool(confirmID string, approved bool) {
	if c.confirm != nil {
		c.confirm.called = true
		c.confirm.confirmID = confirmID
		c.confirm.approved = approved
	}
}

func TestChatStreamsEventsAsSSE(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: chatStub{
		available: true,
		events: []api.ChatEvent{
			{Type: api.ChatEventDelta, Text: "Hello"},
			{Type: api.ChatEventDelta, Text: " world"},
			{Type: api.ChatEventDone},
		},
	}}

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/chat", `{"message":"hi"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", recorder.Code)
	}

	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content type = %q, want text/event-stream", got)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		`{"type":"delta","text":"Hello"}`,
		`{"type":"delta","text":" world"}`,
		`{"type":"done"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream body missing %s\n--- body ---\n%s", want, body)
		}
	}
}

func TestChatCapabilityFollowsAvailability(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{"available": true, "unavailable": false}
	for name, available := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := &api.Server{Service: chatStub{available: available}}
			recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

			var config struct {
				Capabilities struct {
					AIChat bool `json:"aiChat"`
				} `json:"capabilities"`
			}

			err := json.Unmarshal(recorder.Body.Bytes(), &config)
			if err != nil {
				t.Fatalf("decode config: %v", err)
			}

			if config.Capabilities.AIChat != available {
				t.Errorf("capabilities.aiChat = %v, want %v", config.Capabilities.AIChat, available)
			}
		})
	}
}

func TestChatUnavailableReturns501(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: chatStub{available: false}}

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/chat", `{"message":"hi"}`)
	if recorder.Code != http.StatusNotImplemented {
		t.Errorf("unavailable chat status = %d, want 501", recorder.Code)
	}
}

func TestChatEmptyMessageReturns400(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: chatStub{available: true}}

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/chat", `{"message":""}`)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("empty-message status = %d, want 400", recorder.Code)
	}
}

func TestChatRouteUnregisteredWithoutService(t *testing.T) {
	t.Parallel()

	// A plain ClusterService (no ChatService) does not get the chat route; with no StaticFS fallback the
	// mux returns 404.
	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/chat", `{"message":"hi"}`)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("chat route status = %d, want 404 when unregistered", recorder.Code)
	}
}

func TestChatStreamsToolConfirmEvent(t *testing.T) {
	t.Parallel()

	// A backend that needs to run a write tool streams a tool-confirm event carrying the confirmId, the
	// tool name, and a summary; the SPA renders the confirmation card from it.
	server := &api.Server{Service: chatStub{
		available: true,
		events: []api.ChatEvent{
			{
				Type:      api.ChatEventToolConfirm,
				ConfirmID: "cid-123",
				Text:      "cluster_write",
				Summary:   "Create a cluster",
			},
		},
	}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/chat",
		`{"message":"create a cluster"}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()

	want := `{"type":"tool-confirm","text":"cluster_write",` +
		`"confirmId":"cid-123","summary":"Create a cluster"}`
	if !strings.Contains(body, want) {
		t.Errorf("stream body missing %s\n--- body ---\n%s", want, body)
	}
}

func TestChatConfirmRoutesDecision(t *testing.T) {
	t.Parallel()

	record := &confirmRecord{}
	server := &api.Server{Service: chatStub{available: true, confirm: record}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/chat/confirm",
		`{"confirmId":"cid-123","approved":true}`,
	)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("confirm status = %d, want 204", recorder.Code)
	}

	if !record.called {
		t.Fatal("ConfirmTool was not called")
	}

	if record.confirmID != "cid-123" || !record.approved {
		t.Errorf(
			"ConfirmTool got (%q, %v), want (cid-123, true)",
			record.confirmID,
			record.approved,
		)
	}
}

func TestChatConfirmRejectsEmptyConfirmID(t *testing.T) {
	t.Parallel()

	record := &confirmRecord{}
	server := &api.Server{Service: chatStub{available: true, confirm: record}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/chat/confirm",
		`{"approved":true}`,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("empty-confirmId status = %d, want 400", recorder.Code)
	}

	if record.called {
		t.Error("ConfirmTool was called for an empty confirmId, want skipped")
	}
}

func TestChatConfirmRouteUnregisteredWithoutService(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/chat/confirm",
		`{"confirmId":"x","approved":true}`,
	)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("confirm route status = %d, want 404 when unregistered", recorder.Code)
	}
}
