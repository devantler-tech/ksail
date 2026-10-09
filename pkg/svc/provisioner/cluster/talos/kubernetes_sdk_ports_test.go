package talosprovisioner_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/go-connections/nat"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var errUnexpectedSDKProvision = errors.New("unexpected SDK provisioning")

func newSDKPortBindingFixture(t *testing.T) <-chan nat.PortMap {
	t.Helper()

	captures := make(chan nat.PortMap, 1)
	versionPrefix := regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			path := versionPrefix.ReplaceAllString(request.URL.Path, "")

			writer.Header().Set("Content-Type", "application/json")

			switch request.Method + " " + path {
			case "HEAD /_ping", "GET /_ping":
				writer.Header().Set("Api-Version", "1.55")
				writer.Header().Set("Os-Type", "linux")
				writer.WriteHeader(http.StatusOK)
			case "GET /images/json":
				_, _ = io.WriteString(writer, `[{}]`)
			case "GET /networks":
				_, _ = io.WriteString(writer, `[]`)
			case "POST /networks/create":
				writer.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(writer, `{"Id":"fixture-network","Warning":""}`)
			case "POST /containers/create":
				var createRequest struct {
					HostConfig struct {
						PortBindings nat.PortMap `json:"portBindings"`
					} `json:"hostConfig"`
				}

				err := json.NewDecoder(request.Body).Decode(&createRequest)
				if err != nil {
					t.Error(err)
				}

				captures <- createRequest.HostConfig.PortBindings

				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(writer, `{"message":"fixture-stop-after-create"}`)
			default:
				t.Errorf("unexpected SDK request: %s %s", request.Method, request.URL.Path)
				http.Error(writer, "unexpected SDK request", http.StatusBadRequest)
			}
		}),
	)
	t.Cleanup(server.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_API_VERSION", "")

	return captures
}

// The real SDK serializes the KSail request. The fake stops before creating a container.
//
//nolint:paralleltest // The SDK reads process-wide Docker environment set by the fixture.
func TestNestedTalosSDKKeepsBootstrapAndAddsPodBinding(t *testing.T) {
	captures := newSDKPortBindingFixture(t)

	inner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "demo"),
		talosprovisioner.NewOptions().WithExtraPortMappings([]string{"127.0.0.1:18080:8080/tcp"}),
	).WithLogWriter(io.Discard)
	prov, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			InnerProvisioner: inner,
			HostClientset:    fake.NewClientset(ownedDinDPod()),
			ClusterName:      "demo",
		},
	)
	require.NoError(t, err)

	_, err = prov.ProvisionNestedClusterForTest(context.Background(), "demo")
	require.ErrorContains(t, err, "fixture-stop-after-create")

	var captured nat.PortMap

	select {
	case captured = <-captures:
	default:
		t.Fatal("must reach the real SDK's container request")
	}

	require.NotNil(t, captured, "must reach the real SDK's container request")

	defaultHostIP := "0.0.0.0" // The SDK's existing Docker VM default on macOS/Windows.
	if runtime.GOOS == "linux" {
		defaultHostIP = "127.0.0.1"
	}

	assert.Contains(t, captured["6443/tcp"], nat.PortBinding{HostIP: "10.42.0.5", HostPort: ""})
	require.Len(t, captured["6443/tcp"], 2)
	assert.Equal(t, defaultHostIP, captured["6443/tcp"][0].HostIP)
	port, err := strconv.Atoi(captured["6443/tcp"][0].HostPort)
	require.NoError(t, err)
	assert.Positive(t, port)
	require.Len(t, captured["50000/tcp"], 1)
	assert.Equal(t, defaultHostIP, captured["50000/tcp"][0].HostIP)
	port, err = strconv.Atoi(captured["50000/tcp"][0].HostPort)
	require.NoError(t, err)
	assert.Positive(t, port)
	assert.Equal(
		t,
		[]nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "18080"}},
		captured["8080/tcp"],
	)
	assert.Equal(t, []string{"127.0.0.1:18080:8080/tcp"}, inner.Options().ExtraPortMappings)
}

func TestNestedTalosRejectsInvalidPodBeforeSDK(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{"unmanaged", func(p *corev1.Pod) { delete(p.Labels, kubeprovider.LabelManagedBy) }},
		{"foreign_cluster", func(p *corev1.Pod) { p.Labels[kubeprovider.LabelClusterName] = "foreign" }},
		{"foreign_app", func(p *corev1.Pod) { p.Labels[kubeprovider.LabelApp] = "foreign" }},
		{"host_network", func(p *corev1.Pod) { p.Spec.HostNetwork = true }},
		{"not_ready", func(p *corev1.Pod) { p.Status.Conditions = nil }},
		{"not_running", func(p *corev1.Pod) { p.Status.Phase = corev1.PodPending }},
		{"terminating", func(p *corev1.Pod) { now := metav1.Now(); p.DeletionTimestamp = &now }},
		{"missing_ip", func(p *corev1.Pod) { p.Status.PodIP = "" }},
		{"wildcard_ip", func(p *corev1.Pod) { p.Status.PodIP = "0.0.0.0" }},
		{"loopback_ip", func(p *corev1.Pod) { p.Status.PodIP = "127.0.0.1" }},
		{"multicast_ip", func(p *corev1.Pod) { p.Status.PodIP = "224.0.0.1" }},
		{"ipv6_ip", func(p *corev1.Pod) { p.Status.PodIP = "fd00::5" }},
		{"malformed_ip", func(p *corev1.Pod) { p.Status.PodIP = "bad-address" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pod := ownedDinDPod()
			testCase.mutate(pod)

			factoryCalled := false
			inner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "demo"), nil).
				WithProvisionerFactory(func(context.Context) (provision.Provisioner, error) {
					factoryCalled = true

					return nil, errUnexpectedSDKProvision
				})
			prov, err := talosprovisioner.NewKubernetesProvisioner(
				talosprovisioner.KubernetesProvisionerConfig{
					InnerProvisioner: inner,
					HostClientset:    fake.NewClientset(pod),
					ClusterName:      "demo",
				},
			)
			require.NoError(t, err)

			_, err = prov.ProvisionNestedClusterForTest(context.Background(), "demo")
			require.ErrorIs(t, err, talosprovisioner.ErrInvalidDinDAPIHost)
			assert.False(t, factoryCalled, "invalid observations must prevent SDK provisioning")
		})
	}
}

func ownedDinDPod() *corev1.Pod {
	labels := kubeprovider.CommonLabels("demo")
	labels[kubeprovider.LabelApp] = kubeprovider.DinDPodName

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubeprovider.DinDPodName, Namespace: "ksail-demo", Labels: labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.42.0.5",
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}
