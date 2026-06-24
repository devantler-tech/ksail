package clusterapi

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"k8s.io/client-go/rest"
)

// watchEventLines returns two watch JSON objects as a real apiserver WATCH streams them:
// newline-delimited. A function (not a package var) keeps the test fixtures out of package scope.
func watchEventLines() []string {
	return []string{
		`{"type":"ADDED","object":{"metadata":{"uid":"u1"}}}`,
		`{"type":"DELETED","object":{"metadata":{"uid":"u1"}}}`,
	}
}

// fakeWatchAPIServer streams watchEventLines and records the path + watch query param the client sent.
func fakeWatchAPIServer(gotPath, gotWatch *string) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			*gotPath = request.URL.Path
			*gotWatch = request.URL.Query().Get("watch")

			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)

			flusher, _ := writer.(http.Flusher)

			for _, line := range watchEventLines() {
				_, _ = io.WriteString(writer, line+"\n")

				if flusher != nil {
					flusher.Flush()
				}
			}
		}),
	)
}

func TestWatchKubeForcesWatchAndForwardsStream(t *testing.T) {
	t.Parallel()

	var gotPath, gotWatch string

	server := fakeWatchAPIServer(&gotPath, &gotWatch)
	defer server.Close()

	service := &Service{
		restConfigForCluster: func(string) (*rest.Config, error) {
			return &rest.Config{Host: server.URL}, nil
		},
	}

	// The caller's query intentionally omits watch=true; WatchKube must force it on.
	stream, err := service.WatchKube(
		context.Background(),
		"default",
		"kind",
		"api/v1/pods",
		url.Values{"labelSelector": {"app=x"}},
	)
	if err != nil {
		t.Fatalf("WatchKube: %v", err)
	}

	defer func() { _ = stream.Close() }()

	scanner := bufio.NewScanner(stream)
	for index, want := range watchEventLines() {
		if !scanner.Scan() {
			t.Fatalf("expected watch line %d, got none: %v", index, scanner.Err())
		}

		if got := scanner.Text(); got != want {
			t.Errorf("watch line %d = %q, want %q", index, got, want)
		}
	}

	if gotPath != "/api/v1/pods" {
		t.Errorf("apiserver saw path %q, want /api/v1/pods", gotPath)
	}

	if gotWatch != "true" {
		t.Errorf("apiserver saw watch=%q, want watch=true (WatchKube must force it)", gotWatch)
	}
}

func TestWatchKubeCleansPathToStayOnHost(t *testing.T) {
	t.Parallel()

	var gotPath string

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			gotPath = request.URL.Path

			writer.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	service := &Service{
		restConfigForCluster: func(string) (*rest.Config, error) {
			return &rest.Config{Host: server.URL}, nil
		},
	}

	// A traversal-style path is cleaned to an absolute apiserver path; it can never reach another host
	// because the host comes from the resolved rest.Config, not the request.
	stream, err := service.WatchKube(
		context.Background(),
		"default",
		"kind",
		"../../secret",
		nil,
	)
	if err != nil {
		t.Fatalf("WatchKube: %v", err)
	}

	_ = stream.Close()

	if gotPath != "/secret" {
		t.Errorf("cleaned path = %q, want /secret (still on the apiserver host)", gotPath)
	}
}

func TestWatchKubeDoesNotMutateCallerQuery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	service := &Service{
		restConfigForCluster: func(string) (*rest.Config, error) {
			return &rest.Config{Host: server.URL}, nil
		},
	}

	caller := url.Values{"labelSelector": {"app=x"}}

	stream, err := service.WatchKube(context.Background(), "default", "kind", "api/v1/pods", caller)
	if err != nil {
		t.Fatalf("WatchKube: %v", err)
	}

	_ = stream.Close()

	// Forcing watch=true must not leak back into the caller's url.Values (it is shared with the handler).
	if caller.Has("watch") {
		t.Errorf("WatchKube mutated the caller's query: %v", caller)
	}
}
