package aws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	errNodegroupStateVersion = errors.New("unsupported EKS nodegroup state version")
	errNodegroupStateTarget  = errors.New("EKS nodegroup state target mismatch")
	errNodegroupStateDrift   = errors.New("EKS nodegroup capacity state drift")
	// errNodegroupRestorePending marks a nodegroup that is still stopped after a restore request.
	// EKS applies a scaling update asynchronously, so this one state is re-read until the deadline;
	// every other drift fails at once.
	errNodegroupRestorePending = errors.New("EKS nodegroup restore not yet applied")
)

func newNodegroupState(
	clusterName, region string,
	nodegroups []eksctlclient.NodegroupSummary,
) (*state.EKSNodegroupState, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil, fmt.Errorf(
			"refusing to persist regionless EKS nodegroup state: %w",
			errNodegroupStateTarget,
		)
	}

	capacities := make([]state.EKSNodegroupCapacity, 0, len(nodegroups))
	seen := make(map[string]struct{}, len(nodegroups))

	for _, nodegroup := range nodegroups {
		err := validateLiveNodegroup(clusterName, nodegroup)
		if err != nil {
			return nil, err
		}

		if _, duplicate := seen[nodegroup.Name]; duplicate {
			return nil, fmt.Errorf(
				"duplicate nodegroup %q: %w",
				nodegroup.Name,
				errNodegroupStateDrift,
			)
		}

		seen[nodegroup.Name] = struct{}{}
		capacities = append(capacities, state.EKSNodegroupCapacity{
			Name:            nodegroup.Name,
			DesiredCapacity: nodegroup.DesiredCap,
			MinSize:         nodegroup.MinSize,
			MaxSize:         nodegroup.MaxSize,
		})
	}

	_, err := validateSavedCapacities(capacities)
	if err != nil {
		return nil, err
	}

	sort.Slice(capacities, func(i, j int) bool { return capacities[i].Name < capacities[j].Name })

	return &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      region,
		Nodegroups:  capacities,
	}, nil
}

func validateNodegroupTransition(
	clusterName, region string,
	snapshot *state.EKSNodegroupState,
	nodegroups []eksctlclient.NodegroupSummary,
) (map[string]eksctlclient.NodegroupSummary, error) {
	err := validateNodegroupStateIdentity(clusterName, region, snapshot)
	if err != nil {
		return nil, err
	}

	savedByName, err := validateSavedCapacities(snapshot.Nodegroups)
	if err != nil {
		return nil, err
	}

	if len(nodegroups) != len(savedByName) {
		return nil, fmt.Errorf(
			"live nodegroup count %d does not match saved count %d: %w",
			len(nodegroups),
			len(savedByName),
			errNodegroupStateDrift,
		)
	}

	liveByName := make(map[string]eksctlclient.NodegroupSummary, len(nodegroups))

	for _, nodegroup := range nodegroups {
		err = validateLiveNodegroup(clusterName, nodegroup)
		if err != nil {
			return nil, err
		}

		capacity, found := savedByName[nodegroup.Name]
		if !found || (!nodegroupMatchesCapacity(nodegroup, capacity) &&
			!nodegroupIsStopped(nodegroup, capacity)) {
			return nil, fmt.Errorf(
				"live nodegroup %q is neither saved nor safely stopped: %w",
				nodegroup.Name,
				errNodegroupStateDrift,
			)
		}

		if _, duplicate := liveByName[nodegroup.Name]; duplicate {
			return nil, fmt.Errorf(
				"duplicate live nodegroup %q: %w",
				nodegroup.Name,
				errNodegroupStateDrift,
			)
		}

		liveByName[nodegroup.Name] = nodegroup
	}

	return liveByName, nil
}

func validateNodegroupStateIdentity(
	clusterName, region string,
	snapshot *state.EKSNodegroupState,
) error {
	if snapshot == nil {
		return fmt.Errorf("nil EKS nodegroup state: %w", errNodegroupStateDrift)
	}

	if snapshot.Version != state.EKSNodegroupStateVersion {
		return fmt.Errorf(
			"got version %d, want %d: %w",
			snapshot.Version,
			state.EKSNodegroupStateVersion,
			errNodegroupStateVersion,
		)
	}

	if strings.TrimSpace(clusterName) == "" || snapshot.ClusterName != clusterName ||
		strings.TrimSpace(region) == "" || snapshot.Region != region {
		return fmt.Errorf(
			"saved cluster/region %q/%q does not match target %q/%q: %w",
			snapshot.ClusterName,
			snapshot.Region,
			clusterName,
			region,
			errNodegroupStateTarget,
		)
	}

	return nil
}

func validateSavedCapacities(
	capacities []state.EKSNodegroupCapacity,
) (map[string]state.EKSNodegroupCapacity, error) {
	if len(capacities) == 0 {
		return nil, fmt.Errorf("saved nodegroup set is empty: %w", errNodegroupStateDrift)
	}

	byName := make(map[string]state.EKSNodegroupCapacity, len(capacities))

	for _, capacity := range capacities {
		if strings.TrimSpace(capacity.Name) == "" || capacity.MinSize < 0 ||
			capacity.DesiredCapacity < 0 ||
			capacity.DesiredCapacity < capacity.MinSize || capacity.MaxSize <= 0 ||
			capacity.MaxSize < capacity.DesiredCapacity {
			return nil, fmt.Errorf(
				"invalid saved nodegroup capacity for %q: %w",
				capacity.Name,
				errNodegroupStateDrift,
			)
		}

		if _, duplicate := byName[capacity.Name]; duplicate {
			return nil, fmt.Errorf(
				"duplicate saved nodegroup %q: %w",
				capacity.Name,
				errNodegroupStateDrift,
			)
		}

		byName[capacity.Name] = capacity
	}

	return byName, nil
}

func validateLiveNodegroup(clusterName string, nodegroup eksctlclient.NodegroupSummary) error {
	if strings.TrimSpace(nodegroup.Name) == "" ||
		(nodegroup.Cluster != "" && nodegroup.Cluster != clusterName) ||
		nodegroup.MinSize < 0 || nodegroup.DesiredCap < 0 || nodegroup.MaxSize < nodegroup.DesiredCap {
		return fmt.Errorf(
			"invalid live nodegroup %q for cluster %q: %w",
			nodegroup.Name,
			clusterName,
			errNodegroupStateDrift,
		)
	}

	return nil
}

func verifyNodegroupsRestored(
	clusterName, region string,
	snapshot *state.EKSNodegroupState,
	nodegroups []eksctlclient.NodegroupSummary,
) error {
	liveByName, err := validateNodegroupTransition(clusterName, region, snapshot, nodegroups)
	if err != nil {
		return fmt.Errorf("verify restored EKS nodegroups: %w", err)
	}

	for _, capacity := range snapshot.Nodegroups {
		live := liveByName[capacity.Name]
		if !nodegroupMatchesCapacity(live, capacity) {
			return fmt.Errorf(
				"nodegroup %q is not restored (saved desired/min/max %d/%d/%d, live %d/%d/%d): %w: %w",
				capacity.Name,
				capacity.DesiredCapacity,
				capacity.MinSize,
				capacity.MaxSize,
				live.DesiredCap,
				live.MinSize,
				live.MaxSize,
				errNodegroupRestorePending,
				errNodegroupStateDrift,
			)
		}
	}

	return nil
}

// snapshotIsAllStopped reports whether every nodegroup in the snapshot is already fully scaled down.
// Such a snapshot carries no recoverable capacity — restoring to it is a no-op — so it must never be
// persisted as the target state for a later start.
func snapshotIsAllStopped(snapshot *state.EKSNodegroupState) bool {
	if snapshot == nil || len(snapshot.Nodegroups) == 0 {
		return false
	}

	for _, capacity := range snapshot.Nodegroups {
		if capacity.DesiredCapacity > 0 || capacity.MinSize > 0 {
			return false
		}
	}

	return true
}

func nodegroupMatchesCapacity(
	nodegroup eksctlclient.NodegroupSummary,
	capacity state.EKSNodegroupCapacity,
) bool {
	return nodegroup.DesiredCap == capacity.DesiredCapacity &&
		nodegroup.MinSize == capacity.MinSize &&
		nodegroup.MaxSize == capacity.MaxSize
}

func nodegroupIsStopped(
	nodegroup eksctlclient.NodegroupSummary,
	capacity state.EKSNodegroupCapacity,
) bool {
	return nodegroup.DesiredCap == 0 && nodegroup.MinSize == 0 &&
		nodegroup.MaxSize == capacity.MaxSize
}

func (p *Provider) loadNodegroupState(
	clusterName, region string,
) (*state.EKSNodegroupState, bool, error) {
	snapshot, err := state.LoadEKSNodegroupState(clusterName, region)
	if errors.Is(err, state.ErrEKSNodegroupStateNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("load EKS nodegroup state: %w", err)
	}

	return snapshot, true, nil
}

// startNodegroupsWithoutSnapshot preserves the pre-existing lifecycle for groups stopped without a
// capacity snapshot. A KSail stop from this release records the exact desired size first, so its own
// round trip never reaches this compatibility path — but a cluster stopped by an earlier release (or
// scaled to zero outside KSail) has no snapshot, and earlier releases scaled both desired and minimum
// to zero. Those clusters must stay startable after an upgrade, so this path keeps the historical
// max(MinSize, 1) fallback rather than failing closed. Starting nodes is non-destructive and
// reversible; the exact pre-stop size is simply unknown, which the caller is warned about.
func (p *Provider) startNodegroupsWithoutSnapshot(
	ctx context.Context,
	clusterName string,
	nodegroups []eksctlclient.NodegroupSummary,
) error {
	seen := make(map[string]struct{}, len(nodegroups))

	for _, nodegroup := range nodegroups {
		err := validateLiveNodegroup(clusterName, nodegroup)
		if err != nil {
			return err
		}

		if _, duplicate := seen[nodegroup.Name]; duplicate {
			return fmt.Errorf(
				"duplicate live nodegroup %q: %w",
				nodegroup.Name,
				errNodegroupStateDrift,
			)
		}

		seen[nodegroup.Name] = struct{}{}
	}

	sort.Slice(nodegroups, func(i, j int) bool { return nodegroups[i].Name < nodegroups[j].Name })

	for _, nodegroup := range nodegroups {
		if nodegroup.DesiredCap > 0 {
			continue
		}

		err := p.startNodegroupWithoutSnapshot(ctx, clusterName, nodegroup)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) startNodegroupWithoutSnapshot(
	ctx context.Context,
	clusterName string,
	nodegroup eksctlclient.NodegroupSummary,
) error {
	// Without a snapshot the pre-stop desired size is unknowable, so fall back to the smallest
	// running capacity that honours the group's own minimum — the behaviour every release before
	// capacity snapshots used.
	target := max(nodegroup.MinSize, 1)

	slog.Warn(
		"restoring EKS nodegroup without a capacity snapshot; pre-stop desired size is unknown",
		"cluster", clusterName,
		"nodegroup", nodegroup.Name,
		"target", target,
	)

	err := eksidentity.VerifyBeforeMutation(ctx, p.ownershipVerifier)
	if err != nil {
		return fmt.Errorf(
			"reverify immutable EKS ownership before starting nodegroup %q: %w",
			nodegroup.Name,
			err,
		)
	}

	err = p.client.ScaleNodegroup(
		ctx,
		clusterName,
		nodegroup.Name,
		p.region,
		target,
		nodegroup.MinSize,
		nodegroup.MaxSize,
	)
	if err != nil {
		return fmt.Errorf("start nodes: scale nodegroup %s: %w", nodegroup.Name, err)
	}

	return nil
}

func (p *Provider) restoreSavedNodegroups(
	ctx context.Context,
	clusterName string,
	snapshot *state.EKSNodegroupState,
	liveByName map[string]eksctlclient.NodegroupSummary,
) error {
	for _, capacity := range snapshot.Nodegroups {
		if nodegroupMatchesCapacity(liveByName[capacity.Name], capacity) {
			continue
		}

		err := eksidentity.VerifyBeforeMutation(ctx, p.ownershipVerifier)
		if err != nil {
			return fmt.Errorf(
				"reverify immutable EKS ownership before restoring nodegroup %q: %w",
				capacity.Name,
				err,
			)
		}

		err = p.client.ScaleNodegroup(
			ctx,
			clusterName,
			capacity.Name,
			p.region,
			capacity.DesiredCapacity,
			capacity.MinSize,
			capacity.MaxSize,
		)
		if err != nil {
			return fmt.Errorf("start nodes: scale nodegroup %s: %w", capacity.Name, err)
		}
	}

	return nil
}

// restoreReadbackWindow returns the readback poll interval and deadline, falling back to the
// defaults for a Provider built without NewProvider.
func (p *Provider) restoreReadbackWindow() (time.Duration, time.Duration) {
	interval, timeout := p.restoreReadbackInterval, p.restoreReadbackTimeout
	if interval <= 0 {
		interval = defaultRestoreReadbackInterval
	}

	if timeout <= 0 {
		timeout = defaultRestoreReadbackTimeout
	}

	return interval, timeout
}

// awaitNodegroupsRestored re-reads the nodegroups until every one reports its saved capacity.
// `eksctl scale nodegroup` only starts an asynchronous EKS update, so a group may still read as
// stopped for a while after the request; that state is retried until the deadline. Any other drift,
// and any read failure, fails at once. On timeout the last observed drift is returned, naming the
// saved and live values.
func (p *Provider) awaitNodegroupsRestored(
	ctx context.Context,
	clusterName string,
	snapshot *state.EKSNodegroupState,
) error {
	interval, timeout := p.restoreReadbackWindow()

	var lastErr error

	pollErr := wait.PollUntilContextTimeout(
		ctx,
		interval,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			restored, err := p.listNodegroupsForScale(ctx, clusterName)
			if err != nil {
				return false, fmt.Errorf("verify restored nodegroups: %w", err)
			}

			lastErr = verifyNodegroupsRestored(clusterName, p.region, snapshot, restored)
			if lastErr == nil {
				return true, nil
			}

			if errors.Is(lastErr, errNodegroupRestorePending) {
				return false, nil
			}

			return false, lastErr
		},
	)
	if pollErr == nil {
		return nil
	}

	if lastErr != nil && errors.Is(lastErr, errNodegroupRestorePending) &&
		wait.Interrupted(pollErr) && ctx.Err() == nil {
		return fmt.Errorf(
			"nodegroups not restored within %s: %w",
			timeout,
			lastErr,
		)
	}

	return pollErr //nolint:wrapcheck // condition errors are already wrapped; context errors stay matchable
}

func (p *Provider) verifyAndClearNodegroupState(
	ctx context.Context,
	clusterName string,
	snapshot *state.EKSNodegroupState,
) error {
	err := p.awaitNodegroupsRestored(ctx, clusterName, snapshot)
	if err != nil {
		return err
	}

	err = state.DeleteEKSNodegroupState(clusterName, p.region)
	if err != nil {
		return fmt.Errorf("clear restored EKS nodegroup state: %w", err)
	}

	return nil
}
