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

func TestProxyKubeGetForwardsToAPIServer(t *testing.T) {
	t.Parallel()

	var (
		gotPath  string
		gotQuery string
	)

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			gotPath = request.URL.Path
			gotQuery = request.URL.RawQuery

			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(writer, `{"kind":"PodList"}`)
		}),
	)
	defer server.Close()

	service := &Service{
		restConfigForCluster: func(string) (*rest.Config, error) {
			return &rest.Config{Host: server.URL}, nil
		},
	}

	response, err := service.ProxyKubeGet(
		context.Background(),
		"default",
		"kind",
		"api/v1/pods",
		url.Values{"labelSelector": {"app=x"}},
	)
	if err != nil {
		t.Fatalf("ProxyKubeGet: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if response.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", response.Status)
	}

	if string(body) != `{"kind":"PodList"}` {
		t.Errorf("body = %q", body)
	}

	if gotPath != "/api/v1/pods" {
		t.Errorf("apiserver saw path %q, want /api/v1/pods", gotPath)
	}

	if gotQuery != "labelSelector=app%3Dx" {
		t.Errorf("apiserver saw query %q, want labelSelector=app%%3Dx", gotQuery)
	}
}

func TestProxyKubeGetCleansPathToStayOnHost(t *testing.T) {
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
	response, err := service.ProxyKubeGet(
		context.Background(),
		"default",
		"kind",
		"../../secret",
		nil,
	)
	if err != nil {
		t.Fatalf("ProxyKubeGet: %v", err)
	}

	_ = response.Body.Close()

	if gotPath != "/secret" {
		t.Errorf("cleaned path = %q, want /secret (still on the apiserver host)", gotPath)
	}
}
