package mirrorregistry

import (
	"testing"

	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

type filterCase struct {
	name      string
	input     []registry.Info
	wantHosts []string
}

func filterOutLocalRegistryCases() []filterCase {
	local := registry.LocalRegistryBaseName // "local-registry"

	return []filterCase{
		{name: "nil slice is returned unchanged", input: nil, wantHosts: nil},
		{name: "empty slice is returned unchanged", input: []registry.Info{}, wantHosts: nil},
		{
			name: "no local registry leaves the list intact",
			input: []registry.Info{
				{Host: "ghcr.io", Name: "k3d-default-ghcr.io"},
				{Host: "docker.io", Name: "k3d-default-docker.io"},
			},
			wantHosts: []string{"ghcr.io", "docker.io"},
		},
		{
			name: "match by Name (cluster-prefixed) is filtered out",
			input: []registry.Info{
				{Host: "ghcr.io", Name: "k3d-default-ghcr.io"},
				{Host: "127.0.0.1", Name: "k3d-default-" + local},
			},
			wantHosts: []string{"ghcr.io"},
		},
		{
			name: "match by Host is filtered out",
			input: []registry.Info{
				{Host: local, Name: "registry-1"},
				{Host: "docker.io", Name: "k3d-default-docker.io"},
			},
			wantHosts: []string{"docker.io"},
		},
		{
			name: "all entries are local registries leaves an empty result",
			input: []registry.Info{
				{Host: "a-" + local, Name: "x"},
				{Host: "z", Name: "b-" + local + "-c"},
			},
			wantHosts: []string{},
		},
	}
}

// TestFilterOutLocalRegistry pins the pure filter that removes K3d's
// natively-managed local registry from the mirror registry list (matched by
// either Host or Name containing registry.LocalRegistryBaseName).
func TestFilterOutLocalRegistry(t *testing.T) {
	t.Parallel()

	for _, testCase := range filterOutLocalRegistryCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := filterOutLocalRegistry(testCase.input)
			assertHostsEqual(t, infoHosts(got), testCase.wantHosts)
		})
	}
}

// TestResolveVClusterClusterName pins the VCluster cluster-name resolver, which
// follows the same nil/empty/whitespace fall-through to the default as
// localregistry/resolve.go.
func TestResolveVClusterClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *clusterprovisioner.VClusterConfig
		want string
	}{
		{
			name: "nil config falls back to the default name",
			cfg:  nil,
			want: vclusterDefaultClusterName,
		},
		{
			name: "empty name falls back to the default name",
			cfg:  &clusterprovisioner.VClusterConfig{Name: ""},
			want: vclusterDefaultClusterName,
		},
		{
			name: "whitespace-only name falls back to the default name",
			cfg:  &clusterprovisioner.VClusterConfig{Name: "   \t "},
			want: vclusterDefaultClusterName,
		},
		{
			name: "explicit name is used verbatim",
			cfg:  &clusterprovisioner.VClusterConfig{Name: "my-cluster"},
			want: "my-cluster",
		},
		{
			name: "surrounding whitespace is trimmed from an explicit name",
			cfg:  &clusterprovisioner.VClusterConfig{Name: "  my-cluster  "},
			want: "my-cluster",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := resolveVClusterClusterName(testCase.cfg)
			if got != testCase.want {
				t.Errorf("resolveVClusterClusterName() = %q, want %q", got, testCase.want)
			}
		})
	}
}

type talosInfoCase struct {
	name          string
	specs         []registry.MirrorSpec
	wantNil       bool
	wantHosts     []string
	wantUpstreams []string
}

func buildTalosRegistryInfosCases() []talosInfoCase {
	return []talosInfoCase{
		{name: "nil specs return nil", specs: nil, wantNil: true},
		{name: "empty specs return nil", specs: []registry.MirrorSpec{}, wantNil: true},
		{
			name: "each non-empty-host spec maps to one Info, host and upstream preserved",
			specs: []registry.MirrorSpec{
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			wantHosts:     []string{"ghcr.io", "docker.io"},
			wantUpstreams: []string{"https://ghcr.io", "https://registry-1.docker.io"},
		},
		{
			name: "blank-host specs are skipped",
			specs: []registry.MirrorSpec{
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
				{Host: "   ", Remote: "https://ignored"},
			},
			wantHosts:     []string{"ghcr.io"},
			wantUpstreams: []string{"https://ghcr.io"},
		},
	}
}

// TestBuildTalosRegistryInfos pins the empty-guard (returns nil) and the
// spec->Info mapping (host preserved, blank-host specs skipped, explicit Remote
// used as the upstream).
func TestBuildTalosRegistryInfos(t *testing.T) {
	t.Parallel()

	for _, testCase := range buildTalosRegistryInfosCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := buildTalosRegistryInfos(testCase.specs, "talos-default", nil)

			if testCase.wantNil {
				if got != nil {
					t.Errorf("buildTalosRegistryInfos() = %v, want nil", got)
				}

				return
			}

			assertHostsEqual(t, infoHosts(got), testCase.wantHosts)
			assertHostsEqual(t, infoUpstreams(got), testCase.wantUpstreams)
		})
	}
}

func infoHosts(infos []registry.Info) []string {
	hosts := make([]string, 0, len(infos))
	for _, info := range infos {
		hosts = append(hosts, info.Host)
	}

	return hosts
}

func infoUpstreams(infos []registry.Info) []string {
	upstreams := make([]string, 0, len(infos))
	for _, info := range infos {
		upstreams = append(upstreams, info.Upstream)
	}

	return upstreams
}

func assertHostsEqual(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for idx, value := range want {
		if got[idx] != value {
			t.Errorf("element[%d] = %q, want %q", idx, got[idx], value)
		}
	}
}
