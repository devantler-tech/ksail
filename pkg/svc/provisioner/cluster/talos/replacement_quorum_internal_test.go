package talosprovisioner

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
)

const (
	quorumTarget   uint64 = 7001
	quorumSurvivor uint64 = 7002
	quorumThird    uint64 = 7003
)

func quorumMembers(ids ...uint64) []*machineapi.EtcdMember {
	members := make([]*machineapi.EtcdMember, 0, len(ids))
	for _, id := range ids {
		members = append(members, &machineapi.EtcdMember{Id: id})
	}

	return members
}

func quorumView(reporter uint64, members ...uint64) etcdMemberView {
	return etcdMemberView{
		Members: quorumMembers(members...),
		Status:  &machineapi.EtcdMemberStatus{MemberId: reporter, Leader: quorumSurvivor},
	}
}

func controlPlaneQuorumTarget() replacementTarget {
	return replacementTarget{
		ServerID:     101,
		ServerName:   targetName,
		Role:         hetzner.NodeTypeControlPlane,
		NodeUID:      "node-uid-1",
		EtcdMemberID: quorumTarget,
	}
}

func healthyThreeMemberQuorum() etcdQuorumObservation {
	return etcdQuorumObservation{
		Views: []etcdMemberView{
			quorumView(quorumSurvivor, quorumTarget, quorumSurvivor, quorumThird),
			quorumView(quorumThird, quorumTarget, quorumSurvivor, quorumThird),
		},
	}
}

func TestProveSurvivingQuorumAcceptsAHealthyThreeMemberCluster(t *testing.T) {
	t.Parallel()

	require.NoError(t, proveSurvivingQuorum(controlPlaneQuorumTarget(), healthyThreeMemberQuorum()))
}

func TestProveSurvivingQuorumAcceptsATargetThatIsTheLeader(t *testing.T) {
	t.Parallel()

	observation := healthyThreeMemberQuorum()
	for _, view := range observation.Views {
		view.Status.Leader = quorumTarget
	}

	require.NoError(t, proveSurvivingQuorum(controlPlaneQuorumTarget(), observation))
}

func TestProveSurvivingQuorumNeedsNoProofForAWorker(t *testing.T) {
	t.Parallel()

	worker := replacementTarget{ServerID: 201, ServerName: "prod-worker-1", Role: hetzner.NodeTypeWorker}

	require.NoError(t, proveSurvivingQuorum(worker, etcdQuorumObservation{}))
}

//nolint:funlen // one table of refusals keeps each unprovable case beside its mutation
func TestProveSurvivingQuorumRefusesWhatItCannotProve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target func(*replacementTarget)
		mutate func(*etcdQuorumObservation)
	}{
		{
			name:   "control-plane target without an etcd member ID",
			target: func(target *replacementTarget) { target.EtcdMemberID = 0 },
		},
		{
			name:   "no surviving views",
			mutate: func(o *etcdQuorumObservation) { o.Views = nil },
		},
		{
			name: "a two-member cluster cannot lose a member",
			mutate: func(o *etcdQuorumObservation) {
				o.Views = []etcdMemberView{quorumView(quorumSurvivor, quorumTarget, quorumSurvivor)}
			},
		},
		{
			name: "one healthy survivor of three is not a quorum",
			mutate: func(o *etcdQuorumObservation) {
				o.Views = o.Views[:1]
			},
		},
		{
			name: "the target is not a member",
			mutate: func(o *etcdQuorumObservation) {
				o.Views = []etcdMemberView{
					quorumView(quorumSurvivor, quorumSurvivor, quorumThird),
					quorumView(quorumThird, quorumSurvivor, quorumThird),
				}
			},
		},
		{
			name: "survivors disagree about membership",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1] = quorumView(quorumThird, quorumTarget, quorumSurvivor, quorumThird, 7004)
			},
		},
		{
			name: "a member is a learner",
			mutate: func(o *etcdQuorumObservation) {
				for _, view := range o.Views {
					view.Members[2].IsLearner = true
				}
			},
		},
		{
			name: "a member has no ID",
			mutate: func(o *etcdQuorumObservation) {
				for _, view := range o.Views {
					view.Members[2].Id = 0
				}
			},
		},
		{
			name: "a member is listed twice",
			mutate: func(o *etcdQuorumObservation) {
				for i := range o.Views {
					o.Views[i].Members = append(o.Views[i].Members, &machineapi.EtcdMember{Id: quorumThird})
				}
			},
		},
		{
			name: "a view has no status",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status = nil
			},
		},
		{
			name: "the target reports as a survivor",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.MemberId = quorumTarget
			},
		},
		{
			name: "the same survivor reports twice",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.MemberId = quorumSurvivor
			},
		},
		{
			name: "a reporter that is not a member",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.MemberId = 7004
			},
		},
		{
			name: "a survivor reports errors",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.Errors = []string{"etcdserver: no leader"}
			},
		},
		{
			name: "a survivor reports no leader",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.Leader = 0
			},
		},
		{
			name: "survivors disagree about the leader",
			mutate: func(o *etcdQuorumObservation) {
				o.Views[1].Status.Leader = quorumThird
			},
		},
		{
			name: "the leader is not a member",
			mutate: func(o *etcdQuorumObservation) {
				for _, view := range o.Views {
					view.Status.Leader = 7004
				}
			},
		},
		{
			name: "a member carries an active alarm",
			mutate: func(o *etcdQuorumObservation) {
				o.Alarms = []*machineapi.EtcdMemberAlarm{
					{MemberId: quorumThird, Alarm: machineapi.EtcdMemberAlarm_NOSPACE},
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := controlPlaneQuorumTarget()
			if test.target != nil {
				test.target(&target)
			}

			observation := healthyThreeMemberQuorum()
			if test.mutate != nil {
				test.mutate(&observation)
			}

			err := proveSurvivingQuorum(target, observation)
			require.ErrorIs(t, err, ErrEtcdQuorumUnproven)
		})
	}
}

func TestProveSurvivingQuorumIgnoresAnAlarmOfTypeNone(t *testing.T) {
	t.Parallel()

	observation := healthyThreeMemberQuorum()
	observation.Alarms = []*machineapi.EtcdMemberAlarm{
		{MemberId: quorumThird, Alarm: machineapi.EtcdMemberAlarm_NONE},
	}

	require.NoError(t, proveSurvivingQuorum(controlPlaneQuorumTarget(), observation))
}
