package talosprovisioner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/apimachinery/pkg/types"
)

// ErrReplacementLedgerInvalid is returned when a phase record, or a stored ledger, would
// leave the replacement's recovery evidence out of order, unbounded or contradictory.
var ErrReplacementLedgerInvalid = errors.New("replacement ledger is invalid")

// replacementPhase is one step of a single-node replacement, in the only order it runs.
type replacementPhase string

const (
	phasePlanned            replacementPhase = "planned"
	phaseDrained            replacementPhase = "drained"
	phaseMemberRemoved      replacementPhase = "member-removed"
	phaseServerDeleted      replacementPhase = "server-deleted"
	phaseReplacementCreated replacementPhase = "replacement-created"
	phaseJoined             replacementPhase = "joined"
	phaseVerified           replacementPhase = "verified"
)

const (
	replacementLedgerVersion = 1
	// maxLedgerDetailBytes bounds the free-text detail so a record stays a short line of
	// evidence rather than a place to dump command output.
	maxLedgerDetailBytes = 200
	// maxLedgerFileBytes bounds how much of a stored ledger is read back.
	maxLedgerFileBytes = 64 << 10
	ledgerFileMode     = 0o600
	ledgerDirMode      = 0o700
)

// replacementPhaseOrder lists the phases in the order a replacement completes them.
func replacementPhaseOrder() []replacementPhase {
	return []replacementPhase{
		phasePlanned,
		phaseDrained,
		phaseMemberRemoved,
		phaseServerDeleted,
		phaseReplacementCreated,
		phaseJoined,
		phaseVerified,
	}
}

// replacementPhaseRecord is the evidence for one completed phase. It carries identifiers
// and a short detail line only, never secret material. ServerID is the replacement server
// and is set only when the replacement is created; NodeUID is the replacement node and is
// set only when it joins.
type replacementPhaseRecord struct {
	Phase    replacementPhase `json:"phase"`
	At       time.Time        `json:"at"`
	Detail   string           `json:"detail,omitempty"`
	ServerID int64            `json:"serverId,omitempty"`
	NodeUID  types.UID        `json:"nodeUid,omitempty"`
}

// replacementLedger records how far a replacement of Target got, so an interrupted run
// can be recovered from what it actually did rather than from what it meant to do.
type replacementLedger struct {
	Version int                      `json:"version"`
	Target  replacementTarget        `json:"target"`
	Records []replacementPhaseRecord `json:"records"`
}

// replacementRecovery summarises the state an interrupted replacement left behind. It
// names identities only; acting on them is left to the operator or a later guarded step.
type replacementRecovery struct {
	LastPhase replacementPhase
	// IrreversibleCrossed is true once the etcd member was removed: from then on the old
	// node cannot simply be uncordoned.
	IrreversibleCrossed bool
	// RetainedServerID is the target server while it still exists, and zero once deleted.
	RetainedServerID    int64
	RemovedMemberID     uint64
	ReplacementServerID int64
	ReplacementNodeUID  types.UID
	Complete            bool
}

// newReplacementLedger starts a ledger for target. A target without its immutable server
// and node identities cannot anchor recovery evidence and is rejected. Every ledger passes
// through etcd member removal, so a target without an etcd member is rejected too.
func newReplacementLedger(target replacementTarget) (replacementLedger, error) {
	if target.ServerID <= 0 || target.NodeUID == "" || target.EtcdMemberID == 0 {
		return replacementLedger{}, fmt.Errorf(
			"%w: target needs a server ID, a node UID and an etcd member ID",
			ErrReplacementLedgerInvalid)
	}

	return replacementLedger{Version: replacementLedgerVersion, Target: target}, nil
}

// record appends the evidence for the next phase. A record that is out of order, repeated,
// unbounded or contradictory is rejected and leaves the ledger unchanged.
func (ledger *replacementLedger) record(entry replacementPhaseRecord) error {
	err := ledger.validateNext(entry)
	if err != nil {
		return err
	}

	ledger.Records = append(ledger.Records, entry)

	return nil
}

func (ledger *replacementLedger) validateNext(entry replacementPhaseRecord) error {
	order := replacementPhaseOrder()
	if len(ledger.Records) >= len(order) {
		return fmt.Errorf("%w: replacement already verified", ErrReplacementLedgerInvalid)
	}

	expected := order[len(ledger.Records)]
	if entry.Phase != expected {
		return fmt.Errorf("%w: expected phase %q, got %q",
			ErrReplacementLedgerInvalid, expected, entry.Phase)
	}

	err := validateRecordTime(ledger.Records, entry)
	if err != nil {
		return err
	}

	err = validateRecordDetail(entry.Detail)
	if err != nil {
		return err
	}

	return ledger.validateRecordIdentities(entry)
}

func validateRecordTime(previous []replacementPhaseRecord, entry replacementPhaseRecord) error {
	if entry.At.IsZero() {
		return fmt.Errorf("%w: phase %q has no time", ErrReplacementLedgerInvalid, entry.Phase)
	}

	if len(previous) > 0 && entry.At.Before(previous[len(previous)-1].At) {
		return fmt.Errorf("%w: phase %q is recorded before the phase it follows",
			ErrReplacementLedgerInvalid, entry.Phase)
	}

	return nil
}

func validateRecordDetail(detail string) error {
	if len(detail) > maxLedgerDetailBytes {
		return fmt.Errorf("%w: detail is %d bytes, limit %d",
			ErrReplacementLedgerInvalid, len(detail), maxLedgerDetailBytes)
	}

	for _, character := range detail {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: detail must be a single line of printable text",
				ErrReplacementLedgerInvalid)
		}
	}

	return nil
}

func (ledger *replacementLedger) validateRecordIdentities(entry replacementPhaseRecord) error {
	if entry.Phase == phaseReplacementCreated {
		if entry.ServerID <= 0 || entry.ServerID == ledger.Target.ServerID {
			return fmt.Errorf("%w: replacement server ID must be new and non-zero",
				ErrReplacementLedgerInvalid)
		}
	} else if entry.ServerID != 0 {
		return fmt.Errorf("%w: phase %q must not carry a server ID",
			ErrReplacementLedgerInvalid, entry.Phase)
	}

	if entry.Phase == phaseJoined {
		if entry.NodeUID == "" || entry.NodeUID == ledger.Target.NodeUID {
			return fmt.Errorf("%w: replacement node UID must be new and non-empty",
				ErrReplacementLedgerInvalid)
		}
	} else if entry.NodeUID != "" {
		return fmt.Errorf("%w: phase %q must not carry a node UID",
			ErrReplacementLedgerInvalid, entry.Phase)
	}

	return nil
}

// recovery summarises what the recorded phases leave behind.
func (ledger *replacementLedger) recovery() (replacementRecovery, error) {
	if len(ledger.Records) == 0 {
		return replacementRecovery{}, fmt.Errorf(
			"%w: no phase recorded", ErrReplacementLedgerInvalid)
	}

	summary := replacementRecovery{
		LastPhase:        ledger.Records[len(ledger.Records)-1].Phase,
		RetainedServerID: ledger.Target.ServerID,
	}

	for _, entry := range ledger.Records {
		switch entry.Phase {
		case phaseMemberRemoved:
			summary.IrreversibleCrossed = true
			summary.RemovedMemberID = ledger.Target.EtcdMemberID
		case phaseServerDeleted:
			summary.RetainedServerID = 0
		case phaseReplacementCreated:
			summary.ReplacementServerID = entry.ServerID
		case phaseJoined:
			summary.ReplacementNodeUID = entry.NodeUID
		case phaseVerified:
			summary.Complete = true
		case phasePlanned, phaseDrained:
		}
	}

	return summary, nil
}

// saveReplacementLedger writes the ledger privately and atomically: the file is readable by
// its owner only, its directory is private, and an interrupted write leaves the previous
// ledger in place rather than a partial one.
func saveReplacementLedger(path string, ledger replacementLedger) error {
	_, err := replayReplacementLedger(ledger)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)

	err = ensurePrivateDir(dir)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("encode replacement ledger: %w", err)
	}

	err = fsutil.AtomicWriteFile(path, data, ledgerFileMode)
	if err != nil {
		return fmt.Errorf("write replacement ledger: %w", err)
	}

	return nil
}

func ensurePrivateDir(dir string) error {
	err := os.MkdirAll(dir, ledgerDirMode)
	if err != nil {
		return fmt.Errorf("create replacement ledger directory: %w", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspect replacement ledger directory: %w", err)
	}

	if info.Mode().Perm()&^ledgerDirMode != 0 {
		return fmt.Errorf("%w: directory %s is accessible to other users (%v)",
			ErrReplacementLedgerInvalid, dir, info.Mode().Perm())
	}

	return nil
}

// loadReplacementLedger reads a stored ledger back and trusts it only after replaying every
// record through the same checks that recorded it.
func loadReplacementLedger(path string) (replacementLedger, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return replacementLedger{}, fmt.Errorf("open replacement ledger: %w", err)
	}

	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return replacementLedger{}, fmt.Errorf("inspect replacement ledger: %w", err)
	}

	if info.Mode().Perm()&^ledgerFileMode != 0 {
		return replacementLedger{}, fmt.Errorf("%w: %s is accessible to other users (%v)",
			ErrReplacementLedgerInvalid, path, info.Mode().Perm())
	}

	data, err := io.ReadAll(io.LimitReader(file, maxLedgerFileBytes+1))
	if err != nil {
		return replacementLedger{}, fmt.Errorf("read replacement ledger: %w", err)
	}

	if len(data) > maxLedgerFileBytes {
		return replacementLedger{}, fmt.Errorf("%w: ledger exceeds %d bytes",
			ErrReplacementLedgerInvalid, maxLedgerFileBytes)
	}

	var stored replacementLedger

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&stored)
	if err != nil {
		return replacementLedger{}, fmt.Errorf("%w: %w", ErrReplacementLedgerInvalid, err)
	}

	// Decode reads only the first value; anything after it means the file is not one ledger.
	err = decoder.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return replacementLedger{}, fmt.Errorf(
			"%w: trailing data after ledger", ErrReplacementLedgerInvalid)
	}

	return replayReplacementLedger(stored)
}

// replayReplacementLedger rebuilds stored from its target, re-validating every record.
func replayReplacementLedger(stored replacementLedger) (replacementLedger, error) {
	if stored.Version != replacementLedgerVersion {
		return replacementLedger{}, fmt.Errorf("%w: unsupported version %d",
			ErrReplacementLedgerInvalid, stored.Version)
	}

	replayed, err := newReplacementLedger(stored.Target)
	if err != nil {
		return replacementLedger{}, err
	}

	for _, entry := range stored.Records {
		err = replayed.record(entry)
		if err != nil {
			return replacementLedger{}, err
		}
	}

	return replayed, nil
}
