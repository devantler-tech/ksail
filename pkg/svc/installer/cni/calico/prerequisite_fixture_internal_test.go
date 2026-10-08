package calicoinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func readInstallerInventory(
	ctx context.Context,
	installer *Installer,
) (*corev1.ConfigMap, prerequisiteState, error) {
	_, core, _, err := installer.prerequisiteClients()
	if err != nil {
		return nil, prerequisiteState{}, err
	}

	return readPrerequisiteState(ctx, core)
}

const prerequisiteFixtureManifest = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: installations.operator.tigera.io
spec:
  group: operator.tigera.io
  names: {kind: Installation, plural: installations}
  scope: Cluster
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema: {type: object}
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: calico-fixture
spec: {failurePolicy: Fail}
`

type prerequisiteFixture struct {
	mu          sync.Mutex
	objects     map[string]map[string]any
	writes      []string
	beforeWrite func(*http.Request, map[string]any)
	kubeconfig  string
	version     int
}

func newPrerequisiteFixture(t *testing.T) *prerequisiteFixture {
	t.Helper()

	fixture := &prerequisiteFixture{objects: make(map[string]map[string]any)}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	fixture.kubeconfig = filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(fixture.kubeconfig, fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster: {server: %s}
contexts:
- name: test-context
  context: {cluster: fixture, user: fixture}
current-context: test-context
users:
- name: fixture
  user: {}
`, server.URL), 0o600))

	return fixture
}

func (fixture *prerequisiteFixture) serve(writer http.ResponseWriter, request *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	writer.Header().Set("Content-Type", "application/json")

	if request.Method == http.MethodGet {
		fixture.serveGet(writer, request)

		return
	}

	var object map[string]any

	err := json.NewDecoder(request.Body).Decode(&object)
	if err != nil {
		fixtureStatus(writer, http.StatusBadRequest, "BadRequest")

		return
	}

	if fixture.beforeWrite != nil {
		fixture.beforeWrite(request, object)
	}

	if request.Method == http.MethodDelete {
		fixture.serveDelete(writer, request, object)

		return
	}

	fixture.serveWrite(writer, request, object)
}

func (fixture *prerequisiteFixture) serveWrite(
	writer http.ResponseWriter,
	request *http.Request,
	object map[string]any,
) {
	meta := fixtureMetadata(object)

	name, valid := meta["name"].(string)
	if !valid || name == "" {
		fixtureStatus(writer, http.StatusBadRequest, "BadRequest")

		return
	}

	path := request.URL.Path
	if request.Method == http.MethodPost {
		path += "/" + name
	}

	live := fixture.objects[path]
	if request.Method == http.MethodPost && live != nil {
		fixtureStatus(writer, http.StatusConflict, "AlreadyExists")

		return
	}

	if !fixtureWriteIdentity(meta, live, name) {
		fixtureStatus(writer, http.StatusConflict, "Conflict")

		return
	}

	fixture.version++
	meta["resourceVersion"] = strconv.Itoa(fixture.version)

	if object["kind"] == "CustomResourceDefinition" {
		object["status"] = map[string]any{
			"conditions": []any{map[string]any{"type": "Established", "status": "True"}},
		}
	}

	fixture.objects[path] = object
	fixture.writes = append(fixture.writes, request.Method+" "+path)

	fixtureJSON(writer, object)
}

func fixtureWriteIdentity(meta, live map[string]any, name string) bool {
	if live == nil {
		meta["uid"] = "owned-" + name

		return true
	}

	liveMeta := fixtureMetadata(live)
	for _, field := range []string{"uid", "resourceVersion"} {
		if meta[field] != nil && meta[field] != liveMeta[field] {
			return false
		}
	}

	meta["uid"] = liveMeta["uid"]

	return true
}

func fixtureMetadata(object map[string]any) map[string]any {
	meta, _ := object["metadata"].(map[string]any)

	return meta
}

func fixtureLabels(object map[string]any) map[string]any {
	labels, _ := fixtureMetadata(object)["labels"].(map[string]any)

	return labels
}

func fixtureJSON(writer http.ResponseWriter, object any) {
	err := json.NewEncoder(writer).Encode(object)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (fixture *prerequisiteFixture) serveDelete(
	writer http.ResponseWriter,
	request *http.Request,
	object map[string]any,
) {
	path := request.URL.Path

	live := fixture.objects[path]
	if live == nil {
		fixtureStatus(writer, http.StatusNotFound, "NotFound")

		return
	}

	preconditions, _ := object["preconditions"].(map[string]any)

	liveMeta := fixtureMetadata(live)
	if preconditions["uid"] != liveMeta["uid"] ||
		preconditions["resourceVersion"] != liveMeta["resourceVersion"] {
		fixtureStatus(writer, http.StatusConflict, "Conflict")

		return
	}

	delete(fixture.objects, path)
	fixture.writes = append(fixture.writes, request.Method+" "+path)

	fixtureStatus(writer, http.StatusOK, "Success")
}

func (fixture *prerequisiteFixture) serveGet(writer http.ResponseWriter, request *http.Request) {
	if object := fixture.objects[request.URL.Path]; object != nil {
		fixtureJSON(writer, object)

		return
	}

	if !fixtureCollection(request.URL.Path) {
		fixtureStatus(writer, http.StatusNotFound, "NotFound")

		return
	}

	items := make([]any, 0)

	for path, object := range fixture.objects {
		if !strings.HasPrefix(path, request.URL.Path+"/") ||
			!fixtureMatchesSelector(request, object) {
			continue
		}

		items = append(items, object)
	}

	fixtureJSON(writer, map[string]any{
		"apiVersion": "meta.k8s.io/v1", "kind": "PartialObjectMetadataList", "items": items,
	})
}

func fixtureMatchesSelector(request *http.Request, object map[string]any) bool {
	selector := request.URL.Query().Get("labelSelector")
	if selector == "" {
		return true
	}

	expectedRelease := "calico-crds"
	if selector == "owner=helm,name=calico" {
		expectedRelease = "calico"
	}

	labels, _ := fixtureMetadata(object)["labels"].(map[string]any)

	return labels["owner"] == "helm" && labels["name"] == expectedRelease
}

func fixtureCollection(path string) bool {
	for _, name := range []string{
		"namespaces", "secrets", "configmaps", "customresourcedefinitions",
		"mutatingadmissionpolicies", "mutatingadmissionpolicybindings",
		"validatingadmissionpolicies", "validatingadmissionpolicybindings",
	} {
		if strings.HasSuffix(path, "/"+name) {
			return true
		}
	}

	return false
}

func fixtureStatus(writer http.ResponseWriter, code int, reason string) {
	writer.WriteHeader(code)
	fixtureJSON(writer, map[string]any{
		"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": reason, "code": code,
	})
}

const fixtureOperatorHistoryPath = "/api/v1/namespaces/tigera-operator/secrets/sh.helm.release.v1.calico.v1"

func (fixture *prerequisiteFixture) seedOperatorHistory() {
	fixture.objects[fixtureOperatorHistoryPath] = map[string]any{
		"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{
			"name": "sh.helm.release.v1.calico.v1", "namespace": prerequisiteNamespace,
			"uid": "operator-history", "resourceVersion": "4",
			"labels": map[string]any{"owner": "helm", "name": "calico"},
		},
	}
}

func (fixture *prerequisiteFixture) seedRecordedIdentity(t *testing.T) {
	t.Helper()
	fixture.setInventory(t, prerequisiteState{Resources: []prerequisiteRef{{
		Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
		Name: "installations.operator.tigera.io", UID: "original",
	}}})
	fixture.objects[fixtureCRDPath] = fixtureCRD("replacement", "2")
}
