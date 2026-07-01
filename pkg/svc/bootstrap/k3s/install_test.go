package k3sbootstrap_test

import (
	"testing"

	k3sbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/k3s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test enumerating each role and option combination.
func TestRenderValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  k3sbootstrap.InstallConfig
		want string
	}{
		{
			name: "server-init minimal",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    k3sbootstrap.RoleServerInit,
				Token:   "secret",
			},
			want: "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
				"INSTALL_K3S_VERSION='v1.30.2+k3s1' " +
				"K3S_TOKEN='secret' sh -s - server --cluster-init",
		},
		{
			name: "server-init with sans, disables and kubeconfig mode",
			cfg: k3sbootstrap.InstallConfig{
				Version:             "v1.30.2+k3s1",
				Role:                k3sbootstrap.RoleServerInit,
				Token:               "secret",
				TLSSANs:             []string{"lb.example.com", "10.0.0.1"},
				Disable:             []string{"traefik", "servicelb"},
				WriteKubeconfigMode: "0644",
			},
			// SANs and disables are emitted in sorted order for determinism.
			want: "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
				"INSTALL_K3S_VERSION='v1.30.2+k3s1' " +
				"K3S_TOKEN='secret' sh -s - server --cluster-init " +
				"--tls-san '10.0.0.1' --tls-san 'lb.example.com' " +
				"--disable 'servicelb' --disable 'traefik' --write-kubeconfig-mode '0644'",
		},
		{
			name: "additional server joins via --server",
			cfg: k3sbootstrap.InstallConfig{
				Version:   "v1.30.2+k3s1",
				Role:      k3sbootstrap.RoleServer,
				Token:     "secret",
				ServerURL: "https://10.0.0.2:6443",
			},
			want: "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
				"INSTALL_K3S_VERSION='v1.30.2+k3s1' " +
				"K3S_TOKEN='secret' sh -s - server --server 'https://10.0.0.2:6443'",
		},
		{
			name: "agent joins via K3S_URL",
			cfg: k3sbootstrap.InstallConfig{
				Version:   "v1.30.2+k3s1",
				Role:      k3sbootstrap.RoleAgent,
				Token:     "secret",
				ServerURL: "https://10.0.0.2:6443",
			},
			want: "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
				"INSTALL_K3S_VERSION='v1.30.2+k3s1' " +
				"K3S_URL='https://10.0.0.2:6443' K3S_TOKEN='secret' sh -s - agent",
		},
		{
			name: "token with a single quote is shell-escaped",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    k3sbootstrap.RoleServerInit,
				Token:   "a'b",
			},
			want: `script="$(curl -sfL 'https://get.k3s.io')" && printf '%s' "$script" | ` +
				`INSTALL_K3S_VERSION='v1.30.2+k3s1' ` +
				`K3S_TOKEN='a'\''b' sh -s - server --cluster-init`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := k3sbootstrap.Render(test.cfg)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

//nolint:funlen // Table-driven test enumerating each validation error.
func TestRenderInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     k3sbootstrap.InstallConfig
		wantErr error
	}{
		{
			name:    "missing version",
			cfg:     k3sbootstrap.InstallConfig{Role: k3sbootstrap.RoleServerInit, Token: "t"},
			wantErr: k3sbootstrap.ErrMissingVersion,
		},
		{
			name: "missing token",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    k3sbootstrap.RoleServerInit,
			},
			wantErr: k3sbootstrap.ErrMissingToken,
		},
		{
			name: "server-init must not carry a server URL",
			cfg: k3sbootstrap.InstallConfig{
				Version:   "v1.30.2+k3s1",
				Role:      k3sbootstrap.RoleServerInit,
				Token:     "t",
				ServerURL: "https://10.0.0.2:6443",
			},
			wantErr: k3sbootstrap.ErrUnexpectedServerURL,
		},
		{
			name: "additional server needs a server URL",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    k3sbootstrap.RoleServer,
				Token:   "t",
			},
			wantErr: k3sbootstrap.ErrMissingServerURL,
		},
		{
			name: "agent needs a server URL",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    k3sbootstrap.RoleAgent,
				Token:   "t",
			},
			wantErr: k3sbootstrap.ErrMissingServerURL,
		},
		{
			name: "agent rejects server-only options",
			cfg: k3sbootstrap.InstallConfig{
				Version:   "v1.30.2+k3s1",
				Role:      k3sbootstrap.RoleAgent,
				Token:     "t",
				ServerURL: "https://10.0.0.2:6443",
				Disable:   []string{"traefik"},
			},
			wantErr: k3sbootstrap.ErrAgentServerOnlyOption,
		},
		{
			name: "agent rejects kubeconfig mode",
			cfg: k3sbootstrap.InstallConfig{
				Version:             "v1.30.2+k3s1",
				Role:                k3sbootstrap.RoleAgent,
				Token:               "t",
				ServerURL:           "https://10.0.0.2:6443",
				WriteKubeconfigMode: "0644",
			},
			wantErr: k3sbootstrap.ErrAgentServerOnlyOption,
		},
		{
			name: "unknown role",
			cfg: k3sbootstrap.InstallConfig{
				Version: "v1.30.2+k3s1",
				Role:    "worker",
				Token:   "t",
			},
			wantErr: k3sbootstrap.ErrUnknownRole,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := k3sbootstrap.Render(test.cfg)
			require.ErrorIs(t, err, test.wantErr)
			assert.Empty(t, got)
		})
	}
}
