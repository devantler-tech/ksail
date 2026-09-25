package talosprovisioner

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const (
	observedSurvivorIP = "10.0.0.2"
	observedThirdIP    = "10.0.0.3"
)

var errObservationRPC = errors.New("observation RPC failed")

// fakeObservationClient answers the three read-only etcd calls for one node.
type fakeObservationClient struct {
	members     *machineapi.EtcdMemberListResponse
	membersErr  error
	status      *machineapi.EtcdStatusResponse
	statusErr   error
	alarms      *machineapi.EtcdAlarmListResponse
	alarmsErr   error
	listRequest *machineapi.EtcdMemberListRequest
	closed      bool
}

func (client *fakeObservationClient) EtcdMemberList(
	_ context.Context, request *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption,
) (*machineapi.EtcdMemberListResponse, error) {
	client.listRequest = request

	return client.members, client.membersErr
}

func (client *fakeObservationClient) EtcdStatus(
	context.Context, ...grpc.CallOption,
) (*machineapi.EtcdStatusResponse, error) {
	return client.status, client.statusErr
}

func (client *fakeObservationClient) EtcdAlarmList(
	context.Context, ...grpc.CallOption,
) (*machineapi.EtcdAlarmListResponse, error) {
	return client.alarms, client.alarmsErr
}

func (client *fakeObservationClient) Close() error {
	client.closed = true

	return nil
}

// healthyObservationClient reports a three-member cluster in which reporter is healthy.
func healthyObservationClient(reporter uint64) *fakeObservationClient {
	return &fakeObservationClient{
		members: &machineapi.EtcdMemberListResponse{Messages: []*machineapi.EtcdMembers{{
			Members: quorumMembers(quorumTarget, quorumSurvivor, quorumThird),
		}}},
		status: &machineapi.EtcdStatusResponse{Messages: []*machineapi.EtcdStatus{{
			MemberStatus: &machineapi.EtcdMemberStatus{MemberId: reporter, Leader: quorumSurvivor},
		}}},
		alarms: &machineapi.EtcdAlarmListResponse{Messages: []*machineapi.EtcdAlarm{{}}},
	}
}

func openerFor(clients map[string]*fakeObservationClient) etcdObservationOpener {
	return func(_ context.Context, nodeIP string) (etcdObservationClient, error) {
		client, ok := clients[nodeIP]
		if !ok {
			return nil, errObservationRPC
		}

		return client, nil
	}
}

func healthyObservationClients() map[string]*fakeObservationClient {
	return map[string]*fakeObservationClient{
		observedSurvivorIP: healthyObservationClient(quorumSurvivor),
		observedThirdIP:    healthyObservationClient(quorumThird),
	}
}

func TestObserveEtcdQuorumFeedsAProvableQuorum(t *testing.T) {
	t.Parallel()

	clients := healthyObservationClients()

	observation, err := observeEtcdQuorum(
		t.Context(), openerFor(clients), []string{observedSurvivorIP, observedThirdIP},
	)
	require.NoError(t, err)
	require.Len(t, observation.Views, 2)
	require.NoError(t, proveSurvivingQuorum(controlPlaneQuorumTarget(), observation))

	for nodeIP, client := range clients {
		require.True(
			t,
			client.listRequest.GetQueryLocal(),
			"%s must be asked for its local view",
			nodeIP,
		)
		require.True(t, client.closed, "%s connection must be closed", nodeIP)
	}
}

func TestObserveEtcdQuorumCarriesAlarmsFromEverySurvivor(t *testing.T) {
	t.Parallel()

	clients := healthyObservationClients()
	clients[observedThirdIP].alarms = &machineapi.EtcdAlarmListResponse{
		Messages: []*machineapi.EtcdAlarm{{
			MemberAlarms: []*machineapi.EtcdMemberAlarm{{
				MemberId: quorumThird,
				Alarm:    machineapi.EtcdMemberAlarm_NOSPACE,
			}},
		}},
	}

	observation, err := observeEtcdQuorum(
		t.Context(), openerFor(clients), []string{observedSurvivorIP, observedThirdIP},
	)
	require.NoError(t, err)
	require.Len(t, observation.Alarms, 1)
	require.ErrorIs(
		t,
		proveSurvivingQuorum(controlPlaneQuorumTarget(), observation),
		ErrEtcdQuorumUnproven,
	)
}

type incompleteObservationCase struct {
	survivors []string
	mutate    func(map[string]*fakeObservationClient)
}

// incompleteObservationCases each break one read, or the survivor list, of a healthy cluster.
func incompleteObservationCases() map[string]incompleteObservationCase {
	return map[string]incompleteObservationCase{
		"no survivors": {survivors: nil},
		"invalid address": {
			survivors: []string{"cp-2"},
		},
		"duplicate survivor": {
			survivors: []string{observedSurvivorIP, observedSurvivorIP},
		},
		"duplicate survivor in IPv4-mapped form": {
			survivors: []string{observedSurvivorIP, "::ffff:" + observedSurvivorIP},
			// Reachable under both spellings, so only the duplicate check can refuse it.
			mutate: func(clients map[string]*fakeObservationClient) {
				clients["::ffff:"+observedSurvivorIP] = clients[observedSurvivorIP]
			},
		},
		"unreachable survivor": {
			survivors: []string{observedSurvivorIP, "10.0.0.9"},
		},
		"member list fails": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedThirdIP].membersErr = errObservationRPC
			},
		},
		"member list from two nodes": {
			mutate: func(clients map[string]*fakeObservationClient) {
				members := clients[observedThirdIP].members
				members.Messages = append(members.Messages, members.GetMessages()[0])
			},
		},
		"empty member list response": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedThirdIP].members = &machineapi.EtcdMemberListResponse{}
			},
		},
		"status fails": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedSurvivorIP].statusErr = errObservationRPC
			},
		},
		"status without member status": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedSurvivorIP].status = &machineapi.EtcdStatusResponse{
					Messages: []*machineapi.EtcdStatus{{}},
				}
			},
		},
		"alarm list fails": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedThirdIP].alarmsErr = errObservationRPC
			},
		},
		"alarm list without a message": {
			mutate: func(clients map[string]*fakeObservationClient) {
				clients[observedThirdIP].alarms = &machineapi.EtcdAlarmListResponse{}
			},
		},
	}
}

func TestObserveEtcdQuorumRefusesAnIncompleteObservation(t *testing.T) {
	t.Parallel()

	for name, testCase := range incompleteObservationCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clients := healthyObservationClients()
			if testCase.mutate != nil {
				testCase.mutate(clients)
			}

			survivors := testCase.survivors
			if survivors == nil && testCase.mutate != nil {
				survivors = []string{observedSurvivorIP, observedThirdIP}
			}

			observation, err := observeEtcdQuorum(t.Context(), openerFor(clients), survivors)
			require.ErrorIs(t, err, ErrEtcdObservationIncomplete)
			require.Empty(t, observation.Views, "a refused observation must carry no partial views")
		})
	}
}
