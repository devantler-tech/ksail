package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalAWSOptions is the default environment-variable-name mapping.
//
//nolint:gosec // G101: these are environment-variable names, never credential values.
func canonicalAWSOptions() v1alpha1.OptionsAWS {
	return v1alpha1.OptionsAWS{
		ProfileEnvVar:         "AWS_PROFILE",
		RegionEnvVar:          "AWS_REGION",
		AccessKeyIDEnvVar:     "AWS_ACCESS_KEY_ID",
		SecretAccessKeyEnvVar: "AWS_SECRET_ACCESS_KEY",
		SessionTokenEnvVar:    "AWS_SESSION_TOKEN",
	}
}

func TestSaveLoadEKSOwnershipState(t *testing.T) {
	t.Parallel()

	want := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: "ownership-round-trip",
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/ownership-round-trip",
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		AWSOptions: v1alpha1.OptionsAWS{
			ProfileEnvVar:         "KSAIL_PROFILE",
			RegionEnvVar:          "KSAIL_REGION",
			AccessKeyIDEnvVar:     "KSAIL_ACCESS",
			SecretAccessKeyEnvVar: "KSAIL_SECRET",
			SessionTokenEnvVar:    "KSAIL_SESSION",
		},
	}

	require.NoError(t, state.SaveEKSOwnershipState(want.ClusterName, want.Region, want))

	got, err := state.LoadEKSOwnershipState(want.ClusterName, want.Region)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEKSOwnershipStateIsScopedPerRegion(t *testing.T) {
	t.Parallel()

	clusterName := "ownership-region-scope"
	north := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		AWSOptions:  canonicalAWSOptions(),
	}
	east := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "us-east-1",
		AccountID:   "210987654321",
		ClusterARN:  "arn:aws:eks:us-east-1:210987654321:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 1, 0, 0, time.UTC),
		AWSOptions:  canonicalAWSOptions(),
	}

	require.NoError(t, state.SaveEKSOwnershipState(clusterName, north.Region, north))
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, east.Region, east))

	gotNorth, err := state.LoadEKSOwnershipState(clusterName, north.Region)
	require.NoError(t, err)
	assert.Equal(t, north, gotNorth)

	gotEast, err := state.LoadEKSOwnershipState(clusterName, east.Region)
	require.NoError(t, err)
	assert.Equal(t, east, gotEast)
}

func TestListEKSOwnershipStatesReturnsSortedValidatedRegions(t *testing.T) {
	t.Parallel()

	clusterName := "ownership-list-regions"
	for _, region := range []string{"us-west-2", "eu-north-1"} {
		ownership := &state.EKSOwnershipState{
			Version:     state.EKSOwnershipStateVersion,
			ClusterName: clusterName,
			Region:      region,
			AccountID:   "123456789012",
			ClusterARN:  "arn:aws:eks:" + region + ":123456789012:cluster/" + clusterName,
			CreatedAt:   time.Now().UTC(),
			AWSOptions:  canonicalAWSOptions(),
		}
		require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, ownership))
	}

	ownerships, err := state.ListEKSOwnershipStates(clusterName)
	require.NoError(t, err)
	require.Len(t, ownerships, 2)
	assert.Equal(t, "eu-north-1", ownerships[0].Region)
	assert.Equal(t, "us-west-2", ownerships[1].Region)
}

// TestListEKSOwnershipStatesSkipsUnusableRecords proves one legacy record in an unrelated region
// cannot strand a cluster whose target region is recorded correctly. Before ListEKSOwnershipStates
// skipped unusable records, the legacy file below aborted the whole listing.
func TestListEKSOwnershipStatesSkipsUnusableRecords(t *testing.T) {
	t.Parallel()

	const clusterName = "ownership-list-skips-legacy"

	valid := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		AWSOptions:  canonicalAWSOptions(),
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, valid.Region, valid))

	writeLegacyOwnershipRecord(t, clusterName, "us-west-2")

	ownerships, err := state.ListEKSOwnershipStates(clusterName)
	require.NoError(t, err)
	require.Len(t, ownerships, 1)
	assert.Equal(t, "eu-north-1", ownerships[0].Region)
}

// TestListEKSOwnershipStatesReportsAbsenceWhenNoRecordIsUsable proves skipping never degrades into
// silently succeeding with nothing: an all-legacy directory is indistinguishable from no record,
// so the caller's absence path applies and the authoritative rebind error is still produced later.
func TestListEKSOwnershipStatesReportsAbsenceWhenNoRecordIsUsable(t *testing.T) {
	t.Parallel()

	const clusterName = "ownership-list-all-legacy"

	writeLegacyOwnershipRecord(t, clusterName, "eu-north-1")

	_, err := state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
}

// writeLegacyOwnershipRecord writes an ownership record in the pre-awsOptions schema.
func writeLegacyOwnershipRecord(t *testing.T, clusterName, region string) {
	t.Helper()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	legacy := map[string]any{
		"version":     state.EKSOwnershipStateVersion,
		"clusterName": clusterName,
		"region":      region,
		"accountId":   "123456789012",
		"clusterArn":  "arn:aws:eks:" + region + ":123456789012:cluster/" + clusterName,
		"createdAt":   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "eks-ownership-"+region+".json"),
		data,
		0o600,
	))
}

func TestEKSOwnershipStateContainsNoCredentialsAndUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "ownership-private-state"
		region      = "eu-north-1"
	)

	snapshot := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		AWSOptions: v1alpha1.OptionsAWS{
			ProfileEnvVar:         "KSAIL_PROFILE",
			RegionEnvVar:          "KSAIL_REGION",
			AccessKeyIDEnvVar:     "KSAIL_ACCESS",
			SecretAccessKeyEnvVar: "KSAIL_SECRET",
			SessionTokenEnvVar:    "KSAIL_SESSION",
		},
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, snapshot))

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	path := filepath.Join(dir, "eks-ownership-"+region+".json")

	//nolint:gosec // Every path component is fixed by this isolated test fixture.
	contents, err := os.ReadFile(path)
	require.NoError(t, err)

	var persisted map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(contents, &persisted))

	for _, credentialField := range []string{
		"accessKeyId", "secretAccessKey", "sessionToken", "profile",
	} {
		assert.NotContains(t, persisted, credentialField)
	}

	assert.Contains(t, string(persisted["awsOptions"]), `"accessKeyIdEnvVar": "KSAIL_ACCESS"`)
	assert.Contains(t, string(persisted["awsOptions"]), `"secretAccessKeyEnvVar": "KSAIL_SECRET"`)

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestLoadEKSOwnershipStateMissingRequiresMigration(t *testing.T) {
	t.Parallel()

	_, err := state.LoadEKSOwnershipState("legacy-without-identity", "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
}

func TestLoadEKSOwnershipStateRejectsLegacyRecordWithoutAWSOptions(t *testing.T) {
	t.Parallel()

	const (
		clusterName = "legacy-without-aws-options"
		region      = "eu-north-1"
	)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	legacy := map[string]any{
		"version":     state.EKSOwnershipStateVersion,
		"clusterName": clusterName,
		"region":      region,
		"accountId":   "123456789012",
		"clusterArn":  "arn:aws:eks:" + region + ":123456789012:cluster/" + clusterName,
		"createdAt":   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "eks-ownership-"+region+".json"),
		data,
		0o600,
	))

	_, err = state.LoadEKSOwnershipState(clusterName, region)
	require.ErrorIs(t, err, state.ErrInvalidEKSOwnershipState)
}

func TestSaveEKSOwnershipStateRejectsMalformedIdentity(t *testing.T) {
	t.Parallel()

	testCases := map[string]*state.EKSOwnershipState{
		"wrong version": {
			Version: 2, ClusterName: "demo", Region: "eu-north-1",
			AccountID:  "123456789012",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
			CreatedAt:  time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing account": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
			CreatedAt:  time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing arn": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			AccountID: "123456789012",
			CreatedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		},
		"missing creation time": {
			Version: state.EKSOwnershipStateVersion, ClusterName: "demo", Region: "eu-north-1",
			AccountID:  "123456789012",
			ClusterARN: "arn:aws:eks:eu-north-1:123456789012:cluster/demo",
		},
	}

	for name, snapshot := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := state.SaveEKSOwnershipState("demo", "eu-north-1", snapshot)
			require.ErrorIs(t, err, state.ErrInvalidEKSOwnershipState)
		})
	}
}

func TestLoadEKSOwnershipStateRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	clusterName := "ownership-invalid-json"
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "eks-ownership-eu-north-1.json"),
		[]byte("{"),
		0o600,
	))

	_, err = state.LoadEKSOwnershipState(clusterName, "eu-north-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
	assert.ErrorContains(t, err, "unmarshal EKS ownership state")
}

// writeRawOwnershipRecord writes arbitrary bytes where an ownership record for region belongs.
func writeRawOwnershipRecord(t *testing.T, clusterName, region string, data []byte) string {
	t.Helper()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	path := filepath.Join(dir, "eks-ownership-"+region+".json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

// TestListEKSOwnershipStatesRefusesATruncatedRecordAsAbsence proves a record that exists but does not
// parse is reported as unreadable, not as absent. Absence licenses callers to bind from a rendered
// config alone, so a truncated record must not reach that path.
func TestListEKSOwnershipStatesRefusesATruncatedRecordAsAbsence(t *testing.T) {
	t.Parallel()

	const clusterName = "ownership-list-truncated"

	path := writeRawOwnershipRecord(t, clusterName, "eu-north-1", []byte(`{"version":1,"clusterNa`))

	_, err := state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateUnreadable)
	require.NotErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
	assert.ErrorContains(t, err, path)
}

// TestListEKSOwnershipStatesRefusesAnUnreadableRecordAsAbsence covers a record the process cannot
// open at all (mode 000).
func TestListEKSOwnershipStatesRefusesAnUnreadableRecordAsAbsence(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can read a mode-000 file, so this cannot produce a read failure")
	}

	const clusterName = "ownership-list-mode-000"

	path := writeRawOwnershipRecord(t, clusterName, "eu-north-1", []byte("{}"))
	require.NoError(t, os.Chmod(path, 0o000))

	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateUnreadable)
	require.NotErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
	assert.ErrorContains(t, err, path)
}

// TestListEKSOwnershipStatesKeepsAUsableRecordBesideAnUnreadableOne pins that corruption only changes
// the no-usable-record case. A readable record in its own region still wins, exactly as a legacy
// record beside it is skipped, so one damaged file in an unrelated region cannot strand a cluster.
func TestListEKSOwnershipStatesKeepsAUsableRecordBesideAnUnreadableOne(t *testing.T) {
	t.Parallel()

	const clusterName = "ownership-list-truncated-beside-valid"

	valid := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName,
		CreatedAt:   time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		AWSOptions:  canonicalAWSOptions(),
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, valid.Region, valid))

	writeRawOwnershipRecord(t, clusterName, "us-west-2", []byte("{"))

	ownerships, err := state.ListEKSOwnershipStates(clusterName)
	require.NoError(t, err)
	require.Len(t, ownerships, 1)
	assert.Equal(t, "eu-north-1", ownerships[0].Region)
}

// TestListEKSOwnershipStatesRefusesAnUnreadableStateDirectory covers the directory itself. A listing
// that cannot read the directory must not look empty, because empty means absence and absence lets a
// rendered config bind alone.
func TestListEKSOwnershipStatesRefusesAnUnreadableStateDirectory(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can read a mode-000 directory, so this cannot produce a read failure")
	}

	const clusterName = "ownership-list-unreadable-dir"

	path := writeRawOwnershipRecord(t, clusterName, "eu-north-1", []byte("{}"))
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o000))

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := state.ListEKSOwnershipStates(clusterName)
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateUnreadable)
	require.NotErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
}

// TestListEKSOwnershipStatesReportsAbsenceForAMissingStateDirectory is the control: a cluster with no
// state directory at all has no records, which is genuine absence.
func TestListEKSOwnershipStatesReportsAbsenceForAMissingStateDirectory(t *testing.T) {
	t.Parallel()

	_, err := state.ListEKSOwnershipStates("ownership-list-no-state-dir")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
}
