package talosprovisioner

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const upgradeDryRunObject = `{"apiVersion":"v1","kind":"ConfigMap",` +
	`"metadata":{"name":"kubeconfig-in-cluster","namespace":"kube-system"}}`

func gracefulAPIServerShutdown() error {
	return http2.GoAwayError{LastStreamID: 89, ErrCode: http2.ErrCodeNo}
}

// The manifest diff issues this dry-run apply while the API server may still
// be restarting from the upgrade step that preceded it.
func TestKubernetesUpgradeDryRunRecoversAfterGracefulShutdown(t *testing.T) {
	t.Parallel()

	var bodies []string

	config := &rest.Config{
		Host: "https://upgrade.invalid",
		Transport: upgradeRoundTripper(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodPatch, request.Method)
			require.Equal(t, []string{metav1.DryRunAll}, request.URL.Query()["dryRun"])

			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.NoError(t, request.Body.Close())

			bodies = append(bodies, string(body))

			if len(bodies) == 1 {
				return nil, gracefulAPIServerShutdown()
			}

			return upgradeJSONResponse(request, upgradeDryRunObject), nil
		}),
	}
	// Zero waits: the restart is simulated rather than waited out.
	provider := upgradeKubernetesProvider{K8sProvider: upgradeConfigProvider{config: config}}
	upgradeConfig, err := provider.K8sRestConfig(t.Context())
	require.NoError(t, err)

	client, err := dynamic.NewForConfig(upgradeConfig)
	require.NoError(t, err)

	force := true
	options := metav1.PatchOptions{
		DryRun:       []string{metav1.DryRunAll},
		FieldManager: "talos",
		Force:        &force,
	}

	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).
		Namespace("kube-system").
		Patch(t.Context(), "kubeconfig-in-cluster", types.ApplyPatchType,
			[]byte(upgradeDryRunObject), options)
	require.NoError(t, err)

	require.Len(t, bodies, 2)
	assert.JSONEq(t, upgradeDryRunObject, bodies[0])
	assert.Equal(t, bodies[0], bodies[1], "the replay must resend the identical body")
}
