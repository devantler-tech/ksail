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

func TestBuildUserDataErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     cloudinitbootstrap.Config
		wantErr error
	}{
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out, err := cloudinitbootstrap.BuildUserData(testCase.cfg)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, out, "no document should be returned on error")
		})
	}
}
