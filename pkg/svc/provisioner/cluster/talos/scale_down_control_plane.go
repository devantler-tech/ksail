package talosprovisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	minimumRemovalSurvivors  = 3
	removalRecordPermissions = 0o600
	removalReadTimeout       = 45 * time.Second
	removalSnapshotTimeout   = 3 * time.Minute
)

type controlPlaneRemovalState struct {
	target  replacementTarget
	servers []*hcloud.Server
	nodes   []corev1.Node
	quorum  etcdQuorumObservation
}

func (p *Provisioner) removeHetznerControlPlanes(
	ctx context.Context, cloud *hetzner.Provider, existing []*hcloud.Server,
	clusterName string, count int, result *clusterupdate.UpdateResult,
) error {
	if count <= 0 {
		return nil
	}

	if len(existing)-count < minimumRemovalSurvivors {
		return fmt.Errorf(
			"%w: automated removal must retain at least three control planes",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	clientset, err := p.createK8sClient(clusterName)
	if err != nil {
		return err
	}

	targets, floating, err := readRemovalSelection(ctx, cloud, existing, clusterName, count)
	if err != nil {
		return err
	}

	slices.Reverse(targets)

	expected := slices.Clone(existing)
	for _, server := range targets {
		state, readErr := p.readControlPlaneRemoval(
			ctx,
			cloud,
			clientset,
			expected,
			floating,
			clusterName,
			server.Name,
		)
		if readErr != nil {
			return readErr
		}

		err = p.removeOneControlPlane(ctx, cloud, clientset, clusterName, state, floating)
		if err != nil {
			return err
		}

		expected = slices.DeleteFunc(
			expected,
			func(current *hcloud.Server) bool { return current.ID == server.ID },
		)

		recordAppliedChange(result, RoleControlPlane, server.Name, "removed")
	}

	return nil
}

func readRemovalSelection(
	ctx context.Context,
	cloud *hetzner.Provider,
	existing []*hcloud.Server,
	clusterName string,
	count int,
) ([]*hcloud.Server, *hcloud.FloatingIP, error) {
	floating, err := cloud.GetOwnedFloatingIP(ctx, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("read assigned removal endpoint: %w", err)
	}

	if floating == nil {
		return nil, nil, fmt.Errorf(
			"%w: an assigned owned floating endpoint is required for automated control-plane removal",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	targets, err := controlPlaneScaleDownTargets(existing, floating, clusterName, count)
	if err != nil {
		return nil, nil, err
	}

	return targets, floating, nil
}

func (p *Provisioner) readControlPlaneRemoval(
	ctx context.Context, cloud *hetzner.Provider, clientset kubernetes.Interface,
	expected []*hcloud.Server, floating *hcloud.FloatingIP, clusterName, targetName string,
) (controlPlaneRemovalState, error) {
	current, err := p.listHetznerNodesByRole(ctx, cloud, clusterName, RoleControlPlane)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	err = sameRemovalInventory(expected, current)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	err = checkRemovalEndpoint(ctx, cloud, clusterName, floating)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	nodes, err := p.readScaleDownNodes(ctx, clientset, current)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	survivors := slices.DeleteFunc(slices.Clone(current), func(server *hcloud.Server) bool {
		return server.Name == targetName
	})

	quorum, err := p.observeRemovalQuorum(ctx, survivors)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	target, err := resolveReplacementTarget(replacementObservation{
		ClusterName: clusterName,
		NodeName:    targetName,
		Servers:     current,
		Nodes:       nodes,
		EtcdMembers: quorum.Views[0].Members,
	})
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	err = proveInitialRemovalMembership(current, target, quorum)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	err = p.proveRemovalWorkloads(ctx, clientset, target, nodes)
	if err != nil {
		return controlPlaneRemovalState{}, err
	}

	return controlPlaneRemovalState{
		target:  target,
		servers: current,
		nodes:   nodes,
		quorum:  quorum,
	}, nil
}

func proveInitialRemovalMembership(
	servers []*hcloud.Server,
	target replacementTarget,
	quorum etcdQuorumObservation,
) error {
	err := proveSurvivingQuorum(target, quorum)
	if err != nil {
		return err
	}

	err = proveRemovalMembership(servers, quorum)
	if err != nil {
		return err
	}

	if quorum.Views[0].Status.GetLeader() == target.EtcdMemberID {
		return fmt.Errorf("%w: target is the current etcd leader", ErrControlPlaneScaleDownUnsafe)
	}

	return nil
}

func sameRemovalServer(planned, observed *hcloud.Server) bool {
	return planned.ID == observed.ID && planned.Name == observed.Name &&
		planned.Created.Equal(observed.Created) &&
		planned.Labels[hetzner.LabelOwned] == observed.Labels[hetzner.LabelOwned] &&
		planned.Labels[hetzner.LabelClusterName] == observed.Labels[hetzner.LabelClusterName] &&
		planned.Labels[hetzner.LabelNodeType] == observed.Labels[hetzner.LabelNodeType]
}

func sameRemovalInventory(expected, current []*hcloud.Server) error {
	if len(expected) != len(current) {
		return fmt.Errorf("%w: control-plane inventory changed", ErrControlPlaneScaleDownUnsafe)
	}

	for _, planned := range expected {
		matches := 0

		for _, observed := range current {
			if sameRemovalServer(planned, observed) {
				matches++
			}
		}

		if matches != 1 {
			return fmt.Errorf("%w: control-plane identity changed", ErrControlPlaneScaleDownUnsafe)
		}
	}

	return nil
}

func checkRemovalEndpoint(
	ctx context.Context,
	cloud *hetzner.Provider,
	clusterName string,
	planned *hcloud.FloatingIP,
) error {
	current, err := cloud.GetOwnedFloatingIP(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("rebind assigned removal endpoint: %w", err)
	}

	err = validateRemovalEndpoint(planned, clusterName)
	if err != nil {
		return err
	}

	err = validateRemovalEndpoint(current, clusterName)
	if err != nil {
		return err
	}

	if planned.ID != current.ID || planned.IP.String() != current.IP.String() ||
		planned.Server.ID != current.Server.ID {
		return fmt.Errorf("%w: assigned API endpoint changed", ErrControlPlaneScaleDownUnsafe)
	}

	return nil
}

func (p *Provisioner) openRemovalObservation(
	ctx context.Context,
	address string,
) (etcdObservationClient, error) {
	client, err := p.openEtcdMembershipClient(ctx, address)
	if err != nil {
		return nil, err
	}

	reader, ok := client.(etcdObservationClient)
	if !ok {
		_ = client.Close()

		return nil, fmt.Errorf(
			"%w: client cannot observe etcd health",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return reader, nil
}

func (p *Provisioner) observeRemovalQuorum(
	ctx context.Context,
	servers []*hcloud.Server,
) (etcdQuorumObservation, error) {
	addresses := make([]string, 0, len(servers))
	for _, server := range servers {
		address, err := p.hetznerNodeAddress(server)
		if err != nil {
			return etcdQuorumObservation{}, err
		}

		addresses = append(addresses, address)
	}

	return observeEtcdQuorum(ctx, p.openRemovalObservation, addresses)
}

func proveRemovalMembership(servers []*hcloud.Server, quorum etcdQuorumObservation) error {
	if len(quorum.Views) == 0 {
		return fmt.Errorf("%w: no etcd observations", ErrControlPlaneScaleDownUnsafe)
	}

	ids, err := consistentMembership(quorum.Views)
	if err != nil {
		return err
	}

	bound := make([]uint64, 0, len(servers))
	for _, server := range servers {
		memberID, bindErr := etcdMemberForTarget(
			quorum.Views[0].Members,
			replacementTarget{ServerName: server.Name, Role: hetzner.NodeTypeControlPlane},
		)
		if bindErr != nil {
			return bindErr
		}

		bound = append(bound, memberID)
	}

	slices.Sort(bound)

	if !slices.Equal(bound, ids) {
		return fmt.Errorf(
			"%w: provider and etcd inventories differ",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	_, err = healthySurvivors(quorum.Views, ids, 0)
	if err != nil {
		return err
	}

	err = noActiveAlarms(quorum.Alarms)
	if err != nil {
		return err
	}

	for _, view := range quorum.Views {
		if view.Status.GetRaftAppliedIndex() != view.Status.GetRaftIndex() {
			return fmt.Errorf("%w: etcd member is not caught up", ErrControlPlaneScaleDownUnsafe)
		}
	}

	return nil
}

type controlPlaneRemoval struct {
	provisioner *Provisioner
	cloud       *hetzner.Provider
	clientset   kubernetes.Interface
	clusterName string
	state       controlPlaneRemovalState
	floating    *hcloud.FloatingIP
}

func (p *Provisioner) removeOneControlPlane(
	ctx context.Context,
	cloud *hetzner.Provider,
	clientset kubernetes.Interface,
	clusterName string,
	state controlPlaneRemovalState,
	floating *hcloud.FloatingIP,
) error {
	removal := controlPlaneRemoval{p, cloud, clientset, clusterName, state, floating}

	err := removal.prepare(ctx)
	if err != nil {
		return err
	}

	address, err := p.hetznerNodeAddress(serverFromRemoval(state.servers, state.target.ServerID))
	if err != nil {
		return err
	}

	err = p.leaveControlPlaneRemoval(ctx, address, state.target)
	if err != nil {
		return err
	}

	survivors := slices.DeleteFunc(
		slices.Clone(state.servers),
		func(server *hcloud.Server) bool { return server.ID == state.target.ServerID },
	)

	err = removal.proveAfterMembership(ctx, survivors)
	if err != nil {
		return err
	}

	err = removal.deleteProvider(ctx, survivors)
	if err != nil {
		return err
	}

	err = removal.deleteNative(ctx)
	if err != nil {
		return err
	}

	return removal.finish(ctx, survivors)
}

func (removal controlPlaneRemoval) prepare(ctx context.Context) error {
	// Exclusive records persist beside the canonical kubeconfig across invocations.
	kubePath, _, err := removal.provisioner.resolveKubeconfig(removal.clusterName)
	if err != nil {
		return err
	}

	parent := filepath.Dir(kubePath)
	recordName := fmt.Sprintf(".ksail-cp-removal-%d.json", removal.state.target.ServerID)

	err = proveNoRemovalRecord(filepath.Join(parent, recordName))
	if err != nil {
		return err
	}

	artifact, err := removal.provisioner.captureRemovalSnapshot(ctx, removal.state, parent)
	if err != nil {
		return err
	}

	err = removal.provisioner.cordonEmptyControlPlane(ctx, removal.clientset, removal.state.target)
	if err != nil {
		return err
	}

	err = removal.reproveCordon(ctx)
	if err != nil {
		return err
	}

	return writeRemovalRecord(parent, recordName, removal.state.target, artifact)
}

func proveNoRemovalRecord(recordPath string) error {
	_, err := os.Lstat(recordPath)
	if err == nil || !os.IsNotExist(err) {
		return fmt.Errorf(
			"%w: a prior removal attempt needs recovery review",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

func (removal controlPlaneRemoval) reproveCordon(ctx context.Context) error {
	fresh, err := removal.provisioner.readControlPlaneRemoval(
		ctx, removal.cloud, removal.clientset, removal.state.servers,
		removal.floating, removal.clusterName, removal.state.target.ServerName,
	)
	if err != nil {
		return err
	}

	if fresh.target != removal.state.target {
		return fmt.Errorf("%w: target changed after cordon", ErrControlPlaneScaleDownUnsafe)
	}

	return proveRemovalCordon(fresh.nodes, removal.state.target)
}

func writeRemovalRecord(
	parent, recordName string,
	target replacementTarget,
	artifact etcdSnapshotArtifact,
) error {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open private recovery directory: %w", err)
	}
	defer root.Close() //nolint:errcheck

	record, err := root.OpenFile(
		recordName,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		removalRecordPermissions,
	)
	if err != nil {
		return fmt.Errorf("reserve membership attempt: %w", err)
	}

	writeErr := json.NewEncoder(record).Encode(struct {
		Target   replacementTarget    `json:"target"`
		Snapshot etcdSnapshotArtifact `json:"snapshot"`
		Phase    string               `json:"phase"`
	}{target, artifact, "membership-attempted"})
	if writeErr == nil {
		writeErr = record.Sync()
	}

	closeErr := record.Close()

	if writeErr != nil {
		return fmt.Errorf("persist membership attempt: %w", writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close membership attempt: %w", closeErr)
	}

	return nil
}

func (removal controlPlaneRemoval) proveAfterMembership(
	ctx context.Context,
	survivors []*hcloud.Server,
) error {
	return removal.provisioner.proveRemovalAfterMembership(
		ctx, removal.cloud, removal.clientset, removal.clusterName,
		removal.state, survivors, removal.floating,
	)
}

func (removal controlPlaneRemoval) deleteProvider(
	ctx context.Context,
	survivors []*hcloud.Server,
) error {
	allNodes, err := removal.provisioner.readScaleDownNodes(ctx, removal.clientset, survivors)
	if err != nil {
		return err
	}

	err = removal.provisioner.proveRemovalWorkloads(
		ctx,
		removal.clientset,
		removal.state.target,
		allNodes,
	)
	if err != nil {
		return err
	}

	native, err := removal.clientset.CoreV1().
		Nodes().
		Get(ctx, removal.state.target.ServerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("rebind native target before provider deletion: %w", err)
	}

	err = proveRemovalCordon([]corev1.Node{*native}, removal.state.target)
	if err != nil {
		return err
	}

	server, err := removal.cloud.GetServerByID(ctx, removal.state.target.ServerID)
	if err != nil {
		return fmt.Errorf("rebind provider deletion target: %w", err)
	}

	err = removal.proveProviderTarget(server)
	if err != nil {
		return err
	}

	err = removal.provisioner.deleteHetznerServer(ctx, removal.cloud, server)
	if err != nil {
		return err
	}

	return removal.confirmProviderAbsent(ctx)
}

func (removal controlPlaneRemoval) proveProviderTarget(server *hcloud.Server) error {
	planned := serverFromRemoval(removal.state.servers, removal.state.target.ServerID)
	if server == nil || server.Name != removal.state.target.ServerName ||
		!server.Created.Equal(planned.Created) {
		return fmt.Errorf(
			"%w: target provider identity changed before deletion",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	err := validateRemovalCandidate(server, removal.clusterName)
	if err != nil {
		return err
	}

	if len(server.PublicNet.FloatingIPs) > 0 {
		return fmt.Errorf(
			"%w: target currently carries a floating endpoint",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

func (removal controlPlaneRemoval) confirmProviderAbsent(ctx context.Context) error {
	err := wait.PollUntilContextTimeout(
		ctx, time.Second, time.Minute, true,
		func(readCtx context.Context) (bool, error) {
			remaining, readErr := removal.cloud.GetServerByID(
				readCtx,
				removal.state.target.ServerID,
			)
			if readErr != nil {
				return false, fmt.Errorf("read removed provider target: %w", readErr)
			}

			return remaining == nil, nil
		},
	)
	if err != nil {
		return fmt.Errorf("confirm server absence: %w", err)
	}

	return nil
}

func (removal controlPlaneRemoval) deleteNative(ctx context.Context) error {
	uid := removal.state.target.NodeUID

	err := removal.clientset.CoreV1().Nodes().Delete(ctx, removal.state.target.ServerName,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete removed native target: %w", err)
	}

	_, err = removal.clientset.CoreV1().
		Nodes().
		Get(ctx, removal.state.target.ServerName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("confirm native target absence: %w", err)
	}

	return fmt.Errorf("%w: native target deletion is not confirmed", ErrControlPlaneScaleDownUnsafe)
}

func (removal controlPlaneRemoval) finish(ctx context.Context, survivors []*hcloud.Server) error {
	current, err := removal.provisioner.listHetznerNodesByRole(
		ctx,
		removal.cloud,
		removal.clusterName,
		RoleControlPlane,
	)
	if err != nil {
		return err
	}

	err = sameRemovalInventory(survivors, current)
	if err != nil {
		return err
	}

	err = removal.proveAfterMembership(ctx, survivors)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(removal.provisioner.logWriter,
		"  Control-plane removal verified; private recovery evidence retained")

	return nil
}

func proveRemovalCordon(nodes []corev1.Node, target replacementTarget) error {
	for _, node := range nodes {
		if node.Name != target.ServerName {
			continue
		}

		if node.UID != target.NodeUID ||
			node.Spec.ProviderID != fmt.Sprintf("hcloud://%d", target.ServerID) ||
			!node.Spec.Unschedulable ||
			len(node.Status.VolumesAttached) > 0 ||
			len(node.Status.VolumesInUse) > 0 {
			return fmt.Errorf(
				"%w: target identity, cordon or storage state changed",
				ErrControlPlaneScaleDownUnsafe,
			)
		}

		return nil
	}

	return fmt.Errorf("%w: native target is no longer present", ErrControlPlaneScaleDownUnsafe)
}

func (p *Provisioner) leaveControlPlaneRemoval(
	ctx context.Context,
	address string,
	target replacementTarget,
) error {
	writeCtx, cancel := context.WithTimeout(ctx, removalReadTimeout)
	defer cancel()

	client, err := p.openEtcdMembershipClient(writeCtx, address)
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	reader, ok := client.(etcdObservationClient)
	if !ok {
		return fmt.Errorf(
			"%w: membership client identity is unproven",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	status, err := reader.EtcdStatus(writeCtx)
	if err != nil {
		return fmt.Errorf("read removal member identity: %w", err)
	}

	if len(status.GetMessages()) != 1 || status.GetMessages()[0].GetMemberStatus() == nil {
		return fmt.Errorf(
			"%w: target did not report one local member",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	member := status.GetMessages()[0].GetMemberStatus()
	if !removalMemberReady(member, target) {
		return fmt.Errorf(
			"%w: membership client does not prove the planned nonleader target",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	// Exactly once. An error retains the exclusive attempt record and server.
	err = client.EtcdLeaveCluster(writeCtx, &machineapi.EtcdLeaveClusterRequest{})
	if err != nil {
		return fmt.Errorf("leave planned control-plane member: %w", err)
	}

	return nil
}

func removalMemberReady(member *machineapi.EtcdMemberStatus, target replacementTarget) bool {
	return member.GetMemberId() == target.EtcdMemberID &&
		member.GetLeader() != target.EtcdMemberID &&
		member.GetLeader() != 0 &&
		len(member.GetErrors()) == 0 &&
		member.GetRaftAppliedIndex() == member.GetRaftIndex()
}

func serverFromRemoval(servers []*hcloud.Server, id int64) *hcloud.Server {
	for _, server := range servers {
		if server.ID == id {
			return server
		}
	}

	return nil
}

func (p *Provisioner) captureRemovalSnapshot(
	ctx context.Context,
	state controlPlaneRemovalState,
	parent string,
) (etcdSnapshotArtifact, error) {
	ctx, cancel := context.WithTimeout(ctx, removalSnapshotTimeout)
	defer cancel()

	var survivor *hcloud.Server

	for _, server := range state.servers {
		if server.ID != state.target.ServerID {
			survivor = server

			break
		}
	}

	address, err := p.hetznerNodeAddress(survivor)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	client, err := p.openEtcdMembershipClient(ctx, address)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}
	defer client.Close() //nolint:errcheck

	source, ok := client.(etcdSnapshotSource)
	if !ok {
		return etcdSnapshotArtifact{}, fmt.Errorf(
			"%w: client cannot capture recovery snapshot",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	dir, err := os.MkdirTemp(parent, ".ksail-control-plane-recovery-")
	if err != nil {
		return etcdSnapshotArtifact{}, fmt.Errorf("create private recovery directory: %w", err)
	}

	membership, err := consistentMembership(state.quorum.Views)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	return captureVerifiedSnapshot(ctx, source, dir, "etcd-before-removal.db", membership)
}

func (p *Provisioner) proveRemovalAfterMembership(
	ctx context.Context,
	cloud *hetzner.Provider,
	clientset kubernetes.Interface,
	clusterName string,
	before controlPlaneRemovalState,
	survivors []*hcloud.Server,
	floating *hcloud.FloatingIP,
) error {
	err := checkRemovalEndpoint(ctx, cloud, clusterName, floating)
	if err != nil {
		return err
	}

	_, err = p.readScaleDownNodes(ctx, clientset, survivors)
	if err != nil {
		return err
	}

	after, err := p.observeRemovalQuorum(ctx, survivors)
	if err != nil {
		return err
	}

	err = proveRemovalMembership(survivors, after)
	if err != nil {
		return err
	}

	expected, err := consistentMembership(before.quorum.Views)
	if err != nil {
		return err
	}

	expected = slices.DeleteFunc(
		expected,
		func(id uint64) bool { return id == before.target.EtcdMemberID },
	)

	actual, err := consistentMembership(after.Views)
	if err != nil {
		return err
	}

	if !slices.Equal(expected, actual) {
		return fmt.Errorf(
			"%w: membership removal not confirmed exactly",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}
