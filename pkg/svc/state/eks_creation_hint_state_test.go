package state_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	hintAccount      = "123456789012"
	hintOtherAccount = "210987654321"
)

func validEKSCreationHint(clusterName, region string) *state.EKSCreationHint {
	return &state.EKSCreationHint{
		Version:     state.EKSCreationHintVersion,
		ClusterName: clusterName,
		Region:      region,
		Accounts: []state.EKSCreationAttempts{
			{AccountID: hintAccount, CreationIDs: []string{"attempt-8324"}},
		},
	}
}

func TestSaveLoadAndDeleteEKSCreationHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-hint-round-trip"

	want := validEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, state.SaveEKSCreationHint(clusterName, "eu-north-1", want))

	got, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []string{"attempt-8324"}, got.CreationIDsFor(hintAccount))
	assert.Empty(t, got.CreationIDsFor(hintOtherAccount))

	statePath := filepath.Join(
		home, ".ksail", "clusters", clusterName, "eks-creation-hint-eu-north-1.json",
	)
	info, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, err = state.LoadEKSCreationHint(clusterName, "eu-west-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound, "a hint is scoped to its region")

	_, err = state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound, "a hint is never ownership")

	require.NoError(t, state.DeleteEKSCreationHint(clusterName, "eu-north-1"))
	require.NoError(t, state.DeleteEKSCreationHint(clusterName, "eu-north-1"), "absence is fine")

	_, err = state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound)
}

func TestEKSCreationHintRejectsIncompleteOrMismatchedFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		mutate func(*state.EKSCreationHint)
	}{
		{name: "nil hint"},
		{name: "unknown version", mutate: func(h *state.EKSCreationHint) { h.Version = 2 }},
		{name: "other cluster", mutate: func(h *state.EKSCreationHint) { h.ClusterName = "other" }},
		{name: "other region", mutate: func(h *state.EKSCreationHint) { h.Region = "eu-west-1" }},
		{name: "no accounts", mutate: func(h *state.EKSCreationHint) { h.Accounts = nil }},
		{
			name:   "malformed account",
			mutate: func(h *state.EKSCreationHint) { h.Accounts[0].AccountID = "12" },
		},
		{
			name:   "blank creation id",
			mutate: func(h *state.EKSCreationHint) { h.Accounts[0].CreationIDs = []string{" "} },
		},
		{
			name:   "no creation ids",
			mutate: func(h *state.EKSCreationHint) { h.Accounts[0].CreationIDs = nil },
		},
		{
			name: "duplicate account",
			mutate: func(h *state.EKSCreationHint) {
				h.Accounts = append(h.Accounts, h.Accounts[0])
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var hint *state.EKSCreationHint
			if testCase.mutate != nil {
				hint = validEKSCreationHint("eks-hint", "eu-north-1")
				testCase.mutate(hint)
			}

			err := state.SaveEKSCreationHint("eks-hint", "eu-north-1", hint)
			require.ErrorIs(t, err, state.ErrInvalidEKSCreationHint)
		})
	}
}

func TestLoadEKSCreationHintRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".ksail", "clusters", "invalid-hint")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "eks-creation-hint-eu-north-1.json"),
		[]byte("{"),
		0o600,
	))

	_, err := state.LoadEKSCreationHint("invalid-hint", "eu-north-1")
	require.ErrorIs(t, err, state.ErrInvalidEKSCreationHint)
}

// TestRecordEKSCreationAttemptKeepsEveryUnresolvedAttempt pins the review findings on ksail#8335: a
// retry that also fails before AWS reports the cluster, many retries, or an attempt made in another
// account must never drop an earlier attempt whose cluster can still appear.
func TestRecordEKSCreationAttemptKeepsEveryUnresolvedAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		clusterName = "eks-hint-retries"
		attempts    = 40
	)

	want := make([]string, 0, attempts)

	for attempt := range attempts {
		id := fmt.Sprintf("attempt-%d", attempt)
		want = append(want, id)

		require.NoError(
			t,
			state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, id),
		)

		if attempt == attempts/2 {
			require.NoError(t, state.RecordEKSCreationAttempt(
				clusterName, "eu-north-1", hintOtherAccount, "other-account-attempt",
			))
		}
	}

	require.NoError(
		t,
		state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "attempt-0"),
	)

	want = append(want[1:], "attempt-0")

	hint, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, want, hint.CreationIDsFor(hintAccount), "every attempt stays, newest last")
	assert.Equal(
		t,
		[]string{"other-account-attempt"},
		hint.CreationIDsFor(hintOtherAccount),
		"another account's attempt stays beside them",
	)
}

func TestForgetEKSCreationAttemptsRemovesOnlyThatAccount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "eks-hint-forget"

	require.NoError(t, state.ForgetEKSCreationAttempts(clusterName, "eu-north-1", hintAccount),
		"a missing hint is fine")

	require.NoError(t, state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "a"))
	require.NoError(
		t,
		state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintOtherAccount, "b"),
	)

	require.NoError(t, state.ForgetEKSCreationAttempts(clusterName, "eu-north-1", hintAccount))

	hint, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Empty(t, hint.CreationIDsFor(hintAccount))
	assert.Equal(t, []string{"b"}, hint.CreationIDsFor(hintOtherAccount))

	require.NoError(t, state.ForgetEKSCreationAttempts(clusterName, "eu-north-1", hintOtherAccount))

	_, err = state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSCreationHintNotFound, "the last account removes the file")
}

// TestLoadEKSCreationHintRefusesFileOutsideItsDirectory keeps the hint read confined to the cluster's
// state directory: a hint file that is a symbolic link to elsewhere is refused, not followed.
func TestLoadEKSCreationHintRefusesFileOutsideItsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-hint-link"

	outside := filepath.Join(t.TempDir(), "hint.json")
	data := `{"version":1,"clusterName":"eks-hint-link","region":"eu-north-1",` +
		`"accounts":[{"accountId":"123456789012","creationIds":["attempt-8324"]}]}`
	require.NoError(t, os.WriteFile(outside, []byte(data), 0o600))

	stateDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	require.NoError(
		t,
		os.Symlink(outside, filepath.Join(stateDir, "eks-creation-hint-eu-north-1.json")),
	)

	_, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, state.ErrEKSCreationHintNotFound)
}

func TestHasEKSCreationHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "eks-hint-presence"

	assert.False(t, state.HasEKSCreationHint(clusterName))

	require.NoError(t, state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "a"))
	assert.True(t, state.HasEKSCreationHint(clusterName))

	require.NoError(t, state.DeleteEKSCreationHint(clusterName, "eu-north-1"))
	assert.False(t, state.HasEKSCreationHint(clusterName))
}

// TestRecordEKSCreationAttemptKeepsHintItCannotRead pins a review finding on ksail#8335: a hint that
// exists but cannot be read is never replaced, so its attempts are not lost. Only a missing hint or one
// whose content is not a valid hint is started afresh.
func TestRecordEKSCreationAttemptKeepsHintItCannotRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-hint-unreadable"

	outside := filepath.Join(t.TempDir(), "hint.json")
	require.NoError(t, os.WriteFile(outside, []byte("kept"), 0o600))

	stateDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(stateDir, 0o700))

	hintPath := filepath.Join(stateDir, "eks-creation-hint-eu-north-1.json")
	require.NoError(t, os.Symlink(outside, hintPath))

	err := state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "a")
	require.Error(t, err)

	target, err := os.Readlink(hintPath)
	require.NoError(t, err, "the unreadable hint must not be replaced")
	assert.Equal(t, outside, target)

	require.NoError(t, os.Remove(hintPath))
	require.NoError(t, os.WriteFile(hintPath, []byte("{"), 0o600))

	require.NoError(t, state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "a"),
		"a hint whose content is not a valid hint is started afresh")

	hint, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, hint.CreationIDsFor(hintAccount))
}

// TestEKSCreationHintRefusesRelocatedClusterDirectory pins a review finding on ksail#8335: when the
// cluster's state directory resolves outside the clusters root, the hint is neither read, written,
// nor removed there.
func TestEKSCreationHintRefusesRelocatedClusterDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "eks-hint-relocated"

	outsideDir := t.TempDir()
	outsideHint := filepath.Join(outsideDir, "eks-creation-hint-eu-north-1.json")
	data := `{"version":1,"clusterName":"eks-hint-relocated","region":"eu-north-1",` +
		`"accounts":[{"accountId":"123456789012","creationIds":["outside"]}]}`
	require.NoError(t, os.WriteFile(outsideHint, []byte(data), 0o600))

	clustersDir := filepath.Join(home, ".ksail", "clusters")
	require.NoError(t, os.MkdirAll(clustersDir, 0o700))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(clustersDir, clusterName)))

	_, err := state.LoadEKSCreationHint(clusterName, "eu-north-1")
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)

	require.ErrorIs(
		t,
		state.RecordEKSCreationAttempt(clusterName, "eu-north-1", hintAccount, "a"),
		fsutil.ErrPathOutsideBase,
	)
	require.ErrorIs(
		t,
		state.DeleteEKSCreationHint(clusterName, "eu-north-1"),
		fsutil.ErrPathOutsideBase,
	)
	assert.False(t, state.HasEKSCreationHint(clusterName))

	got, err := os.ReadFile(outsideHint) //nolint:gosec // test-private path
	require.NoError(t, err, "the file outside the state root must survive")
	assert.JSONEq(t, data, string(got))
}
