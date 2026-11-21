package talosprovisioner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

	err := proveDurableRecoveryPlatform(runtime.GOOS)
	if err != nil {
		return err
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
	// Storage, delete protection and floating endpoints are target-only properties: they are
	// read fresh on every observation, including the one before each leave RPC, and again
	// before provider deletion.
	err := proveRemovableTarget(serverFromRemoval(servers, target.ServerID))
	if err != nil {
		return err
	}

	err = proveSurvivingQuorum(target, quorum)
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

// sameRemovalServer compares the identity of one control-plane server. Storage and delete
// protection are deliberately not identity: on a retained server they never block removal,
// and on the target proveRemovableTarget rechecks them before the leave and before deletion.
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

	// The provider can reassign the endpoint at any time, so read it once more after the
	// recovery record is published. A move during the leave RPC itself is still caught only
	// by the post-membership check.
	err = checkRemovalEndpoint(ctx, cloud, clusterName, floating)
	if err != nil {
		return err
	}

	// Provider state can change at any time, so prove the target removable one last time,
	// by immutable ID, right before the leave. Hetzner and etcd cannot change together, so a
	// change during the RPC is still caught only by the deletion-time check.
	latest, err := cloud.GetServerByID(ctx, state.target.ServerID)
	if err != nil {
		return fmt.Errorf("rebind removal target before membership change: %w", err)
	}

	err = removal.proveProviderTarget(latest)
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
	recordName := removalRecordName(removal.clusterName, removal.state.target.ServerID)

	err = proveNoRemovalRecord(parent, removal.clusterName)
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

// removalRecordName names the attempt record of one server. Cluster names are DNS labels,
// so the "@" separator cannot occur in them and one cluster's records never match another's.
func removalRecordName(clusterName string, serverID int64) string {
	return fmt.Sprintf(".ksail-cp-removal-%s@%d.json", clusterName, serverID)
}

// removalCompletedName names the record of a removal that finished and was verified.
func removalCompletedName(clusterName string, serverID int64) string {
	return fmt.Sprintf(".ksail-cp-removed-%s@%d.json", clusterName, serverID)
}

// proveNoRemovalRecord refuses a new removal while any attempt for the cluster is
// unresolved, whichever server it targeted. A verified removal renames its record to the
// completed name, so it never blocks later removals.
func proveNoRemovalRecord(parent, clusterName string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("read prior removal attempts: %w", err)
	}

	prefix := fmt.Sprintf(".ksail-cp-removal-%s@", clusterName)

	if slices.ContainsFunc(entries, func(entry os.DirEntry) bool {
		return strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".json")
	}) {
		return fmt.Errorf(
			"%w: a prior removal attempt for this cluster needs recovery review",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

// completeRemovalRecord marks a verified removal by renaming its attempt record. The
// completed record is kept as private recovery evidence.
func completeRemovalRecord(parent, clusterName string, serverID int64) error {
	err := os.Rename(
		filepath.Join(parent, removalRecordName(clusterName, serverID)),
		filepath.Join(parent, removalCompletedName(clusterName, serverID)),
	)
	if err != nil {
		return fmt.Errorf("mark removal attempt complete: %w", err)
	}

	return syncRemovalDirectory(parent)
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

	err = sameRemovalMembership(removal.state.quorum, fresh.quorum)
	if err != nil {
		return err
	}

	return proveRemovalCordon(fresh.nodes, removal.state.target)
}

// sameRemovalMembership refuses any membership change between the observation the recovery
// snapshot and the expected post-removal membership were derived from and the fresh proof
// taken immediately before the leave RPC. A survivor that rejoined under a new member ID
// would otherwise pass the fresh proof while the later exact-membership check could only
// detect the mismatch after the target's membership was already removed.
func sameRemovalMembership(before, fresh etcdQuorumObservation) error {
	expected, err := consistentMembership(before.Views)
	if err != nil {
		return err
	}

	actual, err := consistentMembership(fresh.Views)
	if err != nil {
		return err
	}

	if !slices.Equal(expected, actual) {
		return fmt.Errorf(
			"%w: etcd membership changed after cordon: %v and %v",
			ErrControlPlaneScaleDownUnsafe, expected, actual,
		)
	}

	return nil
}

// writeRemovalRecord publishes the membership-attempt record before the leave RPC. The record
// is written and synced under a partial name unique to this attempt and then linked to its
// final name, which never replaces an existing record. A concurrent attempt for the same
// target therefore cannot publish this attempt's file before it is complete. A failed write
// leaves no final record behind and stays retryable, while every attempt that can reach the
// leave has a complete record.
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

	partial := recordName + "." + rand.Text() + ".partial"

	err = writeRemovalRecordFile(root, partial, target, artifact)
	if err == nil {
		err = root.Link(partial, recordName)
		if err != nil {
			err = fmt.Errorf("publish membership attempt: %w", err)
		}
	}

	removeErr := root.Remove(partial)

	if err != nil {
		return err
	}

	if removeErr != nil {
		return fmt.Errorf("remove partial membership attempt: %w", removeErr)
	}

	// The record and the recovery directory are both entries of parent; sync it so neither
	// can vanish after a crash once the leave has committed.
	return syncRemovalDirectory(parent)
}

func writeRemovalRecordFile(
	root *os.Root,
	name string,
	target replacementTarget,
	artifact etcdSnapshotArtifact,
) error {
	record, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, removalRecordPermissions)
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

	return validateRemovalTarget(server, removal.clusterName)
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

	// A finalizer can keep an accepted deletion visible for a while, so wait for it to finish.
	// A Node with another UID is a replacement: stop without touching it.
	err = wait.PollUntilContextTimeout(
		ctx, time.Second, time.Minute, true,
		func(readCtx context.Context) (bool, error) {
			node, readErr := removal.clientset.CoreV1().
				Nodes().
				Get(readCtx, removal.state.target.ServerName, metav1.GetOptions{})
			if apierrors.IsNotFound(readErr) {
				return true, nil
			}

			if readErr != nil {
				return false, fmt.Errorf("confirm native target absence: %w", readErr)
			}

			if node.UID != uid {
				return false, fmt.Errorf(
					"%w: a replacement Node took the target's name",
					ErrControlPlaneScaleDownUnsafe,
				)
			}

			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("%w: native target deletion is not confirmed: %w",
			ErrControlPlaneScaleDownUnsafe, err)
	}

	return nil
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

	kubePath, _, err := removal.provisioner.resolveKubeconfig(removal.clusterName)
	if err != nil {
		return err
	}

	err = completeRemovalRecord(
		filepath.Dir(kubePath), removal.clusterName, removal.state.target.ServerID,
	)
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

	artifact, err := captureVerifiedSnapshot(ctx, source, dir, "etcd-before-removal.db", membership)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	// The snapshot is named in the recovery record, so its directory entry must survive a
	// host crash before any membership change. The parent entry is synced with the record.
	err = syncRemovalDirectory(dir)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	return artifact, nil
}

// proveDurableRecoveryPlatform refuses automated control-plane removal where the recovery
// record and snapshot names cannot be made durable before the leave RPC. Removal flushes the
// directories that hold them; Windows offers no supported way to flush a directory handle,
// and filesystem journaling keeps metadata consistent without making a completed link durable
// when the call returns. A crash after the leave could then lose the recovery evidence.
func proveDurableRecoveryPlatform(goos string) error {
	if goos == "windows" {
		return fmt.Errorf(
			"%w: automated control-plane removal is not supported on Windows, because the "+
				"recovery record cannot be made durable before the membership change; run it "+
				"from Linux, macOS or WSL2",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	return nil
}

// syncRemovalDirectory flushes a directory so the entries created in it are durable. A file
// sync alone does not persist the name that points at the file.
func syncRemovalDirectory(dir string) error {
	err := proveDurableRecoveryPlatform(runtime.GOOS)
	if err != nil {
		return err
	}

	handle, err := os.Open(dir) //nolint:gosec // dir is a KSail-created recovery path
	if err != nil {
		return fmt.Errorf("open recovery directory for sync: %w", err)
	}

	syncErr := handle.Sync()
	closeErr := handle.Close()

	if syncErr != nil {
		return fmt.Errorf("sync recovery directory: %w", syncErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close recovery directory: %w", closeErr)
	}

	return nil
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
