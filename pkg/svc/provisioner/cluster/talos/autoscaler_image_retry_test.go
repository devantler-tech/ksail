package talosprovisioner_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

const autoscalerRetryEnvironmentVariable = "KSAIL_TEST_IMAGE"

func TestAutoscalerImageRefreshMigratesLegacySecret(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	client := autoscalerImageRetryClient(t)
	secret, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	secret.Data = map[string][]byte{
		"hcloud_image":      []byte("1"),
		"hcloud_cloud_init": []byte("legacy-config"),
		"extra_key":         []byte("preserved"),
	}
	_, err = client.CoreV1().Secrets("kube-system").Update(
		t.Context(), secret, metav1.UpdateOptions{},
	)
	require.NoError(t, err)

	serverLists := &atomic.Int32{}
	serverLists.Store(3)
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	configs := loadConfigs(t)
	require.NoError(t, newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"))

	migrated, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), secret.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	image, err := talosprovisioner.SnapshotImageIDFromSecretForTest(migrated)
	require.NoError(t, err)
	assert.Equal(t, "2", image)
	assert.Equal(t, secret.Data["hcloud_image"], migrated.Data["hcloud_image"])
	assert.Equal(t, secret.Data["hcloud_cloud_init"], migrated.Data["hcloud_cloud_init"])
	assert.Equal(t, secret.Data["extra_key"], migrated.Data["extra_key"])
	assert.Greater(t, serverLists.Load(), int32(3), "migration must converge existing capacity")
	assert.NotContains(t, migrated.Annotations, "ksail.io/autoscaler-image-rollout-pending")
}

// Propagation can return nil while recording individual node failures. Such an
// attempt must keep a durable retry signal even after its Secret is up to date.
func TestAutoscalerImageRefreshRetainsPendingOnRecordedFailure(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	serverLists.Store(3)
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	configs := loadConfigs(t)
	result := clusterupdate.NewEmptyUpdateResult()
	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{})

	err := newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretWithResultForTest(t.Context(), "test-cluster", result)
	require.ErrorContains(t, err, "configuration changes failed")
	secret, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "2", secret.Annotations["ksail.io/autoscaler-image-rollout-pending"])

	require.NoError(t, newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"))
	retried, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		secret.Data,
		retried.Data,
		"the successful retry leaves the desired template unchanged",
	)
	assert.NotContains(t, retried.Annotations, "ksail.io/autoscaler-image-rollout-pending")
}

func TestAutoscalerImageRefreshAtZeroCapacityWaitsForActivation(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	serverLists.Store(3)
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	configs := loadConfigs(t)
	require.NoError(t, newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"))
	secret, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	secret.Annotations["ksail.io/autoscaler-image-rollout-pending"] = "2"
	_, err = client.CoreV1().
		Secrets("kube-system").
		Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)

	deployment := autoscalerDeployment(true)
	deployment.Generation = 2
	deployment.Status.ObservedGeneration = 1
	deployment.Spec.Replicas = new(int32(1))
	_, err = client.AppsV1().Deployments("kube-system").Create(
		t.Context(), deployment, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	provisioner := newAutoscalerImageRetryProvisioner(t, server.URL, configs)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err = provisioner.EnsureAutoscalerSecretIfNeededForTest(ctx, "test-cluster")
	require.ErrorContains(t, err, "rollout")
	secret, err = client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "2", secret.Annotations["ksail.io/autoscaler-image-rollout-pending"])

	deployment.Status.ObservedGeneration = 2
	_, err = client.AppsV1().Deployments("kube-system").UpdateStatus(
		t.Context(), deployment, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	require.NoError(t, newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"))
	secret, err = client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.NotContains(t, secret.Annotations, "ksail.io/autoscaler-image-rollout-pending")
}

// Saving the new image is not evidence that existing autoscaler nodes adopted it.
// A fresh invocation must keep attempting convergence after an interrupted recycle,
// even when the Secret and static-node diff are already up to date.
func TestAutoscalerImageRefreshRetriesInterruptedConvergence(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	configs := loadConfigs(t)
	newProvisioner := func() *talosprovisioner.Provisioner {
		return newAutoscalerImageRetryProvisioner(t, server.URL, configs)
	}

	err := newProvisioner().EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "listing autoscaler nodes")
	secret, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	image, err := talosprovisioner.SnapshotImageIDFromSecretForTest(secret)
	require.NoError(t, err)
	assert.Equal(t, "2", image, "the desired Secret was saved before convergence failed")
	assert.Equal(t, "2", secret.Annotations["ksail.io/autoscaler-image-rollout-pending"])

	err = newProvisioner().EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "resolving address for old-worker",
		"retry must attempt the old node instead of reporting success with an unchanged Secret")
	assert.EqualValues(t, 2, serverLists.Load())

	// The fixture now reports no old capacity. Successful convergence acknowledges
	// the baseline, so another identical invocation does not recycle nodes again.
	require.NoError(
		t,
		newProvisioner().EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"),
	)
	assert.EqualValues(t, 4, serverLists.Load())
	secret, err = client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.NotContains(t, secret.Annotations, "ksail.io/autoscaler-image-rollout-pending")
	assert.Equal(t, "external", secret.Annotations["example.com/preserved"])
	require.NoError(
		t,
		newProvisioner().EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster"),
	)
	// An unchanged Secret with no pending image only audits: one read-only listing
	// that reports servers of removed pools. Any drain or delete would reach a path
	// the fixture rejects.
	assert.EqualValues(t, 5, serverLists.Load())
}

func TestAutoscalerImageRefreshRetriesInterruptedRestart(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	server := autoscalerImageRetryServer(t, client, serverLists, true)
	configs := loadConfigs(t)
	err := newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "restarting cluster-autoscaler")
	assert.Zero(t, serverLists.Load(), "failed restart must not drain any nodes")

	err = newAutoscalerImageRetryProvisioner(t, server.URL, configs).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "resolving address for old-worker")
	assert.EqualValues(
		t,
		1,
		serverLists.Load(),
		"retry must restart and resume the pending recycle",
	)
}

func TestAutoscalerImageRefreshWithoutSnapshotManagerDoesNotRecycle(t *testing.T) {
	t.Setenv(autoscalerRetryEnvironmentVariable, "test-token")

	// Start past the scripted failures: every listing returns no servers, and the
	// fixture rejects any request that would drain or delete one.
	serverLists := &atomic.Int32{}
	serverLists.Store(3)

	client := autoscalerImageRetryClient(t)
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	configs := loadConfigs(t)
	err := newAutoscalerImageRetryProvisioner(t, server.URL, configs).WithSnapshotManager(nil).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.NoError(t, err)
	assert.EqualValues(t, 4, serverLists.Load(),
		"an unavailable image only audits the inventory; it must not start a recycle")

	_, err = talosprovisioner.ApplyAutoscalerConfigSecret(t.Context(), client, "2", nil)
	require.NoError(t, err)
	err = newAutoscalerImageRetryProvisioner(t, server.URL, configs).WithSnapshotManager(nil).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "autoscaler snapshot image is unavailable")
	assert.EqualValues(
		t,
		4,
		serverLists.Load(),
		"a pending rollout must fail before listing or draining without a valid image",
	)
}

func autoscalerImageRetryClient(t *testing.T) *fake.Clientset {
	t.Helper()

	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "hcloud", Namespace: "kube-system"},
		Data: map[string][]byte{
			"token":   []byte("test-token"),
			"network": []byte("test-network"),
		},
	})
	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(t.Context(), client, "1", nil)
	require.NoError(t, err)
	initial, err := client.CoreV1().Secrets("kube-system").Get(
		t.Context(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	initial.Annotations = map[string]string{"example.com/preserved": "external"}
	_, err = client.CoreV1().
		Secrets("kube-system").
		Update(t.Context(), initial, metav1.UpdateOptions{})
	require.NoError(t, err)

	return client
}

// newAutoscalerImageRetryProvisioner returns a fresh provisioner, as a new invocation
// would. Every invocation of one test shares configs: a real update syncs the
// cluster's identity before it renders the autoscaler Secret, so repeated invocations
// render the same per-pool worker config instead of looking like a Secret change.
func newAutoscalerImageRetryProvisioner(
	t *testing.T,
	serverURL string,
	configs *talosconfigmanager.Configs,
) *talosprovisioner.Provisioner {
	t.Helper()

	cloudClient := hcloud.NewClient(hcloud.WithToken("test-token"), hcloud.WithEndpoint(serverURL))

	return talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions().
		WithKubeconfigPath(autoscalerBaselineKubeconfig(t, serverURL)).
		WithKubeconfigContext("test")).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			NetworkName:             "test-network",
			TokenEnvVar:             autoscalerRetryEnvironmentVariable,
			AutoscalerNodePoolNames: []string{"workers"},
			AutoscalerNodePools:     []v1alpha1.NodePool{{Name: "workers"}},
		}).
		WithTalosOptions(v1alpha1.OptionsTalos{SchematicID: "test-schematic", Version: "v1.13.3"}).
		WithTalosConfigsForTest(configs).
		WithInfraProvider(hetzner.NewProvider(cloudClient)).
		WithSnapshotManager(hetzner.NewSnapshotManager(cloudClient, io.Discard)).
		WithLogWriter(io.Discard)
}

func autoscalerImageRetryServer(
	t *testing.T,
	client *fake.Clientset,
	serverLists *atomic.Int32,
	interruptRestart bool,
) *httptest.Server {
	t.Helper()

	deploymentLists := &atomic.Int32{}
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			switch request.URL.Path {
			case "/api/v1/namespaces/kube-system/secrets/hcloud",
				"/api/v1/namespaces/kube-system/secrets/cluster-autoscaler-config":
				serveAutoscalerRetrySecret(t, client, writer, request)
			case "/apis/apps/v1/namespaces/kube-system/deployments":
				if interruptRestart && deploymentLists.Add(1) == 1 {
					writer.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(
						writer,
						`{"apiVersion":"v1","kind":"Status","status":"Failure",`+
							`"message":"fixture interrupts restart","reason":"BadRequest","code":400}`,
					)

					return
				}

				serveAutoscalerRetryDeploymentList(t, client, writer, request)
			case "/apis/apps/v1/namespaces/kube-system/deployments/cluster-autoscaler":
				serveAutoscalerRetryDeployment(t, client, writer, request)
			case "/images":
				_, _ = io.WriteString(
					writer,
					`{"images":[{"id":2,"status":"available","type":"snapshot"}]}`,
				)
			case "/networks":
				_, _ = io.WriteString(
					writer,
					`{"networks":[{"id":42,"name":"test-cluster-network"}]}`,
				)
			case "/servers":
				serveAutoscalerRetryServers(writer, serverLists.Add(1), interruptRestart)
			default:
				t.Errorf("unexpected fixture request: %s %s", request.Method, request.URL.Path)
				http.NotFound(writer, request)
			}
		}),
	)
	t.Cleanup(server.Close)

	return server
}

func serveAutoscalerRetryDeploymentList(
	t *testing.T,
	client *fake.Clientset,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()

	deployments, err := client.AppsV1().Deployments("kube-system").List(
		request.Context(), metav1.ListOptions{},
	)
	require.NoError(t, err)

	deployments.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DeploymentList"}
	assert.NoError(t, json.NewEncoder(writer).Encode(deployments))
}

func serveAutoscalerRetryDeployment(
	t *testing.T,
	client *fake.Clientset,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()

	deployments := client.AppsV1().Deployments("kube-system")

	var deployment *appsv1.Deployment

	var err error

	if request.Method == http.MethodPatch {
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)

		deployment, err = deployments.Patch(request.Context(), "cluster-autoscaler",
			types.StrategicMergePatchType, body, metav1.PatchOptions{})
	} else {
		deployment, err = deployments.Get(
			request.Context(),
			"cluster-autoscaler",
			metav1.GetOptions{},
		)
	}

	require.NoError(t, err)

	deployment.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	assert.NoError(t, json.NewEncoder(writer).Encode(deployment))
}

func serveAutoscalerRetryServers(writer http.ResponseWriter, count int32, interruptRestart bool) {
	if count == 1 && !interruptRestart {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(
			writer,
			`{"error":{"code":"invalid_input","message":"fixture interrupts recycle"}}`,
		)

		return
	}

	if (!interruptRestart && count == 2) || (interruptRestart && count == 1) {
		_, _ = io.WriteString(
			writer,
			`{"servers":[{"id":7,"name":"old-worker","labels":{"hcloud/node-group":"workers"},`+
				`"private_net":[{"network":42}]}]}`,
		)

		return
	}

	_, _ = io.WriteString(writer, `{"servers":[]}`)
}

func serveAutoscalerRetrySecret(
	t *testing.T,
	client *fake.Clientset,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()

	var secret *corev1.Secret

	var err error

	secrets := client.CoreV1().Secrets("kube-system")

	if request.Method == http.MethodPut {
		secret = &corev1.Secret{}

		var body []byte

		body, err = io.ReadAll(request.Body)
		if err == nil {
			_, _, err = scheme.Codecs.UniversalDeserializer().Decode(body, nil, secret)
		}

		if err == nil {
			secret, err = secrets.Update(request.Context(), secret, metav1.UpdateOptions{})
		}
	} else {
		secret, err = secrets.Get(
			request.Context(),
			request.URL.Path[len("/api/v1/namespaces/kube-system/secrets/"):],
			metav1.GetOptions{},
		)
	}

	if err != nil {
		t.Errorf("fixture Secret operation: %v", err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	secret.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
	assert.NoError(t, json.NewEncoder(writer).Encode(secret))
}
