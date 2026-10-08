package helm_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmcli "helm.sh/helm/v4/pkg/cli"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type fluxOwnershipFixture struct {
	item      string
	release   string
	namespace string
	paginated bool
	status    int
	wantOwner bool
	wantError string
}

func fluxOwnershipFixtures() map[string]fluxOwnershipFixture {
	fixtures := map[string]fluxOwnershipFixture{
		"default name and namespace": {
			item:      `{"metadata":{"name":"cert-manager","namespace":"cert-manager"},"spec":{}}`,
			wantOwner: true,
		},
		"explicit release and target in another storage namespace": {
			item: `{"metadata":{"name":"certificates","namespace":"flux-system"},
"spec":{"releaseName":"cert-manager","targetNamespace":"cert-manager","storageNamespace":"helm-storage"}}`,
			wantOwner: true,
		},
		"composed default release name": {
			item:      `{"metadata":{"name":"manager","namespace":"flux-system"},"spec":{"targetNamespace":"cert"}}`,
			namespace: "cert", wantOwner: true,
		},
		"shortened declared release name without history": {
			item: `{"metadata":{"name":"controller","namespace":"cert-manager"},
"spec":{"releaseName":"release-name-with-very-long-name-which-is-longer-than-53-characters"}}`,
			release: "release-name-with-very-long-name-which-i-788ca0d0d7b0", wantOwner: true,
		},
		"still-owned previous release name": {
			item: `{"metadata":{"name":"new-name","namespace":"cert-manager"},"spec":{},
"status":{"history":[{"name":"cert-manager","namespace":"cert-manager"}]}}`,
			wantOwner: true,
		},
		"still-owned previous target namespace": {
			item: `{"metadata":{"name":"cert-manager","namespace":"flux-system"},
"spec":{"targetNamespace":"new-target"},
"status":{"history":[{"name":"cert-manager","namespace":"cert-manager"}]}}`,
			wantOwner: true,
		},
		"owner on a later list page": {
			item:      `{"metadata":{"name":"cert-manager","namespace":"cert-manager"},"spec":{}}`,
			paginated: true, wantOwner: true,
		},
		"wrong release": {
			item: `{"metadata":{"name":"other","namespace":"cert-manager"},"spec":{}}`,
		},
		"wrong target namespace": {
			item: `{"metadata":{"name":"cert-manager","namespace":"other"},"spec":{}}`,
		},
		"remote cluster release": {
			item: `{"metadata":{"name":"cert-manager","namespace":"cert-manager"},
"spec":{"kubeConfig":{"secretRef":{"name":"remote"}}}}`,
		},
		"no HelmReleases": {},
		"Flux API absent": {status: http.StatusNotFound},
	}

	maps.Copy(fixtures, fluxOwnershipErrorFixtures())

	return fixtures
}

func fluxOwnershipErrorFixtures() map[string]fluxOwnershipFixture {
	return map[string]fluxOwnershipFixture{
		"malformed ownership response": {
			item:      `{"metadata":{"name":"cert-manager","namespace":"cert-manager"},"spec":{"releaseName":42}}`,
			wantError: "decode Flux release ownership",
		},
		"ownership access denied": {
			status:    http.StatusForbidden,
			wantError: "Flux release ownership",
		},
		"ownership server failure": {
			status:    http.StatusInternalServerError,
			wantError: "Flux release ownership",
		},
		"access denied after a healthy list page": {
			status: http.StatusForbidden, paginated: true,
			wantError: "Flux release ownership",
		},
		"API disappears after a healthy list page": {
			status: http.StatusNotFound, paginated: true,
			wantError: "Flux release ownership",
		},
		"server failure after a healthy list page": {
			status: http.StatusInternalServerError, paginated: true,
			wantError: "Flux release ownership",
		},
	}
}

func TestReleaseLabelsRecognizeFluxWithoutStorageLabels(t *testing.T) {
	t.Setenv("HELM_DRIVER", "secret")

	for name, test := range fluxOwnershipFixtures() {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HELM_DRIVER", "secret")
			assertFluxOwnership(t, test)
		})
	}
}

func assertFluxOwnership(t *testing.T, test fluxOwnershipFixture) {
	t.Helper()

	releaseName, namespace := test.release, test.namespace
	if releaseName == "" {
		releaseName = "cert-manager"
	}

	if namespace == "" {
		namespace = "cert-manager"
	}

	client := newFluxOwnershipClient(t, test, namespace)

	labels, err := client.GetReleaseStorageLabels(context.Background(), releaseName, namespace)
	if test.wantError != "" {
		require.ErrorContains(t, err, test.wantError)
		assert.Nil(t, labels)

		return
	}

	require.NoError(t, err)

	_, owned := labels["helm.toolkit.fluxcd.io/name"]
	assert.Equal(t, test.wantOwner, owned)
}

func newFluxOwnershipClient(
	t *testing.T,
	fixture fluxOwnershipFixture,
	namespace string,
) *helm.Client {
	t.Helper()

	server := httptest.NewServer(fluxOwnershipHandler(fixture, namespace))
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

	return helm.NewClientFromParts(nil, settings)
}

func fluxOwnershipHandler(fixture fluxOwnershipFixture, namespace string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")

		switch request.URL.Path {
		case "/apis/helm.toolkit.fluxcd.io/v2/helmreleases":
			if fixture.paginated && request.URL.Query().Get("continue") == "" {
				_, _ = fmt.Fprint(
					response,
					`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmReleaseList",
"metadata":{"continue":"next-page"},
"items":[{"metadata":{"name":"unrelated","namespace":"other"},"spec":{}}]}`,
				)

				return
			}

			if fixture.status != 0 {
				response.WriteHeader(fixture.status)
				_, _ = fmt.Fprintf(
					response,
					`{"apiVersion":"v1","kind":"Status","status":"Failure","code":%d}`,
					fixture.status,
				)

				return
			}

			_, _ = fmt.Fprintf(
				response,
				`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmReleaseList","items":[%s]}`,
				fixture.item,
			)
		case "/api/v1/namespaces/" + namespace + "/secrets":
			_, _ = fmt.Fprint(
				response,
				`{"apiVersion":"v1","kind":"SecretList","items":[{"metadata":{
"name":"sh.helm.release.v1.cert-manager.v1","namespace":"cert-manager","uid":"storage-uid",
"labels":{"name":"cert-manager","owner":"helm","version":"1"}}}]}`,
			)
		default:
			http.NotFound(response, request)
		}
	})
}
