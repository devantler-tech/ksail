package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

const (
	// artifactHubBaseURL is the public Artifact Hub API root the plugin catalog queries. It is a fixed,
	// trusted host (so the outbound request is not an SSRF vector); tests point the store at an httptest
	// server instead.
	artifactHubBaseURL = "https://artifacthub.io"
	// headlampPluginKind is Artifact Hub's repository "kind" for Headlamp plugins. Filtering the search on
	// it keeps the catalog to plugins KSail can actually install (Headlamp-format .tar.gz bundles).
	headlampPluginKind = 21
	// catalogSearchLimit caps how many catalog entries one search returns (and so how many detail lookups
	// follow). Kept small to bound the per-search outbound traffic.
	catalogSearchLimit = 20
	// catalogDetailConcurrency bounds how many package-detail lookups run at once (each resolves one
	// entry's tarball URL), so a search fans out to a handful of requests rather than one per result serially.
	catalogDetailConcurrency = 6
	// catalogRequestTimeout bounds the whole catalog lookup (search + detail fan-out).
	catalogRequestTimeout = 20 * time.Second
	// maxCatalogResponseBytes caps each Artifact Hub JSON response read to defend against an oversized body.
	maxCatalogResponseBytes = 4 << 20 // 4 MiB
	// archiveURLDataKey is the Artifact Hub package-data key carrying a Headlamp plugin's installable
	// tarball URL; archiveChecksumDataKey carries its "<algo>:<hex>" checksum.
	archiveURLDataKey      = "headlamp/plugin/archive-url"
	archiveChecksumDataKey = "headlamp/plugin/archive-checksum"
)

// ErrPluginCatalog wraps every catalog lookup failure (unreachable upstream, bad status, unparseable
// body) so callers can match it while the message explains the specific cause.
var ErrPluginCatalog = errors.New("plugin catalog lookup failed")

// Ensure the local backend exposes the plugin catalog.
var _ api.PluginCatalog = (*Service)(nil)

// pluginCatalog browses installable Headlamp plugins from Artifact Hub. baseURL and httpClient are
// seams so tests can point it at an httptest server; the defaults target the public Artifact Hub.
type pluginCatalog struct {
	// baseURL is the Artifact Hub API root (no trailing slash); defaults to artifactHubBaseURL.
	baseURL string
	// httpClient performs the outbound requests; defaults to a client with catalogRequestTimeout.
	httpClient *http.Client
}

// defaultPluginCatalog returns a pluginCatalog targeting the public Artifact Hub.
func defaultPluginCatalog() pluginCatalog {
	return pluginCatalog{
		baseURL:    artifactHubBaseURL,
		httpClient: &http.Client{Timeout: catalogRequestTimeout},
	}
}

// artifactHubSearchResponse is the subset of Artifact Hub's package search response the catalog reads.
type artifactHubSearchResponse struct {
	Packages []artifactHubPackage `json:"packages"`
}

// artifactHubPackage is the subset of one search-result package the catalog reads. repository.name is
// the path segment the package-detail endpoint is keyed on.
type artifactHubPackage struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"` //nolint:tagliatelle // Artifact Hub's API uses snake_case keys.
	Description string `json:"description"`
	Version     string `json:"version"`
	Repository  struct {
		Name string `json:"name"`
	} `json:"repository"`
}

// artifactHubDetail is the subset of Artifact Hub's package-detail response the catalog reads: the
// `data` map carries the installable tarball URL under archiveURLDataKey.
type artifactHubDetail struct {
	Data map[string]string `json:"data"`
}

// ListCatalog searches Artifact Hub for installable Headlamp plugins matching query (an empty query
// lists the default set), resolves each result's tarball URL, and returns the entries the install flow
// can consume. Entries whose tarball URL cannot be resolved are omitted rather than failing the whole
// search, so one unpublishable package does not hide the rest.
func (s *Service) ListCatalog(ctx context.Context, query string) ([]api.CatalogEntry, error) {
	return s.pluginCatalog.list(ctx, query)
}

// list runs the search then resolves tarball URLs for the results.
func (c pluginCatalog) list(ctx context.Context, query string) ([]api.CatalogEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogRequestTimeout)
	defer cancel()

	packages, err := c.search(ctx, query)
	if err != nil {
		return nil, err
	}

	return c.resolveEntries(ctx, packages), nil
}

// search calls the Artifact Hub package-search endpoint filtered to Headlamp plugins, returning the
// matching packages (without their tarball URLs, which the detail endpoint carries).
func (c pluginCatalog) search(ctx context.Context, query string) ([]artifactHubPackage, error) {
	params := url.Values{}
	params.Set("kind", strconv.Itoa(headlampPluginKind))
	params.Set("limit", strconv.Itoa(catalogSearchLimit))
	params.Set("facets", "false")

	trimmed := strings.TrimSpace(query)
	if trimmed != "" {
		params.Set("ts_query_web", trimmed)
	}

	endpoint := c.baseURL + "/api/v1/packages/search?" + params.Encode()

	var parsed artifactHubSearchResponse

	err := c.getJSON(ctx, endpoint, &parsed)
	if err != nil {
		return nil, err
	}

	return parsed.Packages, nil
}

// resolveEntries fetches each package's detail to obtain its tarball URL and builds the catalog entries,
// bounding concurrency with a worker pool. Packages without a resolvable tarball URL are skipped.
func (c pluginCatalog) resolveEntries(
	ctx context.Context,
	packages []artifactHubPackage,
) []api.CatalogEntry {
	entries := make([]api.CatalogEntry, len(packages))

	var waitGroup sync.WaitGroup

	semaphore := make(chan struct{}, catalogDetailConcurrency)

	for index := range packages {
		waitGroup.Add(1)

		semaphore <- struct{}{}

		go func() {
			defer waitGroup.Done()
			defer func() { <-semaphore }()

			entries[index] = c.entryFor(ctx, packages[index])
		}()
	}

	waitGroup.Wait()

	return compactEntries(entries)
}

// entryFor resolves one package's tarball URL and returns its catalog entry, or a zero entry (filtered
// out by compactEntries) when the URL cannot be resolved.
func (c pluginCatalog) entryFor(ctx context.Context, pkg artifactHubPackage) api.CatalogEntry {
	archiveURL, err := c.archiveURL(ctx, pkg)
	if err != nil || archiveURL == "" {
		return api.CatalogEntry{}
	}

	return api.CatalogEntry{
		Name:        firstNonEmpty(pkg.DisplayName, pkg.Name),
		Description: pkg.Description,
		Version:     pkg.Version,
		Repository:  pkg.Repository.Name,
		URL:         archiveURL,
	}
}

// archiveURL fetches a package's detail and returns its Headlamp plugin tarball URL.
func (c pluginCatalog) archiveURL(ctx context.Context, pkg artifactHubPackage) (string, error) {
	if pkg.Repository.Name == "" || pkg.Name == "" {
		return "", nil
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/packages/headlamp/%s/%s",
		c.baseURL,
		url.PathEscape(pkg.Repository.Name),
		url.PathEscape(pkg.Name),
	)

	var detail artifactHubDetail

	err := c.getJSON(ctx, endpoint, &detail)
	if err != nil {
		return "", err
	}

	return detail.Data[archiveURLDataKey], nil
}

// getJSON performs a GET against endpoint (a fixed-host Artifact Hub URL) and decodes the size-capped
// JSON body into out.
func (c pluginCatalog) getJSON(ctx context.Context, endpoint string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %w", ErrPluginCatalog, err)
	}

	request.Header.Set("Accept", "application/json")

	// The endpoint is built from the fixed, trusted Artifact Hub host (or a test server), not from
	// user-controlled input, so this is not an SSRF vector.
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: request: %w", ErrPluginCatalog, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: upstream returned HTTP %d", ErrPluginCatalog, response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: read response: %w", ErrPluginCatalog, err)
	}

	err = json.Unmarshal(data, out)
	if err != nil {
		return fmt.Errorf("%w: parse response: %w", ErrPluginCatalog, err)
	}

	return nil
}

// compactEntries drops the zero entries (packages whose tarball URL did not resolve) so the result is a
// dense, non-nil slice the handler encodes as a JSON array.
func compactEntries(entries []api.CatalogEntry) []api.CatalogEntry {
	out := make([]api.CatalogEntry, 0, len(entries))

	for _, entry := range entries {
		if entry.URL != "" {
			out = append(out, entry)
		}
	}

	return out
}
