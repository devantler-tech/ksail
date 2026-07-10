package talosprovisioner_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

const (
	testTypeCX22                 = "cx22"
	testTypeCX23                 = "cx23"
	testTypeCX33                 = "cx33"
	testRollingServerType        = "cpx41"
	testRollingNodeCP0           = "cp-0"
	testRollingNodeCP1           = "cp-1"
	testFieldHetznerCPServerType = "provider.hetzner.controlPlaneServerType"
)

func TestRolesFromRollingChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fields     []string
		wantCP     bool
		wantWorker bool
	}{
		{name: "no fields", fields: nil, wantCP: false, wantWorker: false},
		{
			name:   "control plane only",
			fields: []string{testFieldHetznerCPServerType},
			wantCP: true,
		},
		{
			name:       "worker only",
			fields:     []string{"provider.hetzner.workerServerType"},
			wantWorker: true,
		},
		{
			name: "both",
			fields: []string{
				testFieldHetznerCPServerType,
				"provider.hetzner.workerServerType",
			},
			wantCP:     true,
			wantWorker: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changes := make([]clusterupdate.Change, 0, len(testCase.fields))
			for _, field := range testCase.fields {
				changes = append(changes, clusterupdate.Change{Field: field})
			}

			gotCP, gotWorker := talosprovisioner.RolesFromRollingChangesForTest(changes)
			assert.Equal(t, testCase.wantCP, gotCP)
			assert.Equal(t, testCase.wantWorker, gotWorker)
		})
	}
}

func TestServersNeedingReplacement(t *testing.T) {
	t.Parallel()

	server := func(name, serverType string) *hcloud.Server {
		srv := &hcloud.Server{Name: name}
		if serverType != "" {
			srv.ServerType = &hcloud.ServerType{Name: serverType}
		}

		return srv
	}

	servers := []*hcloud.Server{
		server(testRollingNodeCP1, testTypeCX23),
		server("cp-2", testRollingServerType),
		server("cp-3", ""),
		nil,
	}

	out := talosprovisioner.ServersNeedingReplacementForTest(servers, testRollingServerType)

	// cp-1 (wrong type), cp-3 (unknown type) need replacement; cp-2 matches; nil skipped.
	names := make([]string, 0, len(out))
	for _, srv := range out {
		names = append(names, srv.Name)
	}

	assert.ElementsMatch(t, []string{testRollingNodeCP1, "cp-3"}, names)
}

func TestServersNeedingReplacement_CaseInsensitive(t *testing.T) {
	t.Parallel()

	servers := []*hcloud.Server{
		{Name: testRollingNodeCP1, ServerType: &hcloud.ServerType{Name: "CPX41"}},
	}

	out := talosprovisioner.ServersNeedingReplacementForTest(servers, testRollingServerType)
	assert.Empty(t, out, "matching type (case-insensitive) should not need replacement")
}

func TestAppendServerTypeChange(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name         string
		role         string
		current      string
		desired      string
		category     clusterupdate.ChangeCategory
		wantRolling  int
		wantRecreate int
	}{
		{
			name:        "rolling control plane",
			role:        talosprovisioner.RoleControlPlane,
			current:     testTypeCX23,
			desired:     testRollingServerType,
			category:    clusterupdate.ChangeCategoryRollingRecreate,
			wantRolling: 1,
		},
		{
			name:         "recreate below quorum",
			role:         talosprovisioner.RoleControlPlane,
			current:      testTypeCX23,
			desired:      testRollingServerType,
			category:     clusterupdate.ChangeCategoryRecreateRequired,
			wantRecreate: 1,
		},
		{
			name:     "in-place worker no existing nodes is dropped",
			role:     talosprovisioner.RoleWorker,
			current:  testTypeCX23,
			desired:  testRollingServerType,
			category: clusterupdate.ChangeCategoryInPlace,
		},
		{
			name:     "no change when types match",
			role:     talosprovisioner.RoleWorker,
			current:  testTypeCX23,
			desired:  testTypeCX23,
			category: clusterupdate.ChangeCategoryRollingRecreate,
		},
		{
			name:     "no change when current unknown",
			role:     talosprovisioner.RoleWorker,
			current:  "",
			desired:  testRollingServerType,
			category: clusterupdate.ChangeCategoryRollingRecreate,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diff := clusterupdate.NewEmptyUpdateResult()
			talosprovisioner.AppendServerTypeChangeForTest(
				diff, testCase.role, testCase.current, testCase.desired, testCase.category,
			)

			assert.Len(t, diff.RollingRecreate, testCase.wantRolling)
			assert.Len(t, diff.RecreateRequired, testCase.wantRecreate)
		})
	}
}

func TestCountServerNodesByRole(t *testing.T) {
	t.Parallel()

	nodes := []provider.NodeInfo{
		{Name: testRollingNodeCP0, Role: talosprovisioner.RoleControlPlane},
		{Name: testRollingNodeCP1, Role: talosprovisioner.RoleControlPlane},
		{Name: "cp-2", Role: talosprovisioner.RoleControlPlane},
		{Name: "worker-0", Role: talosprovisioner.RoleWorker},
		{Name: "unknown-0", Role: "unknown"},
	}

	cp, worker := talosprovisioner.CountServerNodesByRoleForTest(nodes)
	assert.Equal(t, 3, cp)
	assert.Equal(t, 1, worker)

	cpEmpty, workerEmpty := talosprovisioner.CountServerNodesByRoleForTest(nil)
	assert.Equal(t, 0, cpEmpty)
	assert.Equal(t, 0, workerEmpty)
}

func TestApplyRollingRecreateChanges_NoOp(t *testing.T) {
	t.Parallel()

	t.Run("no rolling changes is a no-op", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

		err := provisioner.ApplyRollingRecreateChangesForTest(
			context.Background(), "demo", clusterupdate.NewEmptyUpdateResult(),
		)
		require.NoError(t, err)
	})

	t.Run("rolling changes without a Hetzner provider is a no-op", func(t *testing.T) {
		t.Parallel()

		provisioner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

		result := clusterupdate.NewEmptyUpdateResult()
		result.RollingRecreate = append(result.RollingRecreate, clusterupdate.Change{
			Field: "provider.hetzner.controlPlaneServerType",
		})

		err := provisioner.ApplyRollingRecreateChangesForTest(context.Background(), "demo", result)
		require.NoError(t, err)
	})
}

// TestApplyRollingRecreateChanges_BlocksAbsentFloatingIP verifies that endpoint
// enablement or drift is reconciled in a separate update before any destructive
// control-plane replacement begins.
func TestApplyRollingRecreateChanges_BlocksAbsentFloatingIP(t *testing.T) {
	t.Parallel()

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{FloatingIPEnabled: true}).
		WithInfraProvider(newFipUpdateProvider("http://127.0.0.1")).
		WithLogWriter(io.Discard)

	result := clusterupdate.NewEmptyUpdateResult()
	result.RollingRecreate = append(result.RollingRecreate, clusterupdate.Change{
		Field: testFieldHetznerCPServerType,
	})
	result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
		Field: floatingIPEnabledField,
	})

	err := provisioner.ApplyRollingRecreateChangesForTest(t.Context(), "fip-cluster", result)
	require.ErrorIs(t, err, talosprovisioner.ErrFloatingIPReconcileBeforeControlPlaneRoll)
}

// TestReattachFloatingIPAfterControlPlaneReplacement_Unassigned verifies that
// deleting the control plane that owned the endpoint cannot leave its floating
// IP detached from the replacement server during a rolling recreate.
func TestReattachFloatingIPAfterControlPlaneReplacement_Unassigned(t *testing.T) {
	t.Parallel()

	var assignCalls atomic.Int32

	server := floatingIPEndpointTestServer(t, &assignCalls)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			FloatingIPEnabled:  true,
			FloatingIPLocation: "fsn1",
		}).
		WithLogWriter(io.Discard)

	err := provisioner.ReattachFloatingIPAfterControlPlaneReplacementForTest(
		t.Context(),
		newFipUpdateProvider(server.URL),
		"fip-cluster",
		&hcloud.Server{ID: 11, Name: "fip-cluster-cp-0"},
		&hcloud.Server{ID: 12, Name: "fip-cluster-cp-0"},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), assignCalls.Load())
}

// TestReattachFloatingIPAfterControlPlaneReplacement_DoesNotCreate verifies
// that rolling replacement only preserves an existing endpoint; enablement
// drift remains the later reconciliation step's responsibility.
func TestReattachFloatingIPAfterControlPlaneReplacement_DoesNotCreate(t *testing.T) {
	t.Parallel()

	calls := new(fipUpdateCalls)
	server := fipUpdateTestServer(t, false, calls)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			FloatingIPEnabled:  true,
			FloatingIPLocation: "fsn1",
		}).
		WithLogWriter(io.Discard)

	err := provisioner.ReattachFloatingIPAfterControlPlaneReplacementForTest(
		t.Context(),
		newFipUpdateProvider(server.URL),
		"fip-cluster",
		&hcloud.Server{ID: 11, Name: "fip-cluster-cp-0"},
		&hcloud.Server{ID: 12, Name: "fip-cluster-cp-0"},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(0), calls.create.Load())
	assert.Equal(t, int32(0), calls.assign.Load())
}

// TestReattachFloatingIPAfterControlPlaneReplacement_PreservesSurvivor verifies
// that a surviving control plane which already claimed the endpoint keeps it;
// rolling replacement must not cause a needless second assignment.
func TestReattachFloatingIPAfterControlPlaneReplacement_PreservesSurvivor(t *testing.T) {
	t.Parallel()

	var assignCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			switch request.URL.Path {
			case "/floating_ips":
				ownedBySurvivor := strings.Replace(
					fipUpdateOwnedFloatingIPJSON, `"server":null`, `"server":13`, 1,
				)
				_, _ = responseWriter.Write([]byte(`{"floating_ips":[` + ownedBySurvivor + `]}`))
			case "/floating_ips/7/actions/assign":
				assignCalls.Add(1)
				fipUpdateAssignActionResponse(responseWriter)
			default:
				http.NotFound(responseWriter, request)
			}
		},
	))
	t.Cleanup(server.Close)

	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			FloatingIPEnabled:  true,
			FloatingIPLocation: "fsn1",
		}).
		WithLogWriter(io.Discard)

	err := provisioner.ReattachFloatingIPAfterControlPlaneReplacementForTest(
		t.Context(),
		newFipUpdateProvider(server.URL),
		"fip-cluster",
		&hcloud.Server{ID: 11, Name: "fip-cluster-cp-0"},
		&hcloud.Server{ID: 12, Name: "fip-cluster-cp-0"},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(0), assignCalls.Load())
}

// TestFinishControlPlaneReplacement_WaitsAfterReattachError verifies that a
// failed endpoint operation is retried before the endpoint-dependent Kubernetes
// Ready gate, which must still run after the retry.
func TestFinishControlPlaneReplacement_WaitsAfterReattachError(t *testing.T) {
	t.Parallel()

	events := make([]string, 0, 3)
	attachAttempts := 0

	err := talosprovisioner.FinishControlPlaneReplacementForTest(
		func() error {
			attachAttempts++

			events = append(events, "attach")

			if attachAttempts == 1 {
				return context.DeadlineExceeded
			}

			return nil
		},
		func() error {
			events = append(events, "ready")

			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"attach", "attach", "ready"}, events)
}

func TestNodeMatchesServer(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{}
	node.Name = "ksail-control-plane-4"
	node.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
		{Type: corev1.NodeExternalIP, Address: "203.0.113.7"},
	}

	assert.True(
		t,
		talosprovisioner.NodeMatchesServerForTest(node, "ksail-control-plane-4", "1.2.3.4"),
		"should match by name",
	)
	assert.True(
		t,
		talosprovisioner.NodeMatchesServerForTest(node, "KSAIL-CONTROL-PLANE-4", "1.2.3.4"),
		"name match should be case-insensitive",
	)
	assert.True(t, talosprovisioner.NodeMatchesServerForTest(node, "other", "203.0.113.7"),
		"should match by external IP")
	assert.False(t, talosprovisioner.NodeMatchesServerForTest(node, "other", "9.9.9.9"),
		"should not match unrelated node")
}

func TestNodeIsReady(t *testing.T) {
	t.Parallel()

	ready := &corev1.Node{}
	ready.Status.Conditions = []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
	}
	assert.True(t, talosprovisioner.NodeIsReadyForTest(ready))

	notReady := &corev1.Node{}
	notReady.Status.Conditions = []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
	}
	assert.False(t, talosprovisioner.NodeIsReadyForTest(notReady))

	assert.False(t, talosprovisioner.NodeIsReadyForTest(&corev1.Node{}),
		"node with no conditions is not ready")
}
