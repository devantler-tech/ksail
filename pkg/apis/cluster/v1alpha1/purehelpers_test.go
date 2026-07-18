package v1alpha1_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These black-box tests cover small, pure helper functions across the v1alpha1
// config API that previously had no direct coverage: OIDC validation/enablement,
// Flux signature-verification enablement, Hetzner network/CNI-port resolution,
// provider Docker-need predicate, node-count arithmetic, and the host-cluster
// label predicate.

func TestFluxVerifySpecEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec v1alpha1.FluxVerifySpec
		want bool
	}{
		{
			name: "provider set is enabled",
			spec: v1alpha1.FluxVerifySpec{Provider: "cosign"},
			want: true,
		},
		{name: "empty provider is disabled", spec: v1alpha1.FluxVerifySpec{}, want: false},
		{
			name: "whitespace-only provider is disabled",
			spec: v1alpha1.FluxVerifySpec{Provider: "  \t "},
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := testCase.spec
			assert.Equal(t, testCase.want, spec.Enabled())
		})
	}
}

func TestOIDCSpecEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec v1alpha1.OIDCSpec
		want bool
	}{
		{
			name: "issuer set is enabled",
			spec: v1alpha1.OIDCSpec{IssuerURL: "https://dex.example.com"},
			want: true,
		},
		{name: "empty issuer is disabled", spec: v1alpha1.OIDCSpec{}, want: false},
		{
			name: "client id alone is disabled",
			spec: v1alpha1.OIDCSpec{ClientID: "kubectl"},
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := testCase.spec
			assert.Equal(t, testCase.want, spec.Enabled())
		})
	}
}

func TestValidateOIDCConfig_NilAndDisabled(t *testing.T) {
	t.Parallel()

	t.Run("nil spec is valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, v1alpha1.ValidateOIDCConfig(nil))
	})

	t.Run("empty spec is valid (disabled)", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, v1alpha1.ValidateOIDCConfig(&v1alpha1.OIDCSpec{}))
	})
}

func TestValidateOIDCConfig_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec v1alpha1.OIDCSpec
	}{
		{
			name: "client id without issuer",
			spec: v1alpha1.OIDCSpec{ClientID: "kubectl"},
		},
		{
			name: "issuer without client id",
			spec: v1alpha1.OIDCSpec{IssuerURL: "https://dex.example.com"},
		},
		{
			name: "issuer must be https",
			spec: v1alpha1.OIDCSpec{IssuerURL: "http://dex.example.com", ClientID: "kubectl"},
		},
		{
			name: "empty extra scope rejected",
			spec: v1alpha1.OIDCSpec{
				IssuerURL:   "https://dex.example.com",
				ClientID:    "kubectl",
				ExtraScopes: []string{"groups", "  "},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := testCase.spec
			err := v1alpha1.ValidateOIDCConfig(&spec)
			require.Error(t, err)
			assert.ErrorIs(t, err, v1alpha1.ErrInvalidOIDCConfig)
		})
	}
}

func TestValidateOIDCConfig_NormalizesScopes(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.OIDCSpec{
		IssuerURL:   "https://dex.example.com",
		ClientID:    "kubectl",
		ExtraScopes: []string{" groups ", "email", "groups", "  email"},
	}

	err := v1alpha1.ValidateOIDCConfig(&spec)
	require.NoError(t, err)
	// Whitespace trimmed and duplicates collapsed, original order preserved.
	assert.Equal(t, []string{"groups", "email"}, spec.ExtraScopes)
}

func TestValidateOIDCConfig_ValidWithoutScopes(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.OIDCSpec{
		IssuerURL: "https://dex.example.com",
		ClientID:  "kubectl",
	}

	require.NoError(t, v1alpha1.ValidateOIDCConfig(&spec))
	assert.Empty(t, spec.ExtraScopes)
}

func TestHetznerNetworkCIDR(t *testing.T) {
	t.Parallel()

	t.Run("returns configured cidr", func(t *testing.T) {
		t.Parallel()

		spec := v1alpha1.Spec{}
		spec.Provider.Hetzner.NetworkCIDR = "192.168.0.0/24"
		assert.Equal(t, "192.168.0.0/24", v1alpha1.HetznerNetworkCIDR(spec))
	})

	t.Run("falls back to default when unset", func(t *testing.T) {
		t.Parallel()

		got := v1alpha1.HetznerNetworkCIDR(v1alpha1.Spec{})
		assert.Equal(t, v1alpha1.DefaultHetznerNetworkCIDR, got)
	})
}

func TestHetznerCNIPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cni  v1alpha1.CNI
		want int
	}{
		{name: "cilium uses 8472", cni: v1alpha1.CNICilium, want: 8472},
		{name: "calico uses 4789", cni: v1alpha1.CNICalico, want: 4789},
		{name: "default uses 4789", cni: v1alpha1.CNIDefault, want: 4789},
		{name: "empty uses 4789", cni: "", want: 4789},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := v1alpha1.Spec{}
			spec.Cluster.CNI = testCase.cni
			assert.Equal(t, testCase.want, v1alpha1.HetznerCNIPort(spec))
		})
	}
}

func TestProviderNeedsLocalDocker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider v1alpha1.Provider
		want     bool
	}{
		{name: "docker needs local docker", provider: v1alpha1.ProviderDocker, want: true},
		{name: "empty defaults to docker semantics", provider: "", want: true},
		{name: "hetzner does not", provider: v1alpha1.ProviderHetzner, want: false},
		{name: "omni does not", provider: v1alpha1.ProviderOmni, want: false},
		{name: "aws does not", provider: v1alpha1.ProviderAWS, want: false},
		{name: "kubernetes does not", provider: v1alpha1.ProviderKubernetes, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provider := testCase.provider
			assert.Equal(t, testCase.want, provider.NeedsLocalDocker())
		})
	}
}

func TestClusterSpecTotalNodeCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		controlPlanes int32
		workers       int32
		want          int32
	}{
		{name: "single control plane no workers", controlPlanes: 1, workers: 0, want: 1},
		{name: "ha with workers", controlPlanes: 3, workers: 2, want: 5},
		{name: "all zero", controlPlanes: 0, workers: 0, want: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := v1alpha1.ClusterSpec{
				ControlPlanes: testCase.controlPlanes,
				Workers:       testCase.workers,
			}
			assert.Equal(t, testCase.want, spec.TotalNodeCount())
		})
	}
}

func TestClusterIsHostClusterLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "label true",
			labels: map[string]string{v1alpha1.HostClusterLabel: "true"},
			want:   true,
		},
		{
			name:   "label false",
			labels: map[string]string{v1alpha1.HostClusterLabel: "false"},
			want:   false,
		},
		{name: "label missing", labels: map[string]string{"other": "true"}, want: false},
		{name: "nil labels", labels: nil, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := &v1alpha1.Cluster{}
			cluster.Labels = testCase.labels
			assert.Equal(t, testCase.want, cluster.IsHostCluster())
		})
	}
}

func TestClusterIsHostClusterRegistration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cluster   v1alpha1.Cluster
		namespace string
		want      bool
	}{
		{
			name: "well known host registration",
			cluster: v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      v1alpha1.HostClusterName,
					Namespace: "ksail-system",
					Labels:    map[string]string{v1alpha1.HostClusterLabel: "true"},
				},
			},
			namespace: "ksail-system",
			want:      true,
		},
		{
			name: "forged label with wrong name",
			cluster: v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "evil-alias",
					Namespace: "ksail-system",
					Labels:    map[string]string{v1alpha1.HostClusterLabel: "true"},
				},
			},
			namespace: "ksail-system",
			want:      false,
		},
		{
			name: "forged label with wrong namespace",
			cluster: v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      v1alpha1.HostClusterName,
					Namespace: "attacker",
					Labels:    map[string]string{v1alpha1.HostClusterLabel: "true"},
				},
			},
			namespace: "ksail-system",
			want:      false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(
				t,
				testCase.want,
				testCase.cluster.IsHostClusterRegistration(testCase.namespace),
			)
		})
	}
}
