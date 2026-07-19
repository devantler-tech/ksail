package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLocalRegistryForProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		provider  v1alpha1.Provider
		registry  v1alpha1.LocalRegistry
		wantError bool
	}{
		{
			"disabled registry is always valid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{},
			false,
		},
		{
			"Docker provider with local registry is valid",
			v1alpha1.ProviderDocker,
			v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			false,
		},
		{
			"Docker provider with external registry is valid",
			v1alpha1.ProviderDocker,
			v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			false,
		},
		{
			"Hetzner provider with external registry is valid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			false,
		},
		{
			"Hetzner provider with local registry is invalid",
			v1alpha1.ProviderHetzner,
			v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateLocalRegistryForProvider(testCase.provider, testCase.registry)

			if testCase.wantError {
				require.ErrorIs(t, err, v1alpha1.ErrLocalRegistryNotSupported)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestValidateClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid_simple_name",
			input:     "my-cluster",
			wantError: false,
		},
		{
			name:      "valid_lowercase_letters",
			input:     "test",
			wantError: false,
		},
		{
			name:      "valid_with_numbers",
			input:     "cluster123",
			wantError: false,
		},
		{
			name:      "valid_single_letter",
			input:     "a",
			wantError: false,
		},
		{
			name:      "empty_is_valid",
			input:     "",
			wantError: false,
		},
		{
			name:      "invalid_uppercase",
			input:     "MyCluster",
			wantError: true,
			errorMsg:  "DNS-1123 compliant",
		},
		{
			name:      "invalid_starts_with_number",
			input:     "1cluster",
			wantError: true,
			errorMsg:  "must start with a letter",
		},
		{
			name:      "invalid_ends_with_hyphen",
			input:     "cluster-",
			wantError: true,
			errorMsg:  "must not end with a hyphen",
		},
		{
			name:      "invalid_special_characters",
			input:     "my_cluster",
			wantError: true,
			errorMsg:  "DNS-1123 compliant",
		},
		{
			name:      "invalid_too_long",
			input:     "this-is-a-very-long-cluster-name-that-exceeds-the-maximum-allowed-length",
			wantError: true,
			errorMsg:  "too long",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateClusterName(testCase.input)
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestValidateMirrorRegistriesForProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		provider         v1alpha1.Provider
		mirrorRegistries []string
		wantError        bool
		errorContains    string
	}{
		{
			name:             "Docker: empty registries -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{},
			wantError:        false,
		},
		{
			name:             "Docker: local mirror -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{"docker.io=http://localhost:5000"},
			wantError:        false,
		},
		{
			name:             "Docker: external mirror -> valid",
			provider:         v1alpha1.ProviderDocker,
			mirrorRegistries: []string{"docker.io=https://mirror.gcr.io"},
			wantError:        false,
		},
		{
			name:             "Hetzner: empty registries -> valid",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{},
			wantError:        false,
		},
		{
			name:             "Hetzner: external mirror -> valid",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=https://mirror.gcr.io"},
			wantError:        false,
		},
		{
			name:     "Hetzner: multiple external mirrors -> valid",
			provider: v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{
				"docker.io=https://mirror.gcr.io",
				"ghcr.io=https://ghcr.io",
			},
			wantError: false,
		},
		{
			name:             "Hetzner: localhost mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://localhost:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:             "Hetzner: 127.0.0.1 mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://127.0.0.1:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:             "Hetzner: host.docker.internal mirror -> error",
			provider:         v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{"docker.io=http://host.docker.internal:5000"},
			wantError:        true,
			errorContains:    "local mirror registry not supported",
		},
		{
			name:     "Hetzner: mixed local and external -> error on local",
			provider: v1alpha1.ProviderHetzner,
			mirrorRegistries: []string{
				"docker.io=https://mirror.gcr.io",
				"ghcr.io=http://localhost:5000",
			},
			wantError:     true,
			errorContains: "local mirror registry not supported",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateMirrorRegistriesForProvider(
				testCase.provider,
				testCase.mirrorRegistries,
			)
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestValidateAllowedCIDRs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cidrs         []string
		wantError     bool
		errorContains string
	}{
		{
			name:      "empty slice is valid",
			cidrs:     []string{},
			wantError: false,
		},
		{
			name:      "nil slice is valid",
			cidrs:     nil,
			wantError: false,
		},
		{
			name:      "valid IPv4 CIDR",
			cidrs:     []string{"192.168.1.0/24"},
			wantError: false,
		},
		{
			name:      "valid IPv6 CIDR",
			cidrs:     []string{"2001:db8::/32"},
			wantError: false,
		},
		{
			name:      "multiple valid CIDRs",
			cidrs:     []string{"10.0.0.0/8", "172.16.0.0/12", "2001:db8::/32"},
			wantError: false,
		},
		{
			name:      "whitespace around valid CIDR is accepted",
			cidrs:     []string{"  192.168.0.0/16  "},
			wantError: false,
		},
		{
			name:          "empty string entry rejected",
			cidrs:         []string{""},
			wantError:     true,
			errorContains: "entry[0] must not be empty",
		},
		{
			name:          "whitespace-only entry rejected",
			cidrs:         []string{"   "},
			wantError:     true,
			errorContains: "entry[0] must not be empty",
		},
		{
			name:          "invalid CIDR rejected",
			cidrs:         []string{"not-a-cidr"},
			wantError:     true,
			errorContains: "entry[0]",
		},
		{
			name:          "IP without mask rejected",
			cidrs:         []string{"192.168.1.1"},
			wantError:     true,
			errorContains: "entry[0]",
		},
		{
			name:          "second entry invalid",
			cidrs:         []string{"10.0.0.0/8", "bad"},
			wantError:     true,
			errorContains: "entry[1]",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateAllowedCIDRs(testCase.cidrs)
			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidAllowedCIDR)
				assert.Contains(t, err.Error(), testCase.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateNestedCIDRs(t *testing.T) { //nolint:funlen // table-driven test data
	t.Parallel()

	tests := []struct {
		name        string
		podCIDR     string
		serviceCIDR string
		wantError   bool
	}{
		{
			name:        "default_non_overlapping_cidrs",
			podCIDR:     "10.64.0.0/16",
			serviceCIDR: "10.128.0.0/16",
			wantError:   false,
		},
		{
			name:        "empty_cidrs_valid",
			podCIDR:     "",
			serviceCIDR: "",
			wantError:   false,
		},
		{
			name:        "pod_cidr_overlaps_host_pod_cidr",
			podCIDR:     "10.244.0.0/16",
			serviceCIDR: "10.128.0.0/16",
			wantError:   true,
		},
		{
			name:        "service_cidr_overlaps_host_service_cidr",
			podCIDR:     "10.64.0.0/16",
			serviceCIDR: "10.96.0.0/16",
			wantError:   true,
		},
		{
			name:        "both_overlap",
			podCIDR:     "10.244.0.0/16",
			serviceCIDR: "10.96.0.0/12",
			wantError:   true,
		},
		{
			name:        "invalid_pod_cidr",
			podCIDR:     "not-a-cidr",
			serviceCIDR: "10.128.0.0/16",
			wantError:   true,
		},
		{
			name:        "custom_non_overlapping",
			podCIDR:     "172.20.0.0/16",
			serviceCIDR: "172.21.0.0/16",
			wantError:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateNestedCIDRs(testCase.podCIDR, testCase.serviceCIDR)
			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Omni provider — local registry and mirror validation
// ---------------------------------------------------------------------------

func TestValidateLocalRegistryForProvider_Omni(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		registry  v1alpha1.LocalRegistry
		wantError bool
	}{
		{
			name:      "Omni_local_registry_fails",
			registry:  v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			wantError: true,
		},
		{
			name:      "Omni_external_registry_succeeds",
			registry:  v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			wantError: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateLocalRegistryForProvider(
				v1alpha1.ProviderOmni,
				testCase.registry,
			)

			if testCase.wantError {
				require.ErrorIs(t, err, v1alpha1.ErrLocalRegistryNotSupported)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateMirrorRegistriesForProvider_Omni(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mirrors   []string
		wantError bool
	}{
		{
			name:      "Omni_local_mirror_fails",
			mirrors:   []string{"docker.io=http://localhost:5000"},
			wantError: true,
		},
		{
			name:      "Omni_external_mirror_succeeds",
			mirrors:   []string{"docker.io=https://mirror.gcr.io"},
			wantError: false,
		},
		{
			name:      "Omni_ipv6_localhost_mirror_fails",
			mirrors:   []string{"docker.io=http://[::1]:5000"},
			wantError: true,
		},
		{
			name:      "Omni_0000_mirror_fails",
			mirrors:   []string{"docker.io=http://0.0.0.0:5000"},
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateMirrorRegistriesForProvider(
				v1alpha1.ProviderOmni,
				testCase.mirrors,
			)

			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateEKSConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster *v1alpha1.ClusterSpec
		wantErr error
	}{
		{
			name:    "nil cluster is valid",
			cluster: nil,
		},
		{
			name: "non-EKS distribution ignores the field",
			cluster: &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				EKS: v1alpha1.OptionsEKS{
					AWSLoadBalancerControllerServiceAccount: "Not_A_Valid_SA!",
				},
			},
		},
		{
			name: "empty service account is valid",
			cluster: &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionEKS,
			},
		},
		{
			name: "valid service account",
			cluster: &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionEKS,
				EKS: v1alpha1.OptionsEKS{
					AWSLoadBalancerControllerServiceAccount: "aws-load-balancer-controller",
				},
			},
		},
		{
			name: "invalid service account fails before provisioning",
			cluster: &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionEKS,
				EKS: v1alpha1.OptionsEKS{
					AWSLoadBalancerControllerServiceAccount: "Not_A_Valid_SA!",
				},
			},
			wantErr: v1alpha1.ErrInvalidEKSConfig,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateEKSConfig(testCase.cluster)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
