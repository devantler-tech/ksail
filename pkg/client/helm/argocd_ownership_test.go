package helm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmcli "helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/release/common"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	argoReleaseName      = "cert-manager"
	argoReleaseNamespace = "cert-manager"

	// argoReleaseManifest is what the KSail-installed release rendered: a namespaced object
	// without a namespace (Helm installs it into the release namespace), one with an explicit
	// namespace, and a cluster-scoped object.
	argoReleaseManifest = `---
# Source: cert-manager/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cert-manager
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cert-manager-webhook
  namespace: cert-manager
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-controller
`

	inClusterDestination = `"destination":{"server":"https://kubernetes.default.svc"}`
)

type argoOwnershipFixture struct {
	items     []string
	paginated bool
	status    int
	wantOwner string
	wantError string
}

func argoApplication(name, destination, resources string) string {
	return fmt.Sprintf(
		`{"metadata":{"name":%q,"namespace":"argocd"},"spec":{%s},"status":{"resources":[%s]}}`,
		name, destination, resources,
	)
}

func argoOwnershipFixtures() map[string]argoOwnershipFixture {
	deployment := `{"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager"}`

	return map[string]argoOwnershipFixture{
		"Argo CD API absent": {status: http.StatusNotFound},
		"no Applications":    {},
		"unrelated resources": {items: []string{argoApplication("apps", inClusterDestination,
			`{"group":"apps","kind":"Deployment","namespace":"apps","name":"cert-manager"}`)}},
		"tracks an object installed into the release namespace": {
			items:     []string{argoApplication("certs", inClusterDestination, deployment)},
			wantOwner: "argocd/certs",
		},
		"tracks an object with an explicit namespace": {
			items: []string{
				argoApplication(
					"certs",
					inClusterDestination,
					`{"kind":"ServiceAccount","namespace":"cert-manager","name":"cert-manager-webhook"}`,
				),
			},
			wantOwner: "argocd/certs",
		},
		"tracks a cluster-scoped object": {
			items: []string{
				argoApplication(
					"certs",
					`"destination":{"name":"in-cluster"}`,
					`{"group":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"cert-manager-controller"}`,
				),
			},
			wantOwner: "argocd/certs",
		},
		"same name in another group": {
			items: []string{
				argoApplication(
					"certs",
					inClusterDestination,
					`{"group":"example.com","kind":"Deployment","namespace":"cert-manager","name":"cert-manager"}`,
				),
			},
		},
		"Application for another cluster": {items: []string{argoApplication("remote",
			`"destination":{"server":"https://remote.example:6443"}`, deployment)}},
		"owner on a later list page": {
			items:     []string{argoApplication("certs", inClusterDestination, deployment)},
			paginated: true, wantOwner: "argocd/certs",
		},
	}
}

func argoOwnershipErrorFixtures() map[string]argoOwnershipFixture {
	return map[string]argoOwnershipFixture{
		"malformed Application": {
			items: []string{
				`{"metadata":{"name":"certs","namespace":"argocd"},"status":{"resources":"all"}}`,
			},
			wantError: "decode Argo CD release ownership",
		},
		"ownership access denied": {
			status: http.StatusForbidden, wantError: "Argo CD release ownership",
		},
		"ownership server failure": {
			status: http.StatusInternalServerError, wantError: "Argo CD release ownership",
		},
		"API disappears after a healthy list page": {
			status: http.StatusNotFound, paginated: true, wantError: "Argo CD release ownership",
		},
	}
}

func TestReleaseLabelsRecognizeArgoCDApplicationsThatTrackTheRelease(t *testing.T) {
	fixtures := argoOwnershipFixtures()
	maps.Copy(fixtures, argoOwnershipErrorFixtures())

	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HELM_DRIVER", "secret")

			client := newArgoOwnershipClient(t, fixture, releaseSecretList(t))

			labels, err := client.GetReleaseStorageLabels(
				context.Background(), argoReleaseName, argoReleaseNamespace,
			)
			if fixture.wantError != "" {
				require.ErrorContains(t, err, fixture.wantError)
				assert.Nil(t, labels)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "helm", labels["owner"], "the record's own labels are kept")

			owner, owned := labels[helm.ArgoCDManagedByLabel]
			assert.Equal(t, fixture.wantOwner != "", owned)
			assert.Equal(t, fixture.wantOwner, owner)
		})
	}
}

func TestReleaseLabelsFailClosedWhenTheReleaseRecordIsUnreadable(t *testing.T) {
	t.Setenv("HELM_DRIVER", "secret")

	unreadable := `{"apiVersion":"v1","kind":"SecretList","items":[{"metadata":{
"name":"sh.helm.release.v1.cert-manager.v1","namespace":"cert-manager","uid":"storage-uid",
"labels":{"name":"cert-manager","owner":"helm","version":"1"}},"data":{"release":"bm90IGEgcmVsZWFzZQ=="}}]}`
	client := newArgoOwnershipClient(t, argoOwnershipFixture{
		items: []string{argoApplication("certs", inClusterDestination, "")},
	}, unreadable)

	labels, err := client.GetReleaseStorageLabels(
		context.Background(), argoReleaseName, argoReleaseNamespace,
	)

	require.ErrorContains(t, err, "read Argo CD release ownership")
	assert.Nil(t, labels)
}

func TestReleaseLabelsArgoCDLookupHonoursTheCallerDeadline(t *testing.T) {
	t.Setenv("HELM_DRIVER", "secret")

	secretList := releaseSecretList(t)
	stalled := make(chan struct{})

	t.Cleanup(func() { close(stalled) })

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")

			switch request.URL.Path {
			case "/apis/argoproj.io/v1alpha1/applications":
				select {
				case <-stalled:
				case <-request.Context().Done():
				}
			default:
				writeArgoReleaseSecrets(response, request, secretList)
			}
		},
	))
	t.Cleanup(server.Close)

	client := helm.NewClientFromParts(nil, argoTestSettings(t, server.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()

	_, err := client.GetReleaseStorageLabels(ctx, argoReleaseName, argoReleaseNamespace)

	require.ErrorContains(t, err, "Argo CD release ownership")
	assert.Less(
		t,
		time.Since(started),
		10*time.Second,
		"a stalled Argo CD API must not hang the check",
	)
}

// releaseSecretList renders the Helm release Secret Kubernetes would return for the release,
// encoded by Helm's own storage driver.
func releaseSecretList(t *testing.T) string {
	t.Helper()

	clientset := fake.NewClientset()
	driver := helmv4driver.NewSecrets(clientset.CoreV1().Secrets(argoReleaseNamespace))
	require.NoError(t, driver.Create(
		"sh.helm.release.v1."+argoReleaseName+".v1",
		&v1.Release{
			Name:      argoReleaseName,
			Namespace: argoReleaseNamespace,
			Version:   1,
			Manifest:  argoReleaseManifest,
			Info:      &v1.Info{Status: common.StatusDeployed},
		},
	))

	secrets, err := clientset.CoreV1().Secrets(argoReleaseNamespace).List(
		context.Background(), metav1.ListOptions{},
	)
	require.NoError(t, err)

	secrets.APIVersion, secrets.Kind = "v1", "SecretList"
	for index := range secrets.Items {
		secrets.Items[index].UID = "storage-uid"
	}

	encoded, err := json.Marshal(secrets)
	require.NoError(t, err)

	return string(encoded)
}

func newArgoOwnershipClient(
	t *testing.T, fixture argoOwnershipFixture, secretList string,
) *helm.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")

			if request.URL.Path == "/apis/argoproj.io/v1alpha1/applications" {
				writeArgoApplications(response, request, fixture)

				return
			}

			writeArgoReleaseSecrets(response, request, secretList)
		},
	))
	t.Cleanup(server.Close)

	return helm.NewClientFromParts(nil, argoTestSettings(t, server.URL))
}

func writeArgoApplications(
	response http.ResponseWriter, request *http.Request, fixture argoOwnershipFixture,
) {
	if fixture.paginated && request.URL.Query().Get("continue") == "" {
		_, _ = fmt.Fprint(response, `{"apiVersion":"argoproj.io/v1alpha1","kind":"ApplicationList",
"metadata":{"continue":"next-page"},"items":[`+argoApplication("other", inClusterDestination, "")+`]}`)

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

	items := ""

	for index, item := range fixture.items {
		if index > 0 {
			items += ","
		}

		items += item
	}

	_, _ = fmt.Fprintf(
		response,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"ApplicationList","items":[%s]}`,
		items,
	)
}

func writeArgoReleaseSecrets(
	response http.ResponseWriter,
	request *http.Request,
	secretList string,
) {
	if request.URL.Path != "/api/v1/namespaces/"+argoReleaseNamespace+"/secrets" {
		http.NotFound(response, request)

		return
	}

	_, _ = fmt.Fprint(response, secretList)
}

func argoTestSettings(t *testing.T, serverURL string) *helmcli.EnvSettings {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"test": {Server: serverURL}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"test": {}},
		Contexts: map[string]*clientcmdapi.Context{
			"test": {Cluster: "test", AuthInfo: "test"},
		},
		CurrentContext: "test",
	}, path))

	settings := helmcli.New()
	settings.KubeConfig = path

	return settings
}
