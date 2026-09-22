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
	"slices"
	"strconv"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"go.etcd.io/bbolt"
	"google.golang.org/grpc"
)

// ErrEtcdSnapshotUnverified is returned when a captured etcd snapshot cannot be proven to be a
// complete, readable copy of the datastore holding the membership the replacement observed.
var ErrEtcdSnapshotUnverified = errors.New("etcd snapshot is not verified")

const (
	// snapshotSectorSize is the unit etcd pads a snapshot database to before it appends the
	// SHA-256 digest of the database, so a digested snapshot is n*512+32 bytes long.
	snapshotSectorSize = 512
	// snapshotDirMode and snapshotFileMode keep the recovery artifact owner-only: a snapshot
	// holds every secret the cluster stores.
	snapshotDirMode  os.FileMode = 0o700
	snapshotFileMode os.FileMode = 0o600
	// snapshotOpenTimeout bounds how long verification waits for the database file lock.
	snapshotOpenTimeout = 5 * time.Second
)

// etcdMembersBucket is the etcd backend bucket that stores one key per member, named by the
// member ID in hexadecimal.
var etcdMembersBucket = []byte("members")

// etcdSnapshotSource streams an etcd snapshot from a surviving control-plane node. The Talos
// machinery client satisfies it.
type etcdSnapshotSource interface {
	EtcdSnapshot(
		ctx context.Context,
		req *machineapi.EtcdSnapshotRequest,
		callOptions ...grpc.CallOption,
	) (io.ReadCloser, error)
}

// etcdSnapshotArtifact describes a verified snapshot published as a recovery artifact.
type etcdSnapshotArtifact struct {
	Path    string
	SHA256  string
	Size    int64
	Members []uint64
}

// captureVerifiedSnapshot streams a snapshot into an owner-only file in dir and publishes it
// under name only after it verifies against membership, the voting membership the quorum
// proof observed. It never replaces an existing artifact, and a failed capture leaves nothing
// behind, so the path it returns always names a snapshot that can be restored.
func captureVerifiedSnapshot(
	ctx context.Context,
	source etcdSnapshotSource,
	dir, name string,
	membership []uint64,
) (etcdSnapshotArtifact, error) {
	if name == "" || filepath.Base(name) != name {
		return etcdSnapshotArtifact{}, fmt.Errorf("%w: artifact name %q must be a plain file name",
			ErrEtcdSnapshotUnverified, name)
	}

	err := os.MkdirAll(dir, snapshotDirMode)
	if err != nil {
		return etcdSnapshotArtifact{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	partial, err := streamSnapshot(ctx, source, dir, name)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	artifact, err := publishVerifiedSnapshot(partial, filepath.Join(dir, name), membership)

	removeErr := os.Remove(partial)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
		err = fmt.Errorf("remove partial snapshot: %w", removeErr)
	}

	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	return artifact, nil
}

// streamSnapshot copies the snapshot stream into a new owner-only partial file and returns
// its path. The partial file is removed when the copy fails.
func streamSnapshot(
	ctx context.Context,
	source etcdSnapshotSource,
	dir, name string,
) (string, error) {
	stream, err := source.EtcdSnapshot(ctx, &machineapi.EtcdSnapshotRequest{})
	if err != nil {
		return "", fmt.Errorf("stream etcd snapshot: %w", err)
	}

	defer func() { _ = stream.Close() }()

	file, err := os.CreateTemp(dir, name+".partial-*")
	if err != nil {
		return "", fmt.Errorf("create partial snapshot: %w", err)
	}

	path := file.Name()

	err = file.Chmod(snapshotFileMode)
	if err == nil {
		_, err = io.Copy(file, stream)
	}

	if err == nil {
		err = file.Sync()
	}

	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}

	if err != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("write etcd snapshot: %w", err)
	}

	return path, nil
}

// publishVerifiedSnapshot verifies the partial file and links it to final without replacing
// an existing file.
func publishVerifiedSnapshot(
	partial, final string,
	membership []uint64,
) (etcdSnapshotArtifact, error) {
	artifact, err := verifyEtcdSnapshot(partial, membership)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	err = os.Link(partial, final)
	if err != nil {
		return etcdSnapshotArtifact{}, fmt.Errorf("publish etcd snapshot: %w", err)
	}

	artifact.Path = final

	return artifact, nil
}

// verifyEtcdSnapshot proves that path holds a digested etcd snapshot whose digest matches,
// whose database is readable and internally consistent, and whose members bucket lists
// exactly membership. Anything it cannot prove is refused.
func verifyEtcdSnapshot(path string, membership []uint64) (etcdSnapshotArtifact, error) {
	if len(membership) == 0 {
		return etcdSnapshotArtifact{}, fmt.Errorf("%w: no expected membership",
			ErrEtcdSnapshotUnverified)
	}

	digest, size, err := checkSnapshotDigest(path)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	members, err := snapshotMembers(path)
	if err != nil {
		return etcdSnapshotArtifact{}, err
	}

	expected := slices.Clone(membership)
	slices.Sort(expected)

	if !slices.Equal(members, expected) {
		return etcdSnapshotArtifact{}, fmt.Errorf(
			"%w: snapshot members %v differ from the observed membership %v",
			ErrEtcdSnapshotUnverified, members, expected,
		)
	}

	return etcdSnapshotArtifact{SHA256: digest, Size: size, Members: members}, nil
}

// checkSnapshotDigest compares the SHA-256 etcd appends to a snapshot with the digest of the
// database before it, returning the hex digest and the database size.
func checkSnapshotDigest(path string) (string, int64, error) {
	file, err := os.Open(path) //nolint:gosec // path is the partial file this package created
	if err != nil {
		return "", 0, fmt.Errorf("open etcd snapshot: %w", err)
	}

	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat etcd snapshot: %w", err)
	}

	total := info.Size()
	if total <= sha256.Size || total%snapshotSectorSize != sha256.Size {
		return "", 0, fmt.Errorf("%w: %d bytes is not a digested snapshot",
			ErrEtcdSnapshotUnverified, total)
	}

	size := total - sha256.Size
	hash := sha256.New()

	_, err = io.CopyN(hash, file, size)
	if err != nil {
		return "", 0, fmt.Errorf("hash etcd snapshot: %w", err)
	}

	stored := make([]byte, sha256.Size)

	_, err = io.ReadFull(file, stored)
	if err != nil {
		return "", 0, fmt.Errorf("read etcd snapshot digest: %w", err)
	}

	computed := hash.Sum(nil)
	if !bytes.Equal(stored, computed) {
		return "", 0, fmt.Errorf("%w: digest %x does not match the database %x",
			ErrEtcdSnapshotUnverified, stored, computed)
	}

	return hex.EncodeToString(computed), size, nil
}

// snapshotMembers opens the snapshot database read-only, checks its consistency, and returns
// the sorted member IDs in its members bucket.
func snapshotMembers(path string) ([]uint64, error) {
	database, err := bbolt.Open(path, snapshotFileMode, &bbolt.Options{
		ReadOnly: true,
		Timeout:  snapshotOpenTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: open database: %w", ErrEtcdSnapshotUnverified, err)
	}

	defer func() { _ = database.Close() }()

	var members []uint64

	err = database.View(func(tx *bbolt.Tx) error {
		// Drain every result so the checker goroutine never blocks on an unread error.
		var inconsistency error
		for checkErr := range tx.Check() {
			inconsistency = errors.Join(inconsistency, checkErr)
		}

		if inconsistency != nil {
			return fmt.Errorf("%w: database is inconsistent: %w",
				ErrEtcdSnapshotUnverified, inconsistency)
		}

		ids, idsErr := memberIDs(tx.Bucket(etcdMembersBucket))
		members = ids

		return idsErr
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

func memberIDs(bucket *bbolt.Bucket) ([]uint64, error) {
	if bucket == nil {
		return nil, fmt.Errorf("%w: database has no members bucket", ErrEtcdSnapshotUnverified)
	}

	var ids []uint64

	err := bucket.ForEach(func(key, _ []byte) error {
		id, err := strconv.ParseUint(string(key), 16, 64)
		if err != nil || id == 0 {
			return fmt.Errorf("%w: member key %q is not a member ID",
				ErrEtcdSnapshotUnverified, key)
		}

		ids = append(ids, id)

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(ids)

	return ids, nil
}
