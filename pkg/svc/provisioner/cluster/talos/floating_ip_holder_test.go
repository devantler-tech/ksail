package talosprovisioner_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFloatingIPEndpointPreservesCurrentControlPlaneHolder(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		holder      string
		wantAssign  int32
		wantRefusal bool
	}{
		{name: "unassigned initial endpoint", holder: "null", wantAssign: 1},
		{name: "first control plane already holds endpoint", holder: "1"},
		{name: "another control plane holds Talos VIP lease", holder: "2"},
		{name: "holder outside inventoried control planes", holder: "99", wantRefusal: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

			var assigns atomic.Int32

			server := floatingIPHolderTestServer(t, &assigns, testCase.holder)
			client := hcloud.NewClient(
				hcloud.WithToken("test-token"),
				hcloud.WithEndpoint(server.URL),
			)
			provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
				FloatingIPEnabled: true,
				TokenEnvVar:       testFloatingIPTokenEnvVar,
			})

			err := provisioner.UpdateConfigsWithEndpointForTest(
				t.Context(), hetzner.NewProvider(client), "fip-cluster", []*hcloud.Server{
					controlPlaneServer(1, "cp-1", "203.0.113.5"),
					controlPlaneServer(2, "cp-2", "203.0.113.6"),
				},
			)
			if testCase.wantRefusal {
				require.ErrorContains(t, err, "outside the control-plane inventory")
			} else {
				require.NoError(t, err)
				assertFloatingIPRenderedConfigs(t, provisioner.TalosConfigsForTest())
			}

			assert.Equal(
				t,
				testCase.wantAssign,
				assigns.Load(),
				"reconcile must not override an existing VIP holder",
			)
		})
	}
}

func floatingIPHolderTestServer(
	t *testing.T,
	assigns *atomic.Int32,
	holder string,
) *httptest.Server {
	t.Helper()

	payload := strings.Replace(fipUpdateOwnedFloatingIPJSON, `"server":null`, `"server":`+holder, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /floating_ips", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"floating_ips":[%s]}`, payload)
	})
	mux.HandleFunc(
		"POST /floating_ips/7/actions/assign",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			var assignment struct {
				Server int64 `json:"server"`
			}

			assert.NoError(t, json.NewDecoder(request.Body).Decode(&assignment))
			assert.Equal(
				t,
				int64(1),
				assignment.Server,
				"initial assignment must select the first control plane",
			)
			assigns.Add(1)
			fipUpdateAssignActionResponse(responseWriter)
		},
	)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}
