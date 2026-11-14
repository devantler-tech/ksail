package helm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
---
# A cluster-scoped object whose template still sets the release namespace.
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: cert-manager-webhook
  namespace: cert-manager
`

	inClusterDestination = `"destination":{"server":"https://kubernetes.default.svc"}`
)

type argoOwnershipFixture struct {
	items     []string
	paginated bool
	status    int
	// clusterSecrets are the Argo CD cluster secrets the API serves; clusterSecretsStatus, when
	// set, fails that list instead.
	clusterSecrets       []string
	clusterSecretsStatus int
	wantOwner            string
	wantError            string
}

// storedRecords is a release record list and the storage path the API serves it at.
type storedRecords struct {
	path string
	list string
}

func argoClusterSecret(name, server string) string {
	return fmt.Sprintf(
		`{"metadata":{"name":%q,"namespace":"argocd","labels":{"argocd.argoproj.io/secret-type":"cluster"}},`+
			`"data":{"name":%q,"server":%q}}`,
		name,
		base64.StdEncoding.EncodeToString([]byte(name)),
		base64.StdEncoding.EncodeToString([]byte(server)),
	)
}

// argoOwnershipDestinationFixtures covers destinations that name a registered cluster.
func argoOwnershipDestinationFixtures() map[string]argoOwnershipFixture {
	deployment := `{"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager"}`
	named := `"destination":{"name":"local-cluster"}`

	return map[string]argoOwnershipFixture{
		"destination names the in-cluster server under another name": {
			items: []string{argoApplication("certs", named, deployment)},
			clusterSecrets: []string{
				argoClusterSecret("remote", "https://remote.example:6443"),
				argoClusterSecret("local-cluster", "https://kubernetes.default.svc"),
			},
			wantOwner: "argocd/certs",
		},
		"destination names a remote cluster": {
			items: []string{argoApplication("certs", named, deployment)},
			clusterSecrets: []string{
				argoClusterSecret("local-cluster", "https://remote.example:6443"),
			},
		},
		"destination names an unregistered cluster": {
			items: []string{argoApplication("certs", named, deployment)},
		},
		"cluster secrets unreadable": {
			items:                []string{argoApplication("certs", named, deployment)},
			clusterSecretsStatus: http.StatusForbidden,
			wantError:            "Argo CD cluster secrets",
		},
	}
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

// argoOwnershipTrackingFixtures covers how Argo CD lists objects: a cluster-scoped object
// without the namespace its template set, and an object it only lists for pruning.
func argoOwnershipTrackingFixtures() map[string]argoOwnershipFixture {
	return map[string]argoOwnershipFixture{
		"tracks a cluster-scoped object templated with a namespace": {
			items: []string{
				argoApplication(
					"certs",
					inClusterDestination,
					`{"group":"admissionregistration.k8s.io","kind":"ValidatingWebhookConfiguration","name":"cert-manager-webhook"}`,
				),
			},
			wantOwner: "argocd/certs",
		},
		"object listed only for pruning": {
			items: []string{
				argoApplication(
					"cert-manager",
					inClusterDestination,
					`{"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager","requiresPruning":true}`,
				),
			},
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
	maps.Copy(fixtures, argoOwnershipTrackingFixtures())
	maps.Copy(fixtures, argoOwnershipErrorFixtures())
	maps.Copy(fixtures, argoOwnershipDestinationFixtures())

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

// argoReleaseRecordCase is a release stored in a way the Argo CD lookup must still read.
type argoReleaseRecordCase struct {
	driver  string
	records []*v1.Release
}

func argoReleaseRecordCases() map[string]argoReleaseRecordCase {
	return map[string]argoReleaseRecordCase{
		"ConfigMap release storage": {
			driver:  "configmap",
			records: []*v1.Release{argoRelease(1, argoReleaseManifest, common.StatusDeployed)},
		},
		"failed upgrade dropped the tracked object": {
			driver: "secret",
			records: []*v1.Release{
				argoRelease(1, argoReleaseManifest, common.StatusDeployed),
				argoRelease(2, `apiVersion: v1
kind: ConfigMap
metadata:
  name: cert-manager-settings
`, common.StatusFailed),
			},
		},
		"object declared inside a List document": {
			driver: "secret",
			records: []*v1.Release{argoRelease(1, `apiVersion: v1
kind: List
items:
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: cert-manager
`, common.StatusDeployed)},
		},
	}
}

func TestReleaseLabelsArgoCDLookupReadsEveryReleaseRecordShape(t *testing.T) {
	deployment := `{"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager"}`

	for name, recordCase := range argoReleaseRecordCases() {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HELM_DRIVER", recordCase.driver)

			client := newArgoOwnershipClient(t, argoOwnershipFixture{
				items: []string{argoApplication("certs", inClusterDestination, deployment)},
			}, releaseRecordList(t, recordCase.driver, recordCase.records...))

			labels, err := client.GetReleaseStorageLabels(
				context.Background(), argoReleaseName, argoReleaseNamespace,
			)

			require.NoError(t, err)
			assert.Equal(t, "argocd/certs", labels[helm.ArgoCDManagedByLabel])
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
	}, storedRecords{path: argoReleaseStoragePath("secrets"), list: unreadable})

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

func argoRelease(version int, manifest string, status common.Status) *v1.Release {
	return &v1.Release{
		Name:      argoReleaseName,
		Namespace: argoReleaseNamespace,
		Version:   version,
		Manifest:  manifest,
		Info:      &v1.Info{Status: status},
	}
}

func argoReleaseStoragePath(resource string) string {
	return "/api/v1/namespaces/" + argoReleaseNamespace + "/" + resource
}

// releaseSecretList renders the deployed Helm release Secret Kubernetes would return for the
// release.
func releaseSecretList(t *testing.T) storedRecords {
	t.Helper()

	return releaseRecordList(
		t, "secret", argoRelease(1, argoReleaseManifest, common.StatusDeployed),
	)
}

// releaseRecordList renders the release records Kubernetes would return from the storage the
// HELM_DRIVER value selects, encoded by Helm's own storage driver.
func releaseRecordList(t *testing.T, storage string, records ...*v1.Release) storedRecords {
	t.Helper()

	clientset := fake.NewClientset()

	var driver helmv4driver.Driver = helmv4driver.NewSecrets(
		clientset.CoreV1().Secrets(argoReleaseNamespace),
	)
	if storage == "configmap" {
		driver = helmv4driver.NewConfigMaps(clientset.CoreV1().ConfigMaps(argoReleaseNamespace))
	}

	for _, record := range records {
		require.NoError(t, driver.Create(
			fmt.Sprintf("sh.helm.release.v1.%s.v%d", argoReleaseName, record.Version), record,
		))
	}

	if storage == "configmap" {
		configMaps, err := clientset.CoreV1().ConfigMaps(argoReleaseNamespace).List(
			context.Background(), metav1.ListOptions{},
		)
		require.NoError(t, err)

		configMaps.APIVersion, configMaps.Kind = "v1", "ConfigMapList"
		for index := range configMaps.Items {
			configMaps.Items[index].UID = "storage-uid"
		}

		encoded, err := json.Marshal(configMaps)
		require.NoError(t, err)

		return storedRecords{path: argoReleaseStoragePath("configmaps"), list: string(encoded)}
	}

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

	return storedRecords{path: argoReleaseStoragePath("secrets"), list: string(encoded)}
}

func newArgoOwnershipClient(
	t *testing.T, fixture argoOwnershipFixture, records storedRecords,
) *helm.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")

			switch request.URL.Path {
			case "/apis/argoproj.io/v1alpha1/applications":
				writeArgoApplications(response, request, fixture)
			case "/api/v1/secrets":
				writeArgoClusterSecrets(response, fixture)
			default:
				writeArgoReleaseSecrets(response, request, records)
			}
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
	records storedRecords,
) {
	if request.URL.Path != records.path {
		http.NotFound(response, request)

		return
	}

	_, _ = fmt.Fprint(response, records.list)
}

// writeArgoClusterSecrets serves the cluster-wide list of Argo CD cluster secrets.
func writeArgoClusterSecrets(response http.ResponseWriter, fixture argoOwnershipFixture) {
	if fixture.clusterSecretsStatus != 0 {
		response.WriteHeader(fixture.clusterSecretsStatus)
		_, _ = fmt.Fprintf(
			response,
			`{"apiVersion":"v1","kind":"Status","status":"Failure","code":%d}`,
			fixture.clusterSecretsStatus,
		)

		return
	}

	_, _ = fmt.Fprintf(
		response,
		`{"apiVersion":"v1","kind":"SecretList","items":[%s]}`,
		strings.Join(fixture.clusterSecrets, ","),
	)
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
