package talosprovisioner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
	"google.golang.org/grpc"
)

const (
	snapshotArtifactName = "etcd-before-replacement.db"
	// fixturePageSize pins the fixture layout so a test can corrupt a known page.
	fixturePageSize = 4096
	// bucketLeafPage holds a bucket leaf in the fixture layout, and pageFlagsOffset is where a
	// bbolt page header stores its type, so flipping it corrupts a page the database uses.
	bucketLeafPage  = 4
	pageFlagsOffset = 8
)

var (
	errSnapshotStreamUnavailable = errors.New("snapshot stream unavailable")
	errSnapshotStreamBroken      = errors.New("snapshot stream broken")
)

// fakeSnapshotSource serves a fixed snapshot stream, or a fixed error, in place of a Talos node.
type fakeSnapshotSource struct {
	reader io.Reader
	err    error
}

// EtcdSnapshot returns the configured error, or the configured stream wrapped as a closer.
func (s fakeSnapshotSource) EtcdSnapshot(
	_ context.Context,
	_ *machineapi.EtcdSnapshotRequest,
	_ ...grpc.CallOption,
) (io.ReadCloser, error) {
	if s.err != nil {
		return nil, s.err
	}

	return io.NopCloser(s.reader), nil
}

// failingReader yields its prefix and then fails, simulating a stream that breaks mid-transfer.
type failingReader struct{ prefix []byte }

// Read copies the remaining prefix, then returns errSnapshotStreamBroken once it is exhausted.
func (r *failingReader) Read(buffer []byte) (int, error) {
	if len(r.prefix) == 0 {
		return 0, errSnapshotStreamBroken
	}

	n := copy(buffer, r.prefix)
	r.prefix = r.prefix[n:]

	return n, nil
}

// snapshotDatabase builds a real bbolt database holding the given member IDs, or no members
// bucket at all when withMembers is false, and returns its bytes.
func snapshotDatabase(t *testing.T, withMembers bool, members ...uint64) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "member.db")

	database, err := bbolt.Open(path, snapshotFileMode, &bbolt.Options{PageSize: fixturePageSize})
	require.NoError(t, err)

	err = database.Update(func(transaction *bbolt.Tx) error {
		_, bucketErr := transaction.CreateBucket([]byte("key"))
		if bucketErr != nil {
			return fmt.Errorf("create key bucket: %w", bucketErr)
		}

		if !withMembers {
			return nil
		}

		bucket, bucketErr := transaction.CreateBucket([]byte(etcdMembersBucket))
		if bucketErr != nil {
			return fmt.Errorf("create members bucket: %w", bucketErr)
		}

		for _, memberID := range members {
			putErr := bucket.Put([]byte(strconv.FormatUint(memberID, 16)), []byte(`{}`))
			if putErr != nil {
				return fmt.Errorf("put member: %w", putErr)
			}
		}

		return nil
	})
	require.NoError(t, err)
	require.NoError(t, database.Close())

	data, err := os.ReadFile(path) //nolint:gosec // test-owned temporary path
	require.NoError(t, err)
	require.Zero(t, len(data)%snapshotSectorSize, "fixture database must be sector-aligned")

	return data
}

// digested appends the SHA-256 trailer etcd writes after a snapshot database.
func digested(database []byte) []byte {
	digest := sha256.Sum256(database)

	return append(bytes.Clone(database), digest[:]...)
}

// threeMemberSnapshot returns a digested snapshot holding the three members the safety check observed.
func threeMemberSnapshot(t *testing.T) []byte {
	t.Helper()

	return digested(snapshotDatabase(t, true, quorumTarget, quorumSurvivor, quorumThird))
}

// observedMembership returns the membership the safety check saw, deliberately unsorted.
func observedMembership() []uint64 {
	return []uint64{quorumThird, quorumTarget, quorumSurvivor}
}

// captureFrom captures from source into a fresh recovery directory and returns that directory too.
func captureFrom(t *testing.T, source etcdSnapshotSource) (string, etcdSnapshotArtifact, error) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "recovery")
	artifact, err := captureVerifiedSnapshot(
		context.Background(), source, dir, snapshotArtifactName, observedMembership(),
	)

	return dir, artifact, err
}

// requireNoArtifacts asserts that dir is absent or empty after a failed capture.
func requireNoArtifacts(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}

	require.NoError(t, err)
	require.Empty(t, entries, "a failed capture must leave nothing behind")
}

func TestCaptureVerifiedSnapshotPublishesAPrivateVerifiedArtifact(t *testing.T) {
	t.Parallel()

	stream := threeMemberSnapshot(t)
	database := stream[:len(stream)-sha256.Size]
	digest := sha256.Sum256(database)

	dir, artifact, err := captureFrom(t, fakeSnapshotSource{reader: bytes.NewReader(stream)})
	require.NoError(t, err)

	require.Equal(t, filepath.Join(dir, snapshotArtifactName), artifact.Path)
	require.Equal(t, hex.EncodeToString(digest[:]), artifact.SHA256)
	require.Equal(t, int64(len(database)), artifact.Size)
	require.Equal(t, []uint64{quorumTarget, quorumSurvivor, quorumThird}, artifact.Members)

	written, err := os.ReadFile(artifact.Path)
	require.NoError(t, err)
	require.Equal(t, stream, written)

	fileInfo, err := os.Stat(artifact.Path)
	require.NoError(t, err)
	require.Equal(t, snapshotFileMode, fileInfo.Mode().Perm())

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, snapshotDirMode, dirInfo.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the published artifact remains")
}

func TestCaptureVerifiedSnapshotRejectsUnverifiableStreams(t *testing.T) {
	t.Parallel()

	threeMembers := snapshotDatabase(t, true, quorumTarget, quorumSurvivor, quorumThird)

	// Corrupting a page header and re-digesting passes the digest, so only the database
	// checks can refuse it.
	corrupted := bytes.Clone(threeMembers)
	corrupted[bucketLeafPage*fixturePageSize+pageFlagsOffset] ^= 0xff
	tampered := digested(corrupted)

	// A readable database with the right membership and a wrong digest isolates the digest
	// check: only the digest comparison can refuse it.
	wrongDigest := append(bytes.Clone(threeMembers), make([]byte, sha256.Size)...)

	notADatabase := digested(bytes.Repeat([]byte{0x5a}, 4*snapshotSectorSize))

	cases := map[string][]byte{
		"missing digest":    threeMembers,
		"wrong digest":      wrongDigest,
		"tampered database": tampered,
		"empty stream":      nil,
		"not a database":    notADatabase,
		"no members bucket": digested(snapshotDatabase(t, false)),
		"member missing":    digested(snapshotDatabase(t, true, quorumTarget, quorumSurvivor)),
		"unexpected member": digested(
			snapshotDatabase(t, true, quorumTarget, quorumSurvivor, quorumThird, 7004),
		),
		"target already absent": digested(
			snapshotDatabase(t, true, quorumSurvivor, quorumThird, 7004),
		),
	}

	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir, _, err := captureFrom(t, fakeSnapshotSource{reader: bytes.NewReader(stream)})
			require.ErrorIs(t, err, ErrEtcdSnapshotUnverified)
			requireNoArtifacts(t, dir)
		})
	}
}

func TestCaptureVerifiedSnapshotLeavesNothingWhenTheStreamFails(t *testing.T) {
	t.Parallel()

	dir, _, err := captureFrom(t, fakeSnapshotSource{err: errSnapshotStreamUnavailable})
	require.ErrorIs(t, err, errSnapshotStreamUnavailable)
	requireNoArtifacts(t, dir)

	partial := threeMemberSnapshot(t)[:snapshotSectorSize]
	dir, _, err = captureFrom(t, fakeSnapshotSource{reader: &failingReader{prefix: partial}})
	require.ErrorIs(t, err, errSnapshotStreamBroken)
	requireNoArtifacts(t, dir)
}

func TestCaptureVerifiedSnapshotNeverReplacesAnExistingArtifact(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "recovery")
	require.NoError(t, os.MkdirAll(dir, snapshotDirMode))

	existing := filepath.Join(dir, snapshotArtifactName)
	require.NoError(t, os.WriteFile(existing, []byte("earlier snapshot"), snapshotFileMode))

	_, err := captureVerifiedSnapshot(
		context.Background(),
		fakeSnapshotSource{reader: bytes.NewReader(threeMemberSnapshot(t))},
		dir, snapshotArtifactName, observedMembership(),
	)
	require.ErrorIs(t, err, os.ErrExist)

	kept, err := os.ReadFile(existing) //nolint:gosec // test-owned temporary path
	require.NoError(t, err)
	require.Equal(t, "earlier snapshot", string(kept))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the partial file is removed")
}

func TestCaptureVerifiedSnapshotRejectsUnsafeInputsBeforeStreaming(t *testing.T) {
	t.Parallel()

	source := fakeSnapshotSource{err: errSnapshotStreamUnavailable}

	for _, name := range []string{"", "../escape.db", "nested/escape.db"} {
		_, err := captureVerifiedSnapshot(
			context.Background(), source, t.TempDir(), name, observedMembership(),
		)
		require.ErrorIs(t, err, ErrEtcdSnapshotUnverified, name)
	}

	_, err := captureVerifiedSnapshot(
		context.Background(),
		fakeSnapshotSource{reader: bytes.NewReader(threeMemberSnapshot(t))},
		t.TempDir(), snapshotArtifactName, nil,
	)
	require.ErrorIs(t, err, ErrEtcdSnapshotUnverified)
}
