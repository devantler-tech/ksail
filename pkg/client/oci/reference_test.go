package oci_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test with comprehensive cases
func TestParseReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantHost    string
		wantPort    int32
		wantRepo    string
		wantVariant string
		wantRef     string
		wantErr     bool
		wantNil     bool
	}{
		{
			name:    "empty string returns nil",
			input:   "",
			wantNil: true,
		},
		{
			name:     "full reference with port",
			input:    "oci://localhost:5050/k8s:dev",
			wantHost: "localhost",
			wantPort: 5050,
			wantRepo: "k8s",
			wantRef:  "dev",
		},
		{
			name:        "reference with variant",
			input:       "oci://localhost:5050/my-app/base:v1.0.0",
			wantHost:    "localhost",
			wantPort:    5050,
			wantRepo:    "my-app",
			wantVariant: "base",
			wantRef:     "v1.0.0",
		},
		{
			name:     "host without port",
			input:    "oci://registry.example.com/repo:latest",
			wantHost: "registry.example.com",
			wantPort: 0,
			wantRepo: "repo",
			wantRef:  "latest",
		},
		{
			name:     "no ref tag",
			input:    "oci://localhost:5000/workloads",
			wantHost: "localhost",
			wantPort: 5000,
			wantRepo: "workloads",
			wantRef:  "",
		},
		{
			name:    "invalid scheme",
			input:   "docker://localhost:5000/repo:tag",
			wantErr: true,
		},
		{
			name:    "empty after scheme",
			input:   "oci://",
			wantErr: true,
		},
		{
			name:    "no path",
			input:   "oci://localhost:5000",
			wantErr: true,
		},
		{
			name:    "invalid port",
			input:   "oci://localhost:invalid/repo:tag",
			wantErr: true,
		},
		{
			name:    "port out of range",
			input:   "oci://localhost:99999/repo:tag",
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := oci.ParseReference(testCase.input)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)

			if testCase.wantNil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, testCase.wantHost, got.Host)
			assert.Equal(t, testCase.wantPort, got.Port)
			assert.Equal(t, testCase.wantRepo, got.Repository)
			assert.Equal(t, testCase.wantVariant, got.Variant)
			assert.Equal(t, testCase.wantRef, got.Ref)
		})
	}
}

func TestReferenceString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  oci.Reference
		want string
	}{
		{
			name: "full reference",
			ref: oci.Reference{
				Host:       "localhost",
				Port:       5050,
				Repository: "k8s",
				Ref:        "dev",
			},
			want: "oci://localhost:5050/k8s:dev",
		},
		{
			name: "with variant",
			ref: oci.Reference{
				Host:       "localhost",
				Port:       5050,
				Repository: "my-app",
				Variant:    "base",
				Ref:        "v1.0.0",
			},
			want: "oci://localhost:5050/my-app/base:v1.0.0",
		},
		{
			name: "no port",
			ref: oci.Reference{
				Host:       "registry.example.com",
				Repository: "repo",
				Ref:        "latest",
			},
			want: "oci://registry.example.com/repo:latest",
		},
		{
			name: "no ref",
			ref: oci.Reference{
				Host:       "localhost",
				Port:       5000,
				Repository: "workloads",
			},
			want: "oci://localhost:5000/workloads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.ref.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReferenceFullRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  oci.Reference
		want string
	}{
		{
			name: "without variant",
			ref: oci.Reference{
				Repository: "k8s",
			},
			want: "k8s",
		},
		{
			name: "with variant",
			ref: oci.Reference{
				Repository: "my-app",
				Variant:    "base",
			},
			want: "my-app/base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.ref.FullRepository()
			assert.Equal(t, tt.want, got)
		})
	}
}
