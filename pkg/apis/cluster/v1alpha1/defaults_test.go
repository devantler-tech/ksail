package v1alpha1_test

import (
	"reflect"
	"slices"
	"strconv"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultHetznerFallbackLocations(t *testing.T) {
	t.Parallel()

	got := v1alpha1.DefaultHetznerFallbackLocations()

	// Two distinct fallbacks that exclude the primary location: otherwise the
	// location-fallback retry path would have nothing meaningful to try.
	assert.Len(t, got, 2)
	assert.NotContains(t, got, v1alpha1.DefaultHetznerLocation)

	// Each call must return an independent slice so a caller that retains and
	// mutates the result cannot corrupt the package-level default.
	want := slices.Clone(got)
	got[0] = "mutated"

	assert.Equal(t, want, v1alpha1.DefaultHetznerFallbackLocations())
}

// ---------------------------------------------------------------------------
// Default-const ↔ struct-tag equality guard.
//
// A single default value is encoded both as a `default:"..."` struct tag (read
// by docs/gen_docs.go for the docs Default column and by marshal-time pruning)
// and as a defaults.go const (the canonical source FieldSelector defaults and
// runtime defaulting reference). This table asserts the two never drift: every
// entry pins a (struct type, field) `default:` tag to its const. Adding a new
// single-sourced default const means adding a row here so a future edit to
// either side fails loudly instead of silently diverging.
// ---------------------------------------------------------------------------

// defaultTagConstCase pins one struct field's `default:` tag to the string form
// of its canonical defaults.go const.
type defaultTagConstCase struct {
	name      string // human-readable case name
	structPtr any    // pointer to a zero value of the struct owning the field
	field     string // Go field name carrying the `default:` tag
	wantConst string // expected tag value == const value (as a string)
}

func defaultTagConstCases() []defaultTagConstCase {
	return slices.Concat(
		oidcDefaultTagConstCases(),
		hetznerDefaultTagConstCases(),
		miscDefaultTagConstCases(),
	)
}

func oidcDefaultTagConstCases() []defaultTagConstCase {
	return []defaultTagConstCase{
		{
			"OIDC.UsernameClaim", &v1alpha1.OIDCSpec{},
			"UsernameClaim", v1alpha1.DefaultOIDCUsernameClaim,
		},
		{"OIDC.GroupsClaim", &v1alpha1.OIDCSpec{}, "GroupsClaim", v1alpha1.DefaultOIDCGroupsClaim},
		{
			"OIDC.UsernamePrefix", &v1alpha1.OIDCSpec{},
			"UsernamePrefix", v1alpha1.DefaultOIDCUsernamePrefix,
		},
		{
			"OIDC.GroupsPrefix", &v1alpha1.OIDCSpec{},
			"GroupsPrefix", v1alpha1.DefaultOIDCGroupsPrefix,
		},
	}
}

func hetznerDefaultTagConstCases() []defaultTagConstCase {
	return []defaultTagConstCase{
		{
			"Hetzner.ControlPlaneServerType", &v1alpha1.OptionsHetzner{},
			"ControlPlaneServerType", v1alpha1.DefaultHetznerServerType,
		},
		{
			"Hetzner.WorkerServerType", &v1alpha1.OptionsHetzner{},
			"WorkerServerType", v1alpha1.DefaultHetznerServerType,
		},
		{
			"Hetzner.Location", &v1alpha1.OptionsHetzner{},
			"Location", v1alpha1.DefaultHetznerLocation,
		},
		{
			"Hetzner.NetworkCIDR", &v1alpha1.OptionsHetzner{},
			"NetworkCIDR", v1alpha1.DefaultHetznerNetworkCIDR,
		},
		{
			"Hetzner.TokenEnvVar", &v1alpha1.OptionsHetzner{},
			"TokenEnvVar", v1alpha1.DefaultHetznerTokenEnvVar,
		},
		{
			"Hetzner.ServerLimit", &v1alpha1.OptionsHetzner{},
			"ServerLimit", strconv.Itoa(int(v1alpha1.DefaultHetznerServerLimit)),
		},
	}
}

func miscDefaultTagConstCases() []defaultTagConstCase {
	return []defaultTagConstCase{
		{"SOPS.AgeKeyEnvVar", &v1alpha1.SOPS{}, "AgeKeyEnvVar", v1alpha1.DefaultSOPSAgeKeyEnvVar},
		{
			"Talos.ISO", &v1alpha1.OptionsTalos{},
			"ISO", strconv.FormatInt(v1alpha1.DefaultTalosISO, 10),
		},
		{
			"Connection.Kubeconfig", &v1alpha1.Connection{},
			"Kubeconfig", v1alpha1.DefaultKubeconfigPath,
		},
		{
			"OptionsKubernetes.Kubeconfig", &v1alpha1.OptionsKubernetes{},
			"Kubeconfig", v1alpha1.DefaultKubeconfigPath,
		},
		{
			"Workload.SourceDirectory", &v1alpha1.WorkloadSpec{},
			"SourceDirectory", v1alpha1.DefaultSourceDirectory,
		},
	}
}

// TestDefaultConstsMatchStructTags asserts each defaults.go const equals the
// `default:` struct tag on the field it documents, so the const stays the
// single source of truth and the two encodings cannot silently drift.
func TestDefaultConstsMatchStructTags(t *testing.T) {
	t.Parallel()

	for _, testCase := range defaultTagConstCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			structType := reflect.TypeOf(testCase.structPtr).Elem()
			name := structType.Name()

			field, found := structType.FieldByName(testCase.field)
			require.Truef(t, found, "field %s not found on %s", testCase.field, name)

			tag := field.Tag.Get("default")
			require.NotEmptyf(t, tag, "field %s.%s has no `default:` tag", name, testCase.field)

			assert.Equalf(
				t, testCase.wantConst, tag,
				"`default:` tag on %s.%s drifted from its defaults.go const",
				name, testCase.field,
			)
		})
	}
}
