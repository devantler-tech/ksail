package talosprovisioner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentConfigRequiresEstablishedNodeInventory(t *testing.T) {
	t.Parallel()

	unknownWorker := strings.Replace(
		fipUpdateWorkerServerJSON,
		`"ksail.node.type":"worker"`,
		`"ksail.node.type":"unknown"`,
		1,
	)
	workersOnly := []string{fipUpdateWorkerServerJSON}
	unknownRole := []string{fipUpdateControlPlaneServerJSON, unknownWorker}

	established := []string{
		fipUpdateControlPlaneServerJSON,
		fipUpdateSecondControlPlaneServerJSON,
		fipUpdateWorkerServerJSON,
	}
	for _, testCase := range []struct {
		name    string
		status  int
		servers []string
		refuse  bool
	}{
		{name: "provider read failure", status: http.StatusForbidden, refuse: true},
		{name: "empty inventory", status: http.StatusOK, refuse: true},
		{name: "missing control plane", status: http.StatusOK, servers: workersOnly, refuse: true},
		{name: "unknown role", status: http.StatusOK, servers: unknownRole, refuse: true},
		{name: "established capacity", status: http.StatusOK, servers: established},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provider := nodeInventoryTestProvider(t, testCase.status, testCase.servers)
			provisioner := newFloatingIPTestProvisioner(
				t,
				v1alpha1.OptionsHetzner{},
			).WithInfraProvider(provider)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			spec, providerSpec, err := provisioner.GetCurrentConfig(ctx, "fip-cluster")
			if testCase.refuse {
				require.Error(
					t,
					err,
					"unknown capacity must not become a fabricated scale baseline",
				)
				assert.Nil(t, spec)
				assert.Nil(t, providerSpec)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, spec)
			assert.Equal(t, int32(2), spec.ControlPlanes)
			assert.Equal(t, int32(1), spec.Workers)
		})
	}
}

func nodeInventoryTestProvider(t *testing.T, status int, servers []string) *hetzner.Provider {
	t.Helper()

	handler := fipUpdateServersHandler(servers)
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(
				t,
				http.MethodGet,
				request.Method,
				"current-state discovery must remain read-only",
			)

			if status != http.StatusOK {
				http.Error(writer, "inventory unavailable", status)

				return
			}

			handler(writer, request)
		}),
	)
	t.Cleanup(server.Close)

	return hetzner.NewProvider(
		hcloud.NewClient(hcloud.WithToken("fixture"), hcloud.WithEndpoint(server.URL)),
	)
}
