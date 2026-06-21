package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// pluginStub is a ClusterService that also implements api.PluginService, returning canned plugin
// metadata and asset bytes so the plugin routes and capability can be exercised without a filesystem.
type pluginStub struct {
	stubClusterService

	plugins  []api.PluginInfo
	asset    api.PluginAsset
	assetErr error
}

func (p pluginStub) ListPlugins(_ context.Context) ([]api.PluginInfo, error) {
	return p.plugins, nil
}

func (p pluginStub) PluginAsset(_ context.Context, _, _ string) (api.PluginAsset, error) {
	if p.assetErr != nil {
		return api.PluginAsset{}, p.assetErr
	}

	return p.asset, nil
}

func TestConfigAdvertisesPluginsCapability(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: pluginStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200", recorder.Code)
	}

	var config struct {
		Capabilities struct {
			Plugins bool `json:"plugins"`
		} `json:"capabilities"`
	}

	err := json.Unmarshal(recorder.Body.Bytes(), &config)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if !config.Capabilities.Plugins {
		t.Error("capabilities.plugins = false, want true for a PluginService backend")
	}
}

func TestConfigOmitsPluginsCapabilityWithoutService(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	var config struct {
		Capabilities struct {
			Plugins bool `json:"plugins"`
		} `json:"capabilities"`
	}

	err := json.Unmarshal(recorder.Body.Bytes(), &config)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if config.Capabilities.Plugins {
		t.Error("capabilities.plugins = true, want false for a plain ClusterService backend")
	}
}

func TestListPluginsReturnsInstalledPlugins(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: pluginStub{
		plugins: []api.PluginInfo{
			{Name: "flux", Title: "Flux", Version: "1.2.3", Main: "main.js"},
		},
	}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/plugins", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list plugins status = %d, want 200", recorder.Code)
	}

	var body struct {
		Plugins []api.PluginInfo `json:"plugins"`
	}

	err := json.Unmarshal(recorder.Body.Bytes(), &body)
	if err != nil {
		t.Fatalf("decode plugins: %v", err)
	}

	if len(body.Plugins) != 1 || body.Plugins[0].Name != "flux" {
		t.Fatalf("plugins = %+v, want one entry named flux", body.Plugins)
	}
}

func TestListPluginsEncodesEmptyAsArray(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: pluginStub{plugins: nil}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/plugins", "")

	// A nil slice must serialize as [] (not null) so the SPA can iterate it unconditionally.
	if got := recorder.Body.String(); got != `{"plugins":[]}` {
		t.Errorf("empty plugins body = %s, want {\"plugins\":[]}", got)
	}
}

func TestPluginAssetServesBundleWithContentType(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: pluginStub{
		asset: api.PluginAsset{
			Content:     []byte("console.log('hi')"),
			ContentType: api.PluginContentType("main.js"),
		},
	}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/plugins/flux/main.js", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", recorder.Code)
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/javascript; charset=utf-8" {
		t.Errorf("content type = %q, want application/javascript", contentType)
	}

	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff = %q, want nosniff", got)
	}

	if got := recorder.Body.String(); got != "console.log('hi')" {
		t.Errorf("asset body = %q", got)
	}
}

func TestPluginAssetNotFoundMapsTo404(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: pluginStub{assetErr: api.ErrNotFound}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/plugins/flux/missing.js", "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("missing asset status = %d, want 404", recorder.Code)
	}
}

func TestPluginRoutesUnregisteredWithoutService(t *testing.T) {
	t.Parallel()

	// A plain ClusterService (no PluginService) does not get the plugin routes registered; with no
	// StaticFS fallback the mux returns 404 rather than serving a plugin list.
	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/plugins", "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("plugins route status = %d, want 404 when unregistered", recorder.Code)
	}
}

func TestPluginContentType(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"main.js":   "application/javascript; charset=utf-8",
		"style.css": "text/css; charset=utf-8",
		"meta.json": "application/json; charset=utf-8",
		"icon.svg":  "image/svg+xml",
		"data.bin":  "application/octet-stream",
	}

	for file, want := range cases {
		if got := api.PluginContentType(file); got != want {
			t.Errorf("PluginContentType(%q) = %q, want %q", file, got, want)
		}
	}
}
