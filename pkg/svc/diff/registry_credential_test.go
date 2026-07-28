package diff_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// theSecretPassword is the credential a rotation moves away from. Every
// assertion below checks it never reaches a Change, because Change values are
// rendered by the cluster-update output.
//
// It is a fabricated literal that has never been a real credential — it exists
// precisely so a test can prove this value does NOT escape into output.
//
//nolint:gosec // G101: an intentional fake credential, and the subject of the leak assertions.
const theSecretPassword = "ghp_rotated_token_value_9f3a"

func newCredentialEngine() *specdiff.Engine {
	return specdiff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
}

// TestCheckRegistryCredentialDetectsRotation is the core of issue #6107: the
// structural diff redacts registry passwords, so a rotated token behind an
// otherwise identical configuration must still surface as an in-place change or
// the cluster keeps authenticating with the revoked value.
func TestCheckRegistryCredentialDetectsRotation(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential("digest-old", "digest-new", result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf(
			"a rotated credential must produce exactly one in-place change, got %d",
			len(result.InPlaceChanges),
		)
	}

	change := result.InPlaceChanges[0]
	if change.Field != specdiff.RegistryCredentialField {
		t.Errorf("field = %q, want %q", change.Field, specdiff.RegistryCredentialField)
	}

	if change.Category != clusterupdate.ChangeCategoryInPlace {
		t.Errorf(
			"category = %q, want in-place — a credential rotation must never demand recreation",
			change.Category,
		)
	}
}

// TestCheckRegistryCredentialNeverRendersTheCredential pins the redaction
// property. A digest is not safe to render either: with a known registry and
// username, a digest of a low-entropy password is guessable offline.
func TestCheckRegistryCredentialNeverRendersTheCredential(t *testing.T) {
	t.Parallel()

	digestOfSecret := "sha256-of-" + theSecretPassword

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential(digestOfSecret, "digest-new", result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf("expected one in-place change, got %d", len(result.InPlaceChanges))
	}

	change := result.InPlaceChanges[0]
	for _, rendered := range []string{change.OldValue, change.NewValue, change.Reason} {
		if strings.Contains(rendered, theSecretPassword) {
			t.Errorf("rendered value %q leaks the credential", rendered)
		}

		if strings.Contains(rendered, digestOfSecret) {
			t.Errorf("rendered value %q leaks a digest of the credential", rendered)
		}
	}
}

func TestCheckRegistryCredentialSuppressesNonDrift(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		current string
		desired string
		why     string
	}{
		{
			name:    "unchanged credential",
			current: "same-digest",
			desired: "same-digest",
			why:     "an unrotated credential must not produce a change every update",
		},
		{
			name:    "secret absent or not ksail-managed",
			current: "",
			desired: "digest-new",
			why: "an empty current digest means KSail does not own the Secret; " +
				"claiming it would overwrite an ExternalSecret-managed credential",
		},
		{
			name:    "no external registry credentials configured",
			current: "digest-old",
			desired: "",
			why:     "there is no credential to write",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterupdate.NewEmptyUpdateResult()
			newCredentialEngine().CheckRegistryCredential(testCase.current, testCase.desired, result)

			total := len(result.InPlaceChanges) +
				len(result.RecreateRequired) +
				len(result.RebootRequired) +
				len(result.WipeRequired) +
				len(result.RollingRecreate) +
				len(result.UnknownBaseline)
			if total != 0 {
				t.Errorf("expected no change (%s), got %d", testCase.why, total)
			}
		})
	}
}
