package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
)

// ErrEtcdObservationIncomplete is returned when the etcd facts a single-node replacement depends
// on could not be read in full from every surviving control-plane node.
var ErrEtcdObservationIncomplete = errors.New("etcd observation is incomplete")

// etcdObservationClient is the read-only slice of the Talos API that observeEtcdQuorum needs.
// A *talosclient.Client satisfies it.
type etcdObservationClient interface {
	EtcdMemberList(
		ctx context.Context,
		request *machineapi.EtcdMemberListRequest,
		options ...grpc.CallOption,
	) (*machineapi.EtcdMemberListResponse, error)
	EtcdStatus(
		ctx context.Context,
		options ...grpc.CallOption,
	) (*machineapi.EtcdStatusResponse, error)
	EtcdAlarmList(
		ctx context.Context,
		options ...grpc.CallOption,
	) (*machineapi.EtcdAlarmListResponse, error)
	Close() error
}

// The real Talos client must keep satisfying the observation interface.
var _ etcdObservationClient = (*talosclient.Client)(nil)

// etcdObservationOpener connects to one control-plane node's Talos API.
type etcdObservationOpener func(ctx context.Context, nodeIP string) (etcdObservationClient, error)

// observeEtcdQuorum reads, from every surviving control-plane node, that node's own view of the
// etcd membership, its own member status, and the alarms it reports, and assembles them into
// the observation proveSurvivingQuorum judges.
//
// It only reads. Each node is asked for its LOCAL membership view, so survivors that disagree
// stay visible as a disagreement instead of being hidden behind one linearizable read. Every
// node must answer every read with exactly one message: a failed connection, a failed read, a
// missing or extra message, or an empty status fails the whole observation, because a partial
// observation could omit exactly the survivor that would have refused the replacement. Alarms
// are the union over all survivors.
func observeEtcdQuorum(
	ctx context.Context,
	open etcdObservationOpener,
	survivorIPs []string,
) (etcdQuorumObservation, error) {
	if len(survivorIPs) == 0 {
		return etcdQuorumObservation{}, fmt.Errorf("%w: no surviving control-plane node to query",
			ErrEtcdObservationIncomplete)
	}

	seen := make(map[string]bool, len(survivorIPs))
	observation := etcdQuorumObservation{Views: make([]etcdMemberView, 0, len(survivorIPs))}

	for _, nodeIP := range survivorIPs {
		address, err := netip.ParseAddr(nodeIP)
		if err != nil {
			return etcdQuorumObservation{}, fmt.Errorf("%w: invalid control-plane address %q: %w",
				ErrEtcdObservationIncomplete, nodeIP, err)
		}

		if seen[address.String()] {
			return etcdQuorumObservation{}, fmt.Errorf(
				"%w: control-plane address %s is listed twice",
				ErrEtcdObservationIncomplete,
				address,
			)
		}

		seen[address.String()] = true

		view, alarms, err := observeEtcdNode(ctx, open, address.String())
		if err != nil {
			return etcdQuorumObservation{}, err
		}

		observation.Views = append(observation.Views, view)
		observation.Alarms = append(observation.Alarms, alarms...)
	}

	return observation, nil
}

func observeEtcdNode(
	ctx context.Context,
	open etcdObservationOpener,
	nodeIP string,
) (etcdMemberView, []*machineapi.EtcdMemberAlarm, error) {
	client, err := open(ctx, nodeIP)
	if err != nil {
		return etcdMemberView{}, nil, fmt.Errorf("%w: connect to %s: %w",
			ErrEtcdObservationIncomplete, nodeIP, err)
	}

	defer client.Close() //nolint:errcheck

	members, err := client.EtcdMemberList(ctx, &machineapi.EtcdMemberListRequest{QueryLocal: true})
	if err != nil {
		return etcdMemberView{}, nil, fmt.Errorf("%w: list members on %s: %w",
			ErrEtcdObservationIncomplete, nodeIP, err)
	}

	if len(members.GetMessages()) != 1 {
		return etcdMemberView{}, nil, fmt.Errorf(
			"%w: %s answered the member list with %d messages, want 1",
			ErrEtcdObservationIncomplete,
			nodeIP,
			len(members.GetMessages()),
		)
	}

	status, err := client.EtcdStatus(ctx)
	if err != nil {
		return etcdMemberView{}, nil, fmt.Errorf("%w: read status on %s: %w",
			ErrEtcdObservationIncomplete, nodeIP, err)
	}

	if len(status.GetMessages()) != 1 || status.GetMessages()[0].GetMemberStatus() == nil {
		return etcdMemberView{}, nil, fmt.Errorf("%w: %s returned no single member status",
			ErrEtcdObservationIncomplete, nodeIP)
	}

	alarmResponse, err := client.EtcdAlarmList(ctx)
	if err != nil {
		return etcdMemberView{}, nil, fmt.Errorf("%w: list alarms on %s: %w",
			ErrEtcdObservationIncomplete, nodeIP, err)
	}

	if len(alarmResponse.GetMessages()) != 1 {
		return etcdMemberView{}, nil, fmt.Errorf(
			"%w: %s answered the alarm list with %d messages, want 1",
			ErrEtcdObservationIncomplete,
			nodeIP,
			len(alarmResponse.GetMessages()),
		)
	}

	view := etcdMemberView{
		Members: members.GetMessages()[0].GetMembers(),
		Status:  status.GetMessages()[0].GetMemberStatus(),
	}

	return view, alarmResponse.GetMessages()[0].GetMemberAlarms(), nil
}
