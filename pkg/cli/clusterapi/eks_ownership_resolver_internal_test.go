package clusterapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateHome points state reads and writes at a throwaway home so this package never touches the
// developer's real ~/.ksail/clusters. t.Setenv forbids t.Parallel, which is why these tests are
// serial.
func isolateHome(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	return dir
}

// recordedAliases is a complete variable-name mapping. Completeness is not decoration: an ownership
// record whose mapping is partial fails validation on load, so a record that reaches the resolver
// always names every key.
func recordedAliases() v1alpha1.OptionsAWS {
	return v1alpha1.OptionsAWS{
		ProfileEnvVar:         "RECORDED_PROFILE",
		RegionEnvVar:          "RECORDED_REGION",
		AccessKeyIDEnvVar:     "RECORDED_ACCESS",
		SecretAccessKeyEnvVar: "RECORDED_SECRET",
		SessionTokenEnvVar:    "RECORDED_SESSION",
	}
}

func saveOwnership(t *testing.T, name, region string, options v1alpha1.OptionsAWS) {
	t.Helper()

	require.NoError(t, state.SaveEKSOwnershipState(name, region, &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: name,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:" + region + ":123456789012:cluster/" + name,
		CreatedAt:   time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		AWSOptions:  options,
	}))
}

// writeLegacyOwnership plants a record with no awsOptions mapping. SaveEKSOwnershipState cannot
// produce one — it validates completeness — so the only faithful way to exercise a record written
// before the mapping schema is to write the file the way that older KSail did.
func writeLegacyOwnership(t *testing.T, home, name, region string) {
	t.Helper()

	dir := filepath.Join(home, ".ksail", "clusters", name)
	require.NoError(t, os.MkdirAll(dir, 0o750))

	body := `{"version":1,"clusterName":"` + name + `","region":"` + region + `",` +
		`"accountId":"123456789012",` +
		`"clusterArn":"arn:aws:eks:` + region + `:123456789012:cluster/` + name + `",` +
		`"createdAt":"2026-08-02T12:00:00Z"}`

	require.NoError(t,
		os.WriteFile(filepath.Join(dir, "eks-ownership-"+region+".json"), []byte(body), 0o600))
}

// TestLifecycleCredentialsResolveThroughTheOwnershipRecord is the defect this guards against.
//
// Capture persists the variable names the create actually resolved through. Delete, Start, and Stop
// then built their identity client before eksidentity.NewVerifier ever loaded that record, so the
// record could not influence credential resolution at all: a cluster created with custom
// spec.provider.aws names failed with unavailable credentials, or verified against whatever identity
// the canonical variables happened to name.

func TestLifecycleCredentialsResolveThroughTheOwnershipRecord(t *testing.T) {
	isolateHome(t)
	t.Setenv("RECORDED_ACCESS", "made-this-cluster")

	const (
		name   = "recorded-alias-cluster"
		region = "eu-north-1"
	)

	saveOwnership(t, name, region, recordedAliases())

	service := NewService()

	resolver := service.eksOwnershipResolver(name, region)

	assert.Equal(t, "RECORDED_ACCESS", resolver.EnvVar(credentials.AWSAccessKeyID),
		"the lifecycle path did not resolve through the name the record captured")
	assert.Equal(t, "made-this-cluster", resolver.Value(credentials.AWSAccessKeyID),
		"the credentials that created the cluster did not resolve for its lifecycle mutation")
}

// TestTheRecordIsScopedToItsOwnRegion pins that the record consulted is the one for the region the
// mutation is bound to. Records are region-scoped precisely because a same-named cluster can exist
// elsewhere, and resolving a sibling region's aliases would authorize against the wrong target.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestTheRecordIsScopedToItsOwnRegion(t *testing.T) {
	isolateHome(t)

	const name = "two-region-cluster"

	saveOwnership(t, name, "eu-north-1", recordedAliases())

	injected := credentials.NewAWSOptionsResolver(v1alpha1.OptionsAWS{
		AccessKeyIDEnvVar: "INJECTED_ACCESS",
	})
	service := NewService()
	service.UseCredentials(injected)

	resolver := service.eksOwnershipResolver(name, "us-east-1")

	assert.Equal(t, "INJECTED_ACCESS", resolver.EnvVar(credentials.AWSAccessKeyID),
		"a region with no record of its own borrowed another region's recorded aliases")
}

// TestAnUnrecordedClusterKeepsTheInjectedResolver protects the injection seam. UseCredentials points
// discovery at a Settings-backed resolver, and consulting the record must not replace it for a
// cluster that has none — every unrecorded path has to resolve exactly as it did before.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestAnUnrecordedClusterKeepsTheInjectedResolver(t *testing.T) {
	isolateHome(t)

	injected := credentials.NewAWSOptionsResolver(v1alpha1.OptionsAWS{
		AccessKeyIDEnvVar: "INJECTED_ACCESS",
	})
	service := NewService()
	service.UseCredentials(injected)

	resolver := service.eksOwnershipResolver("no-record-cluster", "eu-north-1")

	assert.Equal(t, "INJECTED_ACCESS", resolver.EnvVar(credentials.AWSAccessKeyID),
		"a cluster with no ownership record lost the injected resolver")
}

// TestALegacyRecordKeepsTheInjectedResolver covers a record written before the mapping schema. It
// carries no names to honour, so it must leave resolution exactly as it is today. The authoritative,
// actionable failure for a legacy record belongs to eksidentity.NewVerifier's migration error, which
// names the rebind command; shadowing it with an opaque credential error here would lose that.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestALegacyRecordKeepsTheInjectedResolver(t *testing.T) {
	home := isolateHome(t)

	const (
		name   = "legacy-record-cluster"
		region = "eu-north-1"
	)

	writeLegacyOwnership(t, home, name, region)

	_, err := state.LoadEKSOwnershipState(name, region)
	require.Error(t, err, "the legacy fixture validated, so it no longer models a legacy record")

	injected := credentials.NewAWSOptionsResolver(v1alpha1.OptionsAWS{
		AccessKeyIDEnvVar: "INJECTED_ACCESS",
	})
	service := NewService()
	service.UseCredentials(injected)

	resolver := service.eksOwnershipResolver(name, region)

	assert.Equal(t, "INJECTED_ACCESS", resolver.EnvVar(credentials.AWSAccessKeyID),
		"a legacy record displaced the injected resolver instead of leaving it alone")
}
