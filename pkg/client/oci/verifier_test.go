package oci_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyOptions_EmptyEndpoint(t *testing.T) {
	t.Parallel()

	verifier := oci.NewRegistryVerifier()

	err := verifier.VerifyAccess(context.Background(), oci.VerifyOptions{
		RegistryEndpoint: "",
		Repository:       "test",
	})

	require.Error(t, err)
	assert.Equal(t, oci.ErrRegistryEndpointRequired, err)
}

func TestVerifyRegistryAccessWithTimeout_EmptyEndpoint(t *testing.T) {
	t.Parallel()

	err := oci.VerifyRegistryAccessWithTimeout(
		context.Background(),
		oci.VerifyOptions{
			RegistryEndpoint: "",
			Repository:       "test",
		},
		100, // timeout
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry endpoint is required")
}

//nolint:funlen // Table-driven test with many cases naturally exceeds limit
func TestErrorVariables(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		err      error
		contains string
	}

	tests := []testCase{
		{
			name:     "ErrRegistryUnreachable",
			err:      oci.ErrRegistryUnreachable,
			contains: "registry is unreachable",
		},
		{
			name:     "ErrRegistryAuthRequired",
			err:      oci.ErrRegistryAuthRequired,
			contains: "registry requires authentication",
		},
		{
			name:     "ErrRegistryPermissionDenied",
			err:      oci.ErrRegistryPermissionDenied,
			contains: "registry access denied",
		},
		{
			name:     "ErrRegistryNotFound",
			err:      oci.ErrRegistryNotFound,
			contains: "registry or repository not found",
		},
		{
			name:     "ErrSourcePathRequired",
			err:      oci.ErrSourcePathRequired,
			contains: "source path is required",
		},
		{
			name:     "ErrSourcePathNotFound",
			err:      oci.ErrSourcePathNotFound,
			contains: "source path does not exist",
		},
		{
			name:     "ErrSourcePathNotDirectory",
			err:      oci.ErrSourcePathNotDirectory,
			contains: "source path must be a directory",
		},
		{
			name:     "ErrRegistryEndpointRequired",
			err:      oci.ErrRegistryEndpointRequired,
			contains: "registry endpoint is required",
		},
		{
			name:     "ErrVersionRequired",
			err:      oci.ErrVersionRequired,
			contains: "version is required",
		},
		{
			name:     "ErrNoManifestFiles",
			err:      oci.ErrNoManifestFiles,
			contains: "no manifest files found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, tc.err)
			assert.Contains(t, tc.err.Error(), tc.contains)
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	errs := []error{
		oci.ErrRegistryUnreachable,
		oci.ErrRegistryAuthRequired,
		oci.ErrRegistryPermissionDenied,
		oci.ErrRegistryNotFound,
		oci.ErrSourcePathRequired,
		oci.ErrSourcePathNotFound,
		oci.ErrSourcePathNotDirectory,
		oci.ErrRegistryEndpointRequired,
		oci.ErrVersionRequired,
		oci.ErrNoManifestFiles,
	}

	// Verify all errors are distinct
	for i, err1 := range errs {
		for j, err2 := range errs {
			if i != j {
				assert.NotErrorIs(t, err1, err2,
					"error %q should not match %q", err1, err2)
			}
		}
	}
}
