package helm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
)

const repositoryStateProbeWait = 100 * time.Millisecond

// The first real registration pauses after reading repositories.yaml, while its
// HTTP index request is in flight. Without a transaction gate the second
// registration finishes against that same old snapshot, then the first writer
// deterministically overwrites its entry when released.
//
//nolint:paralleltest // Fixture isolates process-global HELM_* environment variables.
func TestRepositoryRegistrationsPreserveEntries(t *testing.T) {
	fixture := newRepositoryStateFixture(t)
	first := fixture.start(t.Context(), "first", false)
	fixture.awaitFirst(t)
	second := fixture.start(t.Context(), "second", false)

	select {
	case <-fixture.secondRequested:
		// Let the overlapping transaction finish before resuming the old
		// snapshot. This is the lost-update schedule in the unfixed client.
		result := awaitRepositoryOperation(t, second)
		require.NoError(t, result.err)

		second = nil
	case <-time.After(repositoryStateProbeWait):
		// A serialized registration must wait for the first commit instead.
	}

	fixture.release()
	require.NoError(t, awaitRepositoryOperation(t, first).err)

	if second != nil {
		require.NoError(t, awaitRepositoryOperation(t, second).err)
	}

	repositories, err := repov1.LoadFile(fixture.repoFile)
	require.NoError(t, err)
	require.Len(t, repositories.Repositories, 2, "a completed registration was lost")
	require.NotNil(t, repositories.Get("first"))
	require.NotNil(t, repositories.Get("second"))
	assert.Equal(t, fixture.server.URL+"/first", repositories.Get("first").URL)
	assert.Equal(t, fixture.server.URL+"/second", repositories.Get("second").URL)
}

// Both directions matter: an independent registration lock would preserve
// writers but still expose Helm's config/index readers to an in-flight writer.
//
//nolint:paralleltest // Fixture isolates process-global HELM_* environment variables.
func TestRepositoryRegistrationAndChartAcquisitionExcludeEachOther(t *testing.T) {
	for _, firstRenders := range []bool{false, true} {
		t.Run(fmt.Sprintf("first_renders_%t", firstRenders), func(t *testing.T) {
			fixture := newRepositoryStateFixture(t)
			first := fixture.start(t.Context(), "first", firstRenders)
			fixture.awaitFirst(t)
			second := fixture.start(t.Context(), "second", !firstRenders)

			select {
			case <-fixture.secondRequested:
				t.Error("shared Helm state was accessed before the active transaction committed")
			case <-time.After(repositoryStateProbeWait):
			}

			fixture.release()

			firstResult := awaitRepositoryOperation(t, first)
			secondResult := awaitRepositoryOperation(t, second)
			require.NoError(t, firstResult.err)
			require.NoError(t, secondResult.err)

			if firstRenders {
				assert.Contains(t, firstResult.manifest, "name: fixture")
			} else {
				assert.Contains(t, secondResult.manifest, "name: fixture")
			}
		})
	}
}

//nolint:paralleltest // Fixture isolates process-global HELM_* environment variables.
func TestRepositoryStateWaitCancellation(t *testing.T) {
	for _, render := range []bool{false, true} {
		t.Run(fmt.Sprintf("render_%t", render), func(t *testing.T) {
			fixture := newRepositoryStateFixture(t)
			first := fixture.start(t.Context(), "first", false)
			fixture.awaitFirst(t)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			second := fixture.start(ctx, "second", render)

			// The active transaction remains blocked throughout cancellation.
			// The waiter must return without performing an index request.
			time.Sleep(repositoryStateProbeWait)
			cancel()

			result := awaitRepositoryOperation(t, second)
			require.ErrorIs(t, result.err, context.Canceled)

			select {
			case <-fixture.secondRequested:
				t.Error("cancelled waiter accessed shared Helm state")
			default:
			}

			fixture.release()
			require.NoError(t, awaitRepositoryOperation(t, first).err)
			// A cancelled waiter must not retain the acquisition token.
			require.NoError(t, awaitRepositoryOperation(t,
				fixture.start(t.Context(), "second", render)).err)
		})
	}
}

type repositoryOperationResult struct {
	manifest string
	err      error
}

type repositoryStateFixture struct {
	server          *httptest.Server
	repoFile        string
	firstRequested  chan struct{}
	secondRequested chan struct{}
	release         func()
	operations      sync.WaitGroup
}

func newRepositoryStateFixture(t *testing.T) *repositoryStateFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(root, "repositories.yaml"))
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(root, "repository"))
	t.Setenv("HELM_CONTENT_CACHE", filepath.Join(root, "content"))
	t.Setenv("HELM_REGISTRY_CONFIG", filepath.Join(root, "registry.json"))

	chart := repositoryFixtureArchive(t, root)

	fixture := &repositoryStateFixture{
		repoFile:        filepath.Join(root, "repositories.yaml"),
		firstRequested:  make(chan struct{}),
		secondRequested: make(chan struct{}, 1),
	}
	release := make(chan struct{})

	var releaseOnce, firstOnce sync.Once

	fixture.release = func() { releaseOnce.Do(func() { close(release) }) }
	fixture.server = httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, "/index.yaml") {
				if strings.HasPrefix(request.URL.Path, "/first/") {
					firstOnce.Do(func() { close(fixture.firstRequested) })
					<-release
				} else {
					select {
					case fixture.secondRequested <- struct{}{}:
					default:
					}
				}

				_, _ = writer.Write(
					[]byte("apiVersion: v1\nentries:\n  fixture:\n    - name: fixture\n" +
						"      version: 1.0.0\n      apiVersion: v2\n      urls:\n        - fixture-1.0.0.tgz\n"),
				)

				return
			}

			_, _ = writer.Write(chart)
		}),
	)

	t.Cleanup(func() {
		fixture.release()
		fixture.operations.Wait()
		fixture.server.Close()
	})

	return fixture
}

func repositoryFixtureArchive(t *testing.T, root string) []byte {
	t.Helper()

	archive, err := chartutil.Save(&chartv2.Chart{
		Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "fixture", Version: "1.0.0"},
		Templates: []*common.File{{
			Name: "templates/configmap.yaml",
			Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fixture\n"),
		}},
	}, root)
	require.NoError(t, err)
	directory, err := os.OpenRoot(root)

	require.NoError(t, err)
	defer func() { require.NoError(t, directory.Close()) }()

	chart, err := directory.ReadFile(filepath.Base(archive))
	require.NoError(t, err)

	return chart
}

func (fixture *repositoryStateFixture) start(
	ctx context.Context,
	name string,
	render bool,
) <-chan repositoryOperationResult {
	result := make(chan repositoryOperationResult, 1)

	fixture.operations.Go(func() {
		client, err := helm.NewTemplateOnlyClient()
		if err != nil {
			result <- repositoryOperationResult{err: err}

			return
		}

		if render {
			manifest, renderErr := client.TemplateChart(ctx, &helm.ChartSpec{
				ReleaseName: name, ChartName: "fixture", Version: "1.0.0",
				RepoURL: fixture.server.URL + "/" + name,
			})
			result <- repositoryOperationResult{manifest: manifest, err: renderErr}

			return
		}

		result <- repositoryOperationResult{err: client.AddRepository(ctx,
			&helm.RepositoryEntry{Name: name, URL: fixture.server.URL + "/" + name}, time.Second)}
	})

	return result
}

func (fixture *repositoryStateFixture) awaitFirst(t *testing.T) {
	t.Helper()

	select {
	case <-fixture.firstRequested:
	case <-time.After(5 * time.Second):
		t.Fatal("first transaction never reached its repository index request")
	}
}

func awaitRepositoryOperation(
	t *testing.T,
	result <-chan repositoryOperationResult,
) repositoryOperationResult {
	t.Helper()

	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("Helm operation did not finish")

		return repositoryOperationResult{}
	}
}
