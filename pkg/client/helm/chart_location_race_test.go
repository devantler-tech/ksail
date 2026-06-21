package helm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
)

// serializationProbeDelay widens the window during which a chart acquisition holds
// the repo server, so overlapping acquisitions are reliably observable when the
// serialization lock is absent.
const serializationProbeDelay = 25 * time.Millisecond

// raiseMax atomically raises target to value when value is the larger of the two.
func raiseMax(target *atomic.Int32, value int32) {
	for {
		observed := target.Load()
		if value <= observed || target.CompareAndSwap(observed, value) {
			return
		}
	}
}

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

// TestChartAcquisitionSerialized verifies the fix for devantler-tech/ksail#5362:
// chart acquisition (locate → download → load → values) is serialized across the
// whole process, so concurrent HelmRelease renders never touch the shared,
// process-global Helm cache/config directories at the same time. Every
// NewTemplateOnlyClient resolves the SAME global directories (HELM_REPOSITORY_CACHE
// etc.), so before this fix overlapping acquisitions raced on those files — one
// download writing a chart archive / OCI blob / repo index while another read or
// rewrote it — producing corrupt, non-deterministic render output on the
// HelmRelease-dense overlays.
//
// That race is on the filesystem, so `-race` cannot observe it (the prior
// env-var-only lock passed `-race` yet the corruption persisted). This test
// instead asserts the serialization invariant directly: a local repo server
// records how many acquisitions are in flight at once. With the acquisition lock
// the maximum is exactly 1; without it, the goroutines overlap and the maximum
// exceeds 1. Each goroutine targets a distinct chart so nothing is short-circuited
// by a cache hit, and the handler sleeps briefly to widen the overlap window. The
// repo 404s, so LocateChart fails fast — the assertion is on concurrency, not on a
// successful render.
//
// It mutates HELM_* env vars (to keep the cache hermetic), so it does not call
// t.Parallel and runs in the package's sequential phase, where no parallel test
// is concurrently reading those vars.
func TestChartAcquisitionSerialized(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(cacheDir, "cache"))
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(cacheDir, "repositories.yaml"))

	var inFlight, maxInFlight atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			raiseMax(&maxInFlight, inFlight.Add(1))
			time.Sleep(serializationProbeDelay)
			inFlight.Add(-1)

			http.Error(writer, "not found", http.StatusNotFound)
		}),
	)
	defer server.Close()

	const goroutines = 6

	var waitGroup sync.WaitGroup

	waitGroup.Add(goroutines)

	for index := range goroutines {
		go func() {
			defer waitGroup.Done()

			client, err := helm.NewTemplateOnlyClient()
			if err != nil {
				return
			}

			// Distinct chart per goroutine so no acquisition is skipped by a cache
			// hit; the bogus repo 404s, so this fails fast after hitting the server.
			_, _ = client.TemplateChart(context.Background(), &helm.ChartSpec{
				ReleaseName: "race",
				ChartName:   fmt.Sprintf("chart-%d", index),
				RepoURL:     server.URL,
			})
		}()
	}

	waitGroup.Wait()

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf(
			"chart acquisition not serialized: max concurrent repo requests = %d, want 1 "+
				"(concurrent renders race on the shared Helm cache — see ksail#5362)",
			got,
		)
	}
}
