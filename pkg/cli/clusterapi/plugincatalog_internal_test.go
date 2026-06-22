package clusterapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// fakeArtifactHub builds an httptest server that mimics the two Artifact Hub endpoints the catalog uses:
// the package search (filtered to Headlamp plugins) and the per-package detail (carrying the tarball URL
// in `data`). archives maps a package name to the tarball URL its detail reports ("" omits the URL so the
// catalog drops it). lastQuery captures the ts_query_web sent, so a test can assert query passthrough.
func fakeArtifactHub(
	t *testing.T,
	archives map[string]string,
	lastQuery *string,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(
		"/api/v1/packages/search",
		func(writer http.ResponseWriter, request *http.Request) {
			if lastQuery != nil {
				*lastQuery = request.URL.Query().Get("ts_query_web")
			}

			writeSearchResponse(writer, archives)
		},
	)
	mux.HandleFunc("/api/v1/packages/headlamp/{repo}/{name}",
		func(writer http.ResponseWriter, request *http.Request) {
			writeDetailResponse(writer, archives[request.PathValue("name")])
		})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// writeSearchResponse writes a packages list (one package per archives key) in Artifact Hub's shape.
func writeSearchResponse(writer http.ResponseWriter, archives map[string]string) {
	var builder strings.Builder

	builder.WriteString(`{"packages":[`)

	first := true

	for name := range archives {
		if !first {
			builder.WriteString(",")
		}

		first = false

		builder.WriteString(`{"name":"` + name + `","display_name":"Plugin ` + name +
			`","description":"desc ` + name + `","version":"1.2.3",` +
			`"repository":{"name":"repo-` + name + `"}}`)
	}

	builder.WriteString(`]}`)

	_, _ = writer.Write([]byte(builder.String()))
}

// writeDetailResponse writes one package detail, including the archive URL only when non-empty.
func writeDetailResponse(writer http.ResponseWriter, archiveURL string) {
	if archiveURL == "" {
		_, _ = writer.Write([]byte(`{"data":{}}`))

		return
	}

	_, _ = writer.Write([]byte(`{"data":{"headlamp/plugin/archive-url":"` + archiveURL + `"}}`))
}

// newCatalog returns a pluginCatalog pointed at baseURL.
func newCatalog(baseURL string) pluginCatalog {
	return pluginCatalog{baseURL: baseURL, httpClient: http.DefaultClient}
}

func TestPluginCatalogListMapsEntries(t *testing.T) {
	t.Parallel()

	server := fakeArtifactHub(t, map[string]string{
		"alpha": "https://example.test/alpha-1.2.3.tar.gz",
	}, nil)

	entries, err := newCatalog(server.URL).list(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	want := api.CatalogEntry{
		Name:        "Plugin alpha",
		Description: "desc alpha",
		Version:     "1.2.3",
		Repository:  "repo-alpha",
		URL:         "https://example.test/alpha-1.2.3.tar.gz",
	}
	if entries[0] != want {
		t.Errorf("entry = %+v, want %+v", entries[0], want)
	}
}

func TestPluginCatalogListDropsEntriesWithoutURL(t *testing.T) {
	t.Parallel()

	// "beta" has no archive URL in its detail, so it must be dropped; "alpha" survives.
	server := fakeArtifactHub(t, map[string]string{
		"alpha": "https://example.test/alpha.tar.gz",
		"beta":  "",
	}, nil)

	entries, err := newCatalog(server.URL).list(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (URL-less entry dropped)", len(entries))
	}

	if entries[0].Name != "Plugin alpha" {
		t.Errorf("surviving entry = %q, want %q", entries[0].Name, "Plugin alpha")
	}
}

func TestPluginCatalogListForwardsQuery(t *testing.T) {
	t.Parallel()

	var gotQuery string

	server := fakeArtifactHub(t, map[string]string{
		"alpha": "https://example.test/alpha.tar.gz",
	}, &gotQuery)

	_, err := newCatalog(server.URL).list(context.Background(), "  flux  ")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if gotQuery != "flux" {
		t.Errorf("forwarded query = %q, want %q (trimmed)", gotQuery, "flux")
	}
}

func TestPluginCatalogListEmpty(t *testing.T) {
	t.Parallel()

	server := fakeArtifactHub(t, map[string]string{}, nil)

	entries, err := newCatalog(server.URL).list(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if entries == nil {
		t.Fatal("entries is nil, want a non-nil empty slice")
	}

	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestPluginCatalogListUpstreamError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusInternalServerError)
		}))
	t.Cleanup(server.Close)

	_, err := newCatalog(server.URL).list(context.Background(), "")
	if !errors.Is(err, ErrPluginCatalog) {
		t.Fatalf("err = %v, want ErrPluginCatalog", err)
	}
}
