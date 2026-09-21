package talosprovisioner

import (
	"errors"
	"fmt"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// ErrEtcdQuorumUnproven is returned when the observations do not positively prove that the
// members surviving a replacement form a healthy quorum.
var ErrEtcdQuorumUnproven = errors.New("surviving etcd quorum is not proven")

// quorumDivisor is the raft majority divisor: a quorum of n members is n/quorumDivisor+1.
const quorumDivisor = 2

// etcdMemberView is what one surviving control-plane node reports over the authenticated Talos
// API: the voting membership as that node sees it, and that node's own member status.
type etcdMemberView struct {
	Members []*machineapi.EtcdMember
	Status  *machineapi.EtcdMemberStatus
}

// etcdQuorumObservation is one read-only snapshot of the etcd facts a single-node replacement
// depends on: a view per surviving control-plane node queried, and the cluster's alarms.
type etcdQuorumObservation struct {
	Views  []etcdMemberView
	Alarms []*machineapi.EtcdMemberAlarm
}

// proveSurvivingQuorum succeeds only when the observations prove that removing the target's
// etcd member leaves a healthy quorum of the current membership. Each surviving view must
// report the same learner-free membership containing the target; each reporter must be a
// distinct non-target member with no errors; every reporter must agree on a leader that is a
// member; no member may carry an alarm; and the healthy reporters must reach n/2+1 of the
// current membership. Anything it cannot prove is refused rather than assumed, because the
// next step removes a member. A worker target has no member to remove and needs no proof.
func proveSurvivingQuorum(target replacementTarget, observation etcdQuorumObservation) error {
	if target.Role == hetzner.NodeTypeWorker {
		return nil
	}

	if target.EtcdMemberID == 0 {
		return fmt.Errorf("%w: control-plane target %q has no etcd member ID",
			ErrEtcdQuorumUnproven, target.ServerName)
	}

	if len(observation.Views) == 0 {
		return fmt.Errorf("%w: no surviving member was observed", ErrEtcdQuorumUnproven)
	}

	membership, err := consistentMembership(observation.Views)
	if err != nil {
		return err
	}

	if !slices.Contains(membership, target.EtcdMemberID) {
		return fmt.Errorf("%w: target member %d is not in the membership %v",
			ErrEtcdQuorumUnproven, target.EtcdMemberID, membership)
	}

	healthy, err := healthySurvivors(observation.Views, membership, target.EtcdMemberID)
	if err != nil {
		return err
	}

	err = noActiveAlarms(observation.Alarms)
	if err != nil {
		return err
	}

	quorum := len(membership)/quorumDivisor + 1
	if healthy < quorum {
		return fmt.Errorf(
			"%w: %d healthy survivor(s) of %d member(s); removing member %d needs %d",
			ErrEtcdQuorumUnproven, healthy, len(membership), target.EtcdMemberID, quorum,
		)
	}

	return nil
}

// consistentMembership returns the sorted voting member IDs every view agrees on.
func consistentMembership(views []etcdMemberView) ([]uint64, error) {
	var reference []uint64

	for index, view := range views {
		ids, err := votingMemberIDs(view.Members)
		if err != nil {
			return nil, err
		}

		if index == 0 {
			reference = ids

			continue
		}

		if !slices.Equal(ids, reference) {
			return nil, fmt.Errorf("%w: survivors disagree about membership: %v and %v",
				ErrEtcdQuorumUnproven, reference, ids)
		}
	}

	return reference, nil
}

func votingMemberIDs(members []*machineapi.EtcdMember) ([]uint64, error) {
	ids := make([]uint64, 0, len(members))

	for _, member := range members {
		switch {
		case member == nil || member.GetId() == 0:
			return nil, fmt.Errorf("%w: a member has no ID", ErrEtcdQuorumUnproven)
		case member.GetIsLearner():
			return nil, fmt.Errorf("%w: member %d is a learner; membership is changing",
				ErrEtcdQuorumUnproven, member.GetId())
		case slices.Contains(ids, member.GetId()):
			return nil, fmt.Errorf("%w: member %d is listed twice",
				ErrEtcdQuorumUnproven, member.GetId())
		}

		ids = append(ids, member.GetId())
	}

	slices.Sort(ids)

	return ids, nil
}

// healthySurvivors counts the distinct surviving members whose own status proves them healthy
// and in agreement on one leader.
func healthySurvivors(views []etcdMemberView, membership []uint64, target uint64) (int, error) {
	reporters := make([]uint64, 0, len(views))

	var leader uint64

	for _, view := range views {
		status := view.Status

		err := checkSurvivorStatus(status, membership, target, reporters)
		if err != nil {
			return 0, err
		}

		if leader != 0 && status.GetLeader() != leader {
			return 0, fmt.Errorf("%w: survivors disagree about the leader: %d and %d",
				ErrEtcdQuorumUnproven, leader, status.GetLeader())
		}

		leader = status.GetLeader()
		reporters = append(reporters, status.GetMemberId())
	}

	if !slices.Contains(membership, leader) {
		return 0, fmt.Errorf("%w: leader %d is not a member", ErrEtcdQuorumUnproven, leader)
	}

	return len(reporters), nil
}

// checkSurvivorStatus proves one surviving view's own status: present, from a distinct
// non-target member, without errors, and naming a leader.
func checkSurvivorStatus(
	status *machineapi.EtcdMemberStatus,
	membership []uint64,
	target uint64,
	reporters []uint64,
) error {
	switch {
	case status == nil:
		return fmt.Errorf("%w: a surviving view has no status", ErrEtcdQuorumUnproven)
	case status.GetMemberId() == target:
		return fmt.Errorf("%w: the target member %d reported as a survivor",
			ErrEtcdQuorumUnproven, target)
	case !slices.Contains(membership, status.GetMemberId()):
		return fmt.Errorf("%w: reporter %d is not a member",
			ErrEtcdQuorumUnproven, status.GetMemberId())
	case slices.Contains(reporters, status.GetMemberId()):
		return fmt.Errorf("%w: member %d reported twice",
			ErrEtcdQuorumUnproven, status.GetMemberId())
	case len(status.GetErrors()) > 0:
		return fmt.Errorf("%w: member %d reports errors: %v",
			ErrEtcdQuorumUnproven, status.GetMemberId(), status.GetErrors())
	case status.GetLeader() == 0:
		return fmt.Errorf("%w: member %d reports no leader",
			ErrEtcdQuorumUnproven, status.GetMemberId())
	}

	return nil
}

func noActiveAlarms(alarms []*machineapi.EtcdMemberAlarm) error {
	for _, alarm := range alarms {
		if alarm != nil && alarm.GetAlarm() != machineapi.EtcdMemberAlarm_NONE {
			return fmt.Errorf("%w: member %d carries alarm %s",
				ErrEtcdQuorumUnproven, alarm.GetMemberId(), alarm.GetAlarm())
		}
	}

	return nil
}
