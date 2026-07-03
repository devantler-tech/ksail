package cloudinitbootstrap_test

import (
	"strings"
	"testing"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// parsedConfig mirrors the cloud-config subset BuildUserData emits, so tests can
// assert on the parsed document rather than on brittle substrings.
type parsedConfig struct {
	WriteFiles []struct {
		Path        string `yaml:"path"`
		Permissions string `yaml:"permissions"`
		Content     string `yaml:"content"`
	} `yaml:"write_files"` //nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
	Apt struct {
		Sources map[string]struct {
			Source string `yaml:"source"`
			Key    string `yaml:"key"`
		} `yaml:"sources"`
	} `yaml:"apt"`
	Packages []string `yaml:"packages"`
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys"`
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
	SSHKeys struct {
		//nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
		ED25519Private string `yaml:"ed25519_private"`
		//nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
		ED25519Public string `yaml:"ed25519_public"`
	} `yaml:"ssh_keys"`
	RunCmd [][]string `yaml:"runcmd"`
}

// parse asserts the document carries the cloud-config header and is valid YAML,
// returning the decoded subset.
func parse(t *testing.T, userData string) parsedConfig {
	t.Helper()

	require.True(t,
		strings.HasPrefix(userData, "#cloud-config\n"),
		"user_data must begin with the #cloud-config header",
	)

	var cfg parsedConfig

	require.NoError(t, yaml.Unmarshal([]byte(userData), &cfg), "user_data must be valid YAML")

	return cfg
}

func TestBuildUserDataDefaults(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo first", "echo second"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	file := cfg.WriteFiles[0]
	assert.Equal(t, cloudinitbootstrap.DefaultScriptPath, file.Path)
	assert.Equal(t, "0700", file.Permissions)

	// The script runs the bootstrap directly (argv form) so its exit code is
	// preserved by cloud-init.
	assert.Equal(t, [][]string{{"/bin/sh", cloudinitbootstrap.DefaultScriptPath}}, cfg.RunCmd)

	// Strict mode + an exec redirect to the (shell-quoted) default log, then the
	// commands in order.
	assert.Equal(t,
		"#!/bin/sh\n"+
			"set -eu\n"+
			"exec >> '"+cloudinitbootstrap.DefaultLogPath+"' 2>&1\n"+
			"echo first\n"+
			"echo second\n",
		file.Content,
	)
}

func TestBuildUserDataCustomPaths(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands:   []string{"echo hi"},
		ScriptPath: "/opt/ksail/boot.sh",
		LogPath:    "/var/log/custom.log",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Equal(t, "/opt/ksail/boot.sh", cfg.WriteFiles[0].Path)
	assert.Equal(t, [][]string{{"/bin/sh", "/opt/ksail/boot.sh"}}, cfg.RunCmd)
	assert.Contains(t, cfg.WriteFiles[0].Content, "exec >> '/var/log/custom.log' 2>&1\n")
}

func TestBuildUserDataQuotesLogPath(t *testing.T) {
	t.Parallel()

	// A log path with shell metacharacters (a space, and an injection attempt) must
	// be neutralised by quoting: it stays a single literal redirect target and
	// cannot break the redirect or run an extra command at first boot.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		LogPath:  "/var/log/x; touch /tmp/pwned",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Contains(t,
		cfg.WriteFiles[0].Content,
		"exec >> '/var/log/x; touch /tmp/pwned' 2>&1\n",
	)
	assert.NotContains(t, cfg.WriteFiles[0].Content, "touch /tmp/pwned\n")
}

func TestBuildUserDataDropsBlankCommands(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"", "echo kept", "   ", "\t"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	// Only the one non-blank command survives; no empty script lines are emitted.
	assert.Equal(t,
		"#!/bin/sh\n"+
			"set -eu\n"+
			"exec >> '"+cloudinitbootstrap.DefaultLogPath+"' 2>&1\n"+
			"echo kept\n",
		cfg.WriteFiles[0].Content,
	)
}

func TestBuildUserDataPreservesShellMetacharacters(t *testing.T) {
	t.Parallel()

	// A realistic k3s install command (pipes, quotes, env assignment) must survive
	// YAML round-tripping byte-for-byte.
	command := "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
		"INSTALL_K3S_VERSION='v1.30.2+k3s1' K3S_TOKEN='secret' sh -s - server --cluster-init"

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{command},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Contains(t, cfg.WriteFiles[0].Content, "\n"+command+"\n")
}

func TestBuildUserDataDeterministic(t *testing.T) {
	t.Parallel()

	cfg := cloudinitbootstrap.Config{Commands: []string{"echo a", "echo b", "echo c"}}

	first, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	second, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestBuildUserDataCommandOnlyOmitsDeclarativeKeys(t *testing.T) {
	t.Parallel()

	// A command-only Config must render exactly as before the declarative fields
	// existed: no packages: and no apt: keys leak in.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
	})
	require.NoError(t, err)

	assert.NotContains(t, userData, "packages:")
	assert.NotContains(t, userData, "apt:")
	assert.NotContains(t, userData, "ssh_authorized_keys:")

	cfg := parse(t, userData)
	assert.Empty(t, cfg.Packages)
	assert.Empty(t, cfg.Apt.Sources)
	assert.Empty(t, cfg.SSHAuthorizedKeys)
	require.Len(t, cfg.WriteFiles, 1) // only the boot script
}

func TestBuildUserDataRendersSSHAuthorizedKeys(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		SSHAuthorizedKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap",
			"",
			"   ",
			"ssh-rsa AAAAB3NzaC1yc2E operator",
		},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	// Blank entries dropped, order preserved.
	assert.Equal(t, []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap",
		"ssh-rsa AAAAB3NzaC1yc2E operator",
	}, cfg.SSHAuthorizedKeys)
}

func TestBuildUserDataSSHKeysOnlyRejected(t *testing.T) {
	t.Parallel()

	// A key with nothing to run would create a server that never bootstraps, so a
	// keys-only Config is rejected like an empty one.
	_, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		SSHAuthorizedKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA ksail-bootstrap"},
	})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrNoCommands)
}

func TestBuildUserDataSSHKeyErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"newline": "ssh-ed25519 AAAA\nssh-rsa BBBB",
		"NUL":     "ssh-ed25519 AAAA\x00 key",
	}

	for name, key := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
				Commands:          []string{"echo hi"},
				SSHAuthorizedKeys: []string{key},
			})
			require.ErrorIs(t, err, cloudinitbootstrap.ErrInvalidSSHAuthorizedKey)
		})
	}
}

func TestBuildUserDataRendersHostKeys(t *testing.T) {
	t.Parallel()

	private := "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		HostKeys: &cloudinitbootstrap.HostKeys{
			ED25519Private: private,
			ED25519Public:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA host",
		},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)
	assert.Equal(t, private, cfg.SSHKeys.ED25519Private)
	assert.Equal(t, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA host", cfg.SSHKeys.ED25519Public)
}

func TestBuildUserDataOmitsHostKeysWhenUnset(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
	})
	require.NoError(t, err)

	assert.NotContains(t, userData, "ssh_keys:")
}

func TestBuildUserDataHostKeysOnlyRejected(t *testing.T) {
	t.Parallel()

	// A host identity with nothing to run would create a server that never
	// bootstraps, so an identity-only Config is rejected like an empty one.
	_, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		HostKeys: &cloudinitbootstrap.HostKeys{
			ED25519Private: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n",
			ED25519Public:  "ssh-ed25519 AAAA host",
		},
	})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrNoCommands)
}

func TestBuildUserDataHostKeyErrors(t *testing.T) {
	t.Parallel()

	private := "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"

	tests := map[string]cloudinitbootstrap.HostKeys{
		"missing private": {ED25519Public: "ssh-ed25519 AAAA host"},
		"missing public":  {ED25519Private: private},
		"multi-line public": {
			ED25519Private: private,
			ED25519Public:  "ssh-ed25519 AAAA\nhost",
		},
		"NUL in private": {
			ED25519Private: "-----BEGIN\x00-----",
			ED25519Public:  "ssh-ed25519 AAAA host",
		},
		"NUL in public": {
			ED25519Private: private,
			ED25519Public:  "ssh-ed25519 AAAA\x00host",
		},
	}

	for name, hostKeys := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
				Commands: []string{"echo hi"},
				HostKeys: &hostKeys,
			})
			require.ErrorIs(t, err, cloudinitbootstrap.ErrInvalidHostKeys)
		})
	}
}

func TestBuildUserDataRendersPackages(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Packages: []string{"containerd", "", "kubeadm", "   ", "kubelet"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	// Blank entries dropped, order preserved.
	assert.Equal(t, []string{"containerd", "kubeadm", "kubelet"}, cfg.Packages)
	// Packages-only Config needs no boot script or runcmd.
	assert.Empty(t, cfg.WriteFiles)
	assert.Empty(t, cfg.RunCmd)
}

func TestBuildUserDataRendersAptSources(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		AptSources: []cloudinitbootstrap.AptSource{
			{
				Name:   "kubernetes",
				Source: "deb [signed-by=/etc/apt/keyrings/k8s.gpg] https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /",
				Key:    "-----BEGIN PGP PUBLIC KEY BLOCK-----\nabc\n-----END PGP PUBLIC KEY BLOCK-----",
			},
		},
		Packages: []string{"kubeadm"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	source, ok := cfg.Apt.Sources["kubernetes"]
	require.True(t, ok, "apt source must be keyed by its Name")
	assert.Contains(t, source.Source, "pkgs.k8s.io/core")
	assert.Contains(t, source.Key, "BEGIN PGP PUBLIC KEY BLOCK")
}

func TestBuildUserDataAptSourceKeyOptional(t *testing.T) {
	t.Parallel()

	// A source without a key omits the key: field entirely (rather than key: "").
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		AptSources: []cloudinitbootstrap.AptSource{
			{Name: "extra", Source: "deb https://example.test/repo /"},
		},
	})
	require.NoError(t, err)

	assert.NotContains(t, userData, "key:")

	cfg := parse(t, userData)
	assert.Empty(t, cfg.Apt.Sources["extra"].Key)
}

func TestBuildUserDataAptSourcesDeterministic(t *testing.T) {
	t.Parallel()

	// Two sources declared out of alphabetical order must still render identically
	// every time (yaml.v3 sorts map keys), so provisioning is reproducible.
	cfg := cloudinitbootstrap.Config{
		AptSources: []cloudinitbootstrap.AptSource{
			{Name: "zeta", Source: "deb https://z /"},
			{Name: "alpha", Source: "deb https://a /"},
		},
	}

	first, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	second, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Less(t,
		strings.Index(first, "alpha"), strings.Index(first, "zeta"),
		"sources must be emitted in sorted key order",
	)
}

func TestBuildUserDataRendersFiles(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Files: []cloudinitbootstrap.File{
			{
				Path:        "/etc/kubernetes/ksail/kubeadm.yaml",
				Permissions: "0600",
				Content:     "apiVersion: x",
			},
			{Path: "/etc/motd", Content: "hi"}, // no permissions -> default
		},
		Commands: []string{"kubeadm init"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	// The two declared files come first, then the boot script.
	require.Len(t, cfg.WriteFiles, 3)
	assert.Equal(t, "/etc/kubernetes/ksail/kubeadm.yaml", cfg.WriteFiles[0].Path)
	assert.Equal(t, "0600", cfg.WriteFiles[0].Permissions)
	assert.Equal(t, "apiVersion: x", cfg.WriteFiles[0].Content)
	assert.Equal(t, cloudinitbootstrap.DefaultFilePermissions, cfg.WriteFiles[1].Permissions)
	assert.Equal(t, cloudinitbootstrap.DefaultScriptPath, cfg.WriteFiles[2].Path)
}

func TestBuildUserDataFullDeclarativeInstall(t *testing.T) {
	t.Parallel()

	// The whole point of the slice: a declarative install with no curl|sh — an apt
	// source with its key, packages, a config file, then the install commands.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Files: []cloudinitbootstrap.File{
			{
				Path:        "/etc/kubernetes/ksail/kubeadm.yaml",
				Permissions: "0600",
				Content:     "kind: InitConfiguration",
			},
		},
		AptSources: []cloudinitbootstrap.AptSource{
			{
				Name:   "kubernetes",
				Source: "deb https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /",
				Key:    "KEY",
			},
		},
		Packages: []string{"containerd", "kubeadm", "kubelet"},
		Commands: []string{"kubeadm init --config /etc/kubernetes/ksail/kubeadm.yaml"},
	})
	require.NoError(t, err)

	assert.NotContains(t, userData, "curl")

	cfg := parse(t, userData)
	assert.Len(t, cfg.WriteFiles, 2) // config file + boot script
	assert.NotEmpty(t, cfg.Apt.Sources["kubernetes"].Source)
	assert.Equal(t, []string{"containerd", "kubeadm", "kubelet"}, cfg.Packages)
	require.Len(t, cfg.RunCmd, 1)
	assert.Contains(t,
		cfg.WriteFiles[1].Content,
		"kubeadm init --config /etc/kubernetes/ksail/kubeadm.yaml\n",
	)
}

func TestBuildUserDataErrors(t *testing.T) {
	t.Parallel()

	tests := []errorCase{
		{
			name:    "no commands",
			cfg:     cloudinitbootstrap.Config{},
			wantErr: cloudinitbootstrap.ErrNoCommands,
		},
		{
			name:    "only blank commands",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"", "  ", "\t\n"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand, // the "\t\n" newline is rejected first
		},
		{
			name:    "all-whitespace without newline",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"", "   "}},
			wantErr: cloudinitbootstrap.ErrNoCommands,
		},
		{
			name:    "command with embedded newline",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"echo a\necho b"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand,
		},
		{
			name:    "command with NUL byte",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"echo \x00"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand,
		},
		{
			name: "relative script path",
			cfg: cloudinitbootstrap.Config{
				Commands:   []string{"echo hi"},
				ScriptPath: "relative/boot.sh",
			},
			wantErr: cloudinitbootstrap.ErrPathNotAbsolute,
		},
		{
			name: "relative log path",
			cfg: cloudinitbootstrap.Config{
				Commands: []string{"echo hi"},
				LogPath:  "relative.log",
			},
			wantErr: cloudinitbootstrap.ErrPathNotAbsolute,
		},
	}

	runErrorCases(t, tests)
}

// errorCase is one BuildUserData rejection case, shared by the command-level and
// declarative-field error tests.
type errorCase struct {
	name    string
	cfg     cloudinitbootstrap.Config
	wantErr error
}

// runErrorCases asserts each case returns its expected sentinel and no document.
func runErrorCases(t *testing.T, cases []errorCase) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out, err := cloudinitbootstrap.BuildUserData(testCase.cfg)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, out, "no document should be returned on error")
		})
	}
}

func TestBuildUserDataPackageErrors(t *testing.T) {
	t.Parallel()

	tests := []errorCase{
		{
			name:    "package with embedded newline",
			cfg:     cloudinitbootstrap.Config{Packages: []string{"containerd\nkubeadm"}},
			wantErr: cloudinitbootstrap.ErrInvalidPackage,
		},
		{
			name:    "package with NUL byte",
			cfg:     cloudinitbootstrap.Config{Packages: []string{"kube\x00let"}},
			wantErr: cloudinitbootstrap.ErrInvalidPackage,
		},
	}

	runErrorCases(t, tests)
}

func TestBuildUserDataAptSourceErrors(t *testing.T) {
	t.Parallel()

	tests := []errorCase{
		{
			name: "apt source with blank name",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{{Name: "  ", Source: "deb https://x /"}},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
		{
			name: "apt source with blank source",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{{Name: "k8s", Source: ""}},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
		{
			name: "apt source with multi-line source",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{
					{Name: "k8s", Source: "deb a /\ndeb b /"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
		{
			name: "apt source with NUL in key",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{
					{Name: "k8s", Source: "deb https://x /", Key: "-----BEGIN-----\x00"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
		{
			name: "apt sources with duplicate name",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{
					{Name: "k8s", Source: "deb https://a /"},
					{Name: "k8s", Source: "deb https://b /"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
		{
			name: "apt source name with path separator",
			cfg: cloudinitbootstrap.Config{
				AptSources: []cloudinitbootstrap.AptSource{
					{Name: "../evil", Source: "deb https://x /"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidAptSource,
		},
	}

	runErrorCases(t, tests)
}

func TestBuildUserDataAcceptsThreeDigitMode(t *testing.T) {
	t.Parallel()

	// A three-digit octal mode is valid cloud-init and must pass through verbatim.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Files: []cloudinitbootstrap.File{{Path: "/etc/x", Permissions: "600", Content: "x"}},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Equal(t, "600", cfg.WriteFiles[0].Permissions)
}

func TestBuildUserDataFileErrors(t *testing.T) {
	t.Parallel()

	tests := []errorCase{
		{
			name: "file with relative path",
			cfg: cloudinitbootstrap.Config{
				Files: []cloudinitbootstrap.File{{Path: "etc/kubernetes/config", Content: "x"}},
			},
			wantErr: cloudinitbootstrap.ErrInvalidFile,
		},
		{
			name: "file with multi-line path",
			cfg: cloudinitbootstrap.Config{
				Files: []cloudinitbootstrap.File{{Path: "/etc/a\n/etc/b", Content: "x"}},
			},
			wantErr: cloudinitbootstrap.ErrInvalidFile,
		},
		{
			name: "file with NUL in content",
			cfg: cloudinitbootstrap.Config{
				Files: []cloudinitbootstrap.File{{Path: "/etc/x", Content: "a\x00b"}},
			},
			wantErr: cloudinitbootstrap.ErrInvalidFile,
		},
		{
			name: "file with non-octal permissions",
			cfg: cloudinitbootstrap.Config{
				Files: []cloudinitbootstrap.File{
					{Path: "/etc/x", Permissions: "999", Content: "x"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidFile,
		},
		{
			name: "file with wrong-length mode",
			cfg: cloudinitbootstrap.Config{
				Files: []cloudinitbootstrap.File{
					{Path: "/etc/x", Permissions: "60", Content: "x"},
				},
			},
			wantErr: cloudinitbootstrap.ErrInvalidFile,
		},
	}

	runErrorCases(t, tests)
}
