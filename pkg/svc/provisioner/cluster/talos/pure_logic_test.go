package talosprovisioner_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- dockerNodeName ---

func TestDockerNodeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		role        string
		index       int
		want        string
	}{
		{
			name:        "control-plane index 0",
			clusterName: "my-cluster",
			role:        talosprovisioner.RoleControlPlane,
			index:       0,
			want:        "my-cluster-controlplane-1",
		},
		{
			name:        "control-plane index 2",
			clusterName: "my-cluster",
			role:        talosprovisioner.RoleControlPlane,
			index:       2,
			want:        "my-cluster-controlplane-3",
		},
		{
			name:        "worker index 0",
			clusterName: "test",
			role:        talosprovisioner.RoleWorker,
			index:       0,
			want:        "test-worker-1",
		},
		{
			name:        "worker index 4",
			clusterName: "prod",
			role:        talosprovisioner.RoleWorker,
			index:       4,
			want:        "prod-worker-5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.DockerNodeNameForTest(tc.clusterName, tc.role, tc.index)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- talosTypeFromRole ---

func TestTalosTypeFromRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role string
		want string
	}{
		{
			name: "control-plane maps to controlplane",
			role: talosprovisioner.RoleControlPlane,
			want: "controlplane",
		},
		{
			name: "worker maps to worker",
			role: talosprovisioner.RoleWorker,
			want: "worker",
		},
		{
			name: "unknown role maps to worker",
			role: "unknown",
			want: "worker",
		},
		{
			name: "empty role maps to worker",
			role: "",
			want: "worker",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.TalosTypeFromRoleForTest(tc.role)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- calculateNodeIP ---

func TestCalculateNodeIP(t *testing.T) {
	t.Parallel()

	cidr := netip.MustParsePrefix("10.5.0.0/24")

	tests := []struct {
		name      string
		cidr      netip.Prefix
		role      string
		nodeIndex int
		cpCount   int
		wantIP    string
		wantErr   bool
	}{
		{
			name:      "first control-plane node",
			cidr:      cidr,
			role:      talosprovisioner.RoleControlPlane,
			nodeIndex: 0,
			cpCount:   3,
			wantIP:    "10.5.0.2",
		},
		{
			name:      "third control-plane node",
			cidr:      cidr,
			role:      talosprovisioner.RoleControlPlane,
			nodeIndex: 2,
			cpCount:   3,
			wantIP:    "10.5.0.4",
		},
		{
			name:      "first worker with 3 CPs",
			cidr:      cidr,
			role:      talosprovisioner.RoleWorker,
			nodeIndex: 0,
			cpCount:   3,
			wantIP:    "10.5.0.5",
		},
		{
			name:      "second worker with 3 CPs",
			cidr:      cidr,
			role:      talosprovisioner.RoleWorker,
			nodeIndex: 1,
			cpCount:   3,
			wantIP:    "10.5.0.6",
		},
		{
			name:      "worker with 1 CP",
			cidr:      cidr,
			role:      talosprovisioner.RoleWorker,
			nodeIndex: 0,
			cpCount:   1,
			wantIP:    "10.5.0.3",
		},
		{
			name:      "IPv6 CIDR returns error",
			cidr:      netip.MustParsePrefix("fd00::/64"),
			role:      talosprovisioner.RoleControlPlane,
			nodeIndex: 0,
			cpCount:   1,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := talosprovisioner.CalculateNodeIPForTest(tc.cidr, tc.role, tc.nodeIndex, tc.cpCount)
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, talosprovisioner.ErrIPv6NotSupported)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantIP, got.String())
		})
	}
}

// --- preCalculateNodeSpecs ---

func TestPreCalculateNodeSpecs(t *testing.T) {
	t.Parallel()

	cidr := netip.MustParsePrefix("10.5.0.0/24")

	tests := []struct {
		name      string
		role      string
		nextIndex int
		count     int
		cpCount   int
		wantNames []string
		wantIPs   []string
		wantErr   bool
	}{
		{
			name:      "3 control-plane nodes starting at 0",
			role:      talosprovisioner.RoleControlPlane,
			nextIndex: 0,
			count:     3,
			cpCount:   3,
			wantNames: []string{"test-controlplane-1", "test-controlplane-2", "test-controlplane-3"},
			wantIPs:   []string{"10.5.0.2", "10.5.0.3", "10.5.0.4"},
		},
		{
			name:      "2 workers starting at 0 with 3 CPs",
			role:      talosprovisioner.RoleWorker,
			nextIndex: 0,
			count:     2,
			cpCount:   3,
			wantNames: []string{"test-worker-1", "test-worker-2"},
			wantIPs:   []string{"10.5.0.5", "10.5.0.6"},
		},
		{
			name:      "zero count returns empty",
			role:      talosprovisioner.RoleControlPlane,
			nextIndex: 0,
			count:     0,
			cpCount:   1,
			wantNames: []string{},
			wantIPs:   []string{},
		},
		{
			name:      "continues from non-zero index",
			role:      talosprovisioner.RoleControlPlane,
			nextIndex: 2,
			count:     2,
			cpCount:   4,
			wantNames: []string{"test-controlplane-3", "test-controlplane-4"},
			wantIPs:   []string{"10.5.0.4", "10.5.0.5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			names, ips, err := talosprovisioner.PreCalculateNodeSpecsForTest(
				cidr, "test", tc.role, tc.nextIndex, tc.count, tc.cpCount,
			)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Len(t, names, len(tc.wantNames))
			assert.Len(t, ips, len(tc.wantIPs))

			for i, wantName := range tc.wantNames {
				assert.Equal(t, wantName, names[i], "name mismatch at index %d", i)
			}

			for i, wantIP := range tc.wantIPs {
				assert.Equal(t, wantIP, ips[i].String(), "IP mismatch at index %d", i)
			}
		})
	}
}

// --- nthIPInNetwork ---

func TestNthIPInNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefix  netip.Prefix
		offset  int
		wantIP  string
		wantErr error
	}{
		{
			name:   "offset 0 returns network base",
			prefix: netip.MustParsePrefix("10.0.0.0/24"),
			offset: 0,
			wantIP: "10.0.0.0",
		},
		{
			name:   "offset 1 returns gateway",
			prefix: netip.MustParsePrefix("10.0.0.0/24"),
			offset: 1,
			wantIP: "10.0.0.1",
		},
		{
			name:   "offset 2 returns first node",
			prefix: netip.MustParsePrefix("10.0.0.0/24"),
			offset: 2,
			wantIP: "10.0.0.2",
		},
		{
			name:   "offset crosses byte boundary",
			prefix: netip.MustParsePrefix("10.0.0.0/24"),
			offset: 256,
			wantIP: "10.0.1.0",
		},
		{
			name:    "negative offset returns error",
			prefix:  netip.MustParsePrefix("10.0.0.0/24"),
			offset:  -1,
			wantErr: talosprovisioner.ErrNegativeOffset,
		},
		{
			name:    "IPv6 returns error",
			prefix:  netip.MustParsePrefix("fd00::/64"),
			offset:  1,
			wantErr: talosprovisioner.ErrIPv6NotSupported,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := talosprovisioner.NthIPInNetworkForTest(tc.prefix, tc.offset)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantIP, got.String())
		})
	}
}

// --- rewriteKubeconfigEndpoint ---

func TestRewriteKubeconfigEndpoint(t *testing.T) {
	t.Parallel()

	validKubeconfig := []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://10.0.0.2:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: admin
  name: admin@test-cluster
current-context: admin@test-cluster
users:
- name: admin
  user: {}
`)

	tests := []struct {
		name        string
		kubeconfig  []byte
		endpoint    string
		wantContain string
		wantErr     bool
	}{
		{
			name:        "rewrites endpoint",
			kubeconfig:  validKubeconfig,
			endpoint:    "https://127.0.0.1:6443",
			wantContain: "server: https://127.0.0.1:6443",
		},
		{
			name:        "empty endpoint returns unchanged",
			kubeconfig:  validKubeconfig,
			endpoint:    "",
			wantContain: "server: https://10.0.0.2:6443",
		},
		{
			name:       "invalid YAML returns error",
			kubeconfig: []byte("not: valid: kubeconfig: [broken"),
			endpoint:   "https://127.0.0.1:6443",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := talosprovisioner.RewriteKubeconfigEndpointForTest(tc.kubeconfig, tc.endpoint)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, string(result), tc.wantContain)
		})
	}
}

// --- applyTalosDefaults ---

func TestApplyTalosDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    v1alpha1.OptionsTalos
		wantISO int64
	}{
		{
			name:    "sets default ISO when zero",
			opts:    v1alpha1.OptionsTalos{},
			wantISO: v1alpha1.DefaultTalosISO,
		},
		{
			name:    "preserves non-zero ISO",
			opts:    v1alpha1.OptionsTalos{ISO: 99999},
			wantISO: 99999,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.ApplyTalosDefaultsForTest(tc.opts)
			assert.Equal(t, tc.wantISO, result.ISO)
		})
	}
}

// --- applyHetznerDefaults ---

//nolint:funlen // Table-driven test with comprehensive default scenarios
func TestApplyHetznerDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     v1alpha1.OptionsHetzner
		wantCP   string
		wantWk   string
		wantLoc  string
		wantCIDR string
		wantTok  string
	}{
		{
			name:     "sets all defaults when empty",
			opts:     v1alpha1.OptionsHetzner{},
			wantCP:   v1alpha1.DefaultHetznerServerType,
			wantWk:   v1alpha1.DefaultHetznerServerType,
			wantLoc:  v1alpha1.DefaultHetznerLocation,
			wantCIDR: v1alpha1.DefaultHetznerNetworkCIDR,
			wantTok:  v1alpha1.DefaultHetznerTokenEnvVar,
		},
		{
			name: "preserves all custom values",
			opts: v1alpha1.OptionsHetzner{
				ControlPlaneServerType: "cpx21",
				WorkerServerType:       "cpx31",
				Location:               "nbg1",
				NetworkCIDR:            "192.168.0.0/16",
				TokenEnvVar:            "MY_TOKEN",
			},
			wantCP:   "cpx21",
			wantWk:   "cpx31",
			wantLoc:  "nbg1",
			wantCIDR: "192.168.0.0/16",
			wantTok:  "MY_TOKEN",
		},
		{
			name: "defaults only missing fields",
			opts: v1alpha1.OptionsHetzner{
				ControlPlaneServerType: "cax11",
			},
			wantCP:   "cax11",
			wantWk:   v1alpha1.DefaultHetznerServerType,
			wantLoc:  v1alpha1.DefaultHetznerLocation,
			wantCIDR: v1alpha1.DefaultHetznerNetworkCIDR,
			wantTok:  v1alpha1.DefaultHetznerTokenEnvVar,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.ApplyHetznerDefaultsForTest(tc.opts)
			assert.Equal(t, tc.wantCP, result.ControlPlaneServerType)
			assert.Equal(t, tc.wantWk, result.WorkerServerType)
			assert.Equal(t, tc.wantLoc, result.Location)
			assert.Equal(t, tc.wantCIDR, result.NetworkCIDR)
			assert.Equal(t, tc.wantTok, result.TokenEnvVar)
		})
	}
}

// --- recordAppliedChange / recordFailedChange ---

func TestRecordAppliedChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      string
		nodeName  string
		action    string
		wantField string
		wantLen   int
	}{
		{
			name:      "control-plane added",
			role:      talosprovisioner.RoleControlPlane,
			nodeName:  "cp-1",
			action:    "added",
			wantField: "talos.controlPlanes",
			wantLen:   1,
		},
		{
			name:      "worker added",
			role:      talosprovisioner.RoleWorker,
			nodeName:  "worker-1",
			action:    "removed",
			wantField: "talos.workers",
			wantLen:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := clusterupdate.NewEmptyUpdateResult()
			talosprovisioner.RecordAppliedChangeForTest(result, tc.role, tc.nodeName, tc.action)

			require.Len(t, result.AppliedChanges, tc.wantLen)
			assert.Equal(t, tc.wantField, result.AppliedChanges[0].Field)
			assert.Equal(t, tc.nodeName, result.AppliedChanges[0].NewValue)
			assert.Contains(t, result.AppliedChanges[0].Reason, tc.action)
		})
	}
}

func TestRecordFailedChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      string
		nodeName  string
		err       error
		wantField string
	}{
		{
			name:      "control-plane failure",
			role:      talosprovisioner.RoleControlPlane,
			nodeName:  "cp-1",
			err:       errors.New("docker connection refused"),
			wantField: "talos.controlPlanes",
		},
		{
			name:      "worker failure",
			role:      talosprovisioner.RoleWorker,
			nodeName:  "worker-3",
			err:       errors.New("timeout"),
			wantField: "talos.workers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := clusterupdate.NewEmptyUpdateResult()
			talosprovisioner.RecordFailedChangeForTest(result, tc.role, tc.nodeName, tc.err)

			require.Len(t, result.FailedChanges, 1)
			assert.Equal(t, tc.wantField, result.FailedChanges[0].Field)
			assert.Contains(t, result.FailedChanges[0].Reason, tc.nodeName)
			assert.Contains(t, result.FailedChanges[0].Reason, tc.err.Error())
		})
	}
}

// --- containerName ---

func TestContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctr  container.Summary
		want string
	}{
		{
			name: "strips leading slash",
			ctr:  container.Summary{Names: []string{"/my-cluster-controlplane-1"}},
			want: "my-cluster-controlplane-1",
		},
		{
			name: "no names returns empty",
			ctr:  container.Summary{Names: []string{}},
			want: "",
		},
		{
			name: "name without slash",
			ctr:  container.Summary{Names: []string{"bare-name"}},
			want: "bare-name",
		},
		{
			name: "nil names returns empty",
			ctr:  container.Summary{},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.ContainerNameForTest(tc.ctr)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- RenameKubeconfigContext (additional edge cases) ---

func TestRenameKubeconfigContext_InvalidInput(t *testing.T) {
	t.Parallel()

	_, err := talosprovisioner.RenameKubeconfigContextForTest([]byte("not: valid: [broken"), "ctx")
	require.Error(t, err)
}
