package oci_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Repeated test fixtures (kept as constants/vars to satisfy goconst/err113).
const (
	ociTestRepo     = "test/repo"
	ociTestTag      = "dev"
	ociTestRegistry = "ghcr.io"
)

// Static errors exercising the message-classification branches.
var (
	errMsgUnauthorized     = errors.New("token request failed: unauthorized")
	errMsgAuthRequired     = errors.New("authentication required to pull")
	errMsgDenied           = errors.New("requested access to the resource is denied")
	errMsgForbidden        = errors.New("403 forbidden")
	errMsgNoSuchHost       = errors.New("dial tcp: lookup bad.example: no such host")
	errMsgConnRefused      = errors.New("connection refused")
	errMsgNameUnknownColon = errors.New("NAME_UNKNOWN: repository not present")
	errMsgNameUnknown      = errors.New("name unknown")
	errMsgUnexpected       = errors.New("some unexpected failure")
	errMsgManifestNotFound = errors.New("manifest not found")
	errMsgManifestUnknown  = errors.New("manifest unknown")
	errMsgNameUnknownUpper = errors.New("NAME_UNKNOWN")
)

// transportErr is a small helper constructing a *transport.Error with a status code.
func transportErr(status int) *transport.Error {
	return &transport.Error{StatusCode: status}
}

//nolint:funlen // Table-driven test with many cases naturally exceeds limit.
func TestClassifyRegistryError(t *testing.T) {
	t.Parallel()

	type classifyCase struct {
		name string
		err  error
		// want is the sentinel the result must wrap (errors.Is). nil means the
		// result itself must be nil.
		want error
		// contains, when set, is a substring the result message must contain.
		contains string
		// wantWrapped asserts the result is a non-nil error that is none of the
		// known sentinels (the generic "registry access check failed" path).
		wantWrapped bool
	}

	tests := []classifyCase{
		{name: "nil error", err: nil, want: nil},
		{
			name: "transport 401 unauthorized",
			err:  transportErr(http.StatusUnauthorized),
			want: oci.ErrRegistryAuthRequired,
		},
		{
			name: "transport 403 forbidden",
			err:  transportErr(http.StatusForbidden),
			want: oci.ErrRegistryPermissionDenied,
		},
		{
			// 404 on a tag list is acceptable (repo may not exist yet); the bare
			// transport message "...404 Not Found" matches the not-found pattern.
			name: "transport 404 not found is acceptable",
			err:  transportErr(http.StatusNotFound),
			want: nil,
		},
		{
			name:        "transport 500 falls through to wrapped",
			err:         transportErr(http.StatusInternalServerError),
			wantWrapped: true,
		},
		{name: "message unauthorized", err: errMsgUnauthorized, want: oci.ErrRegistryAuthRequired},
		{
			name: "message authentication required",
			err:  errMsgAuthRequired,
			want: oci.ErrRegistryAuthRequired,
		},
		{name: "message denied", err: errMsgDenied, want: oci.ErrRegistryPermissionDenied},
		{name: "message forbidden", err: errMsgForbidden, want: oci.ErrRegistryPermissionDenied},
		{
			name:     "message no such host extracts detail after colon",
			err:      errMsgNoSuchHost,
			want:     oci.ErrRegistryUnreachable,
			contains: "no such host",
		},
		{
			name:     "message connection refused without colon detail",
			err:      errMsgConnRefused,
			want:     oci.ErrRegistryUnreachable,
			contains: "connection refused",
		},
		{name: "message name_unknown is acceptable", err: errMsgNameUnknownColon, want: nil},
		{name: "message name unknown is acceptable", err: errMsgNameUnknown, want: nil},
		{name: "unrecognized message is wrapped", err: errMsgUnexpected, wantWrapped: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := oci.ClassifyRegistryError(testCase.err)

			switch {
			case testCase.wantWrapped:
				require.Error(t, got)
				assert.Contains(t, got.Error(), "registry access check failed")
			case testCase.want == nil:
				require.NoError(t, got)
			default:
				require.Error(t, got)
				require.ErrorIs(t, got, testCase.want)

				if testCase.contains != "" {
					assert.Contains(t, got.Error(), testCase.contains)
				}
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	t.Parallel()

	type notFoundCase struct {
		name string
		err  error
		want bool
	}

	tests := []notFoundCase{
		{name: "nil is not a not-found", err: nil, want: false},
		{name: "transport 404", err: transportErr(http.StatusNotFound), want: true},
		{
			name: "transport 500 is not a not-found",
			err:  transportErr(http.StatusInternalServerError),
			want: false,
		},
		{name: "message not found", err: errMsgManifestNotFound, want: true},
		{name: "message manifest unknown", err: errMsgManifestUnknown, want: true},
		{name: "message name_unknown", err: errMsgNameUnknownUpper, want: true},
		{name: "message name unknown", err: errMsgNameUnknown, want: true},
		{name: "unrelated message", err: errMsgConnRefused, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, oci.IsNotFoundError(testCase.err))
		})
	}
}

func TestBuildRemoteOptionsWithAuth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Without credentials only the context option is present.
	assert.Len(t, oci.BuildRemoteOptionsWithAuth(ctx, "", ""), 1)
	// A username adds basic-auth.
	assert.Len(t, oci.BuildRemoteOptionsWithAuth(ctx, "user", ""), 2)
	// A password alone also adds basic-auth.
	assert.Len(t, oci.BuildRemoteOptionsWithAuth(ctx, "", "token"), 2)
}

func TestArtifactExists_GuardsAndInvalidReference(t *testing.T) {
	t.Parallel()

	verifier := oci.NewRegistryVerifier()
	ctx := context.Background()

	t.Run("empty endpoint", func(t *testing.T) {
		t.Parallel()

		exists, err := verifier.ArtifactExists(ctx, oci.ArtifactExistsOptions{
			Repository: ociTestRepo,
			Tag:        ociTestTag,
		})

		require.ErrorIs(t, err, oci.ErrRegistryEndpointRequired)
		assert.False(t, exists)
	})

	t.Run("empty tag", func(t *testing.T) {
		t.Parallel()

		exists, err := verifier.ArtifactExists(ctx, oci.ArtifactExistsOptions{
			RegistryEndpoint: ociTestRegistry,
			Repository:       ociTestRepo,
		})

		require.ErrorIs(t, err, oci.ErrVersionRequired)
		assert.False(t, exists)
	})

	t.Run("invalid reference", func(t *testing.T) {
		t.Parallel()

		exists, err := verifier.ArtifactExists(ctx, oci.ArtifactExistsOptions{
			RegistryEndpoint: ociTestRegistry,
			Repository:       "bad repo",
			Tag:              ociTestTag,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse reference")
		assert.False(t, exists)
	})
}

func TestVerifyAccess_InvalidRepository(t *testing.T) {
	t.Parallel()

	verifier := oci.NewRegistryVerifier()

	err := verifier.VerifyAccess(context.Background(), oci.VerifyOptions{
		RegistryEndpoint: ociTestRegistry,
		Repository:       "bad repo",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse repository reference")
}
