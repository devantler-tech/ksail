package helm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/require"
	helmaction "helm.sh/helm/v4/pkg/action"
	helmcli "helm.sh/helm/v4/pkg/cli"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const uninstallFixtureManifest = `apiVersion: v1
kind: ConfigMap
metadata: {name: owned, namespace: fixture}
data: {purpose: uninstall-regression}
`

type uninstallFixture struct {
	mu                                                     sync.Mutex
	deleted, hookCreated, hookCompleted, deletedBeforeHook bool
	hookMode                                               string
}

func writeUninstallJSON(response http.ResponseWriter, body any) {
	err := json.NewEncoder(response).Encode(body)
	if err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
	}
}

func uninstallStatus(reason metav1.StatusReason, code int32) metav1.Status {
	status := metav1.StatusSuccess
	if reason != "" {
		status = metav1.StatusFailure
	}

	return metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   status,
		Reason:   reason,
		Code:     code,
	}
}

func (fixture *uninstallFixture) serve(response http.ResponseWriter, request *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	response.Header().Set("Content-Type", "application/json")

	var body any

	switch request.URL.Path {
	case "/version":
		body = map[string]string{"major": "1", "minor": "35", "gitVersion": "v1.35.0"}
	case "/openapi/v2":
		writeMigrationSchema(response)

		return
	case "/api":
		body = map[string]any{"kind": "APIVersions", "versions": []string{"v1"}}
	case "/apis":
		body = map[string]any{"kind": "APIGroupList", "groups": []any{
			map[string]any{
				"name": "batch",
				"versions": []any{
					map[string]string{"groupVersion": "batch/v1", "version": "v1"},
				},
				"preferredVersion": map[string]string{"groupVersion": "batch/v1", "version": "v1"},
			},
		}}
	case "/api/v1":
		body = uninstallAPIResources("v1", "configmaps", "ConfigMap")
	case "/apis/batch/v1":
		body = uninstallAPIResources("batch/v1", "jobs", "Job")
	case "/api/v1/namespaces/fixture/configmaps/owned":
		fixture.serveConfigMap(response, request)

		return
	case "/apis/batch/v1/namespaces/fixture/jobs",
		"/apis/batch/v1/namespaces/fixture/jobs/pre-delete":
		fixture.serveHook(response, request)

		return
	default:
		http.NotFound(response, request)

		return
	}

	writeUninstallJSON(response, body)
}

func (fixture *uninstallFixture) serveConfigMap(
	response http.ResponseWriter,
	request *http.Request,
) {
	switch {
	case request.Method == http.MethodDelete:
		fixture.deletedBeforeHook = fixture.hookMode != "" && !fixture.hookCompleted
		fixture.deleted = true

		writeUninstallJSON(response, uninstallStatus("", http.StatusOK))
	case fixture.deleted:
		response.WriteHeader(http.StatusNotFound)
		writeUninstallJSON(
			response,
			uninstallStatus(metav1.StatusReasonNotFound, http.StatusNotFound),
		)
	default:
		writeUninstallJSON(response, corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{
				Name:            "owned",
				Namespace:       "fixture",
				UID:             "owned-uid",
				ResourceVersion: "1",
			},
		})
	}
}

func uninstallAPIResources(version, resource, kind string) map[string]any {
	return map[string]any{"kind": "APIResourceList", "groupVersion": version, "resources": []any{
		map[string]any{
			"name": resource, "kind": kind, "namespaced": true,
			"verbs": []string{"get", "list", "watch", "create", "delete", "patch"},
		},
	}}
}

func uninstallHookJob(mode string) batchv1.Job {
	job := batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "pre-delete",
			Namespace:       "fixture",
			UID:             "hook-uid",
			ResourceVersion: "1",
		},
	}

	switch mode {
	case "complete":
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
	case "failed":
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
		}
	}

	return job
}

func writeUninstallHookWatch(response http.ResponseWriter, request *http.Request, job batchv1.Job) {
	writeUninstallJSON(response, map[string]any{"type": "ADDED", "object": job})

	if request.URL.Query().Get("sendInitialEvents") == "true" {
		bookmark := batchv1.Job{
			TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "1",
				Annotations:     map[string]string{"k8s.io/initial-events-end": "true"},
			},
		}
		writeUninstallJSON(response, map[string]any{"type": "BOOKMARK", "object": bookmark})
	}

	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (fixture *uninstallFixture) serveHook(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodDelete {
		response.WriteHeader(http.StatusNotFound)
		writeUninstallJSON(
			response,
			uninstallStatus(metav1.StatusReasonNotFound, http.StatusNotFound),
		)

		return
	}

	if request.Method == http.MethodPost || request.Method == http.MethodPatch {
		fixture.hookCreated = true
		if fixture.hookMode == "create-fails" {
			response.WriteHeader(http.StatusForbidden)
			writeUninstallJSON(
				response,
				uninstallStatus(metav1.StatusReasonForbidden, http.StatusForbidden),
			)

			return
		}
	}

	job := uninstallHookJob(fixture.hookMode)

	switch {
	case request.URL.Query().Get("watch") == "true":
		fixture.hookCompleted = fixture.hookMode == "complete"

		writeUninstallHookWatch(response, request, job)
	case request.URL.Path == "/apis/batch/v1/namespaces/fixture/jobs" && request.Method == http.MethodGet:
		fixture.hookCompleted = fixture.hookMode == "complete"

		writeUninstallJSON(response, batchv1.JobList{
			TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "JobList"},
			ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: []batchv1.Job{job},
		})
	default:
		writeUninstallJSON(response, job)
	}
}

func newUninstallRuntimeClient(t *testing.T, hookMode string) (*helm.Client, *uninstallFixture) {
	t.Helper()

	fixture := &uninstallFixture{hookMode: hookMode}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"test": {}},
		Contexts: map[string]*clientcmdapi.Context{
			"test": {Cluster: "test", AuthInfo: "test"},
		},
		CurrentContext: "test",
	}, path))

	settings := helmcli.New()
	settings.KubeConfig = path
	settings.SetNamespace("fixture")

	cfg := new(helmaction.Configuration)
	require.NoError(t, cfg.Init(settings.RESTClientGetter(), "fixture", "memory"))

	release := &releasev1.Release{
		Name: "fixture", Namespace: "fixture", Version: 1, Manifest: uninstallFixtureManifest,
		Info: &releasev1.Info{Status: releasecommon.StatusDeployed},
	}
	if hookMode != "" {
		release.Hooks = []*releasev1.Hook{
			{
				Name:   "pre-delete",
				Kind:   "Job",
				Path:   "pre-delete.yaml",
				Events: []releasev1.HookEvent{releasev1.HookPreDelete},
				Manifest: "apiVersion: batch/v1\nkind: Job\nmetadata: {name: pre-delete, namespace: fixture}\n" +
					"spec: {template: {spec: {restartPolicy: Never, containers: [{name: hook, image: fixture}]}}}\n",
			},
		}
	}

	require.NoError(t, cfg.Releases.Create(release))

	return helm.NewClientFromParts(cfg, settings), fixture
}

func TestRealHelmUninstallReachesDeletion(t *testing.T) {
	t.Parallel()
	client, fixture := newUninstallRuntimeClient(t, "")
	require.NoError(t, client.UninstallRelease(t.Context(), "fixture", "fixture"))
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	require.True(t, fixture.deleted)
}

func TestRealHelmUninstallRunsPreDeleteHookBeforeDeletion(t *testing.T) {
	t.Parallel()
	client, fixture := newUninstallRuntimeClient(t, "complete")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	require.NoError(t, client.UninstallRelease(ctx, "fixture", "fixture"))
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	require.True(t, fixture.hookCreated)
	require.True(t, fixture.hookCompleted)
	require.False(t, fixture.deletedBeforeHook)
	require.True(t, fixture.deleted)
}

func TestRealHelmUninstallPreservesResourcesWhenHookFailsOrNeverCompletes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"create-fails", "failed", "never-completes"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			client, fixture := newUninstallRuntimeClient(t, mode)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			started := time.Now()
			err := client.UninstallRelease(ctx, "fixture", "fixture")

			require.Less(
				t,
				time.Since(started),
				5*time.Second,
				"caller cancellation must bound the hook wait",
			)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "wait strategy not set")

			switch mode {
			case "never-completes":
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case "failed":
				require.Contains(t, err.Error(), "Job Failed")
			}

			fixture.mu.Lock()
			defer fixture.mu.Unlock()

			require.True(t, fixture.hookCreated)
			require.False(t, fixture.deleted)
		})
	}
}
