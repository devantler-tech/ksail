package clusterapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"k8s.io/client-go/rest"
)

// A plain rest.Config needs no custom TLS, dialer or proxy, so client-go's transport cache hands
// back the process-global http.DefaultTransport. An apiserver watch is long-lived, so sharing that
// pool means any goroutine calling http.DefaultTransport.CloseIdleConnections() can break the
// stream. The apiserver transport must therefore have its own pool.
func TestApiserverTransportIsNotTheProcessGlobalPool(t *testing.T) {
	t.Parallel()

	transport, err := apiserverTransportFor(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("apiserverTransportFor: %v", err)
	}

	if transport == http.DefaultTransport {
		t.Fatal("apiserver transport is the process-global http.DefaultTransport: " +
			"an unrelated CloseIdleConnections() can break a live watch")
	}
}

// Negative control: a config that genuinely needs a custom transport must be left on client-go's
// own cached transport. Without this, the fix could pass by cloning unconditionally, which would
// silently discard client-go's per-config caching and any wrapped auth RoundTripper.
func TestApiserverTransportLeavesCustomConfigsOnClientGoCache(t *testing.T) {
	t.Parallel()

	config := &rest.Config{
		Host:            "https://127.0.0.1:1",
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}

	first, err := apiserverTransportFor(config)
	if err != nil {
		t.Fatalf("apiserverTransportFor: %v", err)
	}

	second, err := apiserverTransportFor(config)
	if err != nil {
		t.Fatalf("apiserverTransportFor: %v", err)
	}

	if first == http.DefaultTransport {
		t.Fatal("a TLS config must not resolve to the process-global transport")
	}

	if first != second {
		t.Fatal("a custom-TLS config was cloned per call: client-go's transport cache is bypassed")
	}
}

// Behavioural regression guard for the failure actually observed in CI:
//
//	WatchKube: open apiserver watch: Get "http://…/api/v1/pods?…&watch=true":
//	net/http: HTTP/1.x transport connection broken: http: CloseIdleConnections called
//
// A watch must not be broken by an unrelated goroutine closing the process-global pool's idle
// connections. The loop reuses keep-alive connections and interleaves the close, which is the
// sequence that produced the CI failure.
func TestWatchKubeSurvivesDefaultTransportCloseIdleConnections(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}\n"))
		}),
	)
	defer server.Close()

	service := &Service{
		restConfigForCluster: func(string) (*rest.Config, error) {
			return &rest.Config{Host: server.URL}, nil
		},
	}

	// Put an idle keep-alive connection on the process-global pool, so closing it below is a real
	// event rather than a no-op.
	warm, err := http.DefaultClient.Get(server.URL) //nolint:noctx // fixture warm-up, not production code
	if err != nil {
		t.Fatalf("warm-up request: %v", err)
	}

	_, _ = io.Copy(io.Discard, warm.Body)
	_ = warm.Body.Close()

	for i := range 25 {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			defaultTransport.CloseIdleConnections()
		}

		stream, err := service.WatchKube(
			context.Background(),
			"default",
			"kind",
			"api/v1/pods",
			url.Values{"labelSelector": {"app=x"}},
		)
		if err != nil {
			t.Fatalf("iteration %d: WatchKube broken by an unrelated CloseIdleConnections: %v", i, err)
		}

		_, _ = io.Copy(io.Discard, stream)
		_ = stream.Close()
	}
}
