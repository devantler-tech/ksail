package talosprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

type removalFixture struct {
	mu                    sync.Mutex
	t                     *testing.T
	servers               map[int64]*hcloud.Server
	nodes                 map[string]*corev1.Node
	members               map[uint64]bool
	pods                  []corev1.Pod
	budgets               []policyv1.PodDisruptionBudget
	floatingHolder        int64
	storageNode           string
	apiBad                bool
	etcdBad               bool
	snapshotBad           bool
	leaveBad              bool
	fipChangesAfterLeave  bool
	targetRoutingChanged  bool
	storageAfterLeave     bool
	retainDeletedNode     bool
	lateRoleChange        bool
	lateFloatingOwner     bool
	lateProviderVolume    bool
	attachVolumeOnRefresh bool
	wrongResponseID       bool
	cordonReplacesNode    bool
	cordonRejoinsSurvivor bool
	rejoined              map[int64]uint64
	volumes               []corev1.PersistentVolume
	events                []string
	leaves                []int64
	deleted               []int64
	apiWrites             []string
	snapshots             int
	provisioner           *Provisioner
}

var (
	errRemovalMembership = errors.New("membership response uncertain")
	errRemovalSnapshot   = errors.New("snapshot unavailable")
)

func newRemovalFixture(t *testing.T) *removalFixture {
	t.Helper()
	fixture := &removalFixture{
		t:              t,
		servers:        map[int64]*hcloud.Server{},
		nodes:          map[string]*corev1.Node{},
		members:        map[uint64]bool{},
		floatingHolder: 5,
	}

	fixture.populateNodes()

	api := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(api.Close)
	kubePath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubePath, []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster:
    server: %s
contexts:
- name: fixture
  context:
    cluster: fixture
    user: fixture
current-context: fixture
users:
- name: fixture
  user: {}
`, api.URL)), 0o600))
	cloud := hetzner.NewProvider(
		hcloud.NewClient(hcloud.WithToken("fixture-token"), hcloud.WithEndpoint(api.URL)),
	)
	fixture.provisioner = NewProvisioner(
		nil,
		NewOptions().WithKubeconfigPath(kubePath).WithKubeconfigContext("fixture"),
	).WithInfraProvider(cloud).
		WithLogWriter(io.Discard)
	fixture.provisioner.etcdClientFactory = func(_ context.Context, address string) (etcdMembershipClient, error) {
		serverID, err := strconv.ParseInt(strings.TrimPrefix(address, "192.0.2."), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse fixture address: %wesponse", err)
		}

		if fixture.targetRoutingChanged && serverID == 1 {
			serverID = 2
		}

		return &removalTalosClient{fixture: fixture, serverID: serverID}, nil
	}

	return fixture
}

func (fixture *removalFixture) populateNodes() {
	for serverID := int64(1); serverID <= 5; serverID++ {
		created := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
		if serverID == 1 || serverID == 3 {
			created = time.Date(2026, 11, 16, 13, 26, 56, 0, time.UTC)
		}

		name := fmt.Sprintf("fixture-control-plane-%d", serverID)
		server := &hcloud.Server{
			ID:      serverID,
			Name:    name,
			Created: created,
			Status:  hcloud.ServerStatusRunning,
			Labels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: "fixture",
				hetzner.LabelNodeType:    "control-plane",
			},
		}
		server.PublicNet.IPv4.IP = net.ParseIP(fmt.Sprintf("192.0.2.%d", serverID))
		fixture.servers[serverID] = server
		fixture.nodes[name] = &corev1.Node{
			Name:            name,
			UID:             types.UID(fmt.Sprintf("native-%d", serverID)),
			ResourceVersion: "1",
			Labels:          map[string]string{corev1.LabelHostname: name},
			Spec:            corev1.NodeSpec{ProviderID: fmt.Sprintf("hcloud://%d", serverID)},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		fixture.members[removalMemberID(serverID)] = true
	}
}

func (fixture *removalFixture) respond(response http.ResponseWriter, value any) {
	err := json.NewEncoder(response).Encode(value)
	if err != nil {
		fixture.t.Errorf("encode protocol fixture response: %v", err)
	}
}

func (fixture *removalFixture) serve(response http.ResponseWriter, request *http.Request) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodGet {
		fixture.apiWrites = append(fixture.apiWrites, request.Method+" "+request.URL.Path)
	}

	switch {
	case request.URL.Path == "/readyz":
		fixture.serveReady(response, request)
	case request.URL.Path == "/servers":
		fixture.serveServers(response, request)
	case strings.HasPrefix(request.URL.Path, "/servers/"):
		fixture.serveServer(response, request)
	case request.URL.Path == "/floating_ips":
		fixture.serveFloatingIP(response, request)
	case request.URL.Path == "/api/v1/nodes":
		fixture.serveNodes(response, request)
	case strings.HasPrefix(request.URL.Path, "/api/v1/nodes/"):
		fixture.serveNode(response, request)
	case request.URL.Path == "/api/v1/pods":
		fixture.servePods(response, request)
	default:
		fixture.serveDiscovery(response, request)
	}
}

func (fixture *removalFixture) serveReady(response http.ResponseWriter, _ *http.Request) {
	if fixture.apiBad {
		response.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(response, "not ready")

		return
	}

	_, _ = io.WriteString(response, "ok")
}

func (fixture *removalFixture) serveServers(response http.ResponseWriter, request *http.Request) {
	targetNode := fixture.nodes["fixture-control-plane-1"]
	if fixture.attachVolumeOnRefresh && targetNode != nil && targetNode.Spec.Unschedulable {
		if target := fixture.servers[1]; target != nil {
			target.Volumes = []*hcloud.Volume{{ID: 71}}
		}
	}

	servers := []any{}
	for _, serverID := range []int64{1, 2, 3, 4, 5} {
		server := fixture.servers[serverID]
		if server == nil {
			continue
		}

		if name := request.URL.Query().Get("name"); name != "" && name != server.Name {
			continue
		}

		servers = append(
			servers,
			map[string]any{
				"id":         serverID,
				"name":       server.Name,
				"created":    server.Created,
				"status":     "running",
				"labels":     server.Labels,
				"volumes":    removalVolumeIDs(server),
				"protection": map[string]any{"delete": server.Protection.Delete},
				"public_net": map[string]any{
					"ipv4": map[string]any{"ip": server.PublicNet.IPv4.IP.String()},
				},
			},
		)
	}

	fixture.respond(response, map[string]any{"servers": servers})
}

func removalVolumeIDs(server *hcloud.Server) []int64 {
	volumes := make([]int64, 0, len(server.Volumes))
	for _, volume := range server.Volumes {
		volumes = append(volumes, volume.ID)
	}

	return volumes
}

func (fixture *removalFixture) serveServer(response http.ResponseWriter, request *http.Request) {
	serverID, _ := strconv.ParseInt(strings.TrimPrefix(request.URL.Path, "/servers/"), 10, 64)
	if request.Method == http.MethodDelete {
		delete(fixture.servers, serverID)
		fixture.deleted = append(fixture.deleted, serverID)
		fixture.events = append(fixture.events, fmt.Sprintf("delete-%d", serverID))
		fixture.respond(
			response,
			map[string]any{"action": map[string]any{"id": serverID, "status": "success"}},
		)

		return
	}

	server := fixture.servers[serverID]
	if server != nil {
		fixture.respond(response, fixture.serverResponse(server))

		return
	}

	response.WriteHeader(http.StatusNotFound)
	fixture.respond(response,
		map[string]any{"error": map[string]any{"code": "not_found", "message": "not found"}},
	)
}

func (fixture *removalFixture) serverResponse(server *hcloud.Server) map[string]any {
	serverID := server.ID
	labels := maps.Clone(server.Labels)
	responseID := server.ID

	if serverID == 1 && fixture.lateRoleChange {
		labels[hetzner.LabelNodeType] = hetzner.NodeTypeWorker
	}

	if serverID == 1 && fixture.lateFloatingOwner {
		fixture.floatingHolder = 1
		fixture.lateFloatingOwner = false
	}

	if serverID == 1 && fixture.wrongResponseID {
		responseID = 99
	}

	floating := []int64{}
	if fixture.floatingHolder == serverID {
		floating = append(floating, 7)
	}

	volumes := removalVolumeIDs(server)
	if serverID == 1 && fixture.lateProviderVolume {
		volumes = []int64{71}
	}

	return map[string]any{
		"server": map[string]any{
			"id":         responseID,
			"name":       server.Name,
			"created":    server.Created,
			"labels":     labels,
			"status":     "running",
			"volumes":    volumes,
			"protection": map[string]any{"delete": server.Protection.Delete},
			"public_net": map[string]any{
				"ipv4":         map[string]any{"ip": server.PublicNet.IPv4.IP.String()},
				"floating_ips": floating,
			},
		},
	}
}

func (fixture *removalFixture) serveFloatingIP(response http.ResponseWriter, _ *http.Request) {
	fixture.respond(response,
		map[string]any{
			"floating_ips": []any{
				map[string]any{
					"id":     7,
					"name":   "fixture-floating-ip",
					"ip":     "192.0.2.100",
					"type":   "ipv4",
					"server": fixture.floatingHolder,
					"labels": map[string]string{
						hetzner.LabelOwned:       "true",
						hetzner.LabelClusterName: "fixture",
					},
				},
			},
		},
	)
}

func (fixture *removalFixture) serveNodes(response http.ResponseWriter, _ *http.Request) {
	list := corev1.NodeList{APIVersion: "v1", Kind: "NodeList"}
	for _, node := range fixture.nodes {
		list.Items = append(list.Items, *node.DeepCopy())
	}

	fixture.respond(response, list)
}

func (fixture *removalFixture) serveNode(response http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/api/v1/nodes/")

	node := fixture.nodes[name]
	if node == nil {
		response.WriteHeader(http.StatusNotFound)
		fixture.respond(
			response,
			metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: 404},
		)

		return
	}

	switch request.Method {
	case http.MethodDelete:
		fixture.deleteNode(response, request, node)
	case http.MethodPatch:
		fixture.patchNode(response, request, node)
	default:
		fixture.respond(response, node)
	}
}

func (fixture *removalFixture) deleteNode(
	response http.ResponseWriter,
	request *http.Request,
	node *corev1.Node,
) {
	var options metav1.DeleteOptions

	body, err := io.ReadAll(io.LimitReader(request.Body, 128<<10))
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)

		return
	}

	err = runtime.DecodeInto(scheme.Codecs.UniversalDeserializer(), body, &options)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)

		return
	}

	if options.Preconditions == nil || options.Preconditions.UID == nil ||
		*options.Preconditions.UID != node.UID {
		response.WriteHeader(http.StatusConflict)

		return
	}

	if !fixture.retainDeletedNode {
		delete(fixture.nodes, node.Name)
	}

	fixture.respond(response, metav1.Status{Status: "Success"})
}

type removalPatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

func (fixture *removalFixture) patchNode(
	response http.ResponseWriter,
	request *http.Request,
	node *corev1.Node,
) {
	if fixture.cordonReplacesNode {
		node.UID = "replacement-node"
		fixture.cordonReplacesNode = false
	}

	if fixture.cordonRejoinsSurvivor {
		// Survivor 2 rejoins etcd on the same server under a new member ID.
		delete(fixture.members, removalMemberID(2))
		fixture.rejoined = map[int64]uint64{2: 2002}
		fixture.members[2002] = true
		fixture.cordonRejoinsSurvivor = false
	}

	if !strings.Contains(request.Header.Get("Content-Type"), "json-patch") {
		response.WriteHeader(http.StatusUnsupportedMediaType)

		return
	}

	var operations []removalPatchOperation

	err := json.NewDecoder(request.Body).Decode(&operations)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)

		return
	}

	if !removalPatchMatches(operations, node.UID) {
		response.WriteHeader(http.StatusConflict)
		fixture.respond(
			response,
			metav1.Status{Status: "Failure", Reason: metav1.StatusReasonConflict, Code: 409},
		)

		return
	}

	for _, operation := range operations {
		if operation.Op == "add" && operation.Path == "/spec/unschedulable" {
			err = json.Unmarshal(operation.Value, &node.Spec.Unschedulable)
			if err != nil {
				response.WriteHeader(http.StatusBadRequest)

				return
			}
		}
	}

	fixture.respond(response, node)
}

func removalPatchMatches(operations []removalPatchOperation, actual types.UID) bool {
	for _, operation := range operations {
		if operation.Op != "test" || operation.Path != "/metadata/uid" {
			continue
		}

		var expected types.UID

		err := json.Unmarshal(operation.Value, &expected)
		if err != nil || actual != expected {
			return false
		}

		return true
	}

	return false
}

func (fixture *removalFixture) servePods(response http.ResponseWriter, request *http.Request) {
	list := corev1.PodList{APIVersion: "v1", Kind: "PodList", ResourceVersion: "1"}

	for _, pod := range fixture.pods {
		if field := request.URL.Query().
			Get("fieldSelector"); field == "" ||
			field == "spec.nodeName="+pod.Spec.NodeName {
			list.Items = append(list.Items, pod)
		}
	}

	fixture.respond(response, list)
}

func (fixture *removalFixture) serveDiscovery(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/v1/persistentvolumes":
		fixture.respond(response, corev1.PersistentVolumeList{
			APIVersion: "v1", Kind: "PersistentVolumeList",
			ResourceVersion: "1",
			Items:           fixture.volumes,
		})
	case "/apis/policy/v1/poddisruptionbudgets":
		fixture.respond(response,
			policyv1.PodDisruptionBudgetList{
				APIVersion: "policy/v1",
				Kind:       "PodDisruptionBudgetList",
				Items:      fixture.budgets,
			},
		)
	case "/apis/longhorn.io":
		fixture.respond(response,
			metav1.APIGroup{
				Kind:       "APIGroup",
				APIVersion: "v1",
				Name:       "longhorn.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{GroupVersion: "longhorn.io/v1beta2", Version: "v1beta2"},
				},
			},
		)
	case "/apis/longhorn.io/v1beta2/nodes":
		items := []any{}
		if fixture.storageNode != "" {
			items = append(
				items,
				map[string]any{"metadata": map[string]any{"name": fixture.storageNode}},
			)
		}

		fixture.respond(response,
			map[string]any{"apiVersion": "longhorn.io/v1beta2", "kind": "NodeList", "items": items},
		)
	case "/api":
		fixture.respond(response,
			map[string]any{"kind": "APIVersions", "apiVersion": "v1", "versions": []string{"v1"}},
		)
	case "/apis":
		fixture.respond(
			response,
			map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}},
		)
	default:
		response.WriteHeader(http.StatusNotFound)
		fixture.respond(
			response,
			metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: 404},
		)
	}
}

func removalMemberID(serverID int64) uint64 {
	switch serverID {
	case 1:
		return 1001
	case 2:
		return 1002
	case 3:
		return 1003
	case 4:
		return 1004
	case 5:
		return 1005
	default:
		return 0
	}
}

// memberID returns the server's current etcd member ID, honouring a simulated rejoin.
func (fixture *removalFixture) memberID(serverID int64) uint64 {
	if id, ok := fixture.rejoined[serverID]; ok {
		return id
	}

	return removalMemberID(serverID)
}

type removalTalosClient struct {
	fixture  *removalFixture
	serverID int64
}

func (c *removalTalosClient) Close() error { return nil }

func (c *removalTalosClient) EtcdForfeitLeadership(
	context.Context,
	*machineapi.EtcdForfeitLeadershipRequest,
	...grpc.CallOption,
) (*machineapi.EtcdForfeitLeadershipResponse, error) {
	return &machineapi.EtcdForfeitLeadershipResponse{}, nil
}

func (c *removalTalosClient) EtcdLeaveCluster(
	context.Context,
	*machineapi.EtcdLeaveClusterRequest,
	...grpc.CallOption,
) error {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()

	c.fixture.leaves = append(c.fixture.leaves, c.serverID)

	c.fixture.events = append(c.fixture.events, fmt.Sprintf("leave-%d", c.serverID))
	if c.fixture.leaveBad {
		return errRemovalMembership
	}

	delete(c.fixture.members, c.fixture.memberID(c.serverID))

	if c.fixture.storageAfterLeave {
		c.fixture.nodes[fmt.Sprintf("fixture-control-plane-%d", c.serverID)].Status.VolumesInUse = []corev1.UniqueVolumeName{
			"new-storage-attachment",
		}
	}

	if c.fixture.fipChangesAfterLeave {
		c.fixture.floatingHolder = 2
	}

	return nil
}

func (c *removalTalosClient) EtcdMemberList(
	context.Context,
	*machineapi.EtcdMemberListRequest,
	...grpc.CallOption,
) (*machineapi.EtcdMemberListResponse, error) {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()

	members := []*machineapi.EtcdMember{}
	for serverID := int64(1); serverID <= 5; serverID++ {
		if c.fixture.members[c.fixture.memberID(serverID)] {
			members = append(
				members,
				&machineapi.EtcdMember{
					Id:       c.fixture.memberID(serverID),
					Hostname: fmt.Sprintf("fixture-control-plane-%d", serverID),
				},
			)
		}
	}

	return &machineapi.EtcdMemberListResponse{
		Messages: []*machineapi.EtcdMembers{{Members: members}},
	}, nil
}

func (c *removalTalosClient) EtcdStatus(
	context.Context,
	...grpc.CallOption,
) (*machineapi.EtcdStatusResponse, error) {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()

	status := &machineapi.EtcdMemberStatus{
		MemberId:         c.fixture.memberID(c.serverID),
		Leader:           1004,
		RaftIndex:        100,
		RaftAppliedIndex: 100,
	}
	if c.fixture.etcdBad {
		status.Errors = []string{"unhealthy fixture"}
	}

	return &machineapi.EtcdStatusResponse{
		Messages: []*machineapi.EtcdStatus{{MemberStatus: status}},
	}, nil
}

func (c *removalTalosClient) EtcdAlarmList(
	context.Context,
	...grpc.CallOption,
) (*machineapi.EtcdAlarmListResponse, error) {
	return &machineapi.EtcdAlarmListResponse{Messages: []*machineapi.EtcdAlarm{{}}}, nil
}

func (c *removalTalosClient) EtcdSnapshot(
	context.Context,
	*machineapi.EtcdSnapshotRequest,
	...grpc.CallOption,
) (io.ReadCloser, error) {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()

	c.fixture.snapshots++

	c.fixture.events = append(c.fixture.events, "snapshot")
	if c.fixture.snapshotBad {
		return nil, errRemovalSnapshot
	}

	ids := []uint64{}
	for serverID := range c.fixture.members {
		ids = append(ids, serverID)
	}

	return io.NopCloser(bytes.NewReader(digested(snapshotDatabase(c.fixture.t, true, ids...)))), nil
}

func TestControlPlaneScaleDownCompletesOnlyExactNewTargets(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, fixture.leaves)
	require.Equal(t, []int64{1, 3}, fixture.deleted)
	require.Equal(t, int64(5), fixture.floatingHolder)
	require.Len(t, fixture.nodes, 3)
	require.Len(t, fixture.members, 3)
	require.Positive(
		t,
		fixture.snapshots,
		"a verified recovery snapshot must precede membership changes",
	)
	require.Equal(
		t,
		[]string{"snapshot", "leave-1", "delete-1", "snapshot", "leave-3", "delete-3"},
		slices.Compact(slices.Clone(fixture.events)),
		"each target needs a fresh snapshot, then membership removal, then provider deletion",
	)

	for _, serverID := range []int64{2, 4, 5} {
		require.Contains(t, fixture.servers, serverID)
		require.Contains(t, fixture.nodes, fmt.Sprintf("fixture-control-plane-%d", serverID))
	}
}

func TestControlPlaneScaleDownRefusesUnprovenPreconditions(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"unready survivor", "native provider mismatch", "PVC on target", "Longhorn target",
		"API unhealthy", "etcd unhealthy", "snapshot unavailable",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)

			switch scenario {
			case "unready survivor":
				fixture.nodes["fixture-control-plane-2"].Status.Conditions[0].Status = corev1.ConditionFalse
			case "native provider mismatch":
				fixture.nodes["fixture-control-plane-1"].Spec.ProviderID = "hcloud://99"
			case "PVC on target":
				fixture.pods = []corev1.Pod{
					{
						Name:      "storage",
						Namespace: "default",
						Spec: corev1.PodSpec{
							NodeName: "fixture-control-plane-1",
							Volumes: []corev1.Volume{
								{
									Name: "data",
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "data",
									},
								},
							},
						},
					},
				}
			case "Longhorn target":
				fixture.storageNode = "fixture-control-plane-1"
			case "API unhealthy":
				fixture.apiBad = true
			case "etcd unhealthy":
				fixture.etcdBad = true
			case "snapshot unavailable":
				fixture.snapshotBad = true
			}

			err := fixture.provisioner.removeHetznerNodes(
				t.Context(),
				"fixture",
				RoleControlPlane,
				2,
				clusterupdate.NewEmptyUpdateResult(),
			)
			require.Error(t, err)
			require.Empty(t, fixture.leaves)
			require.Empty(t, fixture.deleted)
		})
	}
}

// An unmounted volume keeps its data on its node after the pod stops, so neither the pod nor
// the node volume inventory sees it.
func TestControlPlaneScaleDownRefusesTargetPersistentVolumes(t *testing.T) {
	t.Parallel()

	for scenario, volume := range map[string]func() corev1.PersistentVolume{
		"unmounted local PV on target": func() corev1.PersistentVolume {
			return removalLocalVolume("fixture-control-plane-1")
		},
		"local PV without affinity": func() corev1.PersistentVolume {
			volume := removalLocalVolume("fixture-control-plane-2")
			volume.Spec.NodeAffinity = nil

			return volume
		},
		"CSI PV bound to target": func() corev1.PersistentVolume {
			volume := removalLocalVolume("fixture-control-plane-1")
			volume.Spec.Local = nil
			volume.Spec.CSI = &corev1.CSIPersistentVolumeSource{
				Driver:       "local.csi.example",
				VolumeHandle: "data",
			}

			return volume
		},
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)
			fixture.volumes = []corev1.PersistentVolume{volume()}

			err := fixture.provisioner.removeHetznerNodes(
				t.Context(),
				"fixture",
				RoleControlPlane,
				2,
				clusterupdate.NewEmptyUpdateResult(),
			)
			require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
			require.Empty(t, fixture.leaves)
			require.Empty(t, fixture.deleted)
		})
	}
}

func TestControlPlaneScaleDownRefusesSurvivorRejoinDuringCordon(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.cordonRejoinsSurvivor = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.ErrorIs(t, err, ErrControlPlaneScaleDownUnsafe)
	require.Empty(
		t,
		fixture.leaves,
		"a membership change after the snapshot must refuse before the leave RPC",
	)
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneScaleDownStopsAfterUncertainMembership(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.leaveBad = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Equal(t, []int64{1}, fixture.leaves)
	require.Empty(t, fixture.deleted)
	err = fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Equal(
		t,
		[]int64{1},
		fixture.leaves,
		"a new invocation must not blindly retry the uncertain membership RPC",
	)
}

func TestControlPlaneScaleDownStopsIfEndpointMovesAfterMembership(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.fipChangesAfterLeave = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Equal(t, []int64{1}, fixture.leaves)
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneScaleDownBindsMembershipClientToTarget(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.targetRoutingChanged = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Empty(
		t,
		fixture.leaves,
		"a different member on the target address must never receive leave",
	)
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneScaleDownStopsIfStorageAppearsAfterMembership(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.storageAfterLeave = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Equal(t, []int64{1}, fixture.leaves)
	require.Empty(t, fixture.deleted)
}

func TestControlPlaneScaleDownRequiresNativeDeletionReadback(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	fixture.retainDeletedNode = true
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.Error(t, err)
	require.Equal(t, []int64{1}, fixture.leaves)
	require.Equal(t, []int64{1}, fixture.deleted)
}

// The former one-node fixture could no longer reach membership cleanup once
// scale-down began proving quorum. Preserve all its refusal cases on a valid
// five-node plan, including the actual membership error chain.
func TestRemoveHetznerNodes_CleanupFailurePreservesServer(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"missing target address", "connection failure", "leave failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRemovalFixture(t)

			switch scenario {
			case "missing target address":
				fixture.servers[1].PublicNet.IPv4.IP = nil
			case "connection failure":
				fixture.provisioner.etcdClientFactory = func(context.Context, string) (etcdMembershipClient, error) {
					return nil, errRemovalMembership
				}
			case "leave failure":
				fixture.leaveBad = true
			}

			result := clusterupdate.NewEmptyUpdateResult()
			err := fixture.provisioner.removeHetznerNodes(
				t.Context(),
				"fixture",
				RoleControlPlane,
				2,
				result,
			)
			require.Error(t, err)
			require.Empty(t, fixture.deleted)
			require.Empty(t, result.AppliedChanges)
			require.Len(t, result.FailedChanges, 1)

			if scenario != "missing target address" {
				require.ErrorIs(t, err, errRemovalMembership)
			}
		})
	}
}

func removalLocalVolume(nodeName string) corev1.PersistentVolume {
	return corev1.PersistentVolume{
		Name: "local-" + nodeName,
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				Local: &corev1.LocalVolumeSource{Path: "/var/mnt/data"},
			},
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      corev1.LabelHostname,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{nodeName},
						}},
					}},
				},
			},
		},
	}
}

func TestControlPlaneScaleDownAllowsVolumesProvablyElsewhere(t *testing.T) {
	t.Parallel()
	fixture := newRemovalFixture(t)
	network := removalLocalVolume("unused")
	network.Name = "network"
	network.Spec.Local = nil
	network.Spec.CSI = &corev1.CSIPersistentVolumeSource{
		Driver:       "csi.hetzner.cloud",
		VolumeHandle: "7",
	}
	network.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0] = corev1.NodeSelectorRequirement{
		Key:      corev1.LabelTopologyZone,
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{"fsn1-dc14"},
	}
	fixture.volumes = []corev1.PersistentVolume{
		removalLocalVolume("fixture-control-plane-2"),
		network,
	}
	err := fixture.provisioner.removeHetznerNodes(
		t.Context(),
		"fixture",
		RoleControlPlane,
		2,
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, fixture.leaves)
}
