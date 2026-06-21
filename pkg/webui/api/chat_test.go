package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// chatStub is a ClusterService that also implements api.ChatService, emitting canned events so the
// chat transport, capability, and gating can be exercised without GitHub Copilot.
type chatStub struct {
	stubClusterService

	available bool
	events    []api.ChatEvent
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
