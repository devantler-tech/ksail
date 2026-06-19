package helm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
)

// TestLocateChartFromRepoConcurrentNoRace exercises repo-based chart location
// from many goroutines at once. locateChartFromRepo mutates the process-global
// HELM_HTTP_TIMEOUT env var around each LocateChart; without the package-level
// lock added for the GitOps render feature, concurrent calls race on that env
// var. Each goroutine uses its own template-only client (mirroring how the
// renderer constructs a client per Expand) so this specifically guards the
// global env race, not per-client state. Run with `-race` to catch regressions.
//
// The repository is a local httptest server that 404s every request, so
// LocateChart fails fast (no real network, no long timeout); the errors are
// expected — the assertion is simply that all calls complete without a data
// race or panic.
func TestLocateChartFromRepoConcurrentNoRace(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	const goroutines = 8

	var waitGroup sync.WaitGroup

	waitGroup.Add(goroutines)

	for range goroutines {
		go func() {
			defer waitGroup.Done()

			client, err := helm.NewTemplateOnlyClient()
			if err != nil {
				return
			}

			// Expected to error (bogus repo); we only care that the concurrent
			// HELM_HTTP_TIMEOUT mutation is race-free.
			_, _ = client.TemplateChart(context.Background(), &helm.ChartSpec{
				ReleaseName: "race",
				ChartName:   "does-not-exist",
				RepoURL:     srv.URL,
			})
		}()
	}

	waitGroup.Wait()
}
