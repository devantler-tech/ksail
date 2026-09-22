package talosprovisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ledgerTargetServerID      = 1001
	ledgerTargetMemberID      = 77
	ledgerReplacementServerID = 2002
	ledgerTargetNodeUID       = "node-old"
	ledgerReplacementNodeUID  = "node-new"
)

func ledgerStart() time.Time {
	return time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
}

func ledgerTarget() replacementTarget {
	return replacementTarget{
		ServerID:     ledgerTargetServerID,
		ServerName:   "prod-control-plane-1",
		Role:         "control-plane",
		NodeUID:      ledgerTargetNodeUID,
		EtcdMemberID: ledgerTargetMemberID,
	}
}

// ledgerThrough returns a ledger with every phase up to and including last recorded.
func ledgerThrough(t *testing.T, last replacementPhase) replacementLedger {
	t.Helper()

	ledger, err := newReplacementLedger(ledgerTarget())
	require.NoError(t, err)

	for index, phase := range replacementPhaseOrder() {
		require.NoError(t, ledger.record(ledgerRecord(phase, index)))

		if phase == last {
			return ledger
		}
	}

	return ledger
}

func ledgerRecord(phase replacementPhase, index int) replacementPhaseRecord {
	entry := replacementPhaseRecord{
		Phase:  phase,
		At:     ledgerStart().Add(time.Duration(index) * time.Minute),
		Detail: "completed " + string(phase),
	}

	switch phase {
	case phaseReplacementCreated:
		entry.ServerID = ledgerReplacementServerID
	case phaseJoined:
		entry.NodeUID = ledgerReplacementNodeUID
	case phasePlanned, phaseDrained, phaseMemberRemoved, phaseServerDeleted, phaseVerified:
	}

	return entry
}

func TestReplacementLedgerRejectsATargetWithoutIdentities(t *testing.T) {
	t.Parallel()

	noServer := ledgerTarget()
	noServer.ServerID = 0

	_, err := newReplacementLedger(noServer)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)

	noNode := ledgerTarget()
	noNode.NodeUID = ""

	_, err = newReplacementLedger(noNode)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)

	// Every ledger passes through member removal, so a target without an etcd member (a
	// worker) would record the removal of member zero.
	noMember := ledgerTarget()
	noMember.EtcdMemberID = 0

	_, err = newReplacementLedger(noMember)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerRecordsEveryPhaseInOrder(t *testing.T) {
	t.Parallel()

	ledger := ledgerThrough(t, phaseVerified)
	require.Len(t, ledger.Records, len(replacementPhaseOrder()))

	err := ledger.record(ledgerRecord(phaseVerified, len(replacementPhaseOrder())))
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerRejectsAnInvalidRecordWithoutChangingIt(t *testing.T) {
	t.Parallel()

	oversize := ledgerRecord(phaseMemberRemoved, 2)
	oversize.Detail = strings.Repeat("x", maxLedgerDetailBytes+1)

	multiline := ledgerRecord(phaseMemberRemoved, 2)
	multiline.Detail = "first line\nsecond line"

	separated := ledgerRecord(phaseMemberRemoved, 2)
	separated.Detail = "first line\u2028second line"

	untimed := ledgerRecord(phaseMemberRemoved, 2)
	untimed.At = time.Time{}

	backwards := ledgerRecord(phaseMemberRemoved, 2)
	backwards.At = ledgerStart().Add(-time.Minute)

	strayServer := ledgerRecord(phaseMemberRemoved, 2)
	strayServer.ServerID = ledgerReplacementServerID

	strayNode := ledgerRecord(phaseMemberRemoved, 2)
	strayNode.NodeUID = ledgerReplacementNodeUID

	cases := map[string]replacementPhaseRecord{
		"skipped phase":         ledgerRecord(phaseServerDeleted, 2),
		"repeated phase":        ledgerRecord(phaseDrained, 2),
		"oversize detail":       oversize,
		"multi-line detail":     multiline,
		"line-separator detail": separated,
		"missing time":          untimed,
		"time goes back":        backwards,
		"stray server ID":       strayServer,
		"stray node UID":        strayNode,
		"unknown phase name":    {Phase: "rebooted", At: ledgerStart().Add(time.Hour)},
	}

	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ledger := ledgerThrough(t, phaseDrained)

			require.ErrorIs(t, ledger.record(entry), ErrReplacementLedgerInvalid)
			require.Len(t, ledger.Records, 2)
		})
	}
}

func TestReplacementLedgerRequiresNewReplacementIdentities(t *testing.T) {
	t.Parallel()

	for name, serverID := range map[string]int64{"missing": 0, "reused": ledgerTargetServerID} {
		t.Run("server "+name, func(t *testing.T) {
			t.Parallel()

			ledger := ledgerThrough(t, phaseServerDeleted)
			entry := ledgerRecord(phaseReplacementCreated, 4)
			entry.ServerID = serverID

			require.ErrorIs(t, ledger.record(entry), ErrReplacementLedgerInvalid)
		})
	}

	for name, nodeUID := range map[string]string{"missing": "", "reused": ledgerTargetNodeUID} {
		t.Run("node "+name, func(t *testing.T) {
			t.Parallel()

			ledger := ledgerThrough(t, phaseReplacementCreated)
			entry := ledgerRecord(phaseJoined, 5)
			entry.NodeUID = types.UID(nodeUID)

			require.ErrorIs(t, ledger.record(entry), ErrReplacementLedgerInvalid)
		})
	}
}

func TestReplacementLedgerRecoveryNamesWhatEachPhaseLeavesBehind(t *testing.T) {
	t.Parallel()

	cases := map[replacementPhase]replacementRecovery{
		phasePlanned: {LastPhase: phasePlanned, RetainedServerID: ledgerTargetServerID},
		phaseDrained: {LastPhase: phaseDrained, RetainedServerID: ledgerTargetServerID},
		phaseMemberRemoved: {
			LastPhase:           phaseMemberRemoved,
			IrreversibleCrossed: true,
			RetainedServerID:    ledgerTargetServerID,
			RemovedMemberID:     ledgerTargetMemberID,
		},
		phaseServerDeleted: {
			LastPhase:           phaseServerDeleted,
			IrreversibleCrossed: true,
			RemovedMemberID:     ledgerTargetMemberID,
		},
		phaseReplacementCreated: {
			LastPhase:           phaseReplacementCreated,
			IrreversibleCrossed: true,
			RemovedMemberID:     ledgerTargetMemberID,
			ReplacementServerID: ledgerReplacementServerID,
		},
		phaseJoined: {
			LastPhase:           phaseJoined,
			IrreversibleCrossed: true,
			RemovedMemberID:     ledgerTargetMemberID,
			ReplacementServerID: ledgerReplacementServerID,
			ReplacementNodeUID:  ledgerReplacementNodeUID,
		},
		phaseVerified: {
			LastPhase:           phaseVerified,
			IrreversibleCrossed: true,
			RemovedMemberID:     ledgerTargetMemberID,
			ReplacementServerID: ledgerReplacementServerID,
			ReplacementNodeUID:  ledgerReplacementNodeUID,
			Complete:            true,
		},
	}

	for phase, want := range cases {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()

			ledger := ledgerThrough(t, phase)

			got, err := ledger.recovery()
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestReplacementLedgerRecoveryRequiresARecord(t *testing.T) {
	t.Parallel()

	ledger, err := newReplacementLedger(ledgerTarget())
	require.NoError(t, err)

	_, err = ledger.recovery()
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerSavesPrivatelyAndLoadsBack(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "recovery", "ledger.json")
	ledger := ledgerThrough(t, phaseReplacementCreated)

	require.NoError(t, saveReplacementLedger(path, ledger))

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(ledgerFileMode), fileInfo.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(ledgerDirMode), dirInfo.Mode().Perm())

	loaded, err := loadReplacementLedger(path)
	require.NoError(t, err)
	require.Equal(t, ledger.Target, loaded.Target)
	require.Len(t, loaded.Records, len(ledger.Records))

	for index := range ledger.Records {
		require.True(t, ledger.Records[index].At.Equal(loaded.Records[index].At))
		require.Equal(t, ledger.Records[index].Phase, loaded.Records[index].Phase)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file may be left behind")
}

func TestReplacementLedgerRefusesToSaveAnInvalidLedger(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "ledger.json")
	ledger := ledgerThrough(t, phaseDrained)
	ledger.Records[1].Phase = phaseVerified

	require.ErrorIs(t, saveReplacementLedger(path, ledger), ErrReplacementLedgerInvalid)

	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementLedgerRefusesADirectoryOthersCanRead(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(dir, 0o750))
	// Mkdir applies the umask, so set the mode explicitly: a 0o077 umask would otherwise
	// leave the fixture private and fail it before the save runs.
	require.NoError(t, os.Chmod(dir, 0o750)) //nolint:gosec // The fixture must be group-readable.

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.NotZero(
		t,
		info.Mode().Perm()&^ledgerDirMode,
		"fixture directory must be group-readable",
	)

	err = saveReplacementLedger(filepath.Join(dir, "ledger.json"), ledgerThrough(t, phasePlanned))
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsAnOutOfOrderFile(t *testing.T) {
	t.Parallel()

	stored := ledgerThrough(t, phaseMemberRemoved)
	stored.Records[1], stored.Records[2] = stored.Records[2], stored.Records[1]

	path := writeRawLedger(t, stored, ledgerFileMode)

	_, err := loadReplacementLedger(path)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsAnUnsupportedVersion(t *testing.T) {
	t.Parallel()

	stored := ledgerThrough(t, phasePlanned)
	stored.Version = replacementLedgerVersion + 1

	_, err := loadReplacementLedger(writeRawLedger(t, stored, ledgerFileMode))
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsAFileOthersCanRead(t *testing.T) {
	t.Parallel()

	path := writeRawLedger(t, ledgerThrough(t, phasePlanned), 0o644)

	_, err := loadReplacementLedger(path)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	// Otherwise valid, so only the unknown-field check can reject it.
	path := filepath.Join(t.TempDir(), "ledger.json")
	data := `{"version":1,"target":{"serverId":1001,"nodeUid":"node-old","etcdMemberId":77},"records":[],"secret":"x"}`
	require.NoError(t, os.WriteFile(path, []byte(data), ledgerFileMode))
	require.NoError(t, os.Chmod(path, ledgerFileMode))

	_, err := loadReplacementLedger(path)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsTrailingData(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(ledgerThrough(t, phasePlanned))
	require.NoError(t, err)

	// A valid ledger followed by a second value: only the trailing-data check can reject it.
	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, append(valid, []byte("{}")...), ledgerFileMode))
	require.NoError(t, os.Chmod(path, ledgerFileMode))

	_, err = loadReplacementLedger(path)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func TestReplacementLedgerLoadRejectsAnOversizeFile(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(ledgerThrough(t, phasePlanned))
	require.NoError(t, err)

	// A valid ledger padded past the limit: only the size bound can reject it.
	path := filepath.Join(t.TempDir(), "ledger.json")
	data := string(valid) + strings.Repeat(" ", maxLedgerFileBytes)
	require.NoError(t, os.WriteFile(path, []byte(data), ledgerFileMode))

	_, err = loadReplacementLedger(path)
	require.ErrorIs(t, err, ErrReplacementLedgerInvalid)
}

func writeRawLedger(t *testing.T, stored replacementLedger, mode os.FileMode) string {
	t.Helper()

	data, err := json.Marshal(stored)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, data, mode))
	require.NoError(t, os.Chmod(path, mode))

	return path
}

func TestReplacementLedgerRejectsAFirstPhaseWithoutATime(t *testing.T) {
	t.Parallel()

	ledger, err := newReplacementLedger(ledgerTarget())
	require.NoError(t, err)

	entry := ledgerRecord(phasePlanned, 0)
	entry.At = time.Time{}

	require.ErrorIs(t, ledger.record(entry), ErrReplacementLedgerInvalid)
	require.Empty(t, ledger.Records)
}
