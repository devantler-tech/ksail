package talosprovisioner_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

// Saving the new image is not evidence that existing autoscaler nodes adopted it.
// A fresh invocation must keep attempting convergence after an interrupted recycle,
// even when the Secret and static-node diff are already up to date.
func TestAutoscalerImageRefreshRetriesInterruptedConvergence(t *testing.T) {
	t.Setenv("KSAIL_TEST_AUTOSCALER_RETRY_TOKEN", "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	newProvisioner := func() *talosprovisioner.Provisioner {
		return newAutoscalerImageRetryProvisioner(t, server.URL)
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
	assert.EqualValues(t, 3, serverLists.Load())
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
	assert.EqualValues(t, 3, serverLists.Load())
}

func TestAutoscalerImageRefreshRetriesInterruptedRestart(t *testing.T) {
	t.Setenv("KSAIL_TEST_AUTOSCALER_RETRY_TOKEN", "test-token")

	client := autoscalerImageRetryClient(t)
	serverLists := &atomic.Int32{}
	server := autoscalerImageRetryServer(t, client, serverLists, true)
	err := newAutoscalerImageRetryProvisioner(t, server.URL).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "restarting cluster-autoscaler")
	assert.Zero(t, serverLists.Load(), "failed restart must not drain any nodes")

	err = newAutoscalerImageRetryProvisioner(t, server.URL).
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
	t.Setenv("KSAIL_TEST_AUTOSCALER_RETRY_TOKEN", "test-token")

	serverLists := &atomic.Int32{}
	client := autoscalerImageRetryClient(t)
	server := autoscalerImageRetryServer(t, client, serverLists, false)
	err := newAutoscalerImageRetryProvisioner(t, server.URL).WithSnapshotManager(nil).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.NoError(t, err)
	assert.Zero(t, serverLists.Load(), "an unavailable image must not trigger node recycling")

	_, err = talosprovisioner.ApplyAutoscalerConfigSecret(t.Context(), client, "2", nil)
	require.NoError(t, err)
	err = newAutoscalerImageRetryProvisioner(t, server.URL).WithSnapshotManager(nil).
		EnsureAutoscalerSecretIfNeededForTest(t.Context(), "test-cluster")
	require.ErrorContains(t, err, "autoscaler snapshot image is unavailable")
	assert.Zero(
		t,
		serverLists.Load(),
		"a pending rollout must fail before draining without a valid image",
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

func newAutoscalerImageRetryProvisioner(
	t *testing.T,
	serverURL string,
) *talosprovisioner.Provisioner {
	t.Helper()

	cloudClient := hcloud.NewClient(hcloud.WithToken("test-token"), hcloud.WithEndpoint(serverURL))

	return talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions().
		WithKubeconfigPath(autoscalerBaselineKubeconfig(t, serverURL)).
		WithKubeconfigContext("test")).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled: true, NetworkName: "test-network",
			TokenEnvVar: "KSAIL_TEST_AUTOSCALER_RETRY_TOKEN", AutoscalerNodePoolNames: []string{"workers"},
		}).
		WithTalosOptions(v1alpha1.OptionsTalos{SchematicID: "test-schematic", Version: "v1.13.3"}).
		WithTalosConfigsForTest(loadConfigs(t)).
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

				_, _ = io.WriteString(
					writer,
					`{"apiVersion":"apps/v1","kind":"DeploymentList","items":[]}`,
				)
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
			`{"servers":[{"id":7,"name":"old-worker","private_net":[{"network":42}]}]}`,
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
